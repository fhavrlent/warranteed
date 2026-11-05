package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
