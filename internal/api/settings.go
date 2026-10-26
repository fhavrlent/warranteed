package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/dates"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/notify"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

type settingsResponse struct {
	*store.Settings
	HasPaperlessToken bool   `json:"hasPaperlessToken"`
	PaperlessEnabled  bool   `json:"paperlessEnabled"`
	PaperlessWebURL   string `json:"paperlessWebUrl"`
	// PaperlessServerURL is the server's Paperless instance (PAPERLESS_URL).
	PaperlessServerURL string `json:"paperlessServerUrl"`
	// PaperlessServerToken: an admin may use the server's token (PAPERLESS_TOKEN).
	PaperlessServerToken bool `json:"paperlessServerToken"`
	// CanSetPaperlessURL: only admins may point Paperless somewhere else.
	CanSetPaperlessURL bool `json:"canSetPaperlessUrl"`
	// NotifyAllowedHosts limits self-hosted notification services for non-admins.
	NotifyAllowedHosts []string `json:"notifyAllowedHosts"`
	IsAdmin            bool     `json:"isAdmin"`
	DateFormats        []string `json:"dateFormats"`
	Languages          []string `json:"languages"`
	Timezone           string   `json:"timezone"`
}

func (s *Server) settingsResponse(st *store.Settings, admin bool) settingsResponse {
	pc := s.paperlessFor(st, admin)
	webURL := ""
	if pc != nil {
		webURL = pc.BaseURL
		switch {
		case admin && st.PaperlessPublicURL != "":
			webURL = st.PaperlessPublicURL
		case pc.BaseURL == s.cfg.Paperless.URL && s.cfg.Paperless.PublicURL != "":
			webURL = s.cfg.Paperless.PublicURL
		}
	}
	return settingsResponse{
		Settings:             st,
		HasPaperlessToken:    st.PaperlessToken != "",
		PaperlessEnabled:     pc != nil,
		PaperlessWebURL:      webURL,
		PaperlessServerURL:   s.cfg.Paperless.PublicURL,
		PaperlessServerToken: admin && s.cfg.Paperless.URL != "" && s.cfg.Paperless.Token != "",
		CanSetPaperlessURL:   admin,
		NotifyAllowedHosts:   s.cfg.NotifyAllowedHosts,
		IsAdmin:              admin,
		DateFormats:          dates.Formats,
		Languages:            i18n.Languages(),
		Timezone:             s.cfg.Location.String(),
	}
}

// paperlessFor returns the Paperless client a user is allowed to use.
//
// The server-wide token (PAPERLESS_TOKEN) is only ever used for admins:
// handing it to everyone would let any user read every document it can see.
// Everyone else must bring their own token, so Paperless's own permissions
// apply. Only admins may set their own Paperless URL; for everyone else the
// server's PAPERLESS_URL is used, so users can't make the server call
// arbitrary (e.g. internal) addresses.
func (s *Server) paperlessFor(st *store.Settings, admin bool) *paperless.Client {
	env := s.cfg.Paperless
	if admin {
		if st.PaperlessURL != "" && st.PaperlessToken != "" {
			return paperless.New(st.PaperlessURL, st.PaperlessToken)
		}
		if env.URL != "" && st.PaperlessToken != "" {
			return paperless.New(env.URL, st.PaperlessToken)
		}
		if env.URL != "" && env.Token != "" {
			return paperless.New(env.URL, env.Token)
		}
		return nil
	}
	if env.URL != "" && st.PaperlessToken != "" {
		return paperless.New(env.URL, st.PaperlessToken)
	}
	return nil
}

func (s *Server) userPaperless(ctx context.Context, uid int64) (*paperless.Client, error) {
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return nil, err
	}
	admin, err := s.store.IsAdmin(ctx, uid)
	if err != nil {
		return nil, err
	}
	return s.paperlessFor(st, admin), nil
}

func isAdmin(r *http.Request) bool { return auth.UserFrom(r.Context()).IsAdmin }

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	// Remember the browser's language: reminders for users who follow it are
	// sent in it.
	if lang := i18n.Match(r.Header.Get("Accept-Language")); lang != "" && lang != st.BrowserLanguage {
		if err := s.store.SetBrowserLanguage(r.Context(), userID(r), lang); err != nil {
			fail(w, err)
			return
		}
		st.BrowserLanguage = lang
	}
	writeJSON(w, 200, s.settingsResponse(st, isAdmin(r)))
}

type settingsInput struct {
	DateFormat         string  `json:"dateFormat"`
	Currency           string  `json:"currency"`
	Theme              string  `json:"theme"`
	Language           string  `json:"language"` // "" follows the browser
	ExpiringSoonDays   int     `json:"expiringSoonDays"`
	NotifyURLs         string  `json:"notifyUrls"`
	NotifyDays         string  `json:"notifyDays"`
	PaperlessURL       string  `json:"paperlessUrl"`
	PaperlessPublicURL string  `json:"paperlessPublicUrl"`
	PaperlessToken     *string `json:"paperlessToken"` // nil keeps the stored token
	PaperlessDefault   bool    `json:"paperlessDefault"`
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (in *settingsInput) apply(st *store.Settings, admin bool, policy notify.Policy) error {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.PaperlessURL = strings.TrimSuffix(strings.TrimSpace(in.PaperlessURL), "/")
	in.PaperlessPublicURL = strings.TrimSuffix(strings.TrimSpace(in.PaperlessPublicURL), "/")
	msg := func(key string) error { return i18n.NewError("server."+key, nil) }
	switch {
	case !dates.Valid(in.DateFormat):
		return msg("dateFormat")
	case !currencyRe.MatchString(in.Currency):
		return msg("currency")
	case in.Theme != "system" && in.Theme != "light" && in.Theme != "dark":
		return msg("theme")
	case in.Language != "" && !i18n.Supported(in.Language):
		return msg("language")
	case in.ExpiringSoonDays < 1 || in.ExpiringSoonDays > 365:
		return msg("soonDays")
	case in.PaperlessURL != "" && !strings.HasPrefix(in.PaperlessURL, "http://") && !strings.HasPrefix(in.PaperlessURL, "https://"):
		return msg("paperlessUrl")
	case in.PaperlessPublicURL != "" && !strings.HasPrefix(in.PaperlessPublicURL, "http://") && !strings.HasPrefix(in.PaperlessPublicURL, "https://"):
		return msg("paperlessLinkAddress")
	}
	if _, err := notify.ParseDays(in.NotifyDays); err != nil {
		return err
	}
	if !admin && in.PaperlessURL != "" && in.PaperlessURL != st.PaperlessURL {
		return msg("paperlessUrlAdmin")
	}
	urls := notify.ParseURLs(in.NotifyURLs)
	if err := notify.Validate(urls); err != nil {
		return i18n.NewError("server.notifyUrl", i18n.Args{"detail": err.Error()})
	}
	if err := policy.CheckAll(urls, admin); err != nil {
		return err
	}
	st.DateFormat, st.Currency, st.Theme, st.ExpiringSoonDays = in.DateFormat, in.Currency, in.Theme, in.ExpiringSoonDays
	st.Language = in.Language
	st.NotifyURLs, st.NotifyDays = strings.TrimSpace(in.NotifyURLs), strings.TrimSpace(in.NotifyDays)
	if admin {
		st.PaperlessURL = in.PaperlessURL
		st.PaperlessPublicURL = in.PaperlessPublicURL
	}
	st.PaperlessDefault = in.PaperlessDefault
	if in.PaperlessToken != nil {
		st.PaperlessToken = strings.TrimSpace(*in.PaperlessToken)
	}
	return nil
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsInput
	if !decode(w, r, &in) {
		return
	}
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	if err := in.apply(st, isAdmin(r), s.notifyPolicy()); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SaveSettings(r.Context(), userID(r), st); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, s.settingsResponse(st, isAdmin(r)))
}

func (s *Server) notifyPolicy() notify.Policy {
	return notify.Policy{AllowedHosts: s.cfg.NotifyAllowedHosts}
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NotifyURLs string `json:"notifyUrls"`
	}
	if !decode(w, r, &in) {
		return
	}
	urls := notify.ParseURLs(in.NotifyURLs)
	if len(urls) == 0 {
		writeMsg(w, http.StatusBadRequest, "server.notifyNeedsUrl", nil)
		return
	}
	if err := s.notifyPolicy().CheckAll(urls, isAdmin(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	lang := st.EffectiveLanguage()
	if err := notify.Send(urls, i18n.T(lang, "server.testTitle", nil), i18n.T(lang, "server.testBody", nil)); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// testPaperless checks connection details before they are saved. Empty
// fields fall back to the stored token or the server configuration. Only
// admins may test a URL of their choosing; everyone else always tests the
// server's PAPERLESS_URL.
func (s *Server) testPaperless(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL   string  `json:"url"`
		Token *string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	admin := isAdmin(r)
	probe := *st
	if admin {
		probe.PaperlessURL = strings.TrimSuffix(strings.TrimSpace(in.URL), "/")
	}
	if in.Token != nil {
		probe.PaperlessToken = strings.TrimSpace(*in.Token)
	}
	if probe.PaperlessURL != "" && probe.PaperlessToken == "" {
		writeMsg(w, http.StatusBadRequest, "server.paperlessNeedsToken", nil)
		return
	}
	pc := s.paperlessFor(&probe, admin)
	if pc == nil {
		switch {
		case !admin && s.cfg.Paperless.URL == "":
			writeMsg(w, http.StatusBadRequest, "server.paperlessNotSetUp", nil)
		case !admin:
			writeMsg(w, http.StatusBadRequest, "server.paperlessNeedsYourToken", nil)
		default:
			writeMsg(w, http.StatusBadRequest, "server.paperlessNeedsUrlToken", nil)
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info, err := pc.Check(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, 200, info)
}
