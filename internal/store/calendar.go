package store

import (
	"context"
	"database/sql"
	"errors"
)

// CalendarToken returns the secret in the user's calendar feed link, or ""
// when they have no feed.
func (s *Store) CalendarToken(ctx context.Context, userID int64) (string, error) {
	var token sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT calendar_token FROM users WHERE id = ?`, userID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return token.String, err
}

// SetCalendarToken gives the user a feed with this secret, replacing any
// old one; "" turns the feed off.
func (s *Store) SetCalendarToken(ctx context.Context, userID int64, token string) error {
	var v any
	if token != "" {
		v = token
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET calendar_token = ? WHERE id = ?`, v, userID)
	return err
}

// CalendarUser returns the user whose feed has this secret.
func (s *Store) CalendarUser(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE calendar_token = ?`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
