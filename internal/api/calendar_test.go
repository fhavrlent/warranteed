package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
)

func TestCalendarFeed(t *testing.T) {
	f := newHomeBoxFixture(t)
	ctx := context.Background()

	link := func(h http.HandlerFunc, target ...string) string {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, append(target, "/api/calendar")[0], nil)
		h(w, r.WithContext(auth.WithUser(r.Context(), f.bob)))
		var out calendarLink
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if out.URL == nil {
			return ""
		}
		return *out.URL
	}
	feed := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.SetPathValue("file", url[strings.LastIndex(url, "/")+1:])
		f.srv.calendarFeed(w, r)
		return w
	}

	url := link(f.srv.newCalendar)
	if !strings.HasPrefix(url, "https://warranty.example.test/calendar/") || !strings.HasSuffix(url, ".ics") {
		t.Fatalf("link %q", url)
	}
	if got := link(f.srv.getCalendar); got != url {
		t.Fatalf("stored link %q, want %q", got, url)
	}

	// Bob sees the household's warranties, Alice's included.
	w := feed(url)
	body := w.Body.String()
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("feed: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, want := range []string{"SUMMARY:Warranty ends: Dishwasher", "DTSTART;VALUE=DATE:20270301",
		"URL:https://warranty.example.test/w/"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}

	// Void warranties are left out.
	if err := f.srv.store.SetVoid(ctx, f.alice.ID, f.warrantyID, true, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(feed(url).Body.String(), "BEGIN:VEVENT") {
		t.Error("void warranty is in the feed")
	}

	// Making a link again keeps the one there is; only replacing changes it.
	if again := link(f.srv.newCalendar); again != url {
		t.Errorf("making a link again replaced it: %q, want %q", again, url)
	}
	// A new link replaces the old one, and turning it off ends it.
	renewed := link(f.srv.newCalendar, "/api/calendar?replace=1")
	if renewed == url || feed(url).Code != http.StatusNotFound || feed(renewed).Code != 200 {
		t.Error("the old link still works after getting a new one")
	}
	if link(f.srv.deleteCalendar) != "" || feed(renewed).Code != http.StatusNotFound {
		t.Error("the link still works after turning it off")
	}
	if feed("/calendar/.ics").Code != http.StatusNotFound || feed("/calendar/x").Code != http.StatusNotFound {
		t.Error("an empty or malformed link was served")
	}
}
