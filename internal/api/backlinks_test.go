package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// fakePaperless keeps custom fields and documents' values like Paperless:
// PATCH replaces a document's whole list.
type fakePaperless struct {
	mu      sync.Mutex
	fields  map[string]int64 // name -> id
	values  map[int64][]paperless.CustomField
	patches int
}

func (f *fakePaperless) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/custom_fields/" && r.Method == http.MethodGet:
		name := r.URL.Query().Get("name__iexact")
		var results []map[string]any
		for n, id := range f.fields {
			if strings.EqualFold(n, name) {
				results = append(results, map[string]any{"id": id, "name": n})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	case r.URL.Path == "/api/custom_fields/" && r.Method == http.MethodPost:
		var body struct{ Name string }
		json.NewDecoder(r.Body).Decode(&body)
		id := int64(len(f.fields) + 100)
		f.fields[body.Name] = id
		json.NewEncoder(w).Encode(map[string]any{"id": id})
	case strings.HasPrefix(r.URL.Path, "/api/documents/"):
		id, _ := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/documents/"), "/"), 10, 64)
		if r.Method == http.MethodPatch {
			var body struct {
				CustomFields []paperless.CustomField `json:"custom_fields"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			f.values[id] = body.CustomFields
			f.patches++
		}
		json.NewEncoder(w).Encode(map[string]any{"custom_fields": f.values[id]})
	default:
		http.NotFound(w, r)
	}
}

// value returns a document's value for the named field, or "" if unset.
func (f *fakePaperless) value(doc int64, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.values[doc] {
		if v.Field == f.fields[name] {
			s, _ := v.Value.(string)
			return s
		}
	}
	return ""
}

func TestBacklinks(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)

	pl := &fakePaperless{
		fields: map[string]int64{"Note": 1},
		values: map[int64][]paperless.CustomField{5: {{Field: 1, Value: "the user's own field"}}},
	}
	plServer := httptest.NewServer(pl)
	t.Cleanup(plServer.Close)

	alice, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	settings, _ := st.Settings(ctx, alice.ID)
	settings.PaperlessURL, settings.PaperlessToken, settings.PaperlessBacklinks = plServer.URL, "token", true
	if err := st.SaveSettings(ctx, alice.ID, settings); err != nil {
		t.Fatal(err)
	}
	months := 24
	wid, _ := st.CreateWarranty(ctx, alice.ID, &store.WarrantyInput{ProductName: "Dishwasher", PurchaseDate: "2025-03-01", WarrantyMonths: &months})
	doc := int64(5)
	att := &store.Attachment{WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless, Filename: "r.pdf",
		PaperlessDocID: &doc, Status: store.StatusReady, AddedBy: &alice.ID}
	if err := st.CreateAttachment(ctx, att); err != nil {
		t.Fatal(err)
	}

	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{
		AppURL:    appURL,
		Paperless: config.Paperless{WarrantyField: "Záruka", HomeBoxField: "HomeBox"},
		HomeBox:   config.HomeBox{URL: "http://homebox:7745", PublicURL: "https://homebox.example.test"},
	}
	srv := New(cfg, st, nil, nil, nil, nil, "test")
	sync := func() {
		t.Helper()
		if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: doc}); err != nil {
			t.Fatal(err)
		}
	}

	// Attached: the warranty link is added; the HomeBox field isn't created
	// while there is no item.
	sync()
	if got := pl.value(doc, "Záruka"); got != "https://warranty.example.test/w/1" {
		t.Errorf("warranty link = %q", got)
	}
	if _, ok := pl.fields["HomeBox"]; ok {
		t.Error("HomeBox field created without an item")
	}

	// Linked to HomeBox: the item link is added too.
	if err := st.SetHomeBox(ctx, alice.ID, wid, &store.HomeBoxLink{ID: "item-1", AssetID: "000-001"}); err != nil {
		t.Fatal(err)
	}
	sync()
	if got := pl.value(doc, "HomeBox"); got != "https://homebox.example.test/item/item-1" {
		t.Errorf("HomeBox link = %q", got)
	}

	// Nothing changed: nothing is written.
	patches := pl.patches
	sync()
	if pl.patches != patches {
		t.Error("an unchanged document was written again")
	}

	// Detached: both links go, the user's own field stays.
	if err := st.DeleteAttachment(ctx, att.ID); err != nil {
		t.Fatal(err)
	}
	sync()
	if pl.value(doc, "Záruka") != "" || pl.value(doc, "HomeBox") != "" {
		t.Errorf("links left behind: %v", pl.values[doc])
	}
	if got := pl.value(doc, "Note"); got != "the user's own field" {
		t.Errorf("the user's field = %q", got)
	}
}

func TestSetLinkField(t *testing.T) {
	const prefix = "https://warranty.example.test/w/"
	typed := []paperless.CustomField{{Field: 7, Value: "https://example.com/something-else"}}
	if out, changed := setLinkField(typed, 7, prefix+"1", prefix); changed || out[0].Value != typed[0].Value {
		t.Error("a value typed in by hand was overwritten")
	}
	if out, changed := setLinkField(typed, 7, "", prefix); changed || len(out) != 1 {
		t.Error("a value typed in by hand was removed")
	}
	empty := []paperless.CustomField{{Field: 7, Value: nil}}
	if out, changed := setLinkField(empty, 7, prefix+"2", prefix); !changed || out[0].Value != prefix+"2" {
		t.Errorf("an empty field wasn't filled: %v", out)
	}
	ours := []paperless.CustomField{{Field: 3, Value: "kept"}, {Field: 7, Value: prefix + "1"}}
	if out, changed := setLinkField(ours, 7, prefix+"2", prefix); !changed || out[1].Value != prefix+"2" || ours[1].Value != prefix+"1" {
		t.Errorf("updating a link: %v (input changed: %v)", out, ours)
	}
}

func TestBacklinksOff(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	pl := &fakePaperless{fields: map[string]int64{}, values: map[int64][]paperless.CustomField{}}
	plServer := httptest.NewServer(pl)
	t.Cleanup(plServer.Close)
	alice, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	settings, _ := st.Settings(ctx, alice.ID)
	settings.PaperlessURL, settings.PaperlessToken = plServer.URL, "token" // links not turned on
	st.SaveSettings(ctx, alice.ID, settings)
	appURL, _ := url.Parse("https://warranty.example.test")
	srv := New(&config.Config{AppURL: appURL, Paperless: config.Paperless{WarrantyField: "Warranty"}}, st, nil, nil, nil, nil, "test")
	if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: 5}); err != nil {
		t.Fatal(err)
	}
	if pl.patches != 0 || len(pl.fields) != 0 {
		t.Error("Paperless was changed for a user who didn't turn links on")
	}
}
