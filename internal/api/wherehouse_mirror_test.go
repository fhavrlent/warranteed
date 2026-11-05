package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
)

// changedThere makes the next sync see the thing changed in Wherehouse.
func (f *wherehouseFixture) changedThere(t *testing.T, thing int64) {
	t.Helper()
	f.wh.mu.Lock()
	f.wh.changes = []int64{thing}
	f.wh.mu.Unlock()
	if err := f.srv.syncWherehouseUser(context.Background(), f.alice.ID); err != nil {
		t.Fatal(err)
	}
	f.srv.background.Wait()
}

// deleteHere deletes an attachment the way the browser does.
func (f *wherehouseFixture) deleteHere(t *testing.T, id int64) {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r.SetPathValue("id", fmt.Sprint(id))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	f.srv.deleteAttachment(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	f.srv.background.Wait()
}

// photoThere adds a photo to the thing in Wherehouse.
func (f *wherehouseFixture) photoThere(thing int64, data string) int64 {
	f.wh.mu.Lock()
	defer f.wh.mu.Unlock()
	f.wh.last++
	if f.wh.photos == nil {
		f.wh.photos = map[int64][]byte{}
	}
	f.wh.photos[f.wh.last] = []byte(data)
	th := f.wh.things[thing]
	th.Photos = append(th.Photos, map[string]int64{"id": f.wh.last, "size": int64(len(data))})
	return f.wh.last
}

// documents returns the warranty's documents.
func (f *wherehouseFixture) documents(t *testing.T) []store.Attachment {
	t.Helper()
	var out []store.Attachment
	for _, a := range f.warranty(t, f.warrantyID).Attachments {
		if a.Kind != "photo" {
			out = append(out, a)
		}
	}
	return out
}

func TestWherehouseMirrorPhotos(t *testing.T) {
	f := newWherehouseFixture(t)
	f.photosOnly(t, true)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing

	// Added there, it shows here.
	photo := f.photoThere(thing, photoBytes)
	f.changedThere(t, thing)
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageWherehouse || *ps[0].WherehousePhoto != photo {
		t.Fatalf("photos after one was added there = %+v", ps)
	}
	if code, body := f.photoFile(t, ps[0].ID, false); code != 200 || body != photoBytes {
		t.Errorf("photo = %d %q", code, body)
	}
	// Seen again, it's still one.
	f.changedThere(t, thing)
	if ps := f.photos(t, f.warrantyID); len(ps) != 1 {
		t.Errorf("photos after another sync = %d", len(ps))
	}

	// Deleted there, it goes here.
	f.wh.mu.Lock()
	f.wh.things[thing].Photos = nil
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if ps := f.photos(t, f.warrantyID); len(ps) != 0 {
		t.Errorf("photos after it was deleted there = %+v", ps)
	}

	// Deleted here, it goes there.
	f.photoThere(thing, photoBytes)
	f.changedThere(t, thing)
	ps = f.photos(t, f.warrantyID)
	if len(ps) != 1 {
		t.Fatalf("photos = %+v", ps)
	}
	f.deleteHere(t, ps[0].ID)
	if th := f.wh.get(thing); len(th.Photos) != 0 {
		t.Errorf("thing photos after it was deleted here = %+v", th.Photos)
	}
	f.changedThere(t, thing)
	if ps := f.photos(t, f.warrantyID); len(ps) != 0 {
		t.Errorf("photos after it was deleted here = %+v", ps)
	}
}

// With the photos kept here, a copy goes there and none comes back.
func TestWherehousePhotosCopiedStayCopies(t *testing.T) {
	f := newWherehouseFixture(t)
	f.addPhoto(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.photoThere(thing, "another")
	f.changedThere(t, thing)
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageLocal {
		t.Fatalf("photos = %+v", ps)
	}
	f.deleteHere(t, ps[0].ID)
	if th := f.wh.get(thing); len(th.Photos) != 2 {
		t.Errorf("thing photos = %+v, want its 2", th.Photos)
	}
}

func TestWherehouseMirrorDocuments(t *testing.T) {
	f := newWherehouseFixture(t)
	f.wh.paperless = "http://paperless:8000/"
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()

	// Any document added here goes there, as its kind there.
	doc := int64(170)
	other := &store.Attachment{WarrantyID: f.warrantyID, Kind: "terms", Storage: store.StoragePaperless,
		Filename: "terms.pdf", MimeType: "application/pdf", PaperlessDocID: &doc, Status: store.StatusReady, AddedBy: &f.alice.ID}
	if err := f.srv.store.CreateAttachment(ctx, other); err != nil {
		t.Fatal(err)
	}
	f.srv.sendToWherehouseLater(f.alice.ID, f.warrantyID)
	f.srv.background.Wait()
	th := f.wh.get(thing)
	if len(th.Attachments) != 2 || th.Attachments[1]["kind"] != "warranty" || th.Attachments[1]["paperlessDocId"] != doc {
		t.Fatalf("thing documents = %+v", th.Attachments)
	}

	// Added there, they show here: one from Paperless as that document, a
	// file through Wherehouse.
	f.wh.mu.Lock()
	f.wh.last++
	manual := f.wh.last
	f.wh.last++
	file := f.wh.last
	f.wh.files = map[int64][]byte{file: []byte("%PDF scan")}
	f.wh.things[thing].Attachments = append(f.wh.things[thing].Attachments,
		map[string]any{"id": manual, "kind": "manual", "storage": "paperless", "paperlessDocId": int64(9),
			"filename": "manual.pdf", "mimeType": "application/pdf"},
		map[string]any{"id": file, "kind": "receipt", "storage": "local", "filename": "scan.pdf",
			"mimeType": "application/pdf", "size": 9})
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	docs := f.documents(t)
	if len(docs) != 4 {
		t.Fatalf("documents after two were added there = %+v", docs)
	}
	m, s := docs[2], docs[3]
	if m.Kind != "manual" || m.Storage != store.StoragePaperless || *m.PaperlessDocID != 9 {
		t.Errorf("document from Paperless = %+v", m)
	}
	if s.Kind != "purchase" || s.Storage != store.StorageWherehouse || s.Filename != "scan.pdf" {
		t.Errorf("file from Wherehouse = %+v", s)
	}
	if code, body := f.photoFile(t, s.ID, false); code != 200 || body != "%PDF scan" {
		t.Errorf("file = %d %q", code, body)
	}
	// Seen again, nothing is added twice, here or there.
	f.changedThere(t, thing)
	if n, there := len(f.documents(t)), len(f.wh.get(thing).Attachments); n != 4 || there != 4 {
		t.Errorf("after another sync %d here and %d there, want 4", n, there)
	}

	// Deleted there, it goes here.
	f.wh.mu.Lock()
	f.wh.things[thing].Attachments = f.wh.things[thing].Attachments[:3]
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if docs := f.documents(t); len(docs) != 3 {
		t.Errorf("documents after one was deleted there = %+v", docs)
	}

	// Deleted here, it goes there.
	f.deleteHere(t, other.ID)
	for _, a := range f.wh.get(thing).Attachments {
		if a["paperlessDocId"] == doc {
			t.Errorf("still there: %+v", a)
		}
	}
	f.changedThere(t, thing)
	docs = f.documents(t)
	if len(docs) != 2 || !docs[0].OnWherehouse {
		t.Errorf("documents after one was deleted here = %+v", docs)
	}

	// Unlinked, they're no longer on its thing.
	if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code >= 300 {
		t.Fatalf("unlink = %d %s", w.Code, w.Body)
	}
	for _, a := range f.documents(t) {
		if a.OnWherehouse {
			t.Errorf("still on the thing: %+v", a)
		}
	}
}

// Unlinked, a document kept on the thing comes here, and the thing keeps it.
func TestWherehouseDocumentsComeBack(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.wh.mu.Lock()
	f.wh.last++
	file := f.wh.last
	f.wh.files = map[int64][]byte{file: []byte("%PDF scan")}
	f.wh.things[thing].Attachments = append(f.wh.things[thing].Attachments,
		map[string]any{"id": file, "kind": "other", "storage": "local", "filename": "scan.pdf", "mimeType": "application/pdf"})
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code >= 300 {
		t.Fatalf("unlink = %d %s", w.Code, w.Body)
	}
	docs := f.documents(t)
	if len(docs) != 2 || docs[1].Storage != store.StorageLocal || !f.fileThere(docs[1].LocalPath) {
		t.Fatalf("documents after unlinking = %+v", docs)
	}
	if code, body := f.photoFile(t, docs[1].ID, false); code != 200 || body != "%PDF scan" {
		t.Errorf("file = %d %q", code, body)
	}
	// Deleting it here leaves the thing's.
	f.deleteHere(t, docs[1].ID)
	if n := len(f.wh.get(thing).Attachments); n != 1 {
		t.Errorf("thing documents = %d, want 1", n)
	}
}

// A photo kept in HomeBox that was copied into Wherehouse is the one there
// from now on, so it isn't on the warranty twice.
func TestWherehouseHomeBoxPhotoMoves(t *testing.T) {
	f := newWherehouseFixture(t)
	hb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/entities/item-1/attachments/att-1") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(photoBytes))
	}))
	t.Cleanup(hb.Close)
	f.srv.cfg.HomeBox.URL = hb.URL
	ctx := context.Background()
	st, _ := f.srv.store.Settings(ctx, f.alice.ID)
	st.HomeBoxToken = "alice-key"
	if err := f.srv.store.SaveSettings(ctx, f.alice.ID, st); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.CreateAttachment(ctx, &store.Attachment{WarrantyID: f.warrantyID, Kind: "photo",
		Storage: store.StorageHomeBox, Filename: "lamp.jpg", MimeType: "image/jpeg", Size: int64(len(photoBytes)),
		Status: store.StatusReady, AddedBy: &f.alice.ID, HomeBoxItem: "item-1", HomeBoxAttachment: "att-1"}); err != nil {
		t.Fatal(err)
	}
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	// The copy made of it there, and another photo.
	f.photoThere(thing, "another")
	copied := f.photoThere(thing, photoBytes)
	f.photosOnly(t, true)
	f.changedThere(t, thing)
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 2 {
		t.Fatalf("photos = %+v", ps)
	}
	if ps[0].Storage != store.StorageWherehouse || *ps[0].WherehousePhoto != copied || ps[0].HomeBoxItem != "" {
		t.Errorf("photo from HomeBox = %+v", ps[0])
	}
	if th := f.wh.get(thing); len(th.Photos) != 2 {
		t.Errorf("thing photos = %+v", th.Photos)
	}
}

// Documents added before the two were kept in step are brought in step once
// at start.
func TestWherehouseAllInStepAtStart(t *testing.T) {
	f := newWherehouseFixture(t)
	f.wh.paperless = "http://paperless:8000/"
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	doc := int64(170)
	if err := f.srv.store.CreateAttachment(context.Background(), &store.Attachment{WarrantyID: f.warrantyID, Kind: "other",
		Storage: store.StoragePaperless, Filename: "dummy.pdf", MimeType: "application/pdf", PaperlessDocID: &doc,
		Status: store.StatusReady, AddedBy: &f.alice.ID}); err != nil {
		t.Fatal(err)
	}
	f.srv.sendAllToWherehouse(context.Background(), f.alice.ID)
	if th := f.wh.get(thing); len(th.Attachments) != 2 || th.Attachments[1]["kind"] != "other" {
		t.Errorf("thing documents = %+v", th.Attachments)
	}
}
