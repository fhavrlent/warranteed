package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fhavrlent/warranteed/internal/i18n"
)

// Claim is something that went wrong with a product and was reported to the
// shop or maker under its warranty.
type Claim struct {
	ID         int64    `json:"id"`
	WarrantyID int64    `json:"warrantyId"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	ReportedOn string   `json:"reportedOn"`
	ResolvedOn *string  `json:"resolvedOn"`
	CaseNumber string   `json:"caseNumber"`
	Cost       *float64 `json:"cost"`
	Notes      string   `json:"notes"`
	AddedBy    UserRef  `json:"addedBy"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	// HomeBoxEntry is the claim's entry in the HomeBox item's maintenance
	// log, once it's been copied there.
	HomeBoxEntry string `json:"-"`
}

// ClaimStatuses are how a claim can stand; their names are translated as
// "claims.status.<status>".
var ClaimStatuses = []string{"open", "repaired", "replaced", "refunded", "rejected"}

// Limits on what a claim holds, in characters.
const (
	MaxClaimTitle = 200
	MaxClaimNotes = 5000
)

// ClaimInput is what clients send when adding or changing a claim.
type ClaimInput struct {
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	ReportedOn string   `json:"reportedOn"`
	ResolvedOn *string  `json:"resolvedOn"`
	CaseNumber string   `json:"caseNumber"`
	Cost       *float64 `json:"cost"`
	Notes      string   `json:"notes"`
}

// Normalize trims the input and checks it.
func (in *ClaimInput) Normalize() error {
	in.Title, in.CaseNumber = strings.TrimSpace(in.Title), strings.TrimSpace(in.CaseNumber)
	in.Notes = strings.TrimRight(in.Notes, " \n\t")
	if in.Status == "" {
		in.Status = "open"
	}
	if in.ResolvedOn != nil && strings.TrimSpace(*in.ResolvedOn) == "" {
		in.ResolvedOn = nil
	}
	switch {
	case in.Title == "":
		return invalid("claimTitle", nil)
	case utf8.RuneCountInString(in.Title) > MaxClaimTitle:
		return invalid("claimTitleLong", i18n.Args{"max": MaxClaimTitle})
	case utf8.RuneCountInString(in.Notes) > MaxClaimNotes:
		return invalid("claimNotesLong", i18n.Args{"max": MaxClaimNotes})
	case !slices.Contains(ClaimStatuses, in.Status):
		return invalid("claimStatus", nil)
	case in.Cost != nil && *in.Cost < 0:
		return invalid("negativePrice", nil)
	}
	reported, err := time.Parse(DateLayout, in.ReportedOn)
	if err != nil {
		return invalid("claimReported", nil)
	}
	if in.Status == "open" {
		in.ResolvedOn = nil
	}
	if in.ResolvedOn != nil {
		resolved, err := time.Parse(DateLayout, *in.ResolvedOn)
		if err != nil {
			return invalid("claimResolved", nil)
		}
		if resolved.Before(reported) {
			return invalid("claimResolvedBefore", nil)
		}
	}
	return nil
}

const claimCols = `c.id, c.warranty_id, c.title, c.status, c.reported_on, c.resolved_on, c.case_number, c.cost,
	c.notes, COALESCE(c.added_by, 0), COALESCE(u.name, ''), c.created_at, c.updated_at, c.homebox_entry`

const claimFrom = `claims c JOIN warranties w ON w.id = c.warranty_id LEFT JOIN users u ON u.id = c.added_by`

func scanClaim(row interface{ Scan(...any) error }) (*Claim, error) {
	var c Claim
	err := row.Scan(&c.ID, &c.WarrantyID, &c.Title, &c.Status, &c.ReportedOn, &c.ResolvedOn, &c.CaseNumber,
		&c.Cost, &c.Notes, &c.AddedBy.ID, &c.AddedBy.Name, &c.CreatedAt, &c.UpdatedAt, &c.HomeBoxEntry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

// GetClaim returns a claim on a warranty the user can see.
func (s *Store) GetClaim(ctx context.Context, userID, id int64) (*Claim, error) {
	return scanClaim(s.db.QueryRowContext(ctx,
		`SELECT `+claimCols+` FROM `+claimFrom+` WHERE `+visibleTo+` AND c.id = ?`, userID, id))
}

// CreateClaim adds a claim to a warranty the user can see.
func (s *Store) CreateClaim(ctx context.Context, userID, warrantyID int64, in *ClaimInput) (*Claim, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := warrantyAccess(ctx, tx, userID, warrantyID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `
			INSERT INTO claims (warranty_id, title, status, reported_on, resolved_on, case_number, cost, notes, added_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			warrantyID, in.Title, in.Status, in.ReportedOn, in.ResolvedOn, in.CaseNumber, in.Cost, in.Notes, userID,
		).Scan(&id)
	})
	if err != nil {
		return nil, err
	}
	return s.GetClaim(ctx, userID, id)
}

// UpdateClaim changes a claim on a warranty the user can see.
func (s *Store) UpdateClaim(ctx context.Context, userID, id int64, in *ClaimInput) (*Claim, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE claims AS c SET title = ?, status = ?, reported_on = ?, resolved_on = ?, case_number = ?, cost = ?,
			notes = ?, updated_at = ?
		WHERE c.id = ? AND c.warranty_id IN (SELECT w.id FROM warranties w WHERE `+visibleTo+`)`,
		in.Title, in.Status, in.ReportedOn, in.ResolvedOn, in.CaseNumber, in.Cost, in.Notes, now(), id, userID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.GetClaim(ctx, userID, id)
}

// DeleteClaim deletes a claim and returns it, so its HomeBox entry can go
// too. Its documents stay on the warranty.
func (s *Store) DeleteClaim(ctx context.Context, userID, id int64) (*Claim, error) {
	c, err := s.GetClaim(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM claims WHERE id = ?`, id)
	return c, err
}

// ClaimSync is a claim with the HomeBox item of its warranty, for copying it
// there; it isn't limited to what one user can see.
func (s *Store) ClaimSync(ctx context.Context, id int64) (c *Claim, itemID string, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+claimCols+`, w.homebox_id FROM `+claimFrom+` WHERE c.id = ?`, id)
	var cl Claim
	err = row.Scan(&cl.ID, &cl.WarrantyID, &cl.Title, &cl.Status, &cl.ReportedOn, &cl.ResolvedOn, &cl.CaseNumber,
		&cl.Cost, &cl.Notes, &cl.AddedBy.ID, &cl.AddedBy.Name, &cl.CreatedAt, &cl.UpdatedAt, &cl.HomeBoxEntry, &itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return &cl, itemID, err
}

// SetClaimEntry records the claim's entry in HomeBox item itemID. It reports
// false, saving nothing, when the warranty is no longer linked to that item
// (or the claim is gone), so the entry belongs to nothing here any more.
func (s *Store) SetClaimEntry(ctx context.Context, id int64, itemID, entry string) (bool, error) {
	// One statement, so it can't interleave with relinking, which clears entries.
	res, err := s.db.ExecContext(ctx, `UPDATE claims SET homebox_entry = ? WHERE id = ?
		AND warranty_id IN (SELECT w.id FROM warranties w WHERE w.homebox_id = ? AND w.homebox_id != '')`,
		entry, id, itemID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ClaimIDs lists a warranty's claims, oldest first.
func (s *Store) ClaimIDs(ctx context.Context, warrantyID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM claims WHERE warranty_id = ? ORDER BY id`, warrantyID)
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

// ClaimOf checks that claim belongs to the warranty, for attaching a
// document to it.
func (s *Store) ClaimOf(ctx context.Context, warrantyID, claimID int64) error {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM claims WHERE id = ? AND warranty_id = ?`,
		claimID, warrantyID).Scan(&n)
	if err == nil && n == 0 {
		return invalid("claimNotFound", nil)
	}
	return err
}
