package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/receipts"
	"github.com/fhavrlent/warranteed/internal/store"
)

// Drafts from Paperless: a Paperless workflow ("Document added", usually
// filtered by a tag) calls the webhook with the new document's ID, and the
// document's owner gets a draft warranty with it attached, to check and
// confirm. Users opt in; details come from the document itself and, as each
// user chooses, from its custom fields (e.g. filled by paperless-gpt) or
// from Claude.

// webhookHeader carries the shared secret (WARRANTEED_WEBHOOK_SECRET).
const webhookHeader = "X-Warranteed-Secret"

// Draft sources a user can choose from.
const (
	sourceOff       = "off"
	sourcePaperless = "paperless"
	sourceFields    = "fields"
	sourceClaude    = "claude"
)

// draftSources lists the choices this server offers.
func (s *Server) draftSources() []string {
	out := []string{sourceOff, sourcePaperless}
	if len(s.cfg.Paperless.DraftFields) > 0 {
		out = append(out, sourceFields)
	}
	if s.cfg.Claude.APIKey != "" {
		out = append(out, sourceClaude)
	}
	return out
}

// paperlessWebhook accepts {"doc_id": …} as JSON or form data, answers right
// away and creates the draft in the background.
func (s *Server) paperlessWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.Paperless.WebhookSecret
	if secret == "" || s.cfg.Paperless.URL == "" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(webhookHeader)), []byte(secret)) != 1 {
		writeMsg(w, http.StatusUnauthorized, "server.badRequest", nil)
		return
	}
	docID, err := webhookDocID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := s.draftFromPaperless(ctx, docID); err != nil {
			slog.Warn("draft from paperless", "doc", docID, "err", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

// webhookDocID reads doc_id from the body. Paperless sends a text body with
// "as JSON" on as a JSON string holding that text, so a string is unwrapped
// once before reading the object.
func webhookDocID(r *http.Request) (int64, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
	var raw string
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "application/json" {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return 0, err
		}
		var text string
		if json.Unmarshal(data, &text) == nil {
			data = []byte(text)
		}
		var body struct {
			DocID json.RawMessage `json:"doc_id"`
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return 0, errors.New("expected {\"doc_id\": …}")
		}
		raw = strings.Trim(string(body.DocID), `"`)
	} else {
		raw = r.FormValue("doc_id")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("doc_id must be a document ID")
	}
	return id, nil
}

// paperlessUser is a Warranteed user connected to the server's Paperless.
type paperlessUser struct {
	id     int64
	admin  bool
	client *paperless.Client
	// drafts: they turned on drafts from Paperless.
	drafts bool
}

// draftFromPaperless creates a draft for a new document in the server's
// Paperless. It goes to the user who owns the document there, or to an admin
// when nobody does, and only if they turned drafts on. A document already
// attached somewhere is skipped.
func (s *Server) draftFromPaperless(ctx context.Context, docID int64) error {
	users, err := s.paperlessUsers(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(users, func(u paperlessUser) bool { return u.drafts }) {
		return nil // nobody gets drafts
	}
	// Read the document as anyone who can see it.
	var doc *paperless.DocumentInfo
	for _, u := range users {
		if doc, err = u.client.DocumentInfo(ctx, docID); err == nil {
			break
		}
	}
	if doc == nil {
		return errors.New("no connected user can see the document")
	}
	// One delivery per document at a time: an overlapping repeat waits, then
	// finds the draft and stops, instead of making (and paying for) another.
	lock, _ := s.draftLocks.LoadOrStore(docID, new(sync.Mutex))
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	owner := s.documentOwner(ctx, users, doc)
	if owner == nil {
		slog.Info("no draft from paperless: its owner doesn't get drafts", "doc", docID)
		return nil
	}
	if doc, err = owner.client.DocumentInfo(ctx, docID); err != nil {
		return err // the owner must see it, as their connection serves it
	}
	refs, err := s.store.DocumentRefs(ctx, docID)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		for _, u := range users {
			if u.id == ref.Owner {
				return nil // already attached through this Paperless
			}
		}
	}

	st, err := s.store.Settings(ctx, owner.id)
	if err != nil {
		return err
	}
	filename := doc.OriginalFileName
	if doc.Title != "" {
		filename = doc.Title + filepath.Ext(doc.OriginalFileName)
	}
	// One draft per product bought, each with the document attached.
	var wids []int64
	for _, in := range s.draftInputs(ctx, owner.client, doc, st) {
		in.Draft = true
		wid, err := s.store.CreateWarranty(ctx, owner.id, in)
		if err != nil {
			return err
		}
		a := &store.Attachment{
			AddedBy: &owner.id, WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless,
			Filename: cleanFilename(filename), MimeType: doc.MimeType, PaperlessDocID: &doc.ID, Status: store.StatusReady,
		}
		if err := s.store.CreateAttachment(ctx, a); err != nil {
			return err
		}
		wids = append(wids, wid)
	}
	slog.Info("draft from paperless", "doc", docID, "user", owner.id, "warranties", wids)
	s.syncBacklinksLater(store.PaperlessDoc{Owner: owner.id, DocID: docID})

	// paperless-gpt may not have filled the fields yet; look again later.
	// Fields describe one product, so there is one draft.
	if st.DraftSource == sourceFields && s.sourceAvailable(sourceFields) && !hasFieldValues(doc, s.fieldIDs(ctx, owner.client)) {
		s.refillLater(owner, wids[0], docID, st)
	}
	return nil
}

// paperlessUsers lists the users whose connection is the server's Paperless.
func (s *Server) paperlessUsers(ctx context.Context) ([]paperlessUser, error) {
	ids, err := s.store.ListUserIDs(ctx)
	if err != nil {
		return nil, err
	}
	var out []paperlessUser
	for _, id := range ids {
		st, err := s.store.Settings(ctx, id)
		if err != nil {
			return nil, err
		}
		admin, err := s.store.IsAdmin(ctx, id)
		if err != nil {
			return nil, err
		}
		if pc := s.paperlessFor(st, admin); pc != nil && pc.BaseURL == s.cfg.Paperless.URL {
			on := st.DraftSource != sourceOff && s.sourceAvailable(st.DraftSource)
			out = append(out, paperlessUser{id: id, admin: admin, client: pc, drafts: on})
		}
	}
	return out, nil
}

// documentOwner finds who gets the draft: the user who owns doc in Paperless
// (matched by asking Paperless whose each token is), or for a document with
// no matching owner the first admin. Either must have drafts turned on, or
// nobody gets one.
func (s *Server) documentOwner(ctx context.Context, users []paperlessUser, doc *paperless.DocumentInfo) *paperlessUser {
	if doc.Owner != nil {
		for i, u := range users {
			if pid, err := u.client.UserID(ctx); err == nil && pid == *doc.Owner {
				if !u.drafts {
					return nil
				}
				return &users[i]
			}
		}
	}
	for i, u := range users {
		if u.admin && u.drafts {
			return &users[i]
		}
	}
	return nil
}

func (s *Server) sourceAvailable(source string) bool {
	for _, x := range s.draftSources() {
		if x == source {
			return true
		}
	}
	return false
}

// draftInputs builds drafts from the document's own data, improved by the
// user's chosen source where it knows more: one per product the source finds,
// or one when it finds none. A failing source only means fewer details are
// filled in.
func (s *Server) draftInputs(ctx context.Context, pc *paperless.Client, doc *paperless.DocumentInfo, st *store.Settings) []*store.WarrantyInput {
	months := 24
	in := &store.WarrantyInput{
		ProductName:    strings.TrimSpace(doc.Title),
		PurchaseDate:   doc.Created,
		WarrantyMonths: &months,
		Currency:       st.Currency,
	}
	if in.ProductName == "" {
		in.ProductName = strings.TrimSuffix(doc.OriginalFileName, filepath.Ext(doc.OriginalFileName))
	}
	if in.PurchaseDate == "" {
		in.PurchaseDate = time.Now().In(s.cfg.Location).Format(store.DateLayout)
	}
	if doc.Correspondent != nil {
		if name, err := pc.CorrespondentName(ctx, *doc.Correspondent); err == nil {
			in.Vendor = name
		}
	}

	var found []receipts.Details
	switch {
	case st.DraftSource == sourceFields && s.sourceAvailable(sourceFields):
		found = []receipts.Details{*fieldDetails(doc, s.fieldIDs(ctx, pc))}
	case st.DraftSource == sourceClaude && s.sourceAvailable(sourceClaude):
		d, err := s.readWithClaude(ctx, pc, doc.ID)
		if err != nil {
			slog.Warn("read receipt with claude", "doc", doc.ID, "err", err)
		}
		found = d
	}
	if len(found) == 0 {
		return []*store.WarrantyInput{in}
	}
	out := make([]*store.WarrantyInput, len(found))
	for i := range found {
		item := *in
		applyDetails(&item, &found[i])
		addExtension(&item, &found[i], st.EffectiveLanguage())
		out[i] = &item
	}
	return out
}

// applyDetails copies what a source found over the document's own data.
func applyDetails(in *store.WarrantyInput, d *receipts.Details) {
	if d == nil {
		return
	}
	set := func(dst *string, v string) {
		if v = strings.TrimSpace(v); v != "" {
			*dst = v
		}
	}
	set(&in.ProductName, d.ProductName)
	set(&in.Brand, d.Brand)
	set(&in.Model, d.Model)
	set(&in.Vendor, d.Vendor)
	set(&in.OrderNumber, d.OrderNumber)
	if _, err := time.Parse(store.DateLayout, d.PurchaseDate); err == nil {
		in.PurchaseDate = d.PurchaseDate
	}
	if d.Price != nil && *d.Price >= 0 {
		in.Price = d.Price
	}
	if c := strings.ToUpper(strings.TrimSpace(d.Currency)); len(c) == 3 {
		in.Currency = c
	}
	if d.WarrantyMonths != nil && *d.WarrantyMonths > 0 && *d.WarrantyMonths <= 1200 {
		in.WarrantyMonths = d.WarrantyMonths
	}
	for _, sn := range d.SerialNumbers {
		if sn = strings.TrimSpace(sn); sn != "" && !slices.Contains(in.SerialNumbers, sn) {
			in.SerialNumbers = append(in.SerialNumbers, sn)
		}
	}
}

// addExtension lengthens the warranty by an extended warranty bought with the
// product, and says so in the notes, in the user's language.
func addExtension(in *store.WarrantyInput, d *receipts.Details, lang string) {
	if d.ExtendedMonths == nil || *d.ExtendedMonths <= 0 || in.WarrantyMonths == nil {
		return
	}
	months := *in.WarrantyMonths + *d.ExtendedMonths
	if months > 1200 {
		return
	}
	in.WarrantyMonths = &months
	name := strings.TrimSpace(d.Extension)
	key := "server.draftExtension"
	if name == "" {
		key = "server.draftExtensionUnnamed"
	}
	note := i18n.T(lang, key, i18n.Args{"count": *d.ExtendedMonths, "name": name})
	if in.Notes != "" {
		note = in.Notes + "\n" + note
	}
	in.Notes = note
}

// fieldIDs resolves the configured custom field names to IDs in pc's Paperless.
func (s *Server) fieldIDs(ctx context.Context, pc *paperless.Client) map[string]int64 {
	names, err := pc.CustomFieldNames(ctx)
	if err != nil {
		slog.Warn("list paperless custom fields", "err", err)
		return nil
	}
	out := map[string]int64{}
	for key, want := range s.cfg.Paperless.DraftFields {
		for id, name := range names {
			if strings.EqualFold(name, want) {
				out[key] = id
			}
		}
	}
	return out
}

// hasFieldValues reports whether any of the mapped fields has a value yet.
func hasFieldValues(doc *paperless.DocumentInfo, ids map[string]int64) bool {
	for _, id := range ids {
		for _, f := range doc.CustomFields {
			if f.Field == id && f.Value != nil && f.Value != "" {
				return true
			}
		}
	}
	return false
}

// fieldDetails reads the configured custom fields' values off a document.
func fieldDetails(doc *paperless.DocumentInfo, ids map[string]int64) *receipts.Details {
	value := func(key string) any {
		id, ok := ids[key]
		if !ok {
			return nil
		}
		for _, f := range doc.CustomFields {
			if f.Field == id {
				return f.Value
			}
		}
		return nil
	}
	text := func(key string) string {
		switch v := value(key).(type) {
		case string:
			return v
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	}
	d := &receipts.Details{
		ProductName: text("product"), Brand: text("brand"), Model: text("model"), Vendor: text("vendor"),
		PurchaseDate: text("purchase_date"), Currency: text("currency"), OrderNumber: text("order"),
	}
	if sn := text("serial"); sn != "" {
		d.SerialNumbers = strings.FieldsFunc(sn, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	}
	// Paperless's monetary fields look like "CZK1379.00" or "1379.00".
	if p := text("price"); p != "" {
		num := strings.TrimLeft(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		if code := p[:len(p)-len(num)]; len(code) == 3 && d.Currency == "" {
			d.Currency = code
		}
		if v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(num), ",", "."), 64); err == nil {
			d.Price = &v
		}
	}
	if m := text("warranty_months"); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil && v == float64(int(v)) {
			n := int(v)
			d.WarrantyMonths = &n
		}
	}
	return d
}

// readWithClaude downloads the document (Paperless's archived copy is a PDF
// for scans too) and has Claude read it.
func (s *Server) readWithClaude(ctx context.Context, pc *paperless.Client, docID int64) ([]receipts.Details, error) {
	resp, err := pc.Stream(ctx, docID, "download")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// The API takes up to 32 MB per request.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 24<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 24<<20 {
		return nil, errors.New("the document is too large to send to Claude")
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return s.receipts.Read(ctx, data, ct)
}

// refillLater looks at the document's custom fields again over the next few
// minutes and fills in the draft once they have values, unless someone has
// edited the draft in the meantime.
func (s *Server) refillLater(owner *paperlessUser, wid, docID int64, st *store.Settings) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		for _, wait := range s.refillDelays {
			time.Sleep(wait)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			done, err := s.refill(ctx, owner, wid, docID, st)
			cancel()
			if err != nil {
				slog.Warn("fill in draft from paperless fields", "warranty", wid, "err", err)
			}
			if done {
				return
			}
		}
	}()
}

func (s *Server) refill(ctx context.Context, owner *paperlessUser, wid, docID int64, st *store.Settings) (bool, error) {
	unedited, err := s.store.UneditedDraft(ctx, wid)
	if err != nil || !unedited {
		return true, err
	}
	doc, err := owner.client.DocumentInfo(ctx, docID)
	if err != nil {
		return false, err
	}
	ids := s.fieldIDs(ctx, owner.client)
	if !hasFieldValues(doc, ids) {
		return false, nil
	}
	w, err := s.store.GetWarranty(ctx, owner.id, wid)
	if err != nil {
		return true, err
	}
	in := inputOf(w)
	applyDetails(in, fieldDetails(doc, ids))
	// Saved only if nobody edited the draft meanwhile, also while the fields
	// were being read.
	_, err = s.store.FillDraft(ctx, owner.id, wid, in)
	return true, err
}

// inputOf is a warranty's editable fields, to save it with changes.
func inputOf(w *store.Warranty) *store.WarrantyInput {
	in := &store.WarrantyInput{
		ProductName: w.ProductName, Brand: w.Brand, Model: w.Model, Vendor: w.Vendor, OrderNumber: w.OrderNumber,
		PurchaseDate: w.PurchaseDate, WarrantyMonths: w.WarrantyMonths, WarrantyEndDate: w.WarrantyEndDate,
		IsLifetime: w.IsLifetime, Price: w.Price, Currency: w.Currency, ProductURL: w.ProductURL, Notes: w.Notes,
		SerialNumbers: w.SerialNumbers,
	}
	for _, t := range w.Tags {
		in.Tags = append(in.Tags, t.Name)
	}
	return in
}

// confirmWarranty turns a draft into a regular warranty.
func (s *Server) confirmWarranty(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.ConfirmWarranty(r.Context(), userID(r), id); err != nil {
		fail(w, err)
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}
