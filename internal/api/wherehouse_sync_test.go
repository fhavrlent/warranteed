package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

// With nobody connected, nothing can tell whether a photo kept in Wherehouse
// is still there, so it isn't taken for gone.
func TestWherehousePhotoUncheckedIsNotGone(t *testing.T) {
	on, err := photoOnThing(context.Background(), nil, 1, 2)
	if err == nil || on {
		t.Errorf("photoOnThing with no connection = %v, %v; want an error", on, err)
	}
}

// People sharing a warranty may be connected to different households: one
// connection not finding something there doesn't make it gone.
func TestWherehouseNotFoundByOneConnection(t *testing.T) {
	var deleted bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer other.Close()
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleted = r.Method == http.MethodDelete
		w.WriteHeader(http.StatusNoContent)
	}))
	defer owner.Close()
	clients := []*wherehouse.Client{wherehouse.New(other.URL, "a"), wherehouse.New(owner.URL, "b")}
	found, err := foundOnWherehouse(clients, func(wh *wherehouse.Client) error {
		return wh.DeleteAttachment(context.Background(), 7)
	})
	if err != nil || !found || !deleted {
		t.Errorf("found = %v, err = %v, deleted by the owner's connection = %v", found, err, deleted)
	}
	found, err = foundOnWherehouse(clients[:1], func(wh *wherehouse.Client) error {
		return wh.DeleteAttachment(context.Background(), 7)
	})
	if err != nil || found {
		t.Errorf("found by none = %v, %v", found, err)
	}
}

// A document deleted while it's being sent to the thing is deleted there
// too, as it is once the sending is done.
func TestWherehouseDeleteWaitsForSending(t *testing.T) {
	f := newWherehouseFixture(t)
	f.wh.paperless = "http://paperless:8000/"
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	receipt := f.documents(t)[0]
	ctx := context.Background()
	// Not yet known to be there when the delete starts.
	if err := f.srv.store.NotOnWherehouseThing(ctx, receipt.ID); err != nil {
		t.Fatal(err)
	}
	unlock := f.srv.lockPhotos(wherehousePhotosKey(f.warrantyID))
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.deleteHere(t, receipt.ID)
	}()
	time.Sleep(50 * time.Millisecond)
	there := int64(f.wh.get(thing).Attachments[0]["id"].(int64))
	if err := f.srv.store.OnWherehouseThing(ctx, receipt.ID, thing, there, ""); err != nil {
		t.Fatal(err)
	}
	unlock()
	<-done
	if as := f.wh.get(thing).Attachments; len(as) != 0 {
		t.Errorf("thing documents = %+v", as)
	}
}

// A claim's entry added after the thing was read isn't taken for deleted.
func TestWherehouseClaimEntryNewerThanRead(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	wr := f.warranty(t, f.warrantyID)
	st, err := f.srv.store.Settings(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	wh := f.srv.wherehouseFor(st)
	before, err := wh.Get(ctx, wr.Wherehouse.Thing)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Broken lid", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.syncWherehouseClaim(ctx, f.alice.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.mirrorClaims(ctx, f.alice.ID, st, wh, wr, before); err != nil {
		t.Fatal(err)
	}
	f.srv.background.Wait()
	if _, err := f.srv.store.GetClaim(ctx, f.alice.ID, c.ID); err != nil {
		t.Errorf("claim: %v", err)
	}
}

// A thing restored under another ID keeps its claims' entries: editing a
// claim changes the restored entry rather than adding another.
func TestWherehouseRestoredThingKeepsClaimEntries(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	old := f.warranty(t, f.warrantyID).Wherehouse.Thing
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Broken lid", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()

	f.wh.mu.Lock()
	restored := f.wh.add("Lamp", nil)
	restored.app = f.wh.things[old].app
	restored.Maintenance = append(restored.Maintenance, f.wh.things[old].Maintenance...)
	f.wh.last++
	restored.Maintenance[0].ID = f.wh.last
	delete(f.wh.things, old)
	f.wh.mu.Unlock()
	if err := f.srv.adoptWherehouse(ctx, f.alice.ID, true); err != nil {
		t.Fatal(err)
	}
	f.srv.background.Wait()
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse == nil || wr.Wherehouse.Thing != restored.ID {
		t.Fatalf("link = %+v", wr.Wherehouse)
	}

	if _, err := f.srv.store.UpdateClaim(ctx, f.alice.ID, c.ID, &store.ClaimInput{Title: "Broken lid hinge", ReportedOn: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()
	f.changedThere(t, restored.ID)
	ms := f.wh.get(restored.ID).Maintenance
	if len(ms) != 1 || ms[0].Title != "Warranty claim: Broken lid hinge" {
		t.Errorf("entries = %+v", ms)
	}
	if _, err := f.srv.store.GetClaim(ctx, f.alice.ID, c.ID); err != nil {
		t.Errorf("claim: %v", err)
	}
}

// Linked to another thing, a warranty compares that thing with nothing it
// saw of the one before, so their differences stay.
func TestWherehouseRelinkedStartsAfresh(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	first := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.changedThere(t, first)
	if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code/100 != 2 {
		t.Fatalf("unlink = %d %s", w.Code, w.Body)
	}
	f.wh.mu.Lock()
	other := f.wh.add("Lamp in the hall", nil)
	other.Serial, other.Notes = "OLD", "By the door"
	f.wh.mu.Unlock()
	if w := f.call(t, f.srv.linkWherehouseThing, http.MethodPut, fmt.Sprintf(`{"thingId":%d}`, other.ID)); w.Code != 200 {
		t.Fatalf("link = %d %s", w.Code, w.Body)
	}
	f.changedThere(t, other.ID)
	f.changedThere(t, other.ID)
	if wr := f.warranty(t, f.warrantyID); wr.ProductName != "Lamp" || len(wr.SerialNumbers) != 0 || wr.Notes != "" {
		t.Errorf("warranty = %+v", wr)
	}
}

// A thing whose change was read but whose warranty couldn't be brought in
// step with it is tried again, though Wherehouse's changes are read past it.
func TestWherehouseFailedSyncTriedAgain(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.changedThere(t, thing)
	f.wh.mu.Lock()
	f.wh.things[thing].Notes = "On the desk"
	f.wh.failGets = 1
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if wr := f.warranty(t, f.warrantyID); wr.Notes != "" {
		t.Fatalf("notes taken although the thing couldn't be read: %q", wr.Notes)
	}
	// Nothing changed there since.
	f.wh.mu.Lock()
	f.wh.changes = nil
	f.wh.mu.Unlock()
	if err := f.srv.syncWherehouseUser(context.Background(), f.alice.ID); err != nil {
		t.Fatal(err)
	}
	f.srv.background.Wait()
	if wr := f.warranty(t, f.warrantyID); wr.Notes != "On the desk" {
		t.Errorf("notes after another sync = %q", wr.Notes)
	}
}

// A warranty changed here while its thing was read keeps that change, and
// still takes what changed there.
func TestWherehouseDetailsChangedMeanwhile(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.changedThere(t, thing)
	f.wh.mu.Lock()
	f.wh.things[thing].Notes = "On the desk"
	f.wh.mu.Unlock()
	stale := f.warranty(t, f.warrantyID)
	st, err := f.srv.store.Settings(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	wh := f.srv.wherehouseFor(st)
	th, err := wh.Get(ctx, thing)
	if err != nil {
		t.Fatal(err)
	}
	// Saved here meanwhile.
	months := 24
	if err := f.srv.store.UpdateWarranty(ctx, f.alice.ID, f.warrantyID, &store.WarrantyInput{ProductName: "Desk lamp",
		PurchaseDate: stale.PurchaseDate, WarrantyMonths: &months, Quantity: 1, SerialNumbers: []string{"S9"}}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.mirrorDetails(ctx, f.alice.ID, wh, stale, th); err != nil {
		t.Fatal(err)
	}
	wr := f.warranty(t, f.warrantyID)
	if wr.ProductName != "Desk lamp" || len(wr.SerialNumbers) != 1 || wr.SerialNumbers[0] != "S9" || wr.Notes != "On the desk" {
		t.Errorf("warranty = %q %v %q", wr.ProductName, wr.SerialNumbers, wr.Notes)
	}
	if got := f.wh.get(thing); got.Name != "Desk lamp" || got.Serial != "S9" || got.Notes != "On the desk" {
		t.Errorf("thing = %+v", got)
	}
}

// A thing made for a warranty has all the details they share, and one made
// before that has what it lacks filled in.
func TestWherehouseNewThingHasEveryDetail(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	wr := f.warranty(t, f.warrantyID)
	months := 24
	if err := f.srv.store.UpdateWarranty(ctx, f.alice.ID, f.warrantyID, &store.WarrantyInput{ProductName: wr.ProductName,
		PurchaseDate: wr.PurchaseDate, WarrantyMonths: &months, Quantity: 1, Notes: "Support: 123",
		SerialNumbers: []string{"A1", "B2"}}); err != nil {
		t.Fatal(err)
	}
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	if got := f.wh.get(thing); got.Serial != "A1, B2" || got.Notes != "Support: 123" {
		t.Fatalf("new thing = %+v", got)
	}

	// Made before, with the first serial number only and no notes.
	if err := f.srv.store.SetWherehouseFields(ctx, f.warrantyID, ""); err != nil {
		t.Fatal(err)
	}
	f.wh.mu.Lock()
	f.wh.things[thing].Serial, f.wh.things[thing].Notes = "A1", ""
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if got := f.wh.get(thing); got.Serial != "A1, B2" || got.Notes != "Support: 123" {
		t.Errorf("thing made before = %+v", got)
	}
	if wr := f.warranty(t, f.warrantyID); len(wr.SerialNumbers) != 2 || wr.Notes != "Support: 123" {
		t.Errorf("warranty = %v %q", wr.SerialNumbers, wr.Notes)
	}
}

// A photo stored in HomeBox on a warranty whose photos are kept only in
// Wherehouse goes to the thing straight away, like any other photo.
func TestWherehouseHomeBoxUploadGoesThere(t *testing.T) {
	f := newWherehouseFixture(t)
	hb := &fakeHomeBox{}
	hbServer := httptest.NewServer(hb)
	t.Cleanup(hbServer.Close)
	f.srv.cfg.HomeBox.URL, f.srv.cfg.HomeBox.PublicURL = hbServer.URL, hbServer.URL
	ctx := context.Background()
	st, _ := f.srv.store.Settings(ctx, f.alice.ID)
	st.HomeBoxToken = "bob-key" // the key the fake HomeBox hands photos to
	if err := f.srv.store.SaveSettings(ctx, f.alice.ID, st); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.SetHomeBox(ctx, f.alice.ID, f.warrantyID, &store.HomeBoxLink{ID: "item-1"}); err != nil {
		t.Fatal(err)
	}
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	f.photosOnly(t, true)
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("kind", "photo")
	mw.WriteField("storage", store.StorageHomeBox)
	part, _ := mw.CreateFormFile("file", "front.jpg")
	part.Write([]byte(photoBytes))
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	f.srv.uploadAttachment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", w.Code, w.Body)
	}
	var a store.Attachment
	json.Unmarshal(w.Body.Bytes(), &a)
	f.srv.background.Wait()
	if th := f.wh.get(thing); len(th.Photos) != 1 {
		t.Errorf("thing photos = %+v", th.Photos)
	}
	for _, p := range f.photos(t, f.warrantyID) {
		if p.ID == a.ID && p.Storage != store.StorageWherehouse {
			t.Errorf("photo kept in %s", p.Storage)
		}
	}
}

// A claim changed here while its entry's change there was being taken
// keeps the change made here, which then goes there.
func TestWherehouseClaimChangedMeanwhile(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Broken lid", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()
	stale, _, err := f.srv.store.ClaimSync(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Saved here after the claim was read.
	if _, err := f.srv.store.UpdateClaim(ctx, f.alice.ID, c.ID, &store.ClaimInput{Title: "Broken hinge", ReportedOn: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	in := store.ClaimInput{Title: "Cracked lid", ReportedOn: "2026-03-01"}
	if ok, err := f.srv.store.UpdateClaimIfUnchanged(ctx, f.alice.ID, c.ID, stale, &in); err != nil || ok {
		t.Errorf("update of a claim changed meanwhile = %v, %v", ok, err)
	}
	if got, _ := f.srv.store.GetClaim(ctx, f.alice.ID, c.ID); got.Title != "Broken hinge" {
		t.Errorf("claim = %q", got.Title)
	}
	// Unchanged, it's changed.
	now, _, _ := f.srv.store.ClaimSync(ctx, c.ID)
	if ok, err := f.srv.store.UpdateClaimIfUnchanged(ctx, f.alice.ID, c.ID, now, &in); err != nil || !ok {
		t.Errorf("update of an unchanged claim = %v, %v", ok, err)
	}
}

// A split-off warranty's copy of a photo kept in Wherehouse is the same
// photo there until it's settled: deleting the copy leaves the original's.
func TestWherehouseSplitCopyDeletedLeavesOriginal(t *testing.T) {
	f := newWherehouseFixture(t)
	f.photosOnly(t, true)
	f.addPhoto(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	// Settling the copy fails: Wherehouse doesn't answer.
	f.whServer.Close()
	w := f.call(t, f.srv.splitWarranty, http.MethodPost, `{"pieces":1,"serialNumbers":[],"wherehouse":false}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("split = %d %s", w.Code, w.Body)
	}
	var out struct {
		Warranty store.Warranty `json:"warranty"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	ps := f.photos(t, out.Warranty.ID)
	if len(ps) != 1 || ps[0].Storage != store.StorageWherehouse {
		t.Fatalf("split-off photos = %+v", ps)
	}
	f.deleteHere(t, ps[0].ID)
	if th := f.wh.get(thing); len(th.Photos) != 1 {
		t.Errorf("original's thing photos = %+v", th.Photos)
	}
	if o := f.photos(t, f.warrantyID); len(o) != 1 {
		t.Errorf("original's photos = %+v", o)
	}
}
