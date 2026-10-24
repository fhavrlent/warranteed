package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/dates"
	"github.com/fhavrlent/warranteed/internal/notify"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

type settingsResponse struct {
	*store.Settings
	HasPaperlessToken bool     `json:"hasPaperlessToken"`
	PaperlessFromEnv  bool     `json:"paperlessFromEnv"`
	PaperlessEnabled  bool     `json:"paperlessEnabled"`
	PaperlessWebURL   string   `json:"paperlessWebUrl"`
	DateFormats       []string `json:"dateFormats"`
	Timezone          string   `json:"timezone"`
}

func (s *Server) settingsResponse(st *store.Settings) settingsResponse {
	pc := s.paperlessFor(st)
	webURL := ""
	if pc != nil {
		webURL = pc.BaseURL
		if st.PaperlessURL == "" && s.cfg.Paperless.PublicURL != "" {
			webURL = s.cfg.Paperless.PublicURL
		}
	}
	return settingsResponse{
		Settings:          st,
		HasPaperlessToken: st.PaperlessToken != "",
		PaperlessFromEnv:  s.cfg.Paperless.URL != "" && s.cfg.Paperless.Token != "",
		PaperlessEnabled:  pc != nil,
		PaperlessWebURL:   webURL,
		DateFormats:       dates.Formats,
		Timezone:          s.cfg.Location.String(),
	}
}

// paperlessFor returns the user's Paperless client: their own settings when
// filled in, otherwise the server-wide environment configuration.
func (s *Server) paperlessFor(st *store.Settings) *paperless.Client {
	if st.PaperlessURL != "" && st.PaperlessToken != "" {
		return paperless.New(st.PaperlessURL, st.PaperlessToken)
	}
	if s.cfg.Paperless.URL != "" && s.cfg.Paperless.Token != "" {
		return paperless.New(s.cfg.Paperless.URL, s.cfg.Paperless.Token)
	}
	return nil
}

func (s *Server) userPaperless(ctx context.Context, uid int64) (*paperless.Client, error) {
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return nil, err
	}
	return s.paperlessFor(st), nil
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, s.settingsResponse(st))
}

type settingsInput struct {
	DateFormat       string  `json:"dateFormat"`
	Currency         string  `json:"currency"`
	Theme            string  `json:"theme"`
	ExpiringSoonDays int     `json:"expiringSoonDays"`
	NotifyURLs       string  `json:"notifyUrls"`
	NotifyDays       string  `json:"notifyDays"`
	PaperlessURL     string  `json:"paperlessUrl"`
	PaperlessToken   *string `json:"paperlessToken"` // nil keeps the stored token
	PaperlessDefault bool    `json:"paperlessDefault"`
}

var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (in *settingsInput) apply(st *store.Settings) string {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.PaperlessURL = strings.TrimSuffix(strings.TrimSpace(in.PaperlessURL), "/")
	switch {
	case !dates.Valid(in.DateFormat):
		return "Unknown date format"
	case !currencyRe.MatchString(in.Currency):
		return "Currency must be a 3-letter ISO code, e.g. EUR"
	case in.Theme != "system" && in.Theme != "light" && in.Theme != "dark":
		return "Unknown theme"
	case in.ExpiringSoonDays < 1 || in.ExpiringSoonDays > 365:
		return "“Expiring soon” must be between 1 and 365 days"
	case in.PaperlessURL != "" && !strings.HasPrefix(in.PaperlessURL, "http://") && !strings.HasPrefix(in.PaperlessURL, "https://"):
		return "Paperless URL must start with http:// or https://"
	}
	if _, err := notify.ParseDays(in.NotifyDays); err != nil {
		return "Reminder days: " + err.Error()
	}
	if err := notify.Validate(notify.ParseURLs(in.NotifyURLs)); err != nil {
		return "Notification URL not recognised: " + err.Error()
	}
	st.DateFormat, st.Currency, st.Theme, st.ExpiringSoonDays = in.DateFormat, in.Currency, in.Theme, in.ExpiringSoonDays
	st.NotifyURLs, st.NotifyDays = strings.TrimSpace(in.NotifyURLs), strings.TrimSpace(in.NotifyDays)
	st.PaperlessURL, st.PaperlessDefault = in.PaperlessURL, in.PaperlessDefault
	if in.PaperlessToken != nil {
		st.PaperlessToken = strings.TrimSpace(*in.PaperlessToken)
	}
	return ""
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
	if msg := in.apply(st); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.SaveSettings(r.Context(), userID(r), st); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, s.settingsResponse(st))
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
		writeError(w, http.StatusBadRequest, "Add at least one notification URL first")
		return
	}
	if err := notify.Send(urls, "Warranteed test", "Notifications are working. You'll be reminded before warranties expire."); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// testPaperless checks the submitted connection details, falling back to the
// stored token or the environment configuration when fields are left empty.
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
	probe := *st
	probe.PaperlessURL = strings.TrimSuffix(strings.TrimSpace(in.URL), "/")
	if in.Token != nil {
		probe.PaperlessToken = strings.TrimSpace(*in.Token)
	}
	if probe.PaperlessURL != "" && probe.PaperlessToken == "" {
		writeError(w, http.StatusBadRequest, "Enter an API token")
		return
	}
	pc := s.paperlessFor(&probe)
	if pc == nil {
		writeError(w, http.StatusBadRequest, "Enter the Paperless URL and API token")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info, err := pc.Check(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, info)
}
