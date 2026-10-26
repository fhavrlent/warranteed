package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
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
	// AddedBy is who attached it; Paperless documents open with their connection.
	AddedBy   *int64 `json:"-"`
	Error     string `json:"error"`
	CreatedAt string `json:"createdAt"`
}

const (
	StorageLocal     = "local"
	StoragePaperless = "paperless"

	StatusReady      = "ready"
	StatusProcessing = "processing"
	StatusFailed     = "failed"
)

// AttachmentKinds are the document types; their names are translated as
// "kinds.<type>".
var AttachmentKinds = []string{"purchase", "terms", "card", "extended", "claim", "manual", "photo", "other"}

func ValidKind(kind string) bool { return slices.Contains(AttachmentKinds, kind) }

const attachmentCols = `a.id, a.warranty_id, a.kind, a.storage, a.filename, a.mime_type, a.size, a.local_path,
	a.paperless_doc_id, a.paperless_task_id, a.status, a.error, a.created_at, a.added_by`

func scanAttachment(row interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.WarrantyID, &a.Kind, &a.Storage, &a.Filename, &a.MimeType, &a.Size, &a.LocalPath,
		&a.PaperlessDocID, &a.PaperlessTaskID, &a.Status, &a.Error, &a.CreatedAt, &a.AddedBy)
	return &a, err
}

func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO attachments (warranty_id, kind, storage, filename, mime_type, size, local_path,
			paperless_doc_id, paperless_task_id, status, error, added_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, created_at`,
		a.WarrantyID, a.Kind, a.Storage, a.Filename, a.MimeType, a.Size, a.LocalPath,
		a.PaperlessDocID, a.PaperlessTaskID, a.Status, a.Error, a.AddedBy,
	).Scan(&a.ID, &a.CreatedAt)
}

// GetAttachment returns an attachment only if it belongs to the user.
func (s *Store) GetAttachment(ctx context.Context, userID, id int64) (*Attachment, error) {
	a, err := scanAttachment(s.db.QueryRowContext(ctx, `SELECT `+attachmentCols+` FROM attachments a
		JOIN warranties w ON w.id = a.warranty_id WHERE `+visibleTo+` AND a.id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) DeleteAttachment(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateAttachment(ctx context.Context, id int64, kind, filename string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET kind = ?, filename = ? WHERE id = ?`, kind, filename, id)
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
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, COALESCE(a.added_by, w.user_id), a.paperless_task_id, a.created_at FROM attachments a
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
