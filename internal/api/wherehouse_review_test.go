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

// A private draft confirmed while its owner's household is shared makes no
// thing there: nothing of it reaches the household.
func TestWherehousePrivateDraftMakesNoThing(t *testing.T) {
	f := newWherehouseFixture(t)
	f.makePrivate(t)
	f.members(2)
	wr, err := f.srv.addDraftToWherehouse(context.Background(), f.alice.ID, f.warrantyID)
	if err != nil {
		t.Fatal(err)
	}
	if wr.Wherehouse != nil {
		t.Errorf("linked: %+v", wr.Wherehouse)
	}
	f.srv.background.Wait()
	f.wh.mu.Lock()
	n := f.wh.created
	f.wh.mu.Unlock()
	if n != 0 {
		t.Errorf("%d things made there", n)
	}
}

// Splitting a private warranty linked before others joined its household
// makes no thing there for the pieces split off.
func TestWherehousePrivateSplitMakesNoThing(t *testing.T) {
	f := newWherehouseFixture(t)
	f.makePrivate(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	orig := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.members(2)
	w := f.call(t, f.srv.splitWarranty, http.MethodPost, `{"pieces":1,"serialNumbers":[],"wherehouse":true}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("split = %d %s", w.Code, w.Body)
	}
	f.wh.mu.Lock()
	n := f.wh.created
	f.wh.mu.Unlock()
	if n != 1 {
		t.Errorf("%d things made there, want only the original", n)
	}
	if th := f.wh.get(orig); th.Quantity != 2 {
		t.Errorf("original quantity changed to %d", th.Quantity)
	}
}

// A push by someone connected to another household, which doesn't find
// the thing, goes through the connection of someone whose household has it.
func TestWherehousePushTriesTheOwnersConnection(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()
	bob, err := f.srv.store.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "bob", Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.SetWherehouseConnection(ctx, bob.ID, "bob-token", "Elsewhere"); err != nil {
		t.Fatal(err)
	}
	f.wh.mu.Lock()
	f.wh.strangers = map[string]bool{"bob-token": true}
	f.wh.mu.Unlock()
	in := f.input(t, nil)
	in.Vendor = "Bob's shop"
	if err := f.srv.store.UpdateWarranty(ctx, bob.ID, f.warrantyID, in); err != nil {
		t.Fatal(err)
	}
	f.srv.pushWherehouseLater(bob.ID, f.warrantyID)
	f.srv.background.Wait()
	if th := f.wh.get(thing); th.link.Purchase == nil || th.link.Purchase.PurchaseFrom != "Bob's shop" {
		t.Errorf("thing told %+v", th.link.Purchase)
	}
	if p, err := f.srv.store.WherehousePushes(ctx, f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes = %+v, %v", p, err)
	}
}

// A thing linked to another warranty in Wherehouse meanwhile isn't this
// one's any more: the push unlinks it here, so it shows.
func TestWherehousePushTakenThereUnlinks(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.wh.mu.Lock()
	f.wh.things[thing].app = "someone else's"
	f.wh.mu.Unlock()
	in := f.input(t, nil)
	in.Vendor = "Elsewhere"
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse != nil {
		t.Errorf("still linked: %+v", wr.Wherehouse)
	}
	if th := f.wh.get(thing); th.app != "someone else's" {
		t.Errorf("the other warranty's link changed: %q", th.app)
	}
	if p, err := f.srv.store.WherehousePushes(context.Background(), f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes = %+v, %v", p, err)
	}
}

// A claim edited here while Wherehouse was out of reach gets there with the
// next sync, though its entry there didn't change; and only once.
func TestWherehouseClaimEditSentAfterFailing(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()
	f.changedThere(t, thing)

	f.wh.mu.Lock()
	f.wh.rejectToken = true
	f.wh.mu.Unlock()
	if _, err := f.srv.store.UpdateClaim(ctx, f.alice.ID, c.ID, &store.ClaimInput{Title: "Flickers and hums", ReportedOn: "2026-03-01"}); err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, c.ID)
	f.srv.background.Wait()
	if th := f.wh.get(thing); th.Maintenance[0].Title != "Warranty claim: Flickers" {
		t.Fatalf("sent while Wherehouse refused: %+v", th.Maintenance)
	}
	f.wh.mu.Lock()
	f.wh.rejectToken = false
	f.wh.mu.Unlock()

	f.changedThere(t, thing)
	f.srv.background.Wait()
	th := f.wh.get(thing)
	if len(th.Maintenance) != 1 || th.Maintenance[0].Title != "Warranty claim: Flickers and hums" {
		t.Errorf("maintenance there = %+v", th.Maintenance)
	}
	if got, _ := f.srv.store.GetClaim(ctx, f.alice.ID, c.ID); got.Title != "Flickers and hums" {
		t.Errorf("claim here = %+v", got)
	}
	// Sent, it isn't sent again.
	f.wh.mu.Lock()
	writes := f.wh.entryWrites
	f.wh.mu.Unlock()
	f.changedThere(t, thing)
	f.changedThere(t, thing)
	f.wh.mu.Lock()
	again := f.wh.entryWrites
	f.wh.mu.Unlock()
	if again != writes {
		t.Errorf("written %d more times", again-writes)
	}
}

// Splitting off pieces with their serial numbers, with no thing made for
// them, sends the original's details again: the numbers moved off are no
// longer listed on its thing.
func TestWherehouseSplitSendsOriginalDetails(t *testing.T) {
	f := newWherehouseFixture(t)
	in := f.input(t, nil)
	in.Quantity, in.SerialNumbers = 3, []string{"SN-A1", "SN-B2", "SN-C3"}
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	if d := f.wh.get(thing).link.Purchase.WarrantyDetails; !strings.Contains(d, "SN-C3") {
		t.Fatalf("details there before = %q", d)
	}
	w := f.call(t, f.srv.splitWarranty, http.MethodPost, `{"pieces":1,"serialNumbers":["SN-C3"],"wherehouse":false}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("split = %d %s", w.Code, w.Body)
	}
	f.srv.background.Wait()
	if d := f.wh.get(thing).link.Purchase.WarrantyDetails; strings.Contains(d, "SN-C3") || !strings.Contains(d, "SN-B2") {
		t.Errorf("details there after = %q", d)
	}
	if p, err := f.srv.store.WherehousePushes(context.Background(), f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes = %+v, %v", p, err)
	}
}

// A claim whose copy failed is matched by title only to an entry no other
// claim has: one with the same title already synced keeps its entry.
func TestWherehouseMissingClaimLeavesOthersEntry(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ctx := context.Background()
	first, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", ReportedOn: "2026-03-01"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, first.ID)
	f.srv.background.Wait()
	f.changedThere(t, thing)
	firstEntry, _, _ := f.srv.store.ClaimWherehouse(ctx, first.ID)
	if firstEntry == nil {
		t.Fatal("first claim not sent")
	}

	f.wh.mu.Lock()
	f.wh.rejectToken = true
	f.wh.mu.Unlock()
	second, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", ReportedOn: "2026-05-01", Notes: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	f.srv.syncClaimLater(f.alice.ID, second.ID)
	f.srv.background.Wait()
	f.wh.mu.Lock()
	f.wh.rejectToken = false
	f.wh.mu.Unlock()

	f.changedThere(t, thing)
	secondEntry, _, _ := f.srv.store.ClaimWherehouse(ctx, second.ID)
	if secondEntry == nil || *secondEntry == *firstEntry {
		t.Fatalf("second claim's entry = %v, the first's %d", secondEntry, *firstEntry)
	}
	if th := f.wh.get(thing); len(th.Maintenance) != 2 {
		t.Errorf("maintenance there = %+v", th.Maintenance)
	}
	f.changedThere(t, thing)
	if got, _ := f.srv.store.GetClaim(ctx, f.alice.ID, first.ID); got.ReportedOn != "2026-03-01" || got.Notes != "" {
		t.Errorf("first claim = %+v", got)
	}
}

// The details sent there fit what Wherehouse takes, however many serial
// numbers the warranty lists.
func TestWherehouseLinkDetailsFit(t *testing.T) {
	f := newWherehouseFixture(t)
	months := 24
	wr := &store.Warranty{ProductName: "Lamp", WarrantyMonths: &months, Quantity: 3000}
	for i := range 3000 {
		wr.SerialNumbers = append(wr.SerialNumbers, fmt.Sprintf("SERIAL-%05d", i))
	}
	l := f.srv.wherehouseLink(wr, &store.Settings{})
	if n := len([]rune(l.Purchase.WarrantyDetails)); n == 0 || n > 10_000 {
		t.Errorf("details are %d characters", n)
	}
}
