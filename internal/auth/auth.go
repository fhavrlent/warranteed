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
	key   []byte // encrypts stored refresh tokens

	mu       sync.Mutex
	provider *oidc.Provider
	endSess  string

	// One lock per session so concurrent requests don't refresh at once:
	// providers rotate refresh tokens, and a second refresh would fail.
	recheckLocks sync.Map // session hash -> *sync.Mutex
	warnOnce     sync.Once
}

// Sessions without a refresh token can't be re-checked, so they last at most this long.
const unrecheckableSessionTTL = 7 * 24 * time.Hour

func New(cfg *config.Config, st *store.Store, key []byte) *Auth {
	return &Auth{cfg: cfg, store: st, key: key}
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
// Browsers strip tabs and newlines from URLs and treat a backslash like "/",
// so "/<tab>/evil.example" or "/\evil.example" would become "//evil.example", a
// different site. Any control character or backslash is therefore refused,
// and the parsed result must have no scheme or host.
func safeReturn(r string) string {
	if r == "" || r[0] != '/' {
		return "/"
	}
	// "//host" and "/\host" are protocol-relative URLs to another site.
	if len(r) > 1 && (r[1] == '/' || r[1] == '\\') {
		return "/"
	}
	for _, c := range r {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return "/"
		}
	}
	u, err := url.Parse(r)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || !strings.HasPrefix(u.Path, "/") {
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
	a.mergeUserInfo(ctx, p, tok, claims)

	allowed, isAdmin := a.access(claims)
	if !allowed {
		a.failLogin(w, r, "Your account is not allowed to use this app.")
		return
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
	if err := a.startSession(w, r, user.ID, rawID, tok.RefreshToken); err != nil {
		slog.Error("create session", "err", err)
		a.failLogin(w, r, "Could not start a session.")
		return
	}
	slog.Info("user signed in", "user", user.ID, "email", user.Email)
	// Checked again: the value came back from a cookie.
	http.Redirect(w, r, safeReturn(fs.Return), http.StatusFound)
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
	if err := a.startSession(w, r, user.ID, "", ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, safeReturn(r.URL.Query().Get("return")), http.StatusFound)
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, userID int64, idToken, refreshToken string) error {
	ttl := a.cfg.SessionTTL
	if idToken != "" && refreshToken == "" {
		// Without a refresh token, losing access at the provider can't be
		// noticed, so keep such sessions short.
		ttl = min(ttl, unrecheckableSessionTTL)
		a.warnOnce.Do(func() {
			slog.Warn("identity provider issued no refresh token; sessions last at most 7 days " +
				"and removed users keep access until then. Request the offline_access scope if your provider supports it.")
		})
	}
	encRefresh, err := encrypt(a.key, refreshToken)
	if err != nil {
		return err
	}
	token := randomString(32)
	if err := a.store.CreateSession(r.Context(), token, userID, idToken, encRefresh, ttl); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
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
		user, sess, err := a.store.SessionUser(r.Context(), c.Value)
		if errors.Is(err, store.ErrNotFound) {
			unauthorized(w)
			return
		}
		if err != nil {
			slog.Error("session lookup", "err", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		admin, err := a.recheck(r.Context(), user.ID, sess)
		if errors.Is(err, errRevoked) {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.Secure()})
			unauthorized(w)
			return
		}
		if admin != nil {
			user.IsAdmin = *admin
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"not signed in"}`))
}

// mergeUserInfo adds userinfo claims missing from the ID token; some
// providers only put profile and group claims there.
func (a *Auth) mergeUserInfo(ctx context.Context, p *oidc.Provider, tok *oauth2.Token, claims map[string]any) {
	ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok))
	if err != nil {
		return
	}
	extra := map[string]any{}
	if ui.Claims(&extra) != nil {
		return
	}
	for k, v := range extra {
		if _, ok := claims[k]; !ok {
			claims[k] = v
		}
	}
}

// access applies the group rules to a user's claims: whether they may sign in
// and, when admin groups are configured, whether they are an admin.
func (a *Auth) access(claims map[string]any) (allowed bool, isAdmin *bool) {
	groups := stringList(claims[a.cfg.OIDC.GroupsClaim])
	o := a.cfg.OIDC
	if len(o.AllowedGroups) > 0 && !intersects(groups, o.AllowedGroups) && !intersects(groups, o.AdminGroups) {
		return false, nil
	}
	if len(o.AdminGroups) > 0 {
		v := intersects(groups, o.AdminGroups)
		isAdmin = &v
	}
	return true, isAdmin
}

var errRevoked = errors.New("session revoked")

// recheck re-validates an OIDC session with the identity provider once per
// RecheckInterval, using its refresh token. The session ends when the
// provider rejects the refresh (user removed or token revoked) or the user is
// no longer in the allowed groups. When the provider can't be reached the
// request is let through and the check retried shortly, so an outage doesn't
// sign everyone out. Returns the user's updated admin flag, if it changed.
func (a *Auth) recheck(ctx context.Context, userID int64, sess *store.Session) (*bool, error) {
	if !a.dueForRecheck(sess) {
		return nil, nil
	}
	lock, _ := a.recheckLocks.LoadOrStore(sess.Hash, &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Another request may have finished the check while we waited.
	sess, err := a.store.SessionByHash(ctx, sess.Hash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errRevoked
	}
	if err != nil || !a.dueForRecheck(sess) {
		return nil, nil
	}

	end := func(reason string, args ...any) (*bool, error) {
		slog.Info("session ended: "+reason, append([]any{"user", userID}, args...)...)
		a.store.DeleteSessionByHash(ctx, sess.Hash)
		return nil, errRevoked
	}
	retryLater := func(reason string, err error) (*bool, error) {
		slog.Warn("session re-check postponed: "+reason, "user", userID, "err", err)
		// Try again in five minutes rather than on every request.
		a.store.UpdateSessionCheck(ctx, sess.Hash, sess.IDToken, sess.RefreshToken,
			time.Now().Add(-a.cfg.OIDC.RecheckInterval+5*time.Minute))
		return nil, nil
	}

	refresh, err := decrypt(a.key, sess.RefreshToken)
	if err != nil {
		return end("stored refresh token can't be decrypted (was the secret changed?)")
	}
	p, err := a.discover(ctx)
	if err != nil {
		return retryLater("identity provider unreachable", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tok, err := a.oauth(p).TokenSource(cctx, &oauth2.Token{RefreshToken: refresh}).Token()
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) {
		return end("identity provider refused the refresh", "error", rerr.ErrorCode)
	}
	if err != nil {
		return retryLater("refresh failed", err)
	}

	claims := map[string]any{}
	idToken := sess.IDToken
	if raw, _ := tok.Extra("id_token").(string); raw != "" {
		idt, err := p.Verifier(&oidc.Config{ClientID: a.cfg.OIDC.ClientID}).Verify(cctx, raw)
		if err != nil {
			return retryLater("refreshed ID token didn't verify", err)
		}
		if err := idt.Claims(&claims); err != nil {
			return retryLater("refreshed ID token unreadable", err)
		}
		idToken = raw
	}
	a.mergeUserInfo(cctx, p, tok, claims)
	if len(claims) == 0 {
		return retryLater("provider returned no claims", nil)
	}

	allowed, isAdmin := a.access(claims)
	if !allowed {
		return end("no longer in an allowed group")
	}
	if isAdmin != nil {
		if err := a.store.SetAdmin(ctx, userID, *isAdmin); err != nil {
			return nil, err
		}
	}
	encRefresh, err := encrypt(a.key, tok.RefreshToken)
	if err != nil {
		return nil, err
	}
	if err := a.store.UpdateSessionCheck(ctx, sess.Hash, idToken, encRefresh, time.Now()); err != nil {
		return nil, err
	}
	return isAdmin, nil
}

func (a *Auth) dueForRecheck(sess *store.Session) bool {
	return a.cfg.OIDC.Issuer != "" && sess.Issuer != devIssuer && sess.RefreshToken != "" &&
		time.Since(sess.CheckedAt) >= a.cfg.OIDC.RecheckInterval
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
