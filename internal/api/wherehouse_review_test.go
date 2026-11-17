package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/store"
)

// errorCode is the message key of an API error.
func errorCode(w *httptest.ResponseRecorder) string {
	var out struct{ Code string }
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Code
}

// members sets how many people share the fake Wherehouse's household.
func (f *wherehouseFixture) members(n int) {
	f.wh.mu.Lock()
	f.wh.members = n
	f.wh.mu.Unlock()
}

// makePrivate moves the warranty into Alice's own space ("Only me").
func (f *wherehouseFixture) makePrivate(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	spaces, err := f.srv.store.ListSpaces(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spaces {
		if sp.Kind == store.SpacePersonal {
			if err := f.srv.store.UpdateWarranty(ctx, f.alice.ID, f.warrantyID, f.input(t, &sp.ID)); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("no personal space")
}

// input is the warranty as it is, to save it again, in space if not nil.
func (f *wherehouseFixture) input(t *testing.T, space *int64) *store.WarrantyInput {
	t.Helper()
	wr := f.warranty(t, f.warrantyID)
	return &store.WarrantyInput{ProductName: wr.ProductName, PurchaseDate: wr.PurchaseDate,
		WarrantyMonths: wr.WarrantyMonths, Quantity: wr.Quantity, Vendor: wr.Vendor, SpaceID: space}
}

// A private warranty isn't linked to a thing in a household others share,
// and no thing is made for it there.
func TestWherehousePrivateNotLinkedToSharedHousehold(t *testing.T) {
	f := newWherehouseFixture(t)
	f.makePrivate(t)
	f.members(2)
	w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`)
	if w.Code != http.StatusConflict || errorCode(w) != "server.wherehousePrivateShared" {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	if n := len(f.wh.things); n != 0 {
		t.Errorf("%d things made there", n)
	}
	thing := f.wh.add("Lamp", nil)
	if w := f.call(t, f.srv.linkWherehouseThing, http.MethodPut, fmt.Sprintf(`{"thingId":%d}`, thing.ID)); w.Code != http.StatusConflict {
		t.Fatalf("link = %d %s", w.Code, w.Body)
	}
	if f.wh.get(thing.ID).app != "" || f.warranty(t, f.warrantyID).Wherehouse != nil {
		t.Error("linked")
	}
	// A household of one is the owner's own.
	f.members(1)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add to own household = %d %s", w.Code, w.Body)
	}
}

// A linked warranty can't be made private: it's unlinked first.
func TestWherehouseLinkedNotMadePrivate(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	spaces, err := f.srv.store.ListSpaces(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var own int64
	for _, sp := range spaces {
		if sp.Kind == store.SpacePersonal {
			own = sp.ID
		}
	}
	err = f.srv.store.UpdateWarranty(ctx, f.alice.ID, f.warrantyID, f.input(t, &own))
	var ve *i18n.Error
	if !errors.As(err, &ve) || ve.Key != "server.wherehousePrivateLinked" {
		t.Fatalf("made private = %v", err)
	}
	if f.warranty(t, f.warrantyID).SpaceKind == store.SpacePersonal {
		t.Error("private all the same")
	}
	if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code != 200 {
		t.Fatalf("unlink = %d %s", w.Code, w.Body)
	}
	if err := f.srv.store.UpdateWarranty(ctx, f.alice.ID, f.warrantyID, f.input(t, &own)); err != nil {
		t.Fatalf("made private once unlinked = %v", err)
	}
}

// A private warranty linked while its household was its owner's alone
// isn't kept in step once others join it.
func TestWherehousePrivateSkippedOnceShared(t *testing.T) {
	f := newWherehouseFixture(t)
	f.makePrivate(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.members(2)
	in := f.input(t, nil)
	in.Vendor = "Elsewhere"
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	f.addPhoto(t)
	f.srv.sendToWherehouseLater(f.alice.ID, f.warrantyID)
	f.srv.background.Wait()
	th := f.wh.get(thing)
	if th.link.Purchase == nil || th.link.Purchase.PurchaseFrom == "Elsewhere" {
		t.Errorf("details sent: %+v", th.link.Purchase)
	}
	if len(th.Photos) != 0 {
		t.Errorf("photos sent: %+v", th.Photos)
	}
	// Nothing waits to be sent again.
	if p, err := f.srv.store.WherehousePushes(context.Background(), f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes = %+v, %v", p, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Details that couldn't be sent wait in the database, and are sent once
// Wherehouse takes them, after a restart too.
func TestWherehousePushWaits(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()
	f.wh.mu.Lock()
	f.wh.failLink = true
	f.wh.mu.Unlock()
	in := f.input(t, nil)
	in.Vendor = "Elsewhere"
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	pushes, err := f.srv.store.WherehousePushes(ctx, f.warrantyID)
	if err != nil || len(pushes) != 1 || pushes[0].Attempts != 1 {
		t.Fatalf("pushes after a failure = %+v, %v", pushes, err)
	}
	// Not due yet: tried again later.
	if due, err := f.srv.store.WherehousePushes(ctx, 0); err != nil || len(due) != 0 {
		t.Fatalf("due at once = %+v, %v", due, err)
	}

	// Restarted, with Wherehouse back, once it's due.
	f.wh.mu.Lock()
	f.wh.failLink = false
	f.wh.mu.Unlock()
	if err := f.srv.store.RetryWherehousePush(ctx, pushes[0], time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	again := New(f.srv.cfg, f.srv.store, nil, nil, f.srv.files, nil, "test")
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		again.PushWherehouse(runCtx)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if th := f.wh.get(thing); th.link.Purchase != nil && th.link.Purchase.PurchaseFrom == "Elsewhere" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not sent after restarting")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	if p, err := f.srv.store.WherehousePushes(ctx, f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes once sent = %+v, %v", p, err)
	}
}

// A change made while the details are on their way is sent too, not taken
// as sent.
func TestWherehousePushKeepsLaterChange(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	if err := f.srv.store.QueueWherehousePush(ctx, f.alice.ID, f.warrantyID); err != nil {
		t.Fatal(err)
	}
	old, err := f.srv.store.WherehousePushes(ctx, f.warrantyID)
	if err != nil || len(old) != 1 {
		t.Fatalf("pushes = %+v, %v", old, err)
	}
	if err := f.srv.store.QueueWherehousePush(ctx, f.alice.ID, f.warrantyID); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.DoneWherehousePush(ctx, old[0]); err != nil {
		t.Fatal(err)
	}
	if p, err := f.srv.store.WherehousePushes(ctx, f.warrantyID); err != nil || len(p) != 1 {
		t.Errorf("later change forgotten: %+v, %v", p, err)
	}
}

// A thing is one warranty's: linking another to it is refused, by name
// when it's one the person sees.
func TestWherehouseThingTaken(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()
	months := 12
	other, err := f.srv.store.CreateWarranty(ctx, f.alice.ID, &store.WarrantyInput{ProductName: "Kettle",
		PurchaseDate: "2025-04-01", WarrantyMonths: &months})
	if err != nil {
		t.Fatal(err)
	}
	link := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(fmt.Sprintf(`{"thingId":%d}`, thing)))
		r.SetPathValue("id", fmt.Sprint(other))
		r = r.WithContext(auth.WithUser(r.Context(), f.alice))
		w := httptest.NewRecorder()
		f.srv.linkWherehouseThing(w, r)
		f.srv.background.Wait()
		return w
	}
	w := link()
	if w.Code != http.StatusConflict || errorCode(w) != "server.wherehouseThingTaken" || !strings.Contains(w.Body.String(), "Lamp") {
		t.Fatalf("link a taken thing = %d %s", w.Code, w.Body)
	}
	if f.wh.get(thing).app != fmt.Sprint(f.warrantyID) {
		t.Error("the thing's link moved")
	}

	// Taken in Wherehouse by a warranty not known here: its answer is
	// told plainly.
	if err := f.srv.store.SetWherehouse(ctx, f.alice.ID, f.warrantyID, nil); err != nil {
		t.Fatal(err)
	}
	w = link()
	if w.Code != http.StatusConflict || errorCode(w) != "server.wherehouseThingTakenThere" {
		t.Fatalf("link a thing taken there = %d %s", w.Code, w.Body)
	}

	// The database keeps it so too.
	if err := f.srv.store.SetWherehouse(ctx, f.alice.ID, f.warrantyID, &store.WherehouseLink{Thing: thing}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.SetWherehouse(ctx, f.alice.ID, other, &store.WherehouseLink{Thing: thing}); !errors.Is(err, store.ErrThingTaken) {
		t.Errorf("second link to a thing = %v", err)
	}
}

// A detail changed here goes to the thing alone, as one PATCH.
func TestWherehouseDetailsPatchOnlyChanged(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.wh.mu.Lock()
	f.wh.patches = nil
	f.wh.mu.Unlock()
	in := f.input(t, nil)
	in.ProductName = "Desk lamp"
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	f.wh.mu.Lock()
	patches := f.wh.patches
	f.wh.mu.Unlock()
	if len(patches) != 1 || len(patches[0]) != 1 || patches[0]["name"] != "Desk lamp" {
		t.Errorf("patches = %v", patches)
	}
	if th := f.wh.get(thing); th.Name != "Desk lamp" || th.Quantity != 2 {
		t.Errorf("thing = %+v", th)
	}
}

// Keeping photos only in Wherehouse doesn't send a private warranty's
// photos to a household that others joined since it was linked.
func TestWherehousePrivatePhotosStayHome(t *testing.T) {
	f := newWherehouseFixture(t)
	f.makePrivate(t)
	f.addPhoto(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.wh.mu.Lock()
	f.wh.things[thing].Photos = nil // the copy sent while the household was hers alone
	f.wh.mu.Unlock()
	f.members(2)
	f.photosOnly(t, true)
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageLocal || !f.fileThere(ps[0].LocalPath) {
		t.Errorf("photos = %+v", ps)
	}
	if th := f.wh.get(thing); len(th.Photos) != 0 {
		t.Errorf("sent there: %+v", th.Photos)
	}
}

// A claim whose copy there failed when it was made is sent with the next
// sync.
func TestWherehouseClaimSentAfterFailing(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.wh.mu.Lock()
	f.wh.rejectToken = true
	f.wh.mu.Unlock()
	ctx := context.Background()
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()
	if entry, _, _ := f.srv.store.ClaimWherehouse(ctx, c.ID); entry != nil {
		t.Fatal("sent while Wherehouse refused")
	}
	f.wh.mu.Lock()
	f.wh.rejectToken = false
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	if th := f.wh.get(thing); len(th.Maintenance) != 1 {
		t.Errorf("maintenance there = %+v", th.Maintenance)
	}
	if entry, _, _ := f.srv.store.ClaimWherehouse(ctx, c.ID); entry == nil {
		t.Error("no entry recorded")
	}
	// Not sent twice.
	f.changedThere(t, thing)
	if th := f.wh.get(thing); len(th.Maintenance) != 1 {
		t.Errorf("maintenance there after another sync = %+v", th.Maintenance)
	}
}
