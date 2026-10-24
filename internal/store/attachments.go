package store

import (
	"context"
	"database/sql"
	"errors"
)

type Attachment struct {
	ID              int64  `json:"id"`
	WarrantyID      int64  `json:"warrantyId"`
	Kind            string `json:"kind"`
	Storage         string `json:"storage"`
	Filename        string `json:"filename"`
	MimeType        string `json:"mimeType"`
	Size            int64  `json:"size"`
	LocalPath       string `json:"-"`
	PaperlessDocID  *int64 `json:"paperlessDocId"`
	PaperlessTaskID string `json:"-"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	CreatedAt       string `json:"createdAt"`
}

const (
	StorageLocal     = "local"
	StoragePaperless = "paperless"

	StatusReady      = "ready"
	StatusProcessing = "processing"
	StatusFailed     = "failed"
)

// AttachmentKinds maps each document type to its display name, which is
// also used in Paperless document titles.
var AttachmentKinds = map[string]string{
	"purchase": "Proof of purchase",
	"terms":    "Warranty terms",
	"card":     "Warranty card",
	"extended": "Extended warranty",
	"claim":    "Repair & claims",
	"manual":   "Manual",
	"photo":    "Product photo",
	"other":    "Other",
}

func ValidKind(kind string) bool {
	_, ok := AttachmentKinds[kind]
	return ok
}

const attachmentCols = `a.id, a.warranty_id, a.kind, a.storage, a.filename, a.mime_type, a.size, a.local_path,
	a.paperless_doc_id, a.paperless_task_id, a.status, a.error, a.created_at`

func scanAttachment(row interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.WarrantyID, &a.Kind, &a.Storage, &a.Filename, &a.MimeType, &a.Size, &a.LocalPath,
		&a.PaperlessDocID, &a.PaperlessTaskID, &a.Status, &a.Error, &a.CreatedAt)
	return &a, err
}

func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO attachments (warranty_id, kind, storage, filename, mime_type, size, local_path,
			paperless_doc_id, paperless_task_id, status, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, created_at`,
		a.WarrantyID, a.Kind, a.Storage, a.Filename, a.MimeType, a.Size, a.LocalPath,
		a.PaperlessDocID, a.PaperlessTaskID, a.Status, a.Error,
	).Scan(&a.ID, &a.CreatedAt)
}

// GetAttachment returns an attachment only if it belongs to the user.
func (s *Store) GetAttachment(ctx context.Context, userID, id int64) (*Attachment, error) {
	a, err := scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attachmentCols+` FROM attachments a
		JOIN warranties w ON w.id = a.warranty_id WHERE w.user_id = ? AND a.id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) DeleteAttachment(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateAttachmentKind(ctx context.Context, id int64, kind string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET kind = ? WHERE id = ?`, kind, id)
	return err
}

func (s *Store) CompleteAttachment(ctx context.Context, id, docID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE attachments SET status = 'ready', error = '', paperless_doc_id = ? WHERE id = ?`, docID, id)
	return err
}

func (s *Store) FailAttachment(ctx context.Context, id int64, msg string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET status = 'failed', error = ? WHERE id = ?`, msg, id)
	return err
}

// PendingPaperless is a Paperless upload still waiting for its consumption task.
type PendingPaperless struct {
	AttachmentID int64
	UserID       int64
	TaskID       string
	CreatedAt    string
}

func (s *Store) PendingPaperless(ctx context.Context) ([]PendingPaperless, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, w.user_id, a.paperless_task_id, a.created_at FROM attachments a
		JOIN warranties w ON w.id = a.warranty_id WHERE a.status = 'processing'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingPaperless
	for rows.Next() {
		var p PendingPaperless
		if err := rows.Scan(&p.AttachmentID, &p.UserID, &p.TaskID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
