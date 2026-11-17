package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

// callAt is call with a target carrying a query.
func (f *wherehouseFixture) callAt(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	h(w, r)
	f.srv.background.Wait()
	return w
}

// cottage connects Alice to a second household, 3, and returns the
// connection's ID.
func (f *wherehouseFixture) cottage(t *testing.T) int64 {
	t.Helper()
	f.wh.mu.Lock()
	f.wh.households = map[string]int64{"cottage-token": 3}
	f.wh.householdNames = map[int64]string{3: "Cottage"}
	f.wh.mu.Unlock()
	id, err := f.srv.store.SaveWherehouseConn(context.Background(), f.alice.ID, 3, "Cottage", "cottage-token")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// secondWarranty adds another warranty for Alice.
func (f *wherehouseFixture) secondWarranty(t *testing.T, name string) int64 {
	t.Helper()
	months := 24
	id, err := f.srv.store.CreateWarranty(context.Background(), f.alice.ID, &store.WarrantyInput{ProductName: name,
		PurchaseDate: "2025-03-01", WarrantyMonths: &months, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// connectThrough goes through connecting, with query on the connect
// request, and returns where the callback sent the browser; ready can set
// up the fake Wherehouse once it has the code.
func (f *wherehouseFixture) connectThrough(t *testing.T, query string, ready func()) string {
	t.Helper()
	w := f.callAt(t, f.srv.connectWherehouse, http.MethodPost, "/"+query, ``)
	var out struct{ URL string }
	json.Unmarshal(w.Body.Bytes(), &out)
	u, _ := url.Parse(out.URL)
	q := u.Query()
	if q.Get("multiple") != "1" {
		t.Fatalf("connect URL = %s", out.URL)
	}
	f.wh.mu.Lock()
	f.wh.challenge, f.wh.code = q.Get("code_challenge"), "the-code"
	if ready != nil {
		ready()
	}
	f.wh.mu.Unlock()
	r := httptest.NewRequest(http.MethodGet, "/wherehouse/callback?code=the-code&state="+q.Get("state"), nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	cw := httptest.NewRecorder()
	f.srv.wherehouseCallback(cw, r)
	f.srv.background.Wait()
	return cw.Header().Get("Location")
}

// Allowing several households at once connects each, with its own token
// and pings; one connected before gets the new token.
func TestWherehouseConnectsSeveralHouseholds(t *testing.T) {
	f := newWherehouseFixture(t)
	to := f.connectThrough(t, "?household=3", func() {
		var home, cottage wherehouse.Connected
		home.Token, home.Household.ID, home.Household.Name = "home-token-2", 1, "Home"
		cottage.Token, cottage.Household.ID, cottage.Household.Name = "cottage-token", 3, "Cottage"
		f.wh.connectTokens = []wherehouse.Connected{home, cottage}
		f.wh.households = map[string]int64{"cottage-token": 3}
	})
	if !strings.Contains(to, "wherehouse=connected") {
		t.Fatalf("back to %s", to)
	}
	conns, err := f.srv.store.WherehouseConns(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 2 || conns[0].HouseholdID != 1 || conns[0].Token != "home-token-2" ||
		conns[1].HouseholdID != 3 || conns[1].Household != "Cottage" || conns[1].Token != "cottage-token" {
		t.Errorf("connections = %+v", conns)
	}
	if hooks, err := f.srv.store.WherehouseHooks(context.Background()); err != nil || len(hooks) != 2 {
		t.Errorf("hooks = %+v, %v", hooks, err)
	}
}

// A thing is made in the household picked, and the warranty's changes go
// through the connection to that household.
func TestWherehouseThingInPickedHousehold(t *testing.T) {
	f := newWherehouseFixture(t)
	conn := f.cottage(t)
	if w := f.callAt(t, f.srv.addToWherehouse, http.MethodPost, fmt.Sprintf("/?connection=%d", conn), `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	link := f.warranty(t, f.warrantyID).Wherehouse
	if link == nil || link.Household != 3 {
		t.Fatalf("link = %+v", link)
	}
	if th := f.wh.get(link.Thing); th.household != 3 {
		t.Errorf("thing made in household %d", th.household)
	}
	in := f.input(t, nil)
	in.Vendor = "Village shop"
	if w := f.call(t, f.srv.updateWarranty, http.MethodPut, mustJSON(t, in)); w.Code != 200 {
		t.Fatalf("update = %d %s", w.Code, w.Body)
	}
	if th := f.wh.get(link.Thing); th.link.Purchase == nil || th.link.Purchase.PurchaseFrom != "Village shop" {
		t.Errorf("details there = %+v", th.link.Purchase)
	}
	if p, err := f.srv.store.WherehousePushes(context.Background(), f.warrantyID); err != nil || len(p) != 0 {
		t.Errorf("pushes = %+v, %v", p, err)
	}
}

// Disconnecting one household unlinks only the warranties whose things are
// in it; the other household stays connected and linked.
func TestWherehouseDisconnectOneHousehold(t *testing.T) {
	f := newWherehouseFixture(t)
	conn := f.cottage(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add home = %d %s", w.Code, w.Body)
	}
	other := f.secondWarranty(t, "Kettle")
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/?connection=%d", conn), strings.NewReader(`{}`))
	r.SetPathValue("id", fmt.Sprint(other))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	f.srv.addToWherehouse(w, r)
	f.srv.background.Wait()
	if w.Code != 200 {
		t.Fatalf("add cottage = %d %s", w.Code, w.Body)
	}

	dr := httptest.NewRequest(http.MethodDelete, "/api/settings/wherehouse?unlink=1", nil)
	dr.SetPathValue("id", fmt.Sprint(conn))
	dr = dr.WithContext(auth.WithUser(dr.Context(), f.alice))
	dw := httptest.NewRecorder()
	f.srv.disconnectWherehouse(dw, dr)
	f.srv.background.Wait()
	if dw.Code != 200 {
		t.Fatalf("disconnect = %d %s", dw.Code, dw.Body)
	}
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse == nil {
		t.Error("the home warranty was unlinked")
	}
	if wr := f.warranty(t, other); wr.Wherehouse != nil {
		t.Errorf("the cottage warranty is still linked: %+v", wr.Wherehouse)
	}
	conns, _ := f.srv.store.WherehouseConns(context.Background(), f.alice.ID)
	if len(conns) != 1 || conns[0].ID != f.conn {
		t.Errorf("connections = %+v", conns)
	}
}

// A household that's gone in Wherehouse (its token refused) is forgotten,
// and its things' warranties unlinked; the other household carries on.
func TestWherehouseGoneHouseholdForgotten(t *testing.T) {
	f := newWherehouseFixture(t)
	conn := f.cottage(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add home = %d %s", w.Code, w.Body)
	}
	other := f.secondWarranty(t, "Kettle")
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/?connection=%d", conn), strings.NewReader(`{}`))
	r.SetPathValue("id", fmt.Sprint(other))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	f.srv.addToWherehouse(w, r)
	f.srv.background.Wait()
	if w.Code != 200 {
		t.Fatalf("add cottage = %d %s", w.Code, w.Body)
	}
	f.wh.mu.Lock()
	f.wh.gone = map[string]bool{"cottage-token": true}
	f.wh.mu.Unlock()
	f.srv.syncWherehouseUser(context.Background(), f.alice.ID)
	f.srv.background.Wait()
	conns, _ := f.srv.store.WherehouseConns(context.Background(), f.alice.ID)
	if len(conns) != 1 || conns[0].ID != f.conn {
		t.Errorf("connections = %+v", conns)
	}
	if wr := f.warranty(t, other); wr.Wherehouse != nil {
		t.Errorf("the gone household's warranty is still linked: %+v", wr.Wherehouse)
	}
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse == nil {
		t.Error("the home warranty was unlinked")
	}
}

// A connection from before households were recorded learns its household,
// and so do its warranties' links, with the first sync.
func TestWherehouseOldConnectionLearnsHousehold(t *testing.T) {
	f := newWherehouseFixture(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	ctx := context.Background()
	// As migration 0026 leaves them.
	db := f.db
	if _, err := db.ExecContext(ctx, `UPDATE wherehouse_connections SET household_id = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE warranties SET wherehouse_household = NULL`); err != nil {
		t.Fatal(err)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	f.changedThere(t, thing)
	conns, _ := f.srv.store.WherehouseConns(ctx, f.alice.ID)
	if len(conns) != 1 || conns[0].HouseholdID != 1 || conns[0].Household != "Home" {
		t.Errorf("connections = %+v", conns)
	}
	if link := f.warranty(t, f.warrantyID).Wherehouse; link.Household != 1 {
		t.Errorf("link = %+v", link)
	}
}

// The person's households are listed for connecting more.
func TestWherehouseHouseholdsListed(t *testing.T) {
	f := newWherehouseFixture(t)
	w := f.call(t, f.srv.wherehouseHouseholds, http.MethodGet, ``)
	var list []wherehouse.HouseholdChoice
	json.Unmarshal(w.Body.Bytes(), &list)
	if w.Code != 200 || len(list) != 2 || !list[0].Connected || list[1].Connected || list[1].Name != "Cottage" {
		t.Errorf("households = %d %+v", w.Code, list)
	}
}

// personalWarranty adds a warranty only uid can see.
func (f *wherehouseFixture) personalWarranty(t *testing.T, uid int64, name string) int64 {
	t.Helper()
	ctx := context.Background()
	spaces, err := f.srv.store.ListSpaces(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range spaces {
		if sp.Kind == store.SpacePersonal {
			months := 24
			id, err := f.srv.store.CreateWarranty(ctx, uid, &store.WarrantyInput{ProductName: name,
				PurchaseDate: "2025-03-01", WarrantyMonths: &months, Quantity: 1, SpaceID: &sp.ID})
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
	}
	t.Fatal("no personal space")
	return 0
}

// When a household is gone and the last connection to it fails, every
// warranty linked there is unlinked, also ones only someone whose
// connection failed earlier could see.
func TestWherehouseGoneHouseholdUnlinksEveryones(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	bob, err := f.srv.store.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "bob", Name: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	f.cottage(t)
	if err := saveConn(ctx, f.srv.store, bob.ID, 3, "Cottage", "bob-cottage"); err != nil {
		t.Fatal(err)
	}
	mine := f.personalWarranty(t, f.alice.ID, "Kettle")
	his := f.personalWarranty(t, bob.ID, "Radio")
	if err := f.srv.store.SetWherehouse(ctx, f.alice.ID, mine, &store.WherehouseLink{Thing: 50, Household: 3}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.SetWherehouse(ctx, bob.ID, his, &store.WherehouseLink{Thing: 51, Household: 3}); err != nil {
		t.Fatal(err)
	}
	f.wh.mu.Lock()
	f.wh.gone = map[string]bool{"cottage-token": true, "bob-cottage": true}
	f.wh.mu.Unlock()
	f.srv.syncWherehouseUser(ctx, f.alice.ID)
	f.srv.background.Wait()
	if wr := f.warranty(t, mine); wr.Wherehouse == nil {
		t.Fatal("unlinked while Bob may still reach the household")
	}
	f.srv.syncWherehouseUser(ctx, bob.ID)
	f.srv.background.Wait()
	if wr := f.warranty(t, mine); wr.Wherehouse != nil {
		t.Errorf("Alice's warranty is still linked: %+v", wr.Wherehouse)
	}
	if wr, err := f.srv.store.GetWarranty(ctx, bob.ID, his); err != nil || wr.Wherehouse != nil {
		t.Errorf("Bob's warranty = %+v, %v", wr, err)
	}
}

// A photo kept on a thing in one household still comes along when the
// warranty is linked to a thing in another before it was settled.
func TestWherehousePhotoFollowsToOtherHousehold(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	f.photosOnly(t, true)
	f.addPhoto(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	if ps := f.photos(t, f.warrantyID); len(ps) != 1 || ps[0].Storage != store.StorageWherehouse {
		t.Fatalf("photos = %+v", ps)
	}
	f.cottage(t)
	f.wh.mu.Lock()
	f.wh.things[99] = &fakeThing{ID: 99, Name: "Lamp", Quantity: 1, household: 3}
	f.wh.mu.Unlock()
	if err := f.srv.store.SetWherehouse(ctx, f.alice.ID, f.warrantyID, &store.WherehouseLink{Thing: 99, Household: 3}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.settleWherehousePhotos(ctx, f.alice.ID, f.warrantyID); err != nil {
		t.Fatal(err)
	}
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageWherehouse || *ps[0].WherehouseThing != 99 {
		t.Fatalf("photos = %+v", ps)
	}
	if th := f.wh.get(99); len(th.Photos) != 1 {
		t.Errorf("new thing's photos = %+v", th.Photos)
	}
}
