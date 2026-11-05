package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// SetWherehouse links a warranty to a Wherehouse thing; nil removes the
// link, and the claims' entries there with it. What was last seen of the
// thing before is forgotten too, so another thing starts afresh.
func (s *Store) SetWherehouse(ctx context.Context, userID, id int64, link *WherehouseLink) error {
	var thing any
	label, path := "", ""
	if link != nil {
		thing, label, path = link.Thing, link.Label, link.Path
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE warranties AS w SET wherehouse_thing = ?, wherehouse_label = ?, wherehouse_path = ?,
			wherehouse_unlinked = wherehouse_unlinked AND ? IS NULL, wherehouse_fields = '', updated_at = ?
		WHERE `+visibleTo+` AND w.id = ?`, thing, label, path, thing, now(), userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `UPDATE claims SET wherehouse_entry = NULL, wherehouse_seen = '' WHERE warranty_id = ?`, id)
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
	token, err := s.seal(token)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE user_settings SET wherehouse_token = ?, wherehouse_household = ?, wherehouse_since = '',
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
	secret, err := s.seal(secret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE user_settings SET wherehouse_hook_secret = ? WHERE user_id = ?`, secret, userID)
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
		if h.Secret, err = s.open(h.Secret); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SetWherehouseUnlinked marks a warranty someone unlinked from Wherehouse, so
// it isn't linked again by itself.
func (s *Store) SetWherehouseUnlinked(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE warranties SET wherehouse_unlinked = 1 WHERE id = ?`, id)
	return err
}

// WherehouseState is how a warranty stands with Wherehouse: the thing it's
// linked to, or the HomeBox item it's linked to instead.
type WherehouseState struct {
	ID        int64
	Thing     *int64
	HomeBoxID string
}

// WherehouseStates lists the confirmed warranties someone can see that are
// linked to Wherehouse or to HomeBox.
func (s *Store) WherehouseStates(ctx context.Context, userID int64) ([]WherehouseState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.wherehouse_thing, w.homebox_id FROM warranties w
		WHERE `+visibleTo+` AND NOT w.draft
			AND (w.wherehouse_thing IS NOT NULL OR (w.homebox_id != '' AND NOT w.wherehouse_unlinked)) ORDER BY w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WherehouseState
	for rows.Next() {
		var st WherehouseState
		if err := rows.Scan(&st.ID, &st.Thing, &st.HomeBoxID); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SetWherehouseArchived archives a warranty because its thing was sold or
// archived in Wherehouse, or brings back one archived that way. It reports
// whether anything changed: a warranty archived by hand stays as it is.
func (s *Store) SetWherehouseArchived(ctx context.Context, id int64, archived bool) (bool, error) {
	var res sql.Result
	var err error
	if archived {
		res, err = s.db.ExecContext(ctx, `UPDATE warranties SET archived_at = ?, wherehouse_archived = 1, updated_at = ?
			WHERE id = ? AND archived_at IS NULL AND NOT draft`, now(), now(), id)
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE warranties SET archived_at = NULL, wherehouse_archived = 0, updated_at = ?
			WHERE id = ? AND wherehouse_archived`, now(), id)
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// WherehousePhotosToSend lists a warranty's photos stored here that haven't
// been sent to thing yet.
func (s *Store) WherehousePhotosToSend(ctx context.Context, warrantyID, thing int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments a
		WHERE a.warranty_id = ? AND a.kind = 'photo' AND a.storage = ? AND a.wherehouse_thing IS NOT ?
		ORDER BY a.id`, warrantyID, StorageLocal, thing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// SentToWherehouse records that photos were sent to a Wherehouse thing, or
// that it has them already.
func (s *Store) SentToWherehouse(ctx context.Context, thing int64, ids ...int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `UPDATE attachments SET wherehouse_thing = ? WHERE id = ?`, thing, id); err != nil {
			return err
		}
	}
	return nil
}

// AttachmentWarranty returns the warranty an attachment belongs to.
func (s *Store) AttachmentWarranty(ctx context.Context, id int64) (int64, error) {
	var wid int64
	err := s.db.QueryRowContext(ctx, `SELECT warranty_id FROM attachments WHERE id = ?`, id).Scan(&wid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return wid, err
}

// StoredInWherehouse records that a photo is now kept only on a Wherehouse
// thing, as photo there; its file here, or its copy in HomeBox, can go.
func (s *Store) StoredInWherehouse(ctx context.Context, id, thing, photo int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET local_path = '',
		homebox_item = CASE storage WHEN ? THEN '' ELSE homebox_item END,
		homebox_attachment = CASE storage WHEN ? THEN '' ELSE homebox_attachment END,
		storage = ?, wherehouse_thing = ?, wherehouse_photo = ?, wherehouse_attachment = NULL WHERE id = ?`,
		StorageHomeBox, StorageHomeBox, StorageWherehouse, thing, photo, id)
	return err
}

// BackFromWherehouse records that a photo kept in Wherehouse is stored here
// again, as file name. sentTo is the thing that still has it, if any.
func (s *Store) BackFromWherehouse(ctx context.Context, id int64, name string, size int64, sentTo *int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET storage = ?, local_path = ?, size = ?, wherehouse_thing = ?,
		wherehouse_photo = NULL, wherehouse_attachment = NULL WHERE id = ?`, StorageLocal, name, size, sentTo, id)
	return err
}

// FileInUse reports whether any attachment still keeps its file as name.
func (s *Store) FileInUse(ctx context.Context, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM attachments WHERE storage = ? AND local_path = ?`, StorageLocal, name).Scan(&n)
	return n > 0, err
}

// WherehousePhotoWarranties lists the warranties the user can see that have
// photos kept in Wherehouse.
func (s *Store) WherehousePhotoWarranties(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT w.id FROM warranties w JOIN attachments a ON a.warranty_id = w.id
		WHERE `+visibleTo+` AND a.storage = ? ORDER BY w.id`, userID, StorageWherehouse)
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

// ForgetWherehousePhotos drops the warranty's photos and documents that
// were kept on a thing deleted in Wherehouse, as they went with it; the
// others no longer have a copy there.
func (s *Store) ForgetWherehousePhotos(ctx context.Context, warrantyID, thing int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE warranty_id = ? AND storage = ? AND wherehouse_thing = ?`,
		warrantyID, StorageWherehouse, thing); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET wherehouse_attachment = NULL
		WHERE warranty_id = ? AND wherehouse_thing = ? AND wherehouse_attachment IS NOT NULL`, warrantyID, thing)
	return err
}

// OnWherehouseThing records that a document is on a Wherehouse thing too,
// as attachment there.
// seen is its type and name there.
func (s *Store) OnWherehouseThing(ctx context.Context, id, thing, attachment int64, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET wherehouse_thing = ?, wherehouse_attachment = ?, wherehouse_seen = ?
		WHERE id = ?`, thing, attachment, seen, id)
	return err
}

// SeenOnWherehouse records a document's type and name on its thing.
func (s *Store) SeenOnWherehouse(ctx context.Context, id int64, seen string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET wherehouse_seen = ? WHERE id = ?`, seen, id)
	return err
}

// WherehouseFields is what a linked warranty and its thing last agreed on
// (JSON), or "" before that.
func (s *Store) WherehouseFields(ctx context.Context, id int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT wherehouse_fields FROM warranties WHERE id = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetWherehouseFields(ctx context.Context, id int64, v string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE warranties SET wherehouse_fields = ? WHERE id = ?`, v, id)
	return err
}

// ThingDetails are a warranty's details its Wherehouse thing has too.
type ThingDetails struct {
	ProductName, Brand, Model, Notes string
	SerialNumbers                    []string
	Quantity                         int
}

// SetThingDetails changes a warranty's details from what they were, was, to
// d, as they were changed on its thing. It reports false, changing nothing,
// if they aren't was any more: they were changed here meanwhile.
func (s *Store) SetThingDetails(ctx context.Context, id int64, was, d ThingDetails) (bool, error) {
	d.ProductName = strings.TrimSpace(d.ProductName)
	if d.ProductName == "" || d.Quantity < 1 || d.Quantity > MaxQuantity {
		return false, invalid("productName", nil)
	}
	same := true
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var cur ThingDetails
		if err := tx.QueryRowContext(ctx, `SELECT product_name, brand, model, notes, quantity FROM warranties WHERE id = ?`, id).
			Scan(&cur.ProductName, &cur.Brand, &cur.Model, &cur.Notes, &cur.Quantity); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT value FROM serial_numbers WHERE warranty_id = ? ORDER BY position`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sn string
			if err := rows.Scan(&sn); err != nil {
				rows.Close()
				return err
			}
			cur.SerialNumbers = append(cur.SerialNumbers, sn)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if same = cur.ProductName == was.ProductName && cur.Brand == was.Brand && cur.Model == was.Model &&
			cur.Notes == was.Notes && cur.Quantity == was.Quantity &&
			strings.Join(cur.SerialNumbers, "\x00") == strings.Join(was.SerialNumbers, "\x00"); !same {
			return nil
		}
		// Changed by someone, so a draft's details aren't filled in over it.
		if _, err := tx.ExecContext(ctx, `UPDATE warranties SET product_name = ?, brand = ?, model = ?, notes = ?, quantity = ?,
			updated_at = ?, edited = 1 WHERE id = ?`, d.ProductName, strings.TrimSpace(d.Brand), strings.TrimSpace(d.Model),
			strings.TrimRight(d.Notes, " \n\t"), d.Quantity, now(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM serial_numbers WHERE warranty_id = ?`, id); err != nil {
			return err
		}
		for i, sn := range d.SerialNumbers {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO serial_numbers (warranty_id, position, value) VALUES (?, ?, ?)`, id, i, sn); err != nil {
				return err
			}
		}
		return nil
	})
	return same && err == nil, err
}

// ClaimWherehouseSeen is a claim's maintenance entry as last seen (JSON).
func (s *Store) ClaimWherehouseSeen(ctx context.Context, claimID int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT wherehouse_seen FROM claims WHERE id = ?`, claimID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetClaimWherehouseSeen(ctx context.Context, claimID int64, v string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE claims SET wherehouse_seen = ? WHERE id = ?`, v, claimID)
	return err
}

// WherehouseShared reports whether another attachment than id is the same
// Wherehouse photo or document as it, e.g. a split-off warranty's copy not
// yet settled.
func (s *Store) WherehouseShared(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM attachments a JOIN attachments b ON b.id = ?
		WHERE a.id != b.id AND ((b.wherehouse_photo IS NOT NULL AND a.wherehouse_photo = b.wherehouse_photo)
			OR (b.wherehouse_attachment IS NOT NULL AND a.wherehouse_attachment = b.wherehouse_attachment))`, id).Scan(&n)
	return n > 0, err
}

// NotOnWherehouseThing forgets that a document is on a Wherehouse thing,
// e.g. as the warranty is no longer linked to it.
func (s *Store) NotOnWherehouseThing(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET wherehouse_attachment = NULL, wherehouse_seen = '' WHERE id = ?`, id)
	return err
}
