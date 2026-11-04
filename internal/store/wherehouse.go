package store

import (
	"context"
	"database/sql"
	"errors"
)

// SetWherehouse links a warranty to a Wherehouse thing; nil removes the
// link, and the claims' entries there with it.
func (s *Store) SetWherehouse(ctx context.Context, userID, id int64, link *WherehouseLink) error {
	var thing any
	label, path := "", ""
	if link != nil {
		thing, label, path = link.Thing, link.Label, link.Path
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE warranties AS w SET wherehouse_thing = ?, wherehouse_label = ?, wherehouse_path = ?, updated_at = ?
		WHERE `+visibleTo+` AND w.id = ?`, thing, label, path, now(), userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE claims SET wherehouse_entry = NULL WHERE warranty_id = ?`, id)
	return err
}

// LinkedToThing returns the warranties linked to a Wherehouse thing that
// someone with that Wherehouse connection can see.
func (s *Store) LinkedToThing(ctx context.Context, userID, thing int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id FROM warranties w WHERE `+visibleTo+` AND w.wherehouse_thing = ?`, userID, thing)
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

// SetWherehouseSeen records where and what a linked thing is now, as the
// change feed told.
func (s *Store) SetWherehouseSeen(ctx context.Context, warrantyID int64, label, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE warranties SET wherehouse_label = ?, wherehouse_path = ? WHERE id = ?`,
		label, path, warrantyID)
	return err
}

// SetWherehouseConnection saves or (with an empty token) forgets a person's
// Wherehouse connection.
func (s *Store) SetWherehouseConnection(ctx context.Context, userID int64, token, household string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_settings SET wherehouse_token = ?, wherehouse_household = ?, wherehouse_since = '',
		wherehouse_hook_secret = '' WHERE user_id = ?`, token, household, userID)
	return err
}

// SetWherehouseSince records how far a person's Wherehouse changes were read.
func (s *Store) SetWherehouseSince(ctx context.Context, userID int64, since string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_settings SET wherehouse_since = ? WHERE user_id = ?`, since, userID)
	return err
}

// WherehouseConnected lists everyone connected to Wherehouse.
func (s *Store) WherehouseConnected(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM user_settings WHERE wherehouse_token != '' ORDER BY user_id`)
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

// WherehouseUsers lists the people sharing a warranty who are connected to
// Wherehouse, for updating its thing there.
func (s *Store) WherehouseUsers(ctx context.Context, warrantyID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sm.user_id FROM warranties w JOIN space_members sm ON sm.space_id = w.space_id
		JOIN user_settings us ON us.user_id = sm.user_id
		WHERE w.id = ? AND us.wherehouse_token != '' ORDER BY sm.user_id`, warrantyID)
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

// ClaimEntry is a claim's Wherehouse maintenance entry and the thing it is on.
func (s *Store) ClaimWherehouse(ctx context.Context, claimID int64) (entry *int64, thing *int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT c.wherehouse_entry, w.wherehouse_thing FROM claims c
		JOIN warranties w ON w.id = c.warranty_id WHERE c.id = ?`, claimID).Scan(&entry, &thing)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return entry, thing, err
}

// SetClaimWherehouseEntry records a claim's maintenance entry, if its
// warranty is still on thing.
func (s *Store) SetClaimWherehouseEntry(ctx context.Context, claimID, thing, entry int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE claims SET wherehouse_entry = ? WHERE id = ?
		AND warranty_id IN (SELECT id FROM warranties WHERE wherehouse_thing = ?)`, entry, claimID, thing)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetWherehouseHookSecret saves the secret a person's Wherehouse pings are
// signed with.
func (s *Store) SetWherehouseHookSecret(ctx context.Context, userID int64, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_settings SET wherehouse_hook_secret = ? WHERE user_id = ?`, secret, userID)
	return err
}

// WherehouseHook is a person's ping secret.
type WherehouseHook struct {
	UserID int64
	Secret string
}

// WherehouseHooks lists the ping secrets of everyone connected.
func (s *Store) WherehouseHooks(ctx context.Context) ([]WherehouseHook, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, wherehouse_hook_secret FROM user_settings
		WHERE wherehouse_token != '' AND wherehouse_hook_secret != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WherehouseHook
	for rows.Next() {
		var h WherehouseHook
		if err := rows.Scan(&h.UserID, &h.Secret); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
