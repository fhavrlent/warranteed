// Package api wires HTTP routes, middleware and handlers.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/notify"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

type Server struct {
	cfg      *config.Config
	store    *store.Store
	auth     *auth.Auth
	notifier *notify.Notifier
	files    *Files
	assets   fs.FS
	version  string
}

func New(cfg *config.Config, st *store.Store, a *auth.Auth, n *notify.Notifier, files *Files, assets fs.FS, version string) *Server {
	return &Server{cfg: cfg, store: st, auth: a, notifier: n, files: files, assets: assets, version: version}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /auth/login", s.auth.Login)
	mux.HandleFunc("GET /auth/callback", s.auth.Callback)
	mux.HandleFunc("POST /auth/logout", s.auth.Logout)
	if s.cfg.DevLogin {
		mux.HandleFunc("GET /auth/dev", s.auth.DevLogin)
	}
	mux.HandleFunc("GET /api/auth/info", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.auth.Info()) })

	api := http.NewServeMux()
	api.HandleFunc("GET /api/me", s.me)
	api.HandleFunc("GET /api/me/picture", s.mePicture)
	api.HandleFunc("GET /api/settings", s.getSettings)
	api.HandleFunc("PUT /api/settings", s.putSettings)
	api.HandleFunc("POST /api/settings/test-notification", s.testNotification)
	api.HandleFunc("POST /api/settings/test-paperless", s.testPaperless)

	api.HandleFunc("GET /api/warranties", s.listWarranties)
	api.HandleFunc("POST /api/warranties", s.createWarranty)
	api.HandleFunc("GET /api/warranties/{id}", s.getWarranty)
	api.HandleFunc("PUT /api/warranties/{id}", s.updateWarranty)
	api.HandleFunc("DELETE /api/warranties/{id}", s.deleteWarranty)
	api.HandleFunc("POST /api/warranties/{id}/archive", s.archiveWarranty(true))
	api.HandleFunc("POST /api/warranties/{id}/unarchive", s.archiveWarranty(false))
	api.HandleFunc("POST /api/warranties/{id}/attachments", s.uploadAttachment)
	api.HandleFunc("POST /api/warranties/{id}/attachments/paperless", s.linkPaperless)

	api.HandleFunc("GET /api/attachments/{id}/file", s.attachmentFile)
	api.HandleFunc("GET /api/attachments/{id}/thumb", s.attachmentThumb)
	api.HandleFunc("PATCH /api/attachments/{id}", s.patchAttachment)
	api.HandleFunc("DELETE /api/attachments/{id}", s.deleteAttachment)
	api.HandleFunc("POST /api/attachments/{id}/retry", s.retryAttachment)

	api.HandleFunc("GET /api/paperless/search", s.searchPaperless)
	api.HandleFunc("GET /api/paperless/documents/{doc}/thumb", s.paperlessThumb)

	api.HandleFunc("GET /api/spaces", s.listSpaces)
	api.HandleFunc("POST /api/spaces/{id}/move-all", s.moveAllToSpace)

	api.HandleFunc("GET /api/tags", s.listTags)
	api.HandleFunc("PATCH /api/tags/{id}", s.updateTag)
	api.HandleFunc("DELETE /api/tags/{id}", s.deleteTag)

	api.HandleFunc("GET /api/export.csv", s.exportCSV)
	api.HandleFunc("POST /api/import", s.importCSV)

	mux.Handle("/api/", s.auth.Middleware(api))
	mux.Handle("/", s.spa())

	return recoverer(logRequests(securityHeaders(sameOrigin(s.cfg, mux))))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db unavailable"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "version": s.version})
}

// spa serves the embedded frontend, falling back to index.html for client routes.
func (s *Server) spa() http.Handler {
	fileServer := http.FileServerFS(s.assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(s.assets, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "assets/") {
				http.NotFound(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.assets, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

// ---- middleware ----

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		if !strings.HasPrefix(r.URL.Path, "/api/attachments/") {
			h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
				"frame-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin rejects state-changing requests from other origins. Together with
// SameSite=Lax session cookies this protects against CSRF.
func sameOrigin(cfg *config.Config, next http.Handler) http.Handler {
	allowed := strings.TrimSuffix(cfg.AppURL.Scheme+"://"+cfg.AppURL.Host, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			origin := r.Header.Get("Origin")
			if origin == "" {
				if ref := r.Header.Get("Referer"); ref != "" {
					origin = ref
				}
			}
			if origin != "" && !strings.HasPrefix(origin+"/", allowed+"/") && !sameHost(origin, r.Host) {
				writeMsg(w, http.StatusForbidden, "server.crossOrigin", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	if i := strings.IndexByte(o, '/'); i >= 0 {
		o = o[:i]
	}
	return o == host
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") || sw.status >= 500 {
			slog.Debug("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start))
		}
		if sw.status >= 500 {
			slog.Warn("request failed", "method", r.Method, "path", r.URL.Path, "status", sw.status)
		}
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic", "path", r.URL.Path, "panic", v)
				writeMsg(w, http.StatusInternalServerError, "server.internal", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// apiError is the body of every error response. Code is a key in the
// translation files, which the web app shows in the user's language; Error is
// the English text for logs and other clients.
type apiError struct {
	Error  string    `json:"error"`
	Code   string    `json:"code,omitempty"`
	Params i18n.Args `json:"params,omitempty"`
}

// writeError sends text that has no translation, e.g. an error from Paperless.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// writeMsg sends a translatable message.
func writeMsg(w http.ResponseWriter, status int, key string, args i18n.Args) {
	writeJSON(w, status, apiError{Error: i18n.T(i18n.Fallback, key, args), Code: key, Params: args})
}

// writeErr sends err, translated when it is an *i18n.Error or Paperless
// refusing the token. Other errors from Paperless or notification services
// are passed on as they are.
func writeErr(w http.ResponseWriter, status int, err error) {
	var ie *i18n.Error
	var pe *paperless.Error
	switch {
	case errors.As(err, &ie):
		writeMsg(w, status, ie.Key, ie.Args)
	case errors.As(err, &pe) && (pe.Status == http.StatusUnauthorized || pe.Status == http.StatusForbidden):
		writeMsg(w, status, "server.paperlessTokenRejected", nil)
	default:
		writeError(w, status, err.Error())
	}
}

// fail maps store errors to HTTP responses.
func fail(w http.ResponseWriter, err error) {
	var ve *store.ValidationError
	switch {
	case errors.As(err, &ve):
		writeMsg(w, http.StatusBadRequest, ve.Key, ve.Args)
	case errors.Is(err, store.ErrNotFound):
		writeMsg(w, http.StatusNotFound, "server.notFound", nil)
	default:
		slog.Error("request error", "err", err)
		writeMsg(w, http.StatusInternalServerError, "server.internal", nil)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeMsg(w, http.StatusBadRequest, "server.badRequest", nil)
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeMsg(w, http.StatusBadRequest, "server.badId", nil)
		return 0, false
	}
	return id, true
}

func userID(r *http.Request) int64 { return auth.UserFrom(r.Context()).ID }
