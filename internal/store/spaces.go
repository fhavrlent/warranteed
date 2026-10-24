package store

import (
	"context"
	"database/sql"
	"errors"
)

// Space kinds. Every user has a personal space; everyone belongs to the one
// household space. "custom" is reserved for shared spaces with chosen members.
const (
	SpacePersonal  = "personal"
	SpaceHousehold = "household"
)

type Space struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Members int    `json:"members"`
}

// ListSpaces returns the spaces the user belongs to, shared ones first.
func (s *Store) ListSpaces(ctx context.Context, userID int64) ([]Space, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sp.id, sp.name, sp.kind,
			(SELECT COUNT(*) FROM space_members WHERE space_id = sp.id) FROM spaces sp
		JOIN space_members m ON m.space_id = sp.id AND m.user_id = ?
		ORDER BY sp.kind = 'personal', sp.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Space{}
	for rows.Next() {
		var sp Space
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Kind, &sp.Members); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ensureUserSpaces gives a user their personal space and household membership.
func ensureUserSpaces(ctx context.Context, tx *sql.Tx, userID int64) error {
	stmts := []string{
		`INSERT INTO spaces (name, kind, owner_id) SELECT 'Personal', 'personal', ?
			WHERE NOT EXISTS (SELECT 1 FROM spaces WHERE kind = 'personal' AND owner_id = ?)`,
		`INSERT INTO space_members (space_id, user_id)
			SELECT id, owner_id FROM spaces WHERE kind = 'personal' AND owner_id = ? ON CONFLICT DO NOTHING`,
		`INSERT INTO spaces (name, kind) SELECT 'Household', 'household'
			WHERE NOT EXISTS (SELECT 1 FROM spaces WHERE kind = 'household')`,
		`INSERT INTO space_members (space_id, user_id)
			SELECT id, ? FROM spaces WHERE kind = 'household' ON CONFLICT DO NOTHING`,
	}
	args := [][]any{{userID, userID}, {userID}, {}, {userID}}
	for i, q := range stmts {
		if _, err := tx.ExecContext(ctx, q, args[i]...); err != nil {
			return err
		}
	}
	return nil
}

// targetSpace resolves where a warranty should live: the given space if the
// user is a member, or the household when none is given.
func targetSpace(ctx context.Context, tx *sql.Tx, userID int64, spaceID *int64) (int64, string, error) {
	var id int64
	var kind string
	var err error
	if spaceID == nil {
		err = tx.QueryRowContext(ctx, `SELECT sp.id, sp.kind FROM spaces sp
			JOIN space_members m ON m.space_id = sp.id AND m.user_id = ?
			WHERE sp.kind = 'household'`, userID).Scan(&id, &kind)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT sp.id, sp.kind FROM spaces sp
			JOIN space_members m ON m.space_id = sp.id AND m.user_id = ?
			WHERE sp.id = ?`, userID, *spaceID).Scan(&id, &kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", invalid("You can't add warranties there")
	}
	return id, kind, err
}

func personalSpace(ctx context.Context, tx *sql.Tx, userID int64) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM spaces WHERE kind = 'personal' AND owner_id = ?`, userID).Scan(&id)
	return id, err
}
