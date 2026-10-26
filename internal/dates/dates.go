// Package dates holds the supported user-facing date formats. Formats use
// date-fns tokens so the frontend can use them directly; Format renders them
// server-side (e.g. in notifications) so dates look the same everywhere.
package dates

import (
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/i18n"
)

// Formats is the list of formats offered in settings.
var Formats = []string{
	"yyyy-MM-dd",
	"dd.MM.yyyy",
	"d. M. yyyy",
	"dd/MM/yyyy",
	"MM/dd/yyyy",
	"d MMM yyyy",
	"MMM d, yyyy",
	"d MMMM yyyy",
	"MMMM d, yyyy",
}

func Valid(f string) bool {
	for _, x := range Formats {
		if x == f {
			return true
		}
	}
	return false
}

var goTokens = strings.NewReplacer(
	"yyyy", "2006",
	"MMMM", "January",
	"MMM", "Jan",
	"MM", "01",
	"M", "1",
	"dd", "02",
	"d", "2",
)

// Format renders an ISO date (YYYY-MM-DD) using a date-fns style format,
// with month names in lang.
func Format(iso, format, lang string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	if !Valid(format) {
		format = Formats[0]
	}
	out := t.Format(goTokens.Replace(format))
	// Month names come from the translations as comma-separated lists.
	key, english := "", ""
	switch {
	case strings.Contains(format, "MMMM"):
		key, english = "reminder.monthsLong", t.Format("January")
	case strings.Contains(format, "MMM"):
		key, english = "reminder.monthsShort", t.Format("Jan")
	}
	if key != "" {
		if names := strings.Split(i18n.T(lang, key, nil), ","); len(names) == 12 {
			out = strings.Replace(out, english, strings.TrimSpace(names[t.Month()-1]), 1)
		}
	}
	return out
}
