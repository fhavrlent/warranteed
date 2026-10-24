// Package auth implements OIDC single sign-on and cookie sessions.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/store"
)

const (
	sessionCookie = "warranteed_session"
	flowCookie    = "warranteed_oidc"
	devIssuer     = "dev"
)

type ctxKey struct{}

// UserFrom returns the authenticated user attached by Middleware.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxKey{}).(*store.User)
	return u
}

type Auth struct {
	cfg   *config.Config
	store *store.Store

	mu       sync.Mutex
	provider *oidc.Provider
	endSess  string
}

func New(cfg *config.Config, st *store.Store) *Auth {
	return &Auth{cfg: cfg, store: st}
}

// discover loads the provider's metadata lazily, so the app starts (and
// retries later) even when the identity provider is temporarily down.
func (a *Auth) discover(ctx context.Context) (*oidc.Provider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.provider, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, a.cfg.OIDC.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery for %s failed: %w", a.cfg.OIDC.Issuer, err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&meta)
	a.provider, a.endSess = p, meta.EndSession
	return p, nil
}

func (a *Auth) oauth(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.OIDC.ClientID,
		ClientSecret: a.cfg.OIDC.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  a.cfg.RedirectURL(),
		Scopes:       a.cfg.OIDC.Scopes,
	}
}

// Info is public login-page configuration.
type Info struct {
	ProviderName string `json:"providerName"`
	OIDC         bool   `json:"oidc"`
	DevLogin     bool   `json:"devLogin"`
	AutoRedirect bool   `json:"autoRedirect"`
}

func (a *Auth) Info() Info {
	return Info{
		ProviderName: a.cfg.OIDC.ProviderName,
		OIDC:         a.cfg.OIDC.Issuer != "",
		DevLogin:     a.cfg.DevLogin,
		AutoRedirect: a.cfg.OIDC.AutoRedirect && a.cfg.OIDC.Issuer != "",
	}
}

type flowState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Return   string `json:"r"`
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// safeReturn only allows same-origin relative paths as post-login redirects.
func safeReturn(r string) string {
	if !strings.HasPrefix(r, "/") || strings.HasPrefix(r, "//") || strings.HasPrefix(r, "/\\") {
		return "/"
	}
	return r
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if a.cfg.OIDC.Issuer == "" {
		http.Error(w, "OIDC is not configured", http.StatusNotFound)
		return
	}
	p, err := a.discover(r.Context())
	if err != nil {
		slog.Error("oidc discovery", "err", err)
		a.failLogin(w, r, "Could not reach the identity provider. Try again in a moment.")
		return
	}
	fs := flowState{
		State:    randomString(24),
		Nonce:    randomString(24),
		Verifier: oauth2.GenerateVerifier(),
		Return:   safeReturn(r.URL.Query().Get("return")),
	}
	raw, _ := json.Marshal(fs)
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookie,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     "/auth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   a.cfg.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, a.oauth(p).AuthCodeURL(fs.State,
		oidc.Nonce(fs.Nonce),
		oauth2.S256ChallengeOption(fs.Verifier),
	), http.StatusFound)
}

func (a *Auth) failLogin(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(msg), http.StatusFound)
}

func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		a.failLogin(w, r, "Sign-in was cancelled or denied: "+msg)
		return
	}

	c, err := r.Cookie(flowCookie)
	if err != nil {
		a.failLogin(w, r, "Your sign-in attempt expired. Please try again.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Secure()})
	var fs flowState
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(raw, &fs) != nil || fs.State == "" || q.Get("state") != fs.State {
		a.failLogin(w, r, "Sign-in state did not match. Please try again.")
		return
	}

	p, err := a.discover(ctx)
	if err != nil {
		a.failLogin(w, r, "Could not reach the identity provider.")
		return
	}
	tok, err := a.oauth(p).Exchange(ctx, q.Get("code"), oauth2.VerifierOption(fs.Verifier))
	if err != nil {
		slog.Error("oidc code exchange", "err", err)
		a.failLogin(w, r, "The identity provider rejected the sign-in. Check the client ID, secret and callback URL.")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		a.failLogin(w, r, "The identity provider did not return an ID token.")
		return
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: a.cfg.OIDC.ClientID}).Verify(ctx, rawID)
	if err != nil || idt.Nonce != fs.Nonce {
		slog.Error("oidc id token verification", "err", err)
		a.failLogin(w, r, "Could not verify the identity token.")
		return
	}

	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		a.failLogin(w, r, "Could not read identity claims.")
		return
	}
	// Some providers only put profile and group claims in userinfo.
	if ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
		extra := map[string]any{}
		if ui.Claims(&extra) == nil {
			for k, v := range extra {
				if _, ok := claims[k]; !ok {
					claims[k] = v
				}
			}
		}
	}

	groups := stringList(claims[a.cfg.OIDC.GroupsClaim])
	if len(a.cfg.OIDC.AllowedGroups) > 0 && !intersects(groups, a.cfg.OIDC.AllowedGroups) && !intersects(groups, a.cfg.OIDC.AdminGroups) {
		a.failLogin(w, r, "Your account is not allowed to use this app.")
		return
	}
	var isAdmin *bool
	if len(a.cfg.OIDC.AdminGroups) > 0 {
		v := intersects(groups, a.cfg.OIDC.AdminGroups)
		isAdmin = &v
	}

	name := str(claims["name"])
	if name == "" {
		name = strings.TrimSpace(str(claims["given_name"]) + " " + str(claims["family_name"]))
	}
	if name == "" {
		name = str(claims["preferred_username"])
	}
	user, err := a.store.UpsertUser(ctx, store.UpsertUser{
		Issuer:  idt.Issuer,
		Subject: idt.Subject,
		Email:   str(claims["email"]),
		Name:    name,
		Picture: str(claims["picture"]),
		IsAdmin: isAdmin,
	})
	if err != nil {
		slog.Error("upsert user", "err", err)
		a.failLogin(w, r, "Could not create your account.")
		return
	}
	if err := a.startSession(w, r, user.ID, rawID); err != nil {
		slog.Error("create session", "err", err)
		a.failLogin(w, r, "Could not start a session.")
		return
	}
	slog.Info("user signed in", "user", user.ID, "email", user.Email)
	http.Redirect(w, r, fs.Return, http.StatusFound)
}

// DevLogin signs in a fixed local user. Only registered when explicitly enabled.
func (a *Auth) DevLogin(w http.ResponseWriter, r *http.Request) {
	user, err := a.store.UpsertUser(r.Context(), store.UpsertUser{
		Issuer: devIssuer, Subject: "dev", Email: "dev@localhost", Name: "Dev User",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.startSession(w, r, user.ID, ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, safeReturn(r.URL.Query().Get("return")), http.StatusFound)
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, userID int64, idToken string) error {
	token := randomString(32)
	if err := a.store.CreateSession(r.Context(), token, userID, idToken, a.cfg.SessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.cfg.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Logout ends the local session and returns where the browser should go next
// (the provider's end-session endpoint when configured).
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	next := "/login"
	if c, err := r.Cookie(sessionCookie); err == nil {
		idToken, err := a.store.DeleteSession(r.Context(), c.Value)
		if err != nil {
			slog.Error("delete session", "err", err)
		}
		if a.cfg.OIDC.LogoutAtProvider && idToken != "" {
			if _, err := a.discover(r.Context()); err == nil && a.endSess != "" {
				q := url.Values{}
				q.Set("id_token_hint", idToken)
				q.Set("post_logout_redirect_uri", strings.TrimSuffix(a.cfg.AppURL.String(), "/")+"/login")
				next = a.endSess + "?" + q.Encode()
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Secure()})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"redirect": next})
}

// Middleware attaches the session user to the request context, rejecting
// unauthenticated requests with 401.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			unauthorized(w)
			return
		}
		user, err := a.store.SessionUser(r.Context(), c.Value)
		if errors.Is(err, store.ErrNotFound) {
			unauthorized(w)
			return
		}
		if err != nil {
			slog.Error("session lookup", "err", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"not signed in"}`))
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		return strings.Fields(strings.ReplaceAll(t, ",", " "))
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}
