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

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/store"
)

// fakeHomeBox records what Warranteed asks of HomeBox. Creating an item
// waits on release when it is set, to hold a request mid-way.
type fakeHomeBox struct {
	mu      sync.Mutex
	creates int
	links   []string
	created chan struct{}
	release chan struct{}
}

func (f *fakeHomeBox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const item = `{"id":"item-1","assetId":"000-001","name":"Dishwasher","attachments":[]}`
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/entities":
		f.mu.Lock()
		f.creates++
		f.mu.Unlock()
		if f.release != nil {
			f.created <- struct{}{}
			<-f.release
		}
		w.Write([]byte(item))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments/external"):
		var body struct {
			URL string `json:"external_id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.links = append(f.links, body.URL)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	default: // GET and PUT of the item
		w.Write([]byte(item))
	}
}

type homeboxFixture struct {
	srv        *Server
	hb         *fakeHomeBox
	bob        *store.User
	warrantyID int64
}

// A household where Alice added the receipt from her Paperless and Bob, who
// has a HomeBox key but no Paperless, links the warranty.
func newHomeBoxFixture(t *testing.T) *homeboxFixture {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	st := store.New(database)

	alice, err := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "alice", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "bob", Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	aliceSettings, _ := st.Settings(ctx, alice.ID)
	aliceSettings.PaperlessURL = "http://paperless:8000"
	aliceSettings.PaperlessPublicURL = "https://paperless.example.test"
	aliceSettings.PaperlessToken = "alice-token"
	bobSettings, _ := st.Settings(ctx, bob.ID)
	bobSettings.HomeBoxToken = "bob-key"
	if st.SaveSettings(ctx, alice.ID, aliceSettings) != nil || st.SaveSettings(ctx, bob.ID, bobSettings) != nil {
		t.Fatal("saving settings failed")
	}

	months := 24
	wid, err := st.CreateWarranty(ctx, alice.ID, &store.WarrantyInput{ProductName: "Dishwasher", PurchaseDate: "2025-03-01", WarrantyMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	doc := int64(7)
	if err := st.CreateAttachment(ctx, &store.Attachment{
		WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless, Filename: "receipt.pdf",
		MimeType: "application/pdf", PaperlessDocID: &doc, Status: store.StatusReady, AddedBy: &alice.ID,
	}); err != nil {
		t.Fatal(err)
	}

	hb := &fakeHomeBox{}
	hbServer := httptest.NewServer(hb)
	t.Cleanup(hbServer.Close)
	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{AppURL: appURL, HomeBox: config.HomeBox{URL: hbServer.URL, PublicURL: hbServer.URL}}
	return &homeboxFixture{srv: New(cfg, st, nil, nil, nil, nil, "test"), hb: hb, bob: bob, warrantyID: wid}
}

func (f *homeboxFixture) add(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.bob))
	w := httptest.NewRecorder()
	f.srv.createHomeBoxItem(w, r)
	return w
}

func TestHomeBoxReceiptLinkUsesTheOwnersPaperless(t *testing.T) {
	f := newHomeBoxFixture(t)
	if w := f.add(t); w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	want := []string{
		fmt.Sprintf("https://warranty.example.test/w/%d", f.warrantyID),
		"https://paperless.example.test/documents/7/details",
	}
	if strings.Join(f.hb.links, " ") != strings.Join(want, " ") {
		t.Errorf("links = %v, want %v", f.hb.links, want)
	}
}

func TestHomeBoxAddIsOneAtATime(t *testing.T) {
	f := newHomeBoxFixture(t)
	f.hb.created, f.hb.release = make(chan struct{}), make(chan struct{})

	first := make(chan *httptest.ResponseRecorder)
	go func() { first <- f.add(t) }()
	<-f.hb.created // the first request is now creating the item

	if w := f.add(t); w.Code != http.StatusConflict {
		t.Errorf("second add while the first runs: %d, want 409", w.Code)
	}
	close(f.hb.release)
	if w := <-first; w.Code != http.StatusOK {
		t.Fatalf("first add: %d %s", w.Code, w.Body)
	}
	if w := f.add(t); w.Code != http.StatusConflict {
		t.Errorf("add after linking: %d, want 409", w.Code)
	}
	if f.hb.creates != 1 {
		t.Errorf("HomeBox items created: %d, want 1", f.hb.creates)
	}
}
