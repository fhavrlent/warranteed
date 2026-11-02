package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/thumb"
)

func bigJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3000, 2000)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (f *homeboxFixture) thumb(t *testing.T, id int64) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	f.srv.attachmentThumb(w, r.WithContext(auth.WithUser(r.Context(), f.bob)))
	return w
}

func checkPreview(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	cfg, err := jpeg.DecodeConfig(w.Body)
	if err != nil || cfg.Width != thumb.Size {
		t.Errorf("preview is %dx%d (%v)", cfg.Width, cfg.Height, err)
	}
}

func TestPhotoPreviews(t *testing.T) {
	f := newHomeBoxFixture(t)
	ctx := context.Background()
	dir := f.srv.files.dir

	// A photo stored here: its preview is made once, then kept.
	path, size, err := f.srv.files.Save(bytes.NewReader(bigJPEG(t)), ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	local := &store.Attachment{WarrantyID: f.warrantyID, Kind: "photo", Storage: store.StorageLocal, Filename: "p.jpg",
		MimeType: "image/jpeg", Size: size, LocalPath: path, Status: store.StatusReady, AddedBy: &f.bob.ID}
	if err := f.srv.store.CreateAttachment(ctx, local); err != nil {
		t.Fatal(err)
	}
	checkPreview(t, f.thumb(t, local.ID))
	cached := filepath.Join(dir, path+thumbExt)
	if _, err := os.Stat(cached); err != nil {
		t.Fatal("the preview wasn't kept")
	}
	os.WriteFile(filepath.Join(dir, path), []byte("gone"), 0o640) // served from the kept one
	checkPreview(t, f.thumb(t, local.ID))

	// A photo stored in HomeBox.
	f.hb.files = map[string]string{"photo-9": string(bigJPEG(t))}
	remote := &store.Attachment{WarrantyID: f.warrantyID, Kind: "photo", Storage: store.StorageHomeBox, Filename: "h.jpg",
		MimeType: "image/jpeg", HomeBoxItem: "item-1", HomeBoxAttachment: "photo-9", Status: store.StatusReady, AddedBy: &f.bob.ID}
	if err := f.srv.store.CreateAttachment(ctx, remote); err != nil {
		t.Fatal(err)
	}
	checkPreview(t, f.thumb(t, remote.ID))
	remoteCached := filepath.Join(dir, "homebox-photo-9"+thumbExt)
	if _, err := os.Stat(remoteCached); err != nil {
		t.Fatal("the HomeBox preview wasn't kept")
	}

	// An image Go can't read: the original is served.
	bad, _, _ := f.srv.files.Save(bytes.NewReader([]byte("\xff\xd8\xff not really")), ".jpg")
	odd := &store.Attachment{WarrantyID: f.warrantyID, Kind: "photo", Storage: store.StorageLocal, Filename: "o.jpg",
		MimeType: "image/jpeg", LocalPath: bad, Status: store.StatusReady, AddedBy: &f.bob.ID}
	f.srv.store.CreateAttachment(ctx, odd)
	if w := f.thumb(t, odd.ID); w.Code != http.StatusOK || w.Body.String() != "\xff\xd8\xff not really" {
		t.Errorf("got %d %q", w.Code, w.Body)
	}

	// Deleting the photos removes their previews.
	r := httptest.NewRequest(http.MethodDelete, "/", nil)
	r.SetPathValue("id", fmt.Sprint(local.ID))
	f.srv.deleteAttachment(httptest.NewRecorder(), r.WithContext(auth.WithUser(r.Context(), f.bob)))
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Error("the preview outlived its photo")
	}
	files, err := f.srv.store.DeleteWarranty(ctx, f.alice.ID, f.warrantyID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range files {
		f.srv.removeFiles(&a)
	}
	if _, err := os.Stat(remoteCached); !os.IsNotExist(err) {
		t.Error("the HomeBox preview outlived its warranty")
	}
}
