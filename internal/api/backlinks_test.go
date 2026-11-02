package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// fakePaperless keeps custom fields and documents' values like Paperless:
// PATCH replaces a document's whole list.
type fakePaperless struct {
	mu      sync.Mutex
	fields  map[string]int64  // name -> id
	types   map[string]string // name -> data type; "url" when unset
	values  map[int64][]paperless.CustomField
	patches int
	creates int
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
				typ := f.types[n]
				if typ == "" {
					typ = "url"
				}
				results = append(results, map[string]any{"id": id, "name": n, "data_type": typ})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	case r.URL.Path == "/api/custom_fields/" && r.Method == http.MethodPost:
		var body struct{ Name string }
		json.NewDecoder(r.Body).Decode(&body)
		id := int64(len(f.fields) + 100)
		f.fields[body.Name] = id
		f.creates++
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
		types:  map[string]string{"Note": "string"},
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
	number := []paperless.CustomField{{Field: 7, Value: float64(24)}}
	if out, changed := setLinkField(number, 7, "", prefix); changed || len(out) != 1 {
		t.Error("a number someone entered was removed")
	}
	ours := []paperless.CustomField{{Field: 3, Value: "kept"}, {Field: 7, Value: prefix + "1"}}
	if out, changed := setLinkField(ours, 7, prefix+"2", prefix); !changed || out[1].Value != prefix+"2" || ours[1].Value != prefix+"1" {
		t.Errorf("updating a link: %v (input changed: %v)", out, ours)
	}

	// HomeBox lists: only the ones filtered by Warranteed's field are ours.
	hb := "https://homebox.example.test"
	list := homebox.ListURL(hb, "5-a1b2c3")
	shelf := []paperless.CustomField{{Field: 7, Value: hb + "/items?tags=kitchen"}}
	if _, changed := setLinkField(shelf, 7, list, hb+"/item/", homebox.ListPrefix(hb)); changed {
		t.Error("a HomeBox list typed in by hand was overwritten")
	}
	mine := []paperless.CustomField{{Field: 7, Value: list}}
	if out, changed := setLinkField(mine, 7, hb+"/item/x", hb+"/item/", homebox.ListPrefix(hb)); !changed || out[0].Value != hb+"/item/x" {
		t.Errorf("Warranteed's HomeBox list wasn't replaced: %v", out)
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

// Alice and Bob use the same Paperless (the server's) with their own tokens
// and both attached receipt 5: it links to the list of both warranties, and
// to what's still attached when one of them detaches it.
func TestBacklinksSharedDocument(t *testing.T) {
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

	months := 24
	doc := int64(5)
	type person struct {
		id       int64
		warranty int64
		att      *store.Attachment
	}
	join := func(name string, links bool) person {
		u, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: name, Name: name})
		settings, _ := st.Settings(ctx, u.ID)
		settings.PaperlessToken, settings.PaperlessBacklinks = name+"-token", links
		st.SaveSettings(ctx, u.ID, settings)
		wid, _ := st.CreateWarranty(ctx, u.ID, &store.WarrantyInput{ProductName: name + "'s grinder", PurchaseDate: "2025-03-01", WarrantyMonths: &months})
		a := &store.Attachment{WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless, Filename: "r.pdf",
			PaperlessDocID: &doc, Status: store.StatusReady, AddedBy: &u.ID}
		if err := st.CreateAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
		return person{u.ID, wid, a}
	}
	alice := join("alice", true)
	bob := join("bob", false) // Bob never turned links on

	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{AppURL: appURL, Paperless: config.Paperless{URL: plServer.URL, WarrantyField: "Warranty"}}
	srv := New(cfg, st, nil, nil, nil, nil, "test")
	sync := func(by int64) {
		t.Helper()
		if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: by, DocID: doc}); err != nil {
			t.Fatal(err)
		}
	}
	linkTo := func(wid int64) string { return fmt.Sprintf("https://warranty.example.test/w/%d", wid) }

	sync(bob.id) // Bob's attachment triggers it; Alice's credentials write it
	if got, want := pl.value(doc, "Warranty"), fmt.Sprintf("https://warranty.example.test/?warranties=%d,%d&filter=all", alice.warranty, bob.warranty); got != want {
		t.Fatalf("link = %q, want the list of both, %q", got, want)
	}
	st.DeleteAttachment(ctx, alice.att.ID)
	sync(alice.id)
	if got := pl.value(doc, "Warranty"); got != linkTo(bob.warranty) {
		t.Errorf("after Alice detached: link = %q, want Bob's warranty", got)
	}
	st.DeleteAttachment(ctx, bob.att.ID)
	sync(bob.id)
	if got := pl.value(doc, "Warranty"); got != "" {
		t.Errorf("after both detached: link = %q, want none", got)
	}
}

// Updates of one document run one at a time.
func TestBacklinksOneAtATime(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	slow := &slowPaperless{fakePaperless: fakePaperless{fields: map[string]int64{}, values: map[int64][]paperless.CustomField{}}}
	plServer := httptest.NewServer(slow)
	t.Cleanup(plServer.Close)
	alice, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	settings, _ := st.Settings(ctx, alice.ID)
	settings.PaperlessURL, settings.PaperlessToken, settings.PaperlessBacklinks = plServer.URL, "token", true
	st.SaveSettings(ctx, alice.ID, settings)
	appURL, _ := url.Parse("https://warranty.example.test")
	srv := New(&config.Config{AppURL: appURL, Paperless: config.Paperless{WarrantyField: "Warranty"}}, st, nil, nil, nil, nil, "test")

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: 9}) })
	}
	wg.Wait()
	if slow.maxInFlight != 1 {
		t.Errorf("document requests in flight at once: %d, want 1", slow.maxInFlight)
	}
}

// slowPaperless takes a moment over document requests and records how many
// overlap.
type slowPaperless struct {
	fakePaperless
	gate                  sync.Mutex
	inFlight, maxInFlight int
}

func (p *slowPaperless) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/custom_fields/" {
		time.Sleep(20 * time.Millisecond) // room for lookups to overlap
	}
	if strings.HasPrefix(r.URL.Path, "/api/documents/") {
		p.gate.Lock()
		p.inFlight++
		p.maxInFlight = max(p.maxInFlight, p.inFlight)
		p.gate.Unlock()
		time.Sleep(20 * time.Millisecond)
		defer func() {
			p.gate.Lock()
			p.inFlight--
			p.gate.Unlock()
		}()
	}
	p.fakePaperless.ServeHTTP(w, r)
}

// Documents linked at the same time create the missing field once, and a
// field of the configured name that isn't a URL field is never used.
func TestBacklinkFields(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)
	slow := &slowPaperless{fakePaperless: fakePaperless{
		fields: map[string]int64{"Guarantee years": 1},
		types:  map[string]string{"Guarantee years": "integer"},
		values: map[int64][]paperless.CustomField{50: {{Field: 1, Value: float64(2)}}},
	}}
	plServer := httptest.NewServer(slow)
	t.Cleanup(plServer.Close)
	alice, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	settings, _ := st.Settings(ctx, alice.ID)
	settings.PaperlessURL, settings.PaperlessToken, settings.PaperlessBacklinks = plServer.URL, "token", true
	st.SaveSettings(ctx, alice.ID, settings)
	months := 24
	wid, _ := st.CreateWarranty(ctx, alice.ID, &store.WarrantyInput{ProductName: "Grinder", PurchaseDate: "2025-03-01", WarrantyMonths: &months})
	for doc := int64(10); doc < 14; doc++ {
		d := doc
		st.CreateAttachment(ctx, &store.Attachment{WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless,
			Filename: "r.pdf", PaperlessDocID: &d, Status: store.StatusReady, AddedBy: &alice.ID})
	}
	appURL, _ := url.Parse("https://warranty.example.test")
	srv := New(&config.Config{AppURL: appURL, Paperless: config.Paperless{WarrantyField: "Warranty"}}, st, nil, nil, nil, nil, "test")

	var wg sync.WaitGroup
	for doc := int64(10); doc < 14; doc++ {
		wg.Go(func() {
			if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: doc}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if slow.creates != 1 {
		t.Errorf("the Warranty field was created %d times, want once", slow.creates)
	}
	for doc := int64(10); doc < 14; doc++ {
		if slow.value(doc, "Warranty") == "" {
			t.Errorf("document %d got no link", doc)
		}
	}

	// A field named like the configured one but holding numbers.
	srv.cfg.Paperless.WarrantyField = "Guarantee years"
	var fe *paperless.FieldTypeError
	if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: 50}); !errors.As(err, &fe) {
		t.Errorf("err = %v, want a field type error", err)
	}
	if v := slow.values[50]; len(v) != 1 || v[0].Value != float64(2) {
		t.Errorf("the number field was changed: %v", v)
	}
}

// fieldHomeBox serves items with their custom fields, as HomeBox's GET and
// PUT of an item and its item list filtered by a field do.
type fieldHomeBox struct {
	mu    sync.Mutex
	items map[string][]homebox.Field
	puts  int
}

func (f *fieldHomeBox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer bob-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/api/v1/entities" {
		name, value, _ := strings.Cut(r.URL.Query().Get("fields"), "=")
		out := []map[string]string{}
		for id, fields := range f.items {
			if slices.ContainsFunc(fields, func(fl homebox.Field) bool { return fl.Name == name && fl.TextValue == value }) {
				out = append(out, map[string]string{"id": id})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"items": out})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/entities/")
	fields, ok := f.items[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodPut {
		var body struct{ Fields []homebox.Field }
		json.NewDecoder(r.Body).Decode(&body)
		fields = body.Fields
		f.items[id] = fields
		f.puts++
	}
	json.NewEncoder(w).Encode(map[string]any{"id": id, "fields": fields})
}

// values lists the item's link field values.
func (f *fieldHomeBox) values(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, fl := range f.items[id] {
		if fl.Name == homebox.LinkField {
			out = append(out, fl.TextValue)
		}
	}
	slices.Sort(out)
	return out
}

func TestBacklinksSeveralHomeBoxItems(t *testing.T) {
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
	receipt, other := receiptValue(plServer.URL, 5), receiptValue(plServer.URL, 6)
	hb := &fieldHomeBox{items: map[string][]homebox.Field{
		// Marked with its own ID by an earlier version, which stays, as do
		// other fields.
		"item-1": {{Type: "text", Name: homebox.LinkField, TextValue: "item-1"}, {Type: "text", Name: "Colour", TextValue: "red"}},
		"item-2": nil,
		"item-3": nil,
		"item-4": nil,
		// No longer on the receipt.
		"item-9": {{Type: "text", Name: homebox.LinkField, TextValue: receipt}},
	}}
	hbServer := httptest.NewServer(hb)
	t.Cleanup(hbServer.Close)

	// Alice added the receipts from her Paperless; Bob, who shares the
	// warranties, linked them with his HomeBox key.
	alice, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	settings, _ := st.Settings(ctx, alice.ID)
	settings.PaperlessURL, settings.PaperlessToken, settings.PaperlessBacklinks = plServer.URL, "token", true
	st.SaveSettings(ctx, alice.ID, settings)
	bob, _ := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "bob", Name: "Bob"})
	bobSettings, _ := st.Settings(ctx, bob.ID)
	bobSettings.HomeBoxToken = "bob-key"
	st.SaveSettings(ctx, bob.ID, bobSettings)
	months := 24
	// Receipt 5 has items 1 to 3 and a warranty without one; receipt 6 has
	// items 2 and 4.
	warranty := map[string]int64{}
	for _, c := range []struct {
		item string
		docs []int64
	}{{"item-1", []int64{5}}, {"item-2", []int64{5, 6}}, {"item-3", []int64{5}}, {"", []int64{5}}, {"item-4", []int64{6}}} {
		wid, err := st.CreateWarranty(ctx, alice.ID, &store.WarrantyInput{ProductName: "Product " + c.item, PurchaseDate: "2025-03-01", WarrantyMonths: &months})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range c.docs {
			st.CreateAttachment(ctx, &store.Attachment{WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless, Filename: "r.pdf",
				PaperlessDocID: &d, Status: store.StatusReady, AddedBy: &alice.ID})
		}
		if c.item != "" {
			st.SetHomeBox(ctx, alice.ID, wid, &store.HomeBoxLink{ID: c.item})
		}
		warranty[c.item] = wid
	}

	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{
		AppURL:    appURL,
		Paperless: config.Paperless{WarrantyField: "Warranty", HomeBoxField: "HomeBox"},
		HomeBox:   config.HomeBox{URL: hbServer.URL, PublicURL: "https://homebox.example.test"},
	}
	srv := New(cfg, st, nil, nil, nil, nil, "test")
	sync := func(doc int64) {
		t.Helper()
		if err := srv.syncBacklinks(ctx, store.PaperlessDoc{Owner: alice.ID, DocID: doc}); err != nil {
			t.Fatal(err)
		}
	}
	sync(5)
	sync(6)
	for doc, value := range map[int64]string{5: receipt, 6: other} {
		want := "https://homebox.example.test/items?fields=Warranteed%3D" + value
		if got := pl.value(doc, "HomeBox"); got != want {
			t.Errorf("document %d: HomeBox link = %q, want %q", doc, got, want)
		}
	}
	for item, want := range map[string][]string{
		"item-1": {receipt, "item-1"}, "item-2": {receipt, other}, "item-3": {receipt}, "item-4": {other}, "item-9": nil,
	} {
		if got := hb.values(item); !slices.Equal(got, want) {
			t.Errorf("%s holds %v, want %v", item, got, want)
		}
	}
	if got := hb.items["item-1"]; len(got) != 3 || got[1].TextValue != "red" {
		t.Errorf("item-1's other field: %+v", got)
	}

	// Already marked: nothing more is written to HomeBox.
	puts := hb.puts
	sync(5)
	if hb.puts != puts {
		t.Error("HomeBox items were written again")
	}

	// Unlinked items come off the receipt's list; down to one, the receipt
	// links to it directly and the value goes.
	unlink := func(item string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, "/", nil)
		r.SetPathValue("id", fmt.Sprint(warranty[item]))
		w := httptest.NewRecorder()
		srv.unlinkHomeBox(w, r.WithContext(auth.WithUser(ctx, bob)))
		if w.Code != http.StatusOK {
			t.Fatalf("unlink: %d %s", w.Code, w.Body)
		}
		srv.backlinks.Wait()
	}
	unlink("item-3")
	if got := hb.values("item-3"); got != nil {
		t.Errorf("item-3 still holds %v", got)
	}
	unlink("item-4")
	if got := pl.value(6, "HomeBox"); got != "https://homebox.example.test/item/item-2" {
		t.Errorf("document 6: HomeBox link = %q", got)
	}
	if got := hb.values("item-2"); !slices.Equal(got, []string{receipt}) {
		t.Errorf("item-2 holds %v", got)
	}

	// A receipt that listed items and has none left: they lose its value.
	hb.mu.Lock()
	hb.items["item-9"] = []homebox.Field{{Type: "text", Name: homebox.LinkField, TextValue: other}}
	hb.mu.Unlock()
	pl.mu.Lock()
	for i, f := range pl.values[6] {
		if f.Field == pl.fields["HomeBox"] {
			pl.values[6][i].Value = homebox.ListURL("https://homebox.example.test", other)
		}
	}
	pl.mu.Unlock()
	unlink("item-2")
	if got := hb.values("item-9"); got != nil {
		t.Errorf("item-9 still holds %v", got)
	}
	if got := pl.value(6, "HomeBox"); got != "" {
		t.Errorf("document 6: HomeBox link = %q", got)
	}

	// HomeBox can't be reached: the first item is linked instead.
	hbServer.Close()
	sync(5)
	if got := pl.value(5, "HomeBox"); got != "https://homebox.example.test/item/item-1" {
		t.Errorf("HomeBox link = %q", got)
	}
}
