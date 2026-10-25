// Package store contains all database access.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

const timeLayout = "2006-01-02T15:04:05Z"

func now() string { return time.Now().UTC().Format(timeLayout) }

// withTx runs fn in a transaction, committing on success.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---- users ----

type User struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
	IsAdmin bool   `json:"isAdmin"`
}

type UpsertUser struct {
	Issuer, Subject, Email, Name, Picture string
	// IsAdmin is applied when non-nil; nil leaves the stored flag untouched.
	IsAdmin *bool
}

// UpsertUser creates or updates the user identified by (issuer, subject). The
// very first user becomes an admin unless admin groups decide otherwise.
func (s *Store) UpsertUser(ctx context.Context, u UpsertUser) (*User, error) {
	var out User
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		admin := count == 0
		if u.IsAdmin != nil {
			admin = *u.IsAdmin
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO users (issuer, subject, email, name, picture, is_admin, last_login_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (issuer, subject) DO UPDATE SET
				email = excluded.email,
				name = excluded.name,
				picture = excluded.picture,
				is_admin = CASE WHEN ? THEN excluded.is_admin ELSE users.is_admin END,
				last_login_at = excluded.last_login_at`,
			u.Issuer, u.Subject, u.Email, u.Name, u.Picture, admin, now(), u.IsAdmin != nil)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT id, email, name, picture_etag, is_admin FROM users WHERE issuer = ? AND subject = ?`,
			u.Issuer, u.Subject,
		).Scan(&out.ID, &out.Email, &out.Name, &out.Picture, &out.IsAdmin); err != nil {
			return err
		}
		out.Picture = pictureURL(out.Picture)
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id) VALUES (?) ON CONFLICT DO NOTHING`, out.ID); err != nil {
			return err
		}
		return ensureUserSpaces(ctx, tx, out.ID)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// pictureURL is where the app serves a user's picture; the etag in the query
// lets browsers cache it until it changes.
func pictureURL(etag string) string {
	if etag == "" {
		return ""
	}
	return "/api/me/picture?v=" + etag
}

// SetUserPicture stores the user's profile picture; nil data removes it.
func (s *Store) SetUserPicture(ctx context.Context, userID int64, data []byte, contentType string) error {
	etag := ""
	if data != nil {
		sum := sha256.Sum256(data)
		etag = hex.EncodeToString(sum[:8])
	} else {
		contentType = ""
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET picture_data = ?, picture_type = ?, picture_etag = ? WHERE id = ?`,
		data, contentType, etag, userID)
	return err
}

// UserPicture returns the user's stored profile picture and its content type.
func (s *Store) UserPicture(ctx context.Context, userID int64) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := s.db.QueryRowContext(ctx, `SELECT picture_data, picture_type FROM users WHERE id = ?`, userID).Scan(&data, &contentType)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && data == nil) {
		return nil, "", ErrNotFound
	}
	return data, contentType, err
}

// IsAdmin reports whether the user is an admin.
func (s *Store) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	var admin bool
	err := s.db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return admin, err
}

func (s *Store) ListUserIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---- sessions ----

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession stores a new session. refreshToken is already encrypted.
func (s *Store) CreateSession(ctx context.Context, token string, userID int64, idToken, refreshToken string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, id_token, refresh_token, checked_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, idToken, refreshToken, now(), time.Now().Add(ttl).UTC().Format(timeLayout))
	return err
}

// Session is a signed-in session.
type Session struct {
	Hash         string
	Issuer       string // the user's identity provider
	IDToken      string
	RefreshToken string // encrypted; empty when the provider gave none
	CheckedAt    time.Time
}

// SessionUser returns the user and session for a valid, unexpired session token.
func (s *Store) SessionUser(ctx context.Context, token string) (*User, *Session, error) {
	return s.sessionBy(ctx, hashToken(token))
}

// SessionByHash re-reads a session, e.g. after waiting for a lock.
func (s *Store) SessionByHash(ctx context.Context, hash string) (*Session, error) {
	_, sess, err := s.sessionBy(ctx, hash)
	return sess, err
}

func (s *Store) sessionBy(ctx context.Context, hash string) (*User, *Session, error) {
	var u User
	sess := Session{Hash: hash}
	var checked string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.name, u.picture_etag, u.is_admin, u.issuer, s.id_token, s.refresh_token, s.checked_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		hash, now(),
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.IsAdmin, &sess.Issuer, &sess.IDToken, &sess.RefreshToken, &checked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	sess.CheckedAt, _ = time.Parse(timeLayout, checked) // '' = never checked
	u.Picture = pictureURL(u.Picture)
	return &u, &sess, nil
}

// UpdateSessionCheck records a successful re-check with the new tokens.
func (s *Store) UpdateSessionCheck(ctx context.Context, hash, idToken, refreshToken string, checkedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET id_token = ?, refresh_token = ?, checked_at = ? WHERE token_hash = ?`,
		idToken, refreshToken, checkedAt.UTC().Format(timeLayout), hash)
	return err
}

// DeleteSessionByHash ends a session, e.g. when the user lost access.
func (s *Store) DeleteSessionByHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

func (s *Store) SetAdmin(ctx context.Context, userID int64, admin bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, admin, userID)
	return err
}

// DeleteSession removes the session and returns its stored ID token (used for
// RP-initiated logout).
func (s *Store) DeleteSession(ctx context.Context, token string) (string, error) {
	var idToken string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM sessions WHERE token_hash = ? RETURNING id_token`, hashToken(token),
	).Scan(&idToken)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return idToken, err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}

// ---- settings ----

type Settings struct {
	DateFormat       string `json:"dateFormat"`
	Currency         string `json:"currency"`
	Theme            string `json:"theme"`
	ExpiringSoonDays int    `json:"expiringSoonDays"`
	NotifyURLs       string `json:"notifyUrls"`
	NotifyDays       string `json:"notifyDays"`
	PaperlessURL     string `json:"paperlessUrl"`
	PaperlessToken   string `json:"-"`
	PaperlessDefault bool   `json:"paperlessDefault"`
}

func (s *Store) Settings(ctx context.Context, userID int64) (*Settings, error) {
	var st Settings
	err := s.db.QueryRowContext(ctx, `
		SELECT date_format, currency, theme, expiring_soon_days, notify_urls, notify_days,
		       paperless_url, paperless_token, paperless_default
		FROM user_settings WHERE user_id = ?`, userID,
	).Scan(&st.DateFormat, &st.Currency, &st.Theme, &st.ExpiringSoonDays, &st.NotifyURLs,
		&st.NotifyDays, &st.PaperlessURL, &st.PaperlessToken, &st.PaperlessDefault)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &st, err
}

func (s *Store) SaveSettings(ctx context.Context, userID int64, st *Settings) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_settings SET
			date_format = ?, currency = ?, theme = ?, expiring_soon_days = ?, notify_urls = ?,
			notify_days = ?, paperless_url = ?, paperless_token = ?, paperless_default = ?
		WHERE user_id = ?`,
		st.DateFormat, st.Currency, st.Theme, st.ExpiringSoonDays, st.NotifyURLs,
		st.NotifyDays, st.PaperlessURL, st.PaperlessToken, st.PaperlessDefault, userID)
	return err
}
