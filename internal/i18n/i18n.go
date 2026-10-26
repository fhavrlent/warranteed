// Package i18n translates server-side text (reminders, error messages) using
// the web app's translation files, so each language lives in one file.
//
// Keys, plural suffixes (_one, _few, _other, ...) and {{placeholders}} follow
// i18next, which the web app uses.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"

	"github.com/fhavrlent/warranteed/web"
)

// Fallback is the source language every other translation is based on.
const Fallback = "en"

// Args are values for a message's {{placeholders}}. "count" also selects the
// plural form.
type Args map[string]any

// catalogs maps a language to its flattened "section.key" → text entries.
var catalogs = load(web.Locales())

func load(files fs.FS) map[string]map[string]string {
	out := map[string]map[string]string{}
	names, err := fs.Glob(files, "*.json")
	if err != nil {
		panic(err)
	}
	for _, name := range names {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			panic(err)
		}
		var tree map[string]any
		if err := json.Unmarshal(data, &tree); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", name, err))
		}
		flat := map[string]string{}
		flatten("", tree, flat)
		out[strings.TrimSuffix(path.Base(name), ".json")] = flat
	}
	if out[Fallback] == nil {
		panic("i18n: missing " + Fallback + ".json")
	}
	return out
}

func flatten(prefix string, tree map[string]any, out map[string]string) {
	for k, v := range tree {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case string:
			out[key] = t
		case map[string]any:
			flatten(key, t, out)
		}
	}
}

// Languages lists the available languages, sorted.
func Languages() []string {
	langs := make([]string, 0, len(catalogs))
	for l := range catalogs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// Supported reports whether there is a translation for lang.
func Supported(lang string) bool {
	_, ok := catalogs[lang]
	return ok
}

// Match picks the best available language from an Accept-Language header,
// or "" when none fits.
func Match(acceptLanguage string) string {
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return ""
	}
	for _, tag := range tags {
		base, _ := tag.Base()
		if Supported(base.String()) {
			return base.String()
		}
	}
	return ""
}

// T translates key into lang, falling back to English and then to the key.
func T(lang, key string, args Args) string {
	text, ok := lookup(lang, key, args)
	if !ok {
		lang = Fallback
		if text, ok = lookup(lang, key, args); !ok {
			return key
		}
	}
	return interpolate(text, args)
}

func lookup(lang, key string, args Args) (string, bool) {
	cat := catalogs[lang]
	if cat == nil {
		return "", false
	}
	if n, ok := count(args); ok {
		if text, ok := cat[key+"_"+pluralForm(lang, n)]; ok {
			return text, true
		}
		if text, ok := cat[key+"_other"]; ok {
			return text, true
		}
	}
	text, ok := cat[key]
	return text, ok
}

func count(args Args) (float64, bool) {
	switch n := args["count"].(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

var forms = map[plural.Form]string{
	plural.Zero: "zero", plural.One: "one", plural.Two: "two",
	plural.Few: "few", plural.Many: "many", plural.Other: "other",
}

// pluralForm returns the CLDR plural category of n in lang, as i18next names it.
func pluralForm(lang string, n float64) string {
	n = math.Abs(n)
	i := int(n)
	// Up to one decimal, which is all the app shows ("2.5 years").
	v, f := 0, 0
	if frac := math.Round((n-float64(i))*10) / 10; frac > 0 {
		v, f = 1, int(math.Round(frac*10))
	}
	return forms[plural.Cardinal.MatchPlural(language.Make(lang), i, v, v, f, f)]
}

var placeholder = regexp.MustCompile(`\{\{\s*([\w.]+)\s*(?:,\s*\w+\s*)?\}\}`)

func interpolate(text string, args Args) string {
	return placeholder.ReplaceAllStringFunc(text, func(m string) string {
		name := placeholder.FindStringSubmatch(m)[1]
		v, ok := args[name]
		if !ok {
			return m
		}
		return fmt.Sprint(v)
	})
}

// Error is a user-facing error that can be shown in any language.
type Error struct {
	Key  string
	Args Args
}

func NewError(key string, args Args) *Error { return &Error{Key: key, Args: args} }

// Error returns the English text, e.g. for logs and API clients.
func (e *Error) Error() string { return T(Fallback, e.Key, e.Args) }
