package store

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/i18n"
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
	SpaceID         int64        `json:"spaceId"`
	SpaceKind       string       `json:"spaceKind"`
	AddedBy         UserRef      `json:"addedBy"`
}

// UserRef names a user without exposing their details.
type UserRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
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
	// SpaceID chooses who can see it; nil means the household when creating
	// and "unchanged" when updating.
	SpaceID *int64 `json:"spaceId"`
}

// ValidationError is input the user can fix; its message is translatable.
type ValidationError = i18n.Error

// invalid returns the ValidationError for the "server.<key>" message.
func invalid(key string, args i18n.Args) error {
	return i18n.NewError("server."+key, args)
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
		return nil, invalid("productName", nil)
	}
	purchase, err := time.Parse(DateLayout, in.PurchaseDate)
	if err != nil {
		return nil, invalid("purchaseDate", nil)
	}
	if in.WarrantyEndDate != nil && strings.TrimSpace(*in.WarrantyEndDate) == "" {
		in.WarrantyEndDate = nil
	}
	if in.Price != nil && *in.Price < 0 {
		return nil, invalid("negativePrice", nil)
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
			return nil, invalid("endDate", nil)
		}
		if end.Before(purchase) {
			return nil, invalid("endBeforePurchase", nil)
		}
		in.WarrantyMonths = nil
		return in.WarrantyEndDate, nil
	}
	if in.WarrantyMonths == nil || *in.WarrantyMonths <= 0 {
		return nil, invalid("noWarranty", nil)
	}
	if *in.WarrantyMonths > 1200 {
		return nil, invalid("tooLong", nil)
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

// visibleTo limits warranties (alias w) to spaces the user is a member of.
// Every query that touches warranties, or rows hanging off them, uses it.
const visibleTo = `w.space_id IN (SELECT space_id FROM space_members WHERE user_id = ?)`

const warrantyCols = `w.id, w.product_name, w.brand, w.model, w.vendor, w.order_number, w.purchase_date,
	w.warranty_months, w.warranty_end_date, w.is_lifetime, w.expires_on, w.price, w.currency, w.product_url,
	w.notes, w.archived_at, w.created_at, w.updated_at, w.space_id, s.kind, w.user_id, COALESCE(u.name, '')`

const warrantyFrom = `warranties w JOIN spaces s ON s.id = w.space_id LEFT JOIN users u ON u.id = w.user_id`

func scanWarranty(row interface{ Scan(...any) error }) (*Warranty, error) {
	var w Warranty
	err := row.Scan(&w.ID, &w.ProductName, &w.Brand, &w.Model, &w.Vendor, &w.OrderNumber, &w.PurchaseDate,
		&w.WarrantyMonths, &w.WarrantyEndDate, &w.IsLifetime, &w.ExpiresOn, &w.Price, &w.Currency,
		&w.ProductURL, &w.Notes, &w.ArchivedAt, &w.CreatedAt, &w.UpdatedAt, &w.SpaceID, &w.SpaceKind,
		&w.AddedBy.ID, &w.AddedBy.Name)
	if err != nil {
		return nil, err
	}
	w.SerialNumbers, w.Tags, w.Attachments = []string{}, []Tag{}, []Attachment{}
	return &w, nil
}

// ListWarranties returns every warranty the user can see, with serials, tags
// and attachments. Household collections are small, so we return everything
// and let the client filter and sort instantly.
func (s *Store) ListWarranties(ctx context.Context, userID int64) ([]*Warranty, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+warrantyCols+` FROM `+warrantyFrom+`
		WHERE `+visibleTo+` ORDER BY w.expires_on IS NULL, w.expires_on, w.id`, userID)
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
		`SELECT `+warrantyCols+` FROM `+warrantyFrom+` WHERE `+visibleTo+` AND w.id = ?`, userID, id))
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
		WHERE `+visibleTo+` ORDER BY sn.warranty_id, sn.position`, userID)
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
		SELECT wt.warranty_id, t.id, t.name, t.color, t.space_id FROM warranty_tags wt
		JOIN tags t ON t.id = wt.tag_id
		JOIN warranties w ON w.id = wt.warranty_id
		WHERE `+visibleTo+` ORDER BY t.name`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var wid int64
		var t Tag
		if err := rows.Scan(&wid, &t.ID, &t.Name, &t.Color, &t.SpaceID); err != nil {
			rows.Close()
			return err
		}
		if w := byID[wid]; w != nil {
			w.Tags = append(w.Tags, t)
		}
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments a
		JOIN warranties w ON w.id = a.warranty_id WHERE `+visibleTo+` ORDER BY a.id`, userID)
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
		spaceID, _, err := targetSpace(ctx, tx, userID, in.SpaceID)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `
			INSERT INTO warranties (user_id, space_id, product_name, brand, model, vendor, order_number, purchase_date,
				warranty_months, warranty_end_date, is_lifetime, expires_on, price, currency, product_url, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			userID, spaceID, in.ProductName, in.Brand, in.Model, in.Vendor, in.OrderNumber, in.PurchaseDate,
			in.WarrantyMonths, in.WarrantyEndDate, in.IsLifetime, expires, in.Price, in.Currency,
			in.ProductURL, in.Notes,
		).Scan(&id)
		if err != nil {
			return err
		}
		return replaceChildren(ctx, tx, spaceID, id, in)
	})
	return id, err
}

func (s *Store) UpdateWarranty(ctx context.Context, userID, id int64, in *WarrantyInput) error {
	expires, err := in.Normalize()
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := warrantyAccess(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		spaceID := cur.spaceID
		if in.SpaceID != nil && *in.SpaceID != cur.spaceID {
			var kind string
			spaceID, kind, err = targetSpace(ctx, tx, userID, in.SpaceID)
			if err != nil {
				return err
			}
			if cur.kind != SpacePersonal && kind == SpacePersonal && cur.addedBy != userID {
				return invalid("onlyOwnerPrivate", i18n.Args{"name": cur.addedByName})
			}
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE warranties SET space_id = ?, product_name = ?, brand = ?, model = ?, vendor = ?, order_number = ?,
				purchase_date = ?, warranty_months = ?, warranty_end_date = ?, is_lifetime = ?, expires_on = ?,
				price = ?, currency = ?, product_url = ?, notes = ?, updated_at = ?
			WHERE id = ?`,
			spaceID, in.ProductName, in.Brand, in.Model, in.Vendor, in.OrderNumber, in.PurchaseDate,
			in.WarrantyMonths, in.WarrantyEndDate, in.IsLifetime, expires, in.Price, in.Currency,
			in.ProductURL, in.Notes, now(), id)
		if err != nil {
			return err
		}
		if err := replaceChildren(ctx, tx, spaceID, id, in); err != nil {
			return err
		}
		if spaceID != cur.spaceID {
			return pruneTags(ctx, tx, cur.spaceID)
		}
		return nil
	})
}

// access describes a warranty for permission checks.
type access struct {
	spaceID     int64
	kind        string
	addedBy     int64
	addedByName string
}

// warrantyAccess loads a warranty the user can see, or ErrNotFound.
func warrantyAccess(ctx context.Context, tx *sql.Tx, userID, id int64) (*access, error) {
	var a access
	err := tx.QueryRowContext(ctx, `SELECT w.space_id, s.kind, w.user_id, COALESCE(u.name, '')
		FROM `+warrantyFrom+` WHERE `+visibleTo+` AND w.id = ?`, userID, id,
	).Scan(&a.spaceID, &a.kind, &a.addedBy, &a.addedByName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// replaceChildren rewrites serials and tags; tags are looked up (or created)
// by name in the warranty's space.
func replaceChildren(ctx context.Context, tx *sql.Tx, spaceID, id int64, in *WarrantyInput) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM serial_numbers WHERE warranty_id = ?`, id); err != nil {
		return err
	}
	for i, sn := range in.SerialNumbers {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO serial_numbers (warranty_id, position, value) VALUES (?, ?, ?)`, id, i, sn); err != nil {
			return err
		}
	}
	return setTags(ctx, tx, spaceID, id, in.Tags)
}

func setTags(ctx context.Context, tx *sql.Tx, spaceID, warrantyID int64, names []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM warranty_tags WHERE warranty_id = ?`, warrantyID); err != nil {
		return err
	}
	for _, name := range names {
		tagID, err := ensureTag(ctx, tx, spaceID, name)
		if err != nil {
			return err
		}
		if tagID == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO warranty_tags (warranty_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, warrantyID, tagID); err != nil {
			return err
		}
	}
	return nil
}

// pruneTags removes a space's tags that no warranty uses any more, after
// warranties moved out of it.
func pruneTags(ctx context.Context, tx *sql.Tx, spaceID int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE space_id = ?
		AND id NOT IN (SELECT tag_id FROM warranty_tags)`, spaceID)
	return err
}

func (s *Store) SetArchived(ctx context.Context, userID, id int64, archived bool) error {
	var at any
	if archived {
		at = now()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE warranties AS w SET archived_at = ?, updated_at = ? WHERE `+visibleTo+` AND w.id = ?`,
		at, now(), userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWarranty deletes a warranty and returns the local files that belonged
// to it so the caller can remove them from disk. In a shared space only the
// person who added it may delete it.
func (s *Store) DeleteWarranty(ctx context.Context, userID, id int64) ([]string, error) {
	var paths []string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		cur, err := warrantyAccess(ctx, tx, userID, id)
		if err != nil {
			return err
		}
		if cur.kind != SpacePersonal && cur.addedBy != userID {
			return invalid("onlyOwnerDelete", i18n.Args{"name": cur.addedByName})
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT local_path FROM attachments WHERE warranty_id = ? AND storage = 'local'`, id)
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
		if _, err := tx.ExecContext(ctx, `DELETE FROM warranties WHERE id = ?`, id); err != nil {
			return err
		}
		return nil
	})
	return paths, err
}

// MoveAllToSpace moves every warranty in the user's personal space to another
// space they belong to, re-linking tags by name. Returns how many moved.
func (s *Store) MoveAllToSpace(ctx context.Context, userID, spaceID int64) (int, error) {
	moved := 0
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		target, kind, err := targetSpace(ctx, tx, userID, &spaceID)
		if err != nil {
			return err
		}
		if kind == SpacePersonal {
			return invalid("sharedSpace", nil)
		}
		personal, err := personalSpace(ctx, tx, userID)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM warranties WHERE space_id = ?`, personal)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			names, err := tagNames(ctx, tx, id)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE warranties SET space_id = ?, updated_at = ? WHERE id = ?`,
				target, now(), id); err != nil {
				return err
			}
			if err := setTags(ctx, tx, target, id, names); err != nil {
				return err
			}
		}
		moved = len(ids)
		return pruneTags(ctx, tx, personal)
	})
	return moved, err
}

func tagNames(ctx context.Context, tx *sql.Tx, warrantyID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT t.name FROM warranty_tags wt JOIN tags t ON t.id = wt.tag_id
		WHERE wt.warranty_id = ?`, warrantyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// ---- tags ----

type Tag struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	SpaceID int64  `json:"spaceId"`
	Count   int    `json:"count,omitempty"`
}

// TagColors is the palette new tags pick from (by name hash) so a tag keeps
// the same colour everywhere.
var TagColors = []string{"slate", "red", "orange", "amber", "lime", "emerald", "teal", "sky", "indigo", "violet", "pink", "rose"}

func colorFor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return TagColors[int(h.Sum32())%len(TagColors)]
}

func ensureTag(ctx context.Context, tx *sql.Tx, spaceID int64, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	if len(name) > 40 {
		return 0, invalid("tagTooLong", i18n.Args{"name": name})
	}
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO tags (space_id, name, color) VALUES (?, ?, ?)
		ON CONFLICT (space_id, name) DO UPDATE SET name = tags.name
		RETURNING id`, spaceID, name, colorFor(name)).Scan(&id)
	return id, err
}

// tagVisibleTo limits tags (alias t) to the user's spaces.
const tagVisibleTo = `t.space_id IN (SELECT space_id FROM space_members WHERE user_id = ?)`

func (s *Store) ListTags(ctx context.Context, userID int64) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, t.color, t.space_id, COUNT(wt.warranty_id) FROM tags t
		LEFT JOIN warranty_tags wt ON wt.tag_id = t.id
		WHERE `+tagVisibleTo+` GROUP BY t.id ORDER BY t.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.SpaceID, &t.Count); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

func (s *Store) UpdateTag(ctx context.Context, userID, id int64, name, color string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		return invalid("tagName", nil)
	}
	valid := false
	for _, c := range TagColors {
		valid = valid || c == color
	}
	if !valid {
		return invalid("tagColor", nil)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tags AS t SET name = ?, color = ? WHERE `+tagVisibleTo+` AND t.id = ?`,
		name, color, userID, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return invalid("tagExists", nil)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTag(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags AS t WHERE `+tagVisibleTo+` AND t.id = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- reminders ----

// ActiveWithExpiry returns the non-archived, non-lifetime warranties the user can see.
func (s *Store) ActiveWithExpiry(ctx context.Context, userID int64) ([]*Warranty, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+warrantyCols+` FROM `+warrantyFrom+`
		WHERE `+visibleTo+` AND w.archived_at IS NULL AND w.expires_on IS NOT NULL ORDER BY w.expires_on`, userID)
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

// ReminderSent reports whether this user was already reminded; reminders are
// per person so everyone in a shared space gets their own.
func (s *Store) ReminderSent(ctx context.Context, userID, warrantyID int64, threshold int, expiresOn string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders_sent
		WHERE user_id = ? AND warranty_id = ? AND threshold_days = ? AND expires_on = ?`,
		userID, warrantyID, threshold, expiresOn).Scan(&n)
	return n > 0, err
}

func (s *Store) MarkRemindersSent(ctx context.Context, userID, warrantyID int64, thresholds []int, expiresOn string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, t := range thresholds {
			if _, err := tx.ExecContext(ctx, `INSERT INTO reminders_sent (user_id, warranty_id, threshold_days, expires_on)
				VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING`, userID, warrantyID, t, expiresOn); err != nil {
				return err
			}
		}
		return nil
	})
}
