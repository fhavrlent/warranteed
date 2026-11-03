package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/store"
)

func TestSplitWarranty(t *testing.T) {
	f := newHomeBoxFixture(t)
	ctx := t.Context()
	wr, _ := f.srv.store.GetWarranty(ctx, f.bob.ID, f.warrantyID)
	in := inputOf(wr)
	in.Quantity = 2
	if err := f.srv.store.UpdateWarranty(ctx, f.bob.ID, f.warrantyID, in); err != nil {
		t.Fatal(err)
	}
	f.addPhoto(t, "front.jpg")
	if w := f.link(t); w.Code != 200 {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	creates := f.hb.creates

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pieces":1,"homebox":true}`))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	w := httptest.NewRecorder()
	f.srv.splitWarranty(w, r.WithContext(auth.WithUser(r.Context(), f.bob)))
	f.srv.background.Wait()
	if w.Code != http.StatusCreated {
		t.Fatalf("split: %d %s", w.Code, w.Body)
	}
	var out struct {
		Warranty     store.Warranty `json:"warranty"`
		HomeBoxError string         `json:"homeboxError"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	if out.HomeBoxError != "" {
		t.Errorf("homebox: %s", out.HomeBoxError)
	}
	if f.hb.creates != creates+1 || out.Warranty.HomeBox == nil {
		t.Errorf("no HomeBox item made for the new warranty: %d creates, link %v", f.hb.creates-creates, out.Warranty.HomeBox)
	}
	orig, _ := f.srv.store.GetWarranty(ctx, f.bob.ID, f.warrantyID)
	if orig.Quantity != 1 || out.Warranty.Quantity != 1 {
		t.Errorf("quantities %d and %d", orig.Quantity, out.Warranty.Quantity)
	}

	// Both have the photo, each from its own file, and the Paperless receipt.
	read := func(wr *store.Warranty, kind string) (string, *store.Attachment) {
		for _, a := range wr.Attachments {
			if a.Kind == kind {
				full, _ := f.srv.store.GetAttachment(ctx, f.bob.ID, a.ID)
				if full.Storage != store.StorageLocal {
					return "", full
				}
				file, err := f.srv.files.Open(full.LocalPath)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				b, _ := io.ReadAll(file)
				return string(b), full
			}
		}
		t.Fatalf("no %s on warranty %d", kind, wr.ID)
		return "", nil
	}
	origPhoto, a1 := read(orig, "photo")
	newPhoto, a2 := read(&out.Warranty, "photo")
	if a1.LocalPath == a2.LocalPath || origPhoto != newPhoto || !bytes.HasPrefix([]byte(newPhoto), []byte("\xff\xd8\xff")) {
		t.Errorf("photo copies: %q %q", a1.LocalPath, a2.LocalPath)
	}
	if _, receipt := read(&out.Warranty, "purchase"); receipt.PaperlessDocID == nil || *receipt.PaperlessDocID != 7 {
		t.Errorf("receipt: %+v", receipt)
	}
	// It keeps who added the original, who can delete it. Deleting one
	// leaves the other's file.
	if out.Warranty.AddedBy.ID != f.alice.ID {
		t.Errorf("added by %d, want Alice", out.Warranty.AddedBy.ID)
	}
	if _, err := f.srv.store.DeleteWarranty(ctx, f.alice.ID, out.Warranty.ID); err != nil {
		t.Fatal(err)
	}
	f.srv.files.Remove(a2.LocalPath)
	if again, _ := read(orig, "photo"); again != origPhoto {
		t.Error("the original's photo went with the split-off warranty")
	}
}

// A photo stored in HomeBox goes on the split-off warranty's own item too.
func TestSplitCopiesPhotosStoredInHomeBox(t *testing.T) {
	f := newHomeBoxFixture(t)
	ctx := t.Context()
	wr, _ := f.srv.store.GetWarranty(ctx, f.bob.ID, f.warrantyID)
	in := inputOf(wr)
	in.Quantity = 2
	if err := f.srv.store.UpdateWarranty(ctx, f.bob.ID, f.warrantyID, in); err != nil {
		t.Fatal(err)
	}
	if w := f.link(t); w.Code != 200 {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	if w := f.upload(t, "front.jpg", store.StorageHomeBox); w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	f.hb.newItem = "item-2"

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"pieces":1,"homebox":true}`))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	w := httptest.NewRecorder()
	f.srv.splitWarranty(w, r.WithContext(auth.WithUser(r.Context(), f.bob)))
	f.srv.background.Wait()
	var out struct {
		Warranty     store.Warranty `json:"warranty"`
		HomeBoxError string         `json:"homeboxError"`
	}
	json.NewDecoder(w.Body).Decode(&out)
	if w.Code != http.StatusCreated || out.HomeBoxError != "" {
		t.Fatalf("split: %d %s", w.Code, out.HomeBoxError)
	}
	photo := func(id int64) *store.Attachment {
		wr, _ := f.srv.store.GetWarranty(ctx, f.bob.ID, id)
		for _, a := range wr.Attachments {
			if a.Kind == "photo" {
				full, _ := f.srv.store.GetAttachment(ctx, f.bob.ID, a.ID)
				return full
			}
		}
		t.Fatalf("no photo on warranty %d", id)
		return nil
	}
	orig, split := photo(f.warrantyID), photo(out.Warranty.ID)
	if orig.HomeBoxItem != "item-1" || split.HomeBoxItem != "item-2" || split.HomeBoxAttachment == orig.HomeBoxAttachment {
		t.Errorf("photos on %s/%s and %s/%s", orig.HomeBoxItem, orig.HomeBoxAttachment, split.HomeBoxItem, split.HomeBoxAttachment)
	}
	if got, want := f.hb.files[split.HomeBoxAttachment], f.hb.files[orig.HomeBoxAttachment]; got != want {
		t.Errorf("copied photo %q, want %q", got, want)
	}
}

func TestShrinkHomeBoxItem(t *testing.T) {
	st := &store.Settings{Language: "en"}
	months := 24
	before := &store.Warranty{Quantity: 3, WarrantyMonths: &months, SerialNumbers: []string{"A", "B", "C"}}
	rest := &store.Warranty{Quantity: 2, WarrantyMonths: &months, SerialNumbers: []string{"B", "C"}}

	it := &homebox.Item{Quantity: 3, SerialNumber: "A", WarrantyDetails: warrantyDetails(before, st)}
	shrinkHomeBoxItem(it, before, rest, st)
	if it.Quantity != 2 || it.SerialNumber != "B" || it.WarrantyDetails != warrantyDetails(rest, st) {
		t.Errorf("got %d, %q, %q", it.Quantity, it.SerialNumber, it.WarrantyDetails)
	}

	// What was changed in HomeBox stays.
	it = &homebox.Item{Quantity: 5, SerialNumber: "X", WarrantyDetails: "my notes"}
	shrinkHomeBoxItem(it, before, rest, st)
	if it.Quantity != 5 || it.SerialNumber != "X" || it.WarrantyDetails != "my notes" {
		t.Errorf("hand-set values changed: %d, %q, %q", it.Quantity, it.SerialNumber, it.WarrantyDetails)
	}
}
