package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

const DateLayout = "2006-01-02"

type Warranty struct {
	ID              int64        `json:"id"`
	ProductName     string       `json:"productName"`
	Brand           string       `json:"brand"`
	Model           string       `json:"model"`
	Vendor          string       `json:"vendor"`
	OrderNumber     string       `json:"orderNumber"`
	PurchaseDate    string       `json:"purchaseDate"`
	WarrantyMonths  *int         `json:"warrantyMonths"`
	WarrantyEndDate *string      `json:"warrantyEndDate"`
	IsLifetime      bool         `json:"isLifetime"`
	ExpiresOn       *string      `json:"expiresOn"`
	Price           *float64     `json:"price"`
	Currency        string       `json:"currency"`
	ProductURL      string       `json:"productUrl"`
	Notes           string       `json:"notes"`
	ArchivedAt      *string      `json:"archivedAt"`
	CreatedAt       string       `json:"createdAt"`
	UpdatedAt       string       `json:"updatedAt"`
	SerialNumbers   []string     `json:"serialNumbers"`
	Tags            []Tag        `json:"tags"`
	Attachments     []Attachment `json:"attachments"`
}

// WarrantyInput is what clients send when creating or updating a warranty.
type WarrantyInput struct {
	ProductName     string   `json:"productName"`
	Brand           string   `json:"brand"`
	Model           string   `json:"model"`
	Vendor          string   `json:"vendor"`
	OrderNumber     string   `json:"orderNumber"`
	PurchaseDate    string   `json:"purchaseDate"`
	WarrantyMonths  *int     `json:"warrantyMonths"`
	WarrantyEndDate *string  `json:"warrantyEndDate"`
	IsLifetime      bool     `json:"isLifetime"`
	Price           *float64 `json:"price"`
	Currency        string   `json:"currency"`
	ProductURL      string   `json:"productUrl"`
	Notes           string   `json:"notes"`
	SerialNumbers   []string `json:"serialNumbers"`
	Tags            []string `json:"tags"`
}

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Normalize trims input, validates it and returns the computed expiry date
// (nil for lifetime warranties).
func (in *WarrantyInput) Normalize() (*string, error) {
	for _, p := range []*string{&in.ProductName, &in.Brand, &in.Model, &in.Vendor, &in.OrderNumber,
		&in.PurchaseDate, &in.Currency, &in.ProductURL} {
		*p = strings.TrimSpace(*p)
	}
	in.Notes = strings.TrimRight(in.Notes, " \n\t")
	in.Currency = strings.ToUpper(in.Currency)

	if in.ProductName == "" {
		return nil, invalid("Product name is required")
	}
	purchase, err := time.Parse(DateLayout, in.PurchaseDate)
	if err != nil {
		return nil, invalid("Purchase date must be a valid date")
	}
	if in.WarrantyEndDate != nil && strings.TrimSpace(*in.WarrantyEndDate) == "" {
		in.WarrantyEndDate = nil
	}
	if in.Price != nil && *in.Price < 0 {
		return nil, invalid("Price cannot be negative")
	}
	if in.ProductURL != "" && !strings.HasPrefix(in.ProductURL, "http://") && !strings.HasPrefix(in.ProductURL, "https://") {
		in.ProductURL = "https://" + in.ProductURL
	}

	serials := in.SerialNumbers[:0]
	for _, sn := range in.SerialNumbers {
		if sn = strings.TrimSpace(sn); sn != "" {
			serials = append(serials, sn)
		}
	}
	in.SerialNumbers = serials

	if in.IsLifetime {
		in.WarrantyMonths, in.WarrantyEndDate = nil, nil
		return nil, nil
	}
	if in.WarrantyEndDate != nil {
		end, err := time.Parse(DateLayout, *in.WarrantyEndDate)
		if err != nil {
			return nil, invalid("Warranty end date must be a valid date")
		}
		if end.Before(purchase) {
			return nil, invalid("Warranty cannot end before the purchase date")
		}
		in.WarrantyMonths = nil
		return in.WarrantyEndDate, nil
	}
	if in.WarrantyMonths == nil || *in.WarrantyMonths <= 0 {
		return nil, invalid("Set a warranty length, an end date, or mark it as lifetime")
	}
	if *in.WarrantyMonths > 1200 {
		return nil, invalid("Warranty length is unreasonably long")
	}
	end := AddMonths(purchase, *in.WarrantyMonths).Format(DateLayout)
	return &end, nil
}

// AddMonths adds months, clamping to the last day of the target month
// (31 Jan + 1 month = 28/29 Feb, not 3 Mar).
func AddMonths(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

const warrantyCols = `id, product_name, brand, model, vendor, order_number, purchase_date, warranty_months,
	warranty_end_date, is_lifetime, expires_on, price, currency, product_url, notes, archived_at, created_at, updated_at`

func scanWarranty(row interface{ Scan(...any) error }) (*Warranty, error) {
	var w Warranty
	err := row.Scan(&w.ID, &w.ProductName, &w.Brand, &w.Model, &w.Vendor, &w.OrderNumber, &w.PurchaseDate,
		&w.WarrantyMonths, &w.WarrantyEndDate, &w.IsLifetime, &w.ExpiresOn, &w.Price, &w.Currency,
		&w.ProductURL, &w.Notes, &w.ArchivedAt, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	w.SerialNumbers, w.Tags, w.Attachments = []string{}, []Tag{}, []Attachment{}
	return &w, nil
}

// ListWarranties returns all of a user's warranties with serials, tags and
// attachments. Personal collections are small, so we return everything and
// let the client filter and sort instantly.
func (s *Store) ListWarranties(ctx context.Context, userID int64) ([]*Warranty, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+warrantyCols+` FROM warranties WHERE user_id = ? ORDER BY expires_on IS NULL, expires_on, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []*Warranty{}
	byID := map[int64]*Warranty{}
	for rows.Next() {
		w, err := scanWarranty(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, w)
		byID[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.loadChildren(ctx, userID, byID); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) GetWarranty(ctx context.Context, userID, id int64) (*Warranty, error) {
	w, err := scanWarranty(s.db.QueryRowContext(ctx,
		`SELECT `+warrantyCols+` FROM warranties WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadChildren(ctx, userID, map[int64]*Warranty{w.ID: w}); err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Store) loadChildren(ctx context.Context, userID int64, byID map[int64]*Warranty) error {
	if len(byID) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT sn.warranty_id, sn.value FROM serial_numbers sn
		JOIN warranties w ON w.id = sn.warranty_id
		WHERE w.user_id = ? ORDER BY sn.warranty_id, sn.position`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var wid int64
		var v string
		if err := rows.Scan(&wid, &v); err != nil {
			rows.Close()
			return err
		}
		if w := byID[wid]; w != nil {
			w.SerialNumbers = append(w.SerialNumbers, v)
		}
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `
		SELECT wt.warranty_id, t.id, t.name, t.color FROM warranty_tags wt
		JOIN tags t ON t.id = wt.tag_id
		WHERE t.user_id = ? ORDER BY t.name`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var wid int64
		var t Tag
		if err := rows.Scan(&wid, &t.ID, &t.Name, &t.Color); err != nil {
			rows.Close()
			return err
		}
		if w := byID[wid]; w != nil {
			w.Tags = append(w.Tags, t)
		}
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments a
		JOIN warranties w ON w.id = a.warranty_id WHERE w.user_id = ? ORDER BY a.id`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return err
		}
		if w := byID[a.WarrantyID]; w != nil {
			w.Attachments = append(w.Attachments, *a)
		}
	}
	return rows.Err()
}

func (s *Store) CreateWarranty(ctx context.Context, userID int64, in *WarrantyInput) (int64, error) {
	expires, err := in.Normalize()
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO warranties (user_id, product_name, brand, model, vendor, order_number, purchase_date,
				warranty_months, warranty_end_date, is_lifetime, expires_on, price, currency, product_url, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			userID, in.ProductName, in.Brand, in.Model, in.Vendor, in.OrderNumber, in.PurchaseDate,
			in.WarrantyMonths, in.WarrantyEndDate, in.IsLifetime, expires, in.Price, in.Currency,
			in.ProductURL, in.Notes,
		).Scan(&id)
		if err != nil {
			return err
		}
		return replaceChildren(ctx, tx, userID, id, in)
	})
	return id, err
}

func (s *Store) UpdateWarranty(ctx context.Context, userID, id int64, in *WarrantyInput) error {
	expires, err := in.Normalize()
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE warranties SET product_name = ?, brand = ?, model = ?, vendor = ?, order_number = ?,
				purchase_date = ?, warranty_months = ?, warranty_end_date = ?, is_lifetime = ?, expires_on = ?,
				price = ?, currency = ?, product_url = ?, notes = ?, updated_at = ?
			WHERE user_id = ? AND id = ?`,
			in.ProductName, in.Brand, in.Model, in.Vendor, in.OrderNumber, in.PurchaseDate,
			in.WarrantyMonths, in.WarrantyEndDate, in.IsLifetime, expires, in.Price, in.Currency,
			in.ProductURL, in.Notes, now(), userID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return replaceChildren(ctx, tx, userID, id, in)
	})
}

func replaceChildren(ctx context.Context, tx *sql.Tx, userID, id int64, in *WarrantyInput) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM serial_numbers WHERE warranty_id = ?`, id); err != nil {
		return err
	}
	for i, sn := range in.SerialNumbers {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO serial_numbers (warranty_id, position, value) VALUES (?, ?, ?)`, id, i, sn); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM warranty_tags WHERE warranty_id = ?`, id); err != nil {
		return err
	}
	for _, name := range in.Tags {
		tagID, err := ensureTag(ctx, tx, userID, name)
		if err != nil {
			return err
		}
		if tagID == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO warranty_tags (warranty_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, tagID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = now()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE warranties SET archived_at = ?, updated_at = ? WHERE user_id = ? AND id = ?`, at, now(), userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWarranty deletes a warranty and returns the local files that belonged
// to it so the caller can remove them from disk.
func (s *Store) DeleteWarranty(ctx context.Context, userID, id int64) ([]string, error) {
	var paths []string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT local_path FROM attachments a JOIN warranties w ON w.id = a.warranty_id
			WHERE w.user_id = ? AND w.id = ? AND a.storage = 'local'`, userID, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				rows.Close()
				return err
			}
			paths = append(paths, p)
		}
		rows.Close()
		res, err := tx.ExecContext(ctx, `DELETE FROM warranties WHERE user_id = ? AND id = ?`, userID, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
	return paths, err
}

// ---- tags ----

type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count,omitempty"`
}

// TagColors is the palette new tags pick from (by name hash) so a tag keeps
// the same colour everywhere.
var TagColors = []string{"slate", "red", "orange", "amber", "lime", "emerald", "teal", "sky", "indigo", "violet", "pink", "rose"}

func colorFor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return TagColors[int(h.Sum32())%len(TagColors)]
}

func ensureTag(ctx context.Context, tx *sql.Tx, userID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	if len(name) > 40 {
		return 0, invalid("Tag %q is too long", name)
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO tags (user_id, name, color) VALUES (?, ?, ?)
		ON CONFLICT (user_id, name) DO UPDATE SET name = tags.name
		RETURNING id`, userID, name, colorFor(name)).Scan(&id)
	return id, err
}

func (s *Store) ListTags(ctx context.Context, userID int64) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.color, COUNT(wt.warranty_id) FROM tags t
		LEFT JOIN warranty_tags wt ON wt.tag_id = t.id
		WHERE t.user_id = ? GROUP BY t.id ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Count); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (s *Store) UpdateTag(ctx context.Context, userID, id int64, name, color string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		return invalid("Tag name must be 1–40 characters")
	}
	valid := false
	for _, c := range TagColors {
		valid = valid || c == color
	}
	if !valid {
		return invalid("Unknown tag colour")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tags SET name = ?, color = ? WHERE user_id = ? AND id = ?`, name, color, userID, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return invalid("A tag with that name already exists")
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTag(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE user_id = ? AND id = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- reminders ----

type Expiring struct {
	Warranty  *Warranty
	DaysLeft  int
	Threshold int
}

// ActiveWithExpiry returns the user's non-archived, non-lifetime warranties.
func (s *Store) ActiveWithExpiry(ctx context.Context, userID int64) ([]*Warranty, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+warrantyCols+` FROM warranties
		WHERE user_id = ? AND archived_at IS NULL AND expires_on IS NOT NULL ORDER BY expires_on`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Warranty
	for rows.Next() {
		w, err := scanWarranty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ReminderSent(ctx context.Context, warrantyID int64, threshold int, expiresOn string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders_sent
		WHERE warranty_id = ? AND threshold_days = ? AND expires_on = ?`, warrantyID, threshold, expiresOn).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkRemindersSent(ctx context.Context, warrantyID int64, thresholds []int, expiresOn string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, t := range thresholds {
			if _, err := tx.ExecContext(ctx, `INSERT INTO reminders_sent (warranty_id, threshold_days, expires_on)
				VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, warrantyID, t, expiresOn); err != nil {
				return err
			}
		}
		return nil
	})
}
