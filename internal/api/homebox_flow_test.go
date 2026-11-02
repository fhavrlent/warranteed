package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
	photos  []string // "name primary"
	created chan struct{}
	release chan struct{}
	// attachments are the item's attachments as JSON objects.
	attachments []string
	// files are the uploaded photos by attachment ID.
	files map[string]string
}

func (f *fakeHomeBox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	item := `{"id":"item-1","assetId":"000-001","name":"Dishwasher","attachments":[` + strings.Join(f.attachments, ",") + `]}`
	f.mu.Unlock()
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
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments"):
		file, hdr, err := r.FormFile("file")
		if err != nil || r.FormValue("type") != "photo" || r.FormValue("name") != hdr.Filename {
			http.Error(w, "bad upload", http.StatusUnprocessableEntity)
			return
		}
		data, _ := io.ReadAll(file)
		file.Close()
		f.mu.Lock()
		f.photos = append(f.photos, hdr.Filename+" "+r.FormValue("primary"))
		id := fmt.Sprintf("photo-%d", len(f.photos))
		if f.files == nil {
			f.files = map[string]string{}
		}
		f.files[id] = string(data)
		f.attachments = append(f.attachments, fmt.Sprintf(`{"id":%q,"createdAt":"2026-01-01T10:00:%02d.5Z","type":"photo","primary":%s}`,
			id, len(f.photos), r.FormValue("primary")))
		item = `{"id":"item-1","attachments":[` + strings.Join(f.attachments, ",") + `]}`
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(item))
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/attachments/"):
		f.mu.Lock()
		data, ok := f.files[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]
		f.mu.Unlock()
		if !ok || r.Header.Get("Authorization") != "Bearer bob-key" {
			http.Error(w, "no such attachment", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(data))
	default: // GET and PUT of the item
		w.Write([]byte(item))
	}
}

type homeboxFixture struct {
	srv        *Server
	hb         *fakeHomeBox
	alice, bob *store.User
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

	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hb := &fakeHomeBox{}
	hbServer := httptest.NewServer(hb)
	t.Cleanup(hbServer.Close)
	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{AppURL: appURL, MaxUploadMB: 10, HomeBox: config.HomeBox{URL: hbServer.URL, PublicURL: hbServer.URL}}
	return &homeboxFixture{srv: New(cfg, st, nil, nil, files, nil, "test"), hb: hb, alice: alice, bob: bob, warrantyID: wid}
}

// addPhoto stores a product photo on the warranty, as an upload would.
func (f *homeboxFixture) addPhoto(t *testing.T, name string) {
	t.Helper()
	path, size, err := f.srv.files.Save(strings.NewReader("\xff\xd8\xff photo"), ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.CreateAttachment(context.Background(), &store.Attachment{
		WarrantyID: f.warrantyID, Kind: "photo", Storage: store.StorageLocal, Filename: name,
		MimeType: "image/jpeg", Size: size, LocalPath: path, Status: store.StatusReady, AddedBy: &f.bob.ID,
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *homeboxFixture) link(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"itemId":"item-1"}`))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.bob))
	w := httptest.NewRecorder()
	f.srv.linkHomeBoxItem(w, r)
	return w
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

func TestHomeBoxGetsPhotosOnce(t *testing.T) {
	f := newHomeBoxFixture(t)
	f.addPhoto(t, "front.jpg")
	f.addPhoto(t, "back.jpg")
	if w := f.add(t); w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	if got, want := strings.Join(f.hb.photos, ", "), "front.jpg true, back.jpg false"; got != want {
		t.Fatalf("photos = %s, want %s", got, want)
	}

	// A photo added later is sent on its own, and the others aren't sent again.
	f.addPhoto(t, "side.jpg")
	hb := f.srv.homeboxFor(&store.Settings{HomeBoxToken: "bob-key"})
	for range 2 {
		if err := f.srv.sendHomeBoxPhotos(context.Background(), hb, f.bob.ID, f.warrantyID); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := strings.Join(f.hb.photos, ", "), "front.jpg true, back.jpg false, side.jpg false"; got != want {
		t.Errorf("photos = %s, want %s", got, want)
	}
}

func TestHomeBoxKeepsTheItemsMainPhoto(t *testing.T) {
	f := newHomeBoxFixture(t)
	f.hb.attachments = []string{`{"id":"theirs","type":"photo","primary":true}`}
	f.addPhoto(t, "front.jpg")
	if w := f.link(t); w.Code != http.StatusOK {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	if got, want := strings.Join(f.hb.photos, ", "), "front.jpg false"; got != want {
		t.Errorf("photos = %s, want %s", got, want)
	}
}

// upload posts a photo to the warranty as Bob, stored where storage says.
func (f *homeboxFixture) upload(t *testing.T, name, storage string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("kind", "photo")
	mw.WriteField("storage", storage)
	part, _ := mw.CreateFormFile("file", name)
	part.Write([]byte("\xff\xd8\xff photo of " + name))
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.bob))
	w := httptest.NewRecorder()
	f.srv.uploadAttachment(w, r)
	return w
}

func TestPhotoStoredInHomeBox(t *testing.T) {
	f := newHomeBoxFixture(t)
	if w := f.upload(t, "front.jpg", store.StorageHomeBox); w.Code != http.StatusBadRequest {
		t.Errorf("upload before linking: %d, want 400", w.Code)
	}
	if w := f.link(t); w.Code != http.StatusOK {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	w := f.upload(t, "front.jpg", store.StorageHomeBox)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	var a store.Attachment
	json.Unmarshal(w.Body.Bytes(), &a)
	if a.Storage != store.StorageHomeBox {
		t.Errorf("storage = %q, want homebox", a.Storage)
	}
	if w := f.upload(t, "back.jpg", store.StorageHomeBox); w.Code != http.StatusCreated {
		t.Fatalf("second upload: %d %s", w.Code, w.Body)
	}
	if got, want := strings.Join(f.hb.photos, ", "), "front.jpg true, back.jpg false"; got != want {
		t.Errorf("photos = %s, want %s", got, want)
	}

	// Alice has no HomeBox key, but sees the photo through Bob's.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", fmt.Sprint(a.ID))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	rec := httptest.NewRecorder()
	f.srv.attachmentFile(rec, r)
	if rec.Code != http.StatusOK || rec.Body.String() != "\xff\xd8\xff photo of front.jpg" {
		t.Errorf("view: %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content type %q", ct)
	}

	// Photos stored in HomeBox aren't sent to it again.
	hb := f.srv.homeboxFor(&store.Settings{HomeBoxToken: "bob-key"})
	if err := f.srv.sendHomeBoxPhotos(context.Background(), hb, f.bob.ID, f.warrantyID); err != nil {
		t.Fatal(err)
	}
	if len(f.hb.photos) != 2 {
		t.Errorf("photos sent again: %v", f.hb.photos)
	}
}
