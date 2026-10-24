package auth

import (
	"net/url"
	"testing"

	"github.com/fhavrlent/warranteed/internal/config"
)

func TestSafeReturn(t *testing.T) {
	// From the query string "?return=/%09/evil.example".
	q, _ := url.ParseQuery("return=/%09/evil.example")
	tab := q.Get("return")

	cases := map[string]string{
		"/":                         "/",
		"/w/12":                     "/w/12",
		"/?filter=expired&tag=Home": "/?filter=expired&tag=Home",
		"/settings#data":            "/settings#data",
		tab:                         "/",
		"/\n/evil.example":          "/",
		"/\r/evil.example":          "/",
		"/\x00/evil.example":        "/",
		"/\x7f":                     "/",
		"//evil.example":            "/",
		"/\\evil.example":           "/",
		"/\\/evil.example":          "/",
		"\\\\evil.example":          "/",
		"https://evil.example":      "/",
		"evil.example":              "/",
		"javascript:alert(1)":       "/",
		"":                          "/",
	}
	for in, want := range cases {
		if got := safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	key, err := LoadKey("", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	enc, err := encrypt(key, "refresh-token-value")
	if err != nil || enc == "" || enc == "refresh-token-value" {
		t.Fatalf("encrypt: %q %v", enc, err)
	}
	if got, err := decrypt(key, enc); err != nil || got != "refresh-token-value" {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	other, _ := LoadKey("another secret", "")
	if _, err := decrypt(other, enc); err == nil {
		t.Fatal("decrypting with the wrong key must fail")
	}
	if enc, _ := encrypt(key, ""); enc != "" {
		t.Fatal("empty input should stay empty")
	}
}

func TestLoadKeyIsStable(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadKey("", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadKey("", dir)
	if err != nil || string(a) != string(b) {
		t.Fatal("key must be reused across restarts")
	}
}

func TestAccess(t *testing.T) {
	a := &Auth{cfg: &config.Config{OIDC: config.OIDC{
		GroupsClaim:   "groups",
		AllowedGroups: []string{"household"},
		AdminGroups:   []string{"admins"},
	}}}
	claims := func(groups ...any) map[string]any { return map[string]any{"groups": groups} }

	if ok, _ := a.access(claims("other")); ok {
		t.Error("user outside allowed groups must be refused")
	}
	if ok, admin := a.access(claims("household")); !ok || admin == nil || *admin {
		t.Error("household member: allowed, not admin")
	}
	if ok, admin := a.access(claims("admins")); !ok || admin == nil || !*admin {
		t.Error("admin group implies access and admin")
	}
	if ok, _ := a.access(map[string]any{}); ok {
		t.Error("no groups claim must be refused when groups are required")
	}
}
