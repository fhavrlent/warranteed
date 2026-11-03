package store

import (
	"context"
	"database/sql"
	"slices"

	"github.com/fhavrlent/warranteed/internal/i18n"
)

// SplitInput says what goes to the new warranty when splitting one.
type SplitInput struct {
	// Pieces is how many pieces move to the new warranty.
	Pieces int `json:"pieces"`
	// SerialNumbers are the serial numbers that go with them.
	SerialNumbers []string `json:"serialNumbers"`
}

// SplitWarranty moves pieces of a warranty to a new one with the same
// purchase, coverage, tags and documents, and returns its ID. Claims stay
// with the original. localCopies gives, for each document stored here, a
// copy of its file for the new warranty, so each can be deleted on its own.
func (s *Store) SplitWarranty(ctx context.Context, userID, id int64, in *SplitInput, localCopies map[int64]string) (int64, error) {
	var newID int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := warrantyAccess(ctx, tx, userID, id); err != nil {
			return err
		}
		var quantity int
		var draft bool
		if err := tx.QueryRowContext(ctx, `SELECT quantity, draft FROM warranties WHERE id = ?`, id).Scan(&quantity, &draft); err != nil {
			return err
		}
		if draft {
			return invalid("splitDraft", nil)
		}
		if in.Pieces < 1 || in.Pieces >= quantity {
			return invalid("splitPieces", i18n.Args{"max": quantity - 1})
		}

		err := tx.QueryRowContext(ctx, `
			INSERT INTO warranties (user_id, space_id, product_name, brand, model, vendor, order_number, purchase_date,
				warranty_months, warranty_end_date, is_lifetime, expires_on, price, currency, product_url, notes,
				archived_at, date_sold, date_received, voided_at, void_reason, quantity, edited)
			SELECT user_id, space_id, product_name, brand, model, vendor, order_number, purchase_date,
				warranty_months, warranty_end_date, is_lifetime, expires_on, price, currency, product_url, notes,
				archived_at, date_sold, date_received, voided_at, void_reason, ?, 1
			FROM warranties WHERE id = ? RETURNING id`, in.Pieces, id).Scan(&newID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE warranties SET quantity = quantity - ?, updated_at = ? WHERE id = ?`,
			in.Pieces, now(), id); err != nil {
			return err
		}
		if err := splitSerials(ctx, tx, id, newID, in.SerialNumbers); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO warranty_tags (warranty_id, tag_id)
			SELECT ?, tag_id FROM warranty_tags WHERE warranty_id = ?`, newID, id); err != nil {
			return err
		}
		// Reminders already sent count for both, so none come twice.
		if _, err := tx.ExecContext(ctx, `INSERT INTO reminders_sent (user_id, warranty_id, threshold_days, expires_on, sent_at)
			SELECT user_id, ?, threshold_days, expires_on, sent_at FROM reminders_sent WHERE warranty_id = ?`, newID, id); err != nil {
			return err
		}
		return copyAttachments(ctx, tx, id, newID, localCopies)
	})
	return newID, err
}

// splitSerials moves the chosen serial numbers from one warranty to another.
func splitSerials(ctx context.Context, tx *sql.Tx, from, to int64, moving []string) error {
	rows, err := tx.QueryContext(ctx, `SELECT value FROM serial_numbers WHERE warranty_id = ? ORDER BY position`, from)
	if err != nil {
		return err
	}
	var all []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	rows.Close()
	var keep, move []string
	for _, v := range all {
		if slices.Contains(moving, v) {
			move = append(move, v)
		} else {
			keep = append(keep, v)
		}
	}
	if len(move) != len(moving) {
		return invalid("splitSerial", nil)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM serial_numbers WHERE warranty_id = ?`, from); err != nil {
		return err
	}
	for _, list := range []struct {
		id     int64
		values []string
	}{{from, keep}, {to, move}} {
		for i, v := range list.values {
			if _, err := tx.ExecContext(ctx, `INSERT INTO serial_numbers (warranty_id, position, value) VALUES (?, ?, ?)`,
				list.id, i, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyAttachments gives the new warranty the documents and photos of the
// old one, outside any claim. Photos stored here haven't been sent to the new
// warranty's HomeBox item yet.
func copyAttachments(ctx context.Context, tx *sql.Tx, from, to int64, localCopies map[int64]string) error {
	rows, err := tx.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments a WHERE a.warranty_id = ? ORDER BY a.id`, from)
	if err != nil {
		return err
	}
	var list []*Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, a)
	}
	rows.Close()
	for _, a := range list {
		if a.Storage == StorageLocal {
			path, ok := localCopies[a.ID]
			if !ok {
				return invalid("splitFiles", nil)
			}
			a.LocalPath, a.HomeBoxItem = path, ""
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO attachments (warranty_id, kind, storage, filename, mime_type, size, local_path,
				paperless_doc_id, paperless_task_id, status, error, added_by, homebox_item, homebox_attachment, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			to, a.Kind, a.Storage, a.Filename, a.MimeType, a.Size, a.LocalPath,
			a.PaperlessDocID, a.PaperlessTaskID, a.Status, a.Error, a.AddedBy, a.HomeBoxItem, a.HomeBoxAttachment, a.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}
