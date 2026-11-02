package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/dates"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/ical"
	"github.com/fhavrlent/warranteed/internal/store"
)

// A calendar feed is a secret link, since calendar apps can't sign in. It
// lists when the warranties the user can see end.

// calendarLink is the user's feed address, or nil when they have no feed.
type calendarLink struct {
	URL *string `json:"url"`
}

func (s *Server) calendarURL(token string) *string {
	if token == "" {
		return nil
	}
	u := strings.TrimSuffix(s.cfg.AppURL.String(), "/") + "/calendar/" + token + ".ics"
	return &u
}

func (s *Server) getCalendar(w http.ResponseWriter, r *http.Request) {
	token, err := s.store.CalendarToken(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, calendarLink{s.calendarURL(token)})
}

// newCalendar makes a feed link. With ?replace=1 it replaces the one there
// is, which stops working; otherwise an existing link is kept, so a page that
// didn't know about it can't replace it by accident.
func (s *Server) newCalendar(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("replace") != "1" {
		token, err := s.store.CalendarToken(r.Context(), userID(r))
		if err != nil {
			fail(w, err)
			return
		}
		if token != "" {
			writeJSON(w, 200, calendarLink{s.calendarURL(token)})
			return
		}
	}
	b := make([]byte, 24)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	if err := s.store.SetCalendarToken(r.Context(), userID(r), token); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, calendarLink{s.calendarURL(token)})
}

func (s *Server) deleteCalendar(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetCalendarToken(r.Context(), userID(r), ""); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, calendarLink{})
}

// calendarFeed serves the feed whose secret is in the path.
func (s *Server) calendarFeed(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutSuffix(r.PathValue("file"), ".ics")
	if !ok {
		http.NotFound(w, r)
		return
	}
	uid, err := s.store.CalendarUser(r.Context(), token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	st, err := s.store.Settings(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	list, err := s.store.ActiveWithExpiry(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	lang := st.EffectiveLanguage()
	app := strings.TrimSuffix(s.cfg.AppURL.String(), "/")
	events := make([]ical.Event, 0, len(list))
	for _, wr := range list {
		day, err := time.Parse(store.DateLayout, *wr.ExpiresOn)
		if err != nil {
			continue
		}
		updated, _ := time.Parse(time.RFC3339, wr.UpdatedAt)
		events = append(events, ical.Event{
			UID:         fmt.Sprintf("warranty-%d@%s", wr.ID, s.cfg.AppURL.Host),
			Date:        day,
			Summary:     i18n.T(lang, "calendar.event", i18n.Args{"name": productLabel(wr)}),
			Description: calendarDescription(wr, st, lang, app),
			URL:         fmt.Sprintf("%s/w/%d", app, wr.ID),
			Updated:     updated,
		})
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Robots-Tag", "noindex")
	if err := ical.Write(w, i18n.T(lang, "calendar.name", nil), events); err != nil {
		slog.Warn("write calendar feed", "err", err)
	}
}

// productLabel is the product with its brand, as the app shows it.
func productLabel(wr *store.Warranty) string {
	if wr.Brand != "" && !strings.Contains(strings.ToLower(wr.ProductName), strings.ToLower(wr.Brand)) {
		return wr.Brand + " " + wr.ProductName
	}
	return wr.ProductName
}

// calendarDescription says when and where it was bought, and links to it.
func calendarDescription(wr *store.Warranty, st *store.Settings, lang, app string) string {
	args := i18n.Args{"date": dates.Format(wr.PurchaseDate, st.DateFormat, lang), "vendor": wr.Vendor}
	key := "calendar.bought"
	if wr.Vendor != "" {
		key = "calendar.boughtFrom"
	}
	return i18n.T(lang, key, args) + "\n" + fmt.Sprintf("%s/w/%d", app, wr.ID)
}
