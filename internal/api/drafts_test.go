package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// draftPaperless serves documents, each user's identity (by token) and
// correspondents like Paperless does.
type draftPaperless struct {
	mu     sync.Mutex
	users  map[string]int64 // token -> Paperless user ID
	docs   map[int64]map[string]any
	fields map[int64]string // custom field ID -> name
}

func (p *draftPaperless) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Token ")
	switch {
	case r.URL.Path == "/api/ui_settings/":
		json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": p.users[token]}})
	case r.URL.Path == "/api/correspondents/3/":
		json.NewEncoder(w).Encode(map[string]any{"name": "Datart"})
	case r.URL.Path == "/api/custom_fields/":
		var results []map[string]any
		for id, name := range p.fields {
			results = append(results, map[string]any{"id": id, "name": name, "data_type": "string"})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	case strings.HasPrefix(r.URL.Path, "/api/documents/"):
		var id int64
		fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/api/documents/"), "%d", &id)
		doc, ok := p.docs[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(doc)
	default:
		http.NotFound(w, r)
	}
}

type draftFixture struct {
	srv        *Server
	st         *store.Store
	pl         *draftPaperless
	alice, bob int64
}

// Alice (admin) and Bob both use the server's Paperless with their own
// tokens; there they are users 10 and 20.
func newDraftFixture(t *testing.T, aliceSource, bobSource string) *draftFixture {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	pl := &draftPaperless{
		users:  map[string]int64{"alice-token": 10, "bob-token": 20},
		docs:   map[int64]map[string]any{},
		fields: map[int64]string{1: "Product", 2: "Price", 3: "Warranty months"},
	}
	plServer := httptest.NewServer(pl)
	t.Cleanup(plServer.Close)
	join := func(name, source string) int64 {
		u, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: name, Name: name})
		s, _ := st.Settings(ctx, u.ID)
		s.PaperlessToken, s.DraftSource, s.Currency = name+"-token", source, "CZK"
		if err := st.SaveSettings(ctx, u.ID, s); err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	f := &draftFixture{st: st, pl: pl}
	f.alice = join("alice", aliceSource)
	f.bob = join("bob", bobSource)
	appURL, _ := url.Parse("https://warranty.example.test")
	f.srv = New(&config.Config{
		AppURL: appURL,
		Paperless: config.Paperless{
			URL: plServer.URL, WebhookSecret: "s3cret",
			DraftFields: map[string]string{"product": "Product", "price": "Price", "warranty_months": "Warranty months"},
		},
	}, st, nil, nil, nil, nil, "test")
	return f
}

func (f *draftFixture) addDoc(id, owner int64, custom []paperless.CustomField) {
	f.pl.mu.Lock()
	defer f.pl.mu.Unlock()
	f.pl.docs[id] = map[string]any{
		"id": id, "title": "Dishwasher receipt", "created": "2025-03-01T00:00:00+01:00", "owner": owner,
		"correspondent": 3, "original_file_name": "scan.pdf", "mime_type": "application/pdf", "custom_fields": custom,
	}
}

func (f *draftFixture) drafts(t *testing.T, uid int64) []*store.Warranty {
	t.Helper()
	list, err := f.st.ListWarranties(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	var out []*store.Warranty
	for _, w := range list {
		if w.Draft {
			out = append(out, w)
		}
	}
	return out
}

func TestWebhookSecret(t *testing.T) {
	f := newDraftFixture(t, "paperless", "off")
	send := func(secret, body string) int {
		r := httptest.NewRequest(http.MethodPost, "/hooks/paperless", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if secret != "" {
			r.Header.Set(webhookHeader, secret)
		}
		w := httptest.NewRecorder()
		f.srv.paperlessWebhook(w, r)
		return w.Code
	}
	if got := send("", `{"doc_id": "1"}`); got != http.StatusUnauthorized {
		t.Errorf("no secret: %d", got)
	}
	if got := send("wrong", `{"doc_id": "1"}`); got != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d", got)
	}
	if got := send("s3cret", `{"doc_id": "x"}`); got != http.StatusBadRequest {
		t.Errorf("bad doc_id: %d", got)
	}
	if got := send("s3cret", `{"doc_id": "1"}`); got != http.StatusAccepted {
		t.Errorf("good request: %d", got)
	}
	f.srv.background.Wait()

	f.srv.cfg.Paperless.WebhookSecret = ""
	if got := send("", `{"doc_id": "1"}`); got != http.StatusNotFound {
		t.Errorf("webhook off: %d, want 404", got)
	}
}

func TestWebhookDocID(t *testing.T) {
	for body, ct := range map[string]string{
		`{"doc_id": "42"}`: "application/json",
		// What Paperless sends for a text body with "as JSON" on.
		`"{\"doc_id\": \"42\"}"`: "application/json",
		`{"doc_id": 42}`:         "application/json; charset=utf-8",
		`doc_id=42`:              "application/x-www-form-urlencoded",
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", ct)
		if id, err := webhookDocID(r); err != nil || id != 42 {
			t.Errorf("%s: %d, %v", body, id, err)
		}
	}
}

func TestDraftGoesToTheOwner(t *testing.T) {
	ctx := context.Background()
	f := newDraftFixture(t, "paperless", "off")

	// Alice's receipt: a draft for her, from the document's own data.
	f.addDoc(1, 10, nil)
	if err := f.srv.draftFromPaperless(ctx, 1); err != nil {
		t.Fatal(err)
	}
	drafts := f.drafts(t, f.alice)
	if len(drafts) != 1 {
		t.Fatalf("Alice's drafts: %d, want 1", len(drafts))
	}
	if n := len(f.drafts(t, f.bob)); n != 0 {
		t.Errorf("Bob sees Alice's draft (%d)", n)
	}
	d := drafts[0]
	if d.ProductName != "Dishwasher receipt" || d.PurchaseDate != "2025-03-01" || d.Vendor != "Datart" ||
		len(d.Attachments) != 1 || *d.Attachments[0].PaperlessDocID != 1 || *d.WarrantyMonths != 24 || d.Currency != "CZK" {
		t.Errorf("draft: %+v", d)
	}

	// The same document again: no second draft.
	f.srv.draftFromPaperless(ctx, 1)
	if n := len(f.drafts(t, f.alice)); n != 1 {
		t.Errorf("after a repeated webhook: %d drafts", n)
	}

	// Bob's receipt: Bob hasn't turned drafts on, so nobody gets one.
	f.addDoc(2, 20, nil)
	f.srv.draftFromPaperless(ctx, 2)
	if refs, _ := f.st.DocumentRefs(ctx, 2); len(refs) != 0 {
		t.Errorf("Bob's document made a draft: %+v", refs)
	}

	// A document nobody here owns goes to the admin, who has drafts on.
	f.addDoc(3, 99, nil)
	f.srv.draftFromPaperless(ctx, 3)
	if n := len(f.drafts(t, f.alice)); n != 2 {
		t.Errorf("unowned document: Alice has %d drafts, want 2", n)
	}

	// Drafts get no reminders until confirmed.
	active, _ := f.st.ActiveWithExpiry(ctx, f.alice)
	if len(active) != 0 {
		t.Errorf("drafts are due reminders: %d", len(active))
	}
	if err := f.st.ConfirmWarranty(ctx, f.alice, d.ID); err != nil {
		t.Fatal(err)
	}
	if active, _ = f.st.ActiveWithExpiry(ctx, f.alice); len(active) != 1 {
		t.Errorf("confirmed warranty isn't due reminders")
	}
}

func TestNobodyGetsDraftsByDefault(t *testing.T) {
	f := newDraftFixture(t, "off", "off")
	f.addDoc(1, 10, nil)
	if err := f.srv.draftFromPaperless(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if n := len(f.drafts(t, f.alice)); n != 0 {
		t.Errorf("drafts made with drafts off: %d", n)
	}
}

func TestDraftFromCustomFields(t *testing.T) {
	ctx := context.Background()
	f := newDraftFixture(t, "fields", "off")

	// Filled in already: the draft takes the fields' values.
	f.addDoc(1, 10, []paperless.CustomField{{Field: 1, Value: "Dishwasher Series 6"}, {Field: 2, Value: "CZK17490.00"}, {Field: 3, Value: float64(36)}})
	if err := f.srv.draftFromPaperless(ctx, 1); err != nil {
		t.Fatal(err)
	}
	d := f.drafts(t, f.alice)[0]
	if d.ProductName != "Dishwasher Series 6" || d.Price == nil || *d.Price != 17490 || *d.WarrantyMonths != 36 {
		t.Errorf("draft from fields: %+v", d)
	}

	// Not filled in yet: the draft starts from the document, and is filled
	// in once paperless-gpt has written the fields.
	f.srv.refillDelays = nil
	f.addDoc(2, 10, nil)
	f.srv.draftFromPaperless(ctx, 2)
	var later *store.Warranty
	for _, w := range f.drafts(t, f.alice) {
		if w.Attachments[0].PaperlessDocID != nil && *w.Attachments[0].PaperlessDocID == 2 {
			later = w
		}
	}
	if later == nil || later.ProductName != "Dishwasher receipt" {
		t.Fatalf("draft before the fields were filled: %+v", later)
	}
	f.addDoc(2, 10, []paperless.CustomField{{Field: 1, Value: "Robot vacuum"}, {Field: 2, Value: "27990"}})
	owner := &paperlessUser{id: f.alice, client: paperless.New(f.srv.cfg.Paperless.URL, "alice-token")}
	settings, _ := f.st.Settings(ctx, f.alice)
	if done, err := f.srv.refill(ctx, owner, later.ID, 2, settings); err != nil || !done {
		t.Fatalf("refill: %v %v", done, err)
	}
	w, _ := f.st.GetWarranty(ctx, f.alice, later.ID)
	if w.ProductName != "Robot vacuum" || *w.Price != 27990 || len(w.Attachments) != 1 {
		t.Errorf("after refill: %+v", w)
	}
	if unedited, _ := f.st.UneditedDraft(ctx, later.ID); !unedited {
		t.Error("an automatic refill counted as an edit")
	}
}
