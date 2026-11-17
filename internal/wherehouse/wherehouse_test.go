package wherehouse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Changing details is one PATCH with only what changed, nothing read first.
func TestSetDetailsPatches(t *testing.T) {
	var calls []string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		body = nil
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":3}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "token")
	if err := c.SetQuantity(context.Background(), 3, 4); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "PATCH /api/things/3" || len(body) != 1 || body["quantity"] != float64(4) {
		t.Errorf("calls %v, body %v", calls, body)
	}
	calls = nil
	if err := c.SetDetails(context.Background(), 3, map[string]any{"name": "Lamp", "serial": "S1"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "PATCH /api/things/3" || len(body) != 2 || body["name"] != "Lamp" {
		t.Errorf("calls %v, body %v", calls, body)
	}
}

// Deleting the token: a token Wherehouse no longer takes is gone already.
func TestRevokeToken(t *testing.T) {
	status := http.StatusNoContent
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/token" {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := New(srv.URL, "secret")
	for _, s := range []int{http.StatusNoContent, http.StatusUnauthorized} {
		status = s
		if err := c.RevokeToken(context.Background()); err != nil {
			t.Errorf("%d: %v", s, err)
		}
	}
	if auth != "Bearer secret" {
		t.Errorf("auth = %q", auth)
	}
	status = http.StatusBadGateway
	if err := c.RevokeToken(context.Background()); err == nil {
		t.Error("a failure taken as deleted")
	}
}

// The connect URL carries the verifier's S256 challenge, unpadded.
func TestConnectURLChallenge(t *testing.T) {
	v := NewVerifier()
	if len(v) < 43 || len(v) > 128 {
		t.Fatalf("verifier is %d long", len(v))
	}
	u, err := url.Parse(ConnectURL("https://wh.example.test/", "https://w.example.test/cb", "st", v))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	// RFC 7636, appendix B.
	if got := Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Errorf("challenge = %s", got)
	}
	if q.Get("code_challenge") != Challenge(v) || q.Get("code_challenge_method") != "S256" || u.Path != "/connect" {
		t.Errorf("connect URL = %s", u)
	}
}

// A household is shared when anyone else is in it, as listed or counted;
// one that can't be told is taken as shared.
func TestHouseholdShared(t *testing.T) {
	for body, shared := range map[string]bool{
		`{"id":1,"name":"Home","members":[{"userId":1},{"userId":2}]}`: true,
		`{"id":1,"name":"Home","members":[{"userId":1}]}`:              false,
		`{"id":1,"name":"Home","members":2}`:                           true,
		`{"id":1,"name":"Home","members":1}`:                           false,
		`{"id":1,"name":"Home"}`:                                       true,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		h, err := New(srv.URL, "t").Household(context.Background())
		srv.Close()
		if err != nil || h.Shared() != shared {
			t.Errorf("%s: shared = %v, %v", body, h.Shared(), err)
		}
	}
}
