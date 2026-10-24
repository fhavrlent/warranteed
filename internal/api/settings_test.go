package api

import (
	"testing"

	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/store"
)

func TestPaperlessAccess(t *testing.T) {
	srv := &Server{cfg: &config.Config{Paperless: config.Paperless{URL: "http://paperless:8000", Token: "server-token"}}}
	none := &store.Settings{}
	ownToken := &store.Settings{PaperlessToken: "user-token"}
	ownURL := &store.Settings{PaperlessURL: "http://10.0.0.5", PaperlessToken: "user-token"}

	cases := []struct {
		name      string
		st        *store.Settings
		admin     bool
		wantURL   string
		wantToken string
	}{
		{"non-admin never gets the server token", none, false, "", ""},
		{"non-admin uses own token on the server URL", ownToken, false, "http://paperless:8000", "user-token"},
		{"non-admin can't point at another URL", ownURL, false, "http://paperless:8000", "user-token"},
		{"admin falls back to the server token", none, true, "http://paperless:8000", "server-token"},
		{"admin's own token wins", ownToken, true, "http://paperless:8000", "user-token"},
		{"admin may use another URL", ownURL, true, "http://10.0.0.5", "user-token"},
	}
	for _, tc := range cases {
		pc := srv.paperlessFor(tc.st, tc.admin)
		gotURL, gotToken := "", ""
		if pc != nil {
			gotURL, gotToken = pc.BaseURL, pc.Token
		}
		if gotURL != tc.wantURL || gotToken != tc.wantToken {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, gotURL, gotToken, tc.wantURL, tc.wantToken)
		}
	}
}
