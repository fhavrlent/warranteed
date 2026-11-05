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
	AddedBy *int64 `json:"-"`
	// HomeBoxItem is the HomeBox item a photo was sent to, or that holds it
	// when it is stored in HomeBox; HomeBoxAttachment is the photo there.
	HomeBoxItem       string `json:"-"`
	HomeBoxAttachment string `json:"-"`
	// WherehouseThing is the Wherehouse thing a photo was sent to, or that
	// holds it when it is stored in Wherehouse; WherehousePhoto is the
	// photo there.
	WherehouseThing *int64 `json:"-"`
	WherehousePhoto *int64 `json:"-"`
	// ClaimID is the claim the document belongs to, if any.
	ClaimID   *int64 `json:"claimId"`
	Error     string `json:"error"`
	CreatedAt string `json:"createdAt"`
}

const (
	StorageLocal     = "local"
	StoragePaperless = "paperless"
	StorageHomeBox   = "homebox"
	// StorageWherehouse is a photo kept only on a Wherehouse thing.
	StorageWherehouse = "wherehouse"

	StatusReady      = "ready"
	StatusProcessing = "processing"
	StatusFailed     = "failed"
)

// AttachmentKinds are the document types; their names are translated as
// "kinds.<type>".
var AttachmentKinds = []string{"purchase", "terms", "card", "extended", "claim", "manual", "photo", "other"}

func ValidKind(kind string) bool { return slices.Contains(AttachmentKinds, kind) }

const attachmentCols = `a.id, a.warranty_id, a.kind, a.storage, a.filename, a.mime_type, a.size, a.local_path,
	a.paperless_doc_id, a.paperless_task_id, a.status, a.error, a.created_at, a.added_by, a.homebox_item, a.homebox_attachment, a.claim_id,
	a.wherehouse_thing, a.wherehouse_photo`

func scanAttachment(row interface{ Scan(...any) error }) (*Attachment, error) {
	var a Attachment
	err := row.Scan(&a.ID, &a.WarrantyID, &a.Kind, &a.Storage, &a.Filename, &a.MimeType, &a.Size, &a.LocalPath,
		&a.PaperlessDocID, &a.PaperlessTaskID, &a.Status, &a.Error, &a.CreatedAt, &a.AddedBy, &a.HomeBoxItem, &a.HomeBoxAttachment, &a.ClaimID,
		&a.WherehouseThing, &a.WherehousePhoto)
	return &a, err
}

func (s *Store) CreateAttachment(ctx context.Context, a *Attachment) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO attachments (warranty_id, kind, storage, filename, mime_type, size, local_path,
			paperless_doc_id, paperless_task_id, status, error, added_by, homebox_item, homebox_attachment, claim_id,
			wherehouse_thing, wherehouse_photo)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id, created_at`,
		a.WarrantyID, a.Kind, a.Storage, a.Filename, a.MimeType, a.Size, a.LocalPath,
		a.PaperlessDocID, a.PaperlessTaskID, a.Status, a.Error, a.AddedBy, a.HomeBoxItem, a.HomeBoxAttachment, a.ClaimID,
		a.WherehouseThing, a.WherehousePhoto,
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

// SentToHomeBox records that a photo was sent to a HomeBox item.
func (s *Store) SentToHomeBox(ctx context.Context, id int64, itemID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET homebox_item = ? WHERE id = ?`, itemID, id)
	return err
}

// MovedInHomeBox records that a photo stored in HomeBox is now on another item.
func (s *Store) MovedInHomeBox(ctx context.Context, id int64, itemID, attachmentID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET homebox_item = ?, homebox_attachment = ? WHERE id = ?`,
		itemID, attachmentID, id)
	return err
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

// docOwner is whose Paperless connection an attachment (alias a, on warranty
// w) was stored through.
const docOwner = `COALESCE(a.added_by, w.user_id)`

// PaperlessDoc is a document in one user's Paperless.
type PaperlessDoc struct {
	Owner int64
	DocID int64
}

func (s *Store) paperlessDocs(ctx context.Context, where string, arg int64) ([]PaperlessDoc, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT `+docOwner+`, a.paperless_doc_id
		FROM attachments a JOIN warranties w ON w.id = a.warranty_id
		WHERE a.storage = 'paperless' AND a.paperless_doc_id IS NOT NULL AND `+where, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaperlessDoc
	for rows.Next() {
		var d PaperlessDoc
		if err := rows.Scan(&d.Owner, &d.DocID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// WarrantyPaperlessDocs lists the Paperless documents attached to a warranty.
func (s *Store) WarrantyPaperlessDocs(ctx context.Context, warrantyID int64) ([]PaperlessDoc, error) {
	return s.paperlessDocs(ctx, `w.id = ?`, warrantyID)
}

// OwnerPaperlessDocs lists the documents attached through a user's Paperless.
func (s *Store) OwnerPaperlessDocs(ctx context.Context, owner int64) ([]PaperlessDoc, error) {
	return s.paperlessDocs(ctx, docOwner+` = ?`, owner)
}

// AttachmentPaperlessDoc returns the Paperless document an attachment holds,
// if any.
func (s *Store) AttachmentPaperlessDoc(ctx context.Context, attachmentID int64) (*PaperlessDoc, error) {
	docs, err := s.paperlessDocs(ctx, `a.id = ?`, attachmentID)
	if err != nil || len(docs) == 0 {
		return nil, err
	}
	return &docs[0], nil
}

// DocumentRef is one attachment of a Paperless document: whose Paperless it
// came through and which warranty holds it.
type DocumentRef struct {
	Owner      int64
	WarrantyID int64
	HomeBoxID  string
}

// DocumentRefs lists every attachment of Paperless document docID, oldest
// warranty first. Document IDs are per Paperless instance, so callers keep
// only the owners whose Paperless is the one they mean.
func (s *Store) DocumentRefs(ctx context.Context, docID int64) ([]DocumentRef, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT `+docOwner+`, w.id, w.homebox_id
		FROM attachments a JOIN warranties w ON w.id = a.warranty_id
		WHERE a.storage = 'paperless' AND a.paperless_doc_id = ?
		ORDER BY w.id`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DocumentRef
	for rows.Next() {
		var d DocumentRef
		if err := rows.Scan(&d.Owner, &d.WarrantyID, &d.HomeBoxID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
