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
			`SELECT id, email, name, picture, is_admin FROM users WHERE issuer = ? AND subject = ?`,
			u.Issuer, u.Subject,
		).Scan(&out.ID, &out.Email, &out.Name, &out.Picture, &out.IsAdmin); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO user_settings (user_id) VALUES (?) ON CONFLICT DO NOTHING`, out.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
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

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, idToken string, ttl time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, id_token, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, idToken, time.Now().Add(ttl).UTC().Format(timeLayout))
	return err
}

// SessionUser returns the user for a valid, unexpired session token.
func (s *Store) SessionUser(ctx context.Context, token string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.name, u.picture, u.is_admin
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`,
		hashToken(token), now(),
	).Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
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
