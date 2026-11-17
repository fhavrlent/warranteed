package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

type fakeThing struct {
	ID           int64                    `json:"id"`
	ParentID     *int64                   `json:"parentId"`
	Name         string                   `json:"name"`
	Quantity     int                      `json:"quantity"`
	Manufacturer string                   `json:"manufacturer"`
	Model        string                   `json:"model"`
	Serial       string                   `json:"serial"`
	Notes        string                   `json:"notes"`
	SoldDate     *string                  `json:"soldDate"`
	Photos       []map[string]int64       `json:"photos"`
	Maintenance  []wherehouse.Maintenance `json:"maintenance"`
	Attachments  []map[string]any         `json:"attachments"`
	Links        []map[string]string      `json:"links"`
	Kind         map[string]any           `json:"kind"`
	// app is the warranty it's linked to, or "", and link what it was told.
	app  string
	link wherehouse.Link
	// household is the household it's in; 0 is household 1.
	household int64
}

// fakeWherehouse keeps things in memory, the way Wherehouse's API answers
// Warranteed.
type fakeWherehouse struct {
	mu     sync.Mutex
	things map[int64]*fakeThing
	// fromHomeBox are things by the HomeBox ID they were copied from.
	fromHomeBox map[string]int64
	last        int64
	changes     []int64
	// noPaperless makes linking Paperless documents fail, as without a
	// Paperless token in Wherehouse.
	noPaperless bool
	// failLink makes linking a thing to a warranty fail.
	failLink bool
	// paperless is the Paperless it links documents from, or "".
	paperless string
	// files are the contents of documents kept in Wherehouse, by ID.
	files map[int64][]byte
	// photos are the photos' contents by ID.
	photos map[int64][]byte
	// rejectToken refuses every request, as a Wherehouse set up afresh
	// refuses an old token.
	rejectToken bool
	// failGets fails that many reads of a thing, as when Wherehouse doesn't
	// answer in time.
	failGets int
	// onHook runs while pings are being turned on or off, before the answer.
	onHook func()
	// revoked is set once the token was deleted.
	revoked bool
	// members is how many people are in the household; 0 is one.
	members int
	// challenge is the PKCE challenge of the last connect, and code the code
	// it hands back.
	challenge, code string
	// patches are the bodies of the thing edits, in order.
	patches []map[string]any
	// strangers are tokens of another household, which finds none of
	// these things.
	strangers map[string]bool
	// created counts the things made, deleted since or not.
	created int
	// entryWrites counts maintenance entries written over.
	entryWrites int
	// households gives the household of each token, 1 if not given, and
	// householdNames their names.
	households     map[string]int64
	householdNames map[int64]string
	// gone are tokens Wherehouse no longer takes, e.g. of a deleted
	// household.
	gone map[string]bool
	// connectTokens is what connecting with multiple=1 hands back.
	connectTokens []wherehouse.Connected
}

// householdOf is the household a request's token is to.
func (f *fakeWherehouse) householdOf(r *http.Request) int64 {
	if h := f.households[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]; h != 0 {
		return h
	}
	return 1
}

func (t *fakeThing) in() int64 { return max(t.household, 1) }

func (f *fakeWherehouse) add(name string, parent *int64) *fakeThing {
	f.last++
	t := &fakeThing{ID: f.last, ParentID: parent, Name: name, Quantity: 1, Kind: map[string]any{"id": 5}}
	f.things[t.ID] = t
	return t
}

func (f *fakeWherehouse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectToken {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/connect/token" {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != f.code || in["code_verifier"] == "" || wherehouse.Challenge(in["code_verifier"]) != f.challenge {
			http.Error(w, `{"error":"invalid code"}`, http.StatusBadRequest)
			return
		}
		if f.connectTokens != nil {
			json.NewEncoder(w).Encode(map[string]any{"tokens": f.connectTokens})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"token": "wh-token-2", "household": map[string]any{"id": 1, "name": "Home"}})
		return
	}
	if f.gone[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
		return
	}
	// A thing in another household is not found, as Wherehouse scopes
	// everything to the token's household.
	if rest, ok := strings.CutPrefix(r.URL.Path, "/api/things/"); ok {
		n, _ := strconv.ParseInt(strings.Split(rest, "/")[0], 10, 64)
		if t := f.things[n]; t != nil && t.in() != f.householdOf(r) {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
	}
	if f.strangers[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] &&
		(strings.HasPrefix(r.URL.Path, "/api/things/") || strings.HasPrefix(r.URL.Path, "/api/apps/")) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	id, _ := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	thing := func(i int) *fakeThing {
		n, _ := strconv.ParseInt(parts[i], 10, 64)
		return f.things[n]
	}
	send := func(v any) { json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/things":
		var in struct {
			Name                               string `json:"name"`
			ParentID                           *int64 `json:"parentId"`
			Quantity                           int    `json:"quantity"`
			Manufacturer, Model, Serial, Notes string
		}
		json.NewDecoder(r.Body).Decode(&in)
		f.created++
		t := f.add(in.Name, in.ParentID)
		t.household = f.householdOf(r)
		t.Quantity = in.Quantity
		t.Manufacturer, t.Model, t.Serial, t.Notes = in.Manufacturer, in.Model, in.Serial, in.Notes
		send(t)
	case len(parts) == 2 && parts[0] == "things":
		t := f.things[id]
		if t == nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if f.failGets > 0 {
				f.failGets--
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			send(t)
		case http.MethodPut:
			// Not for a connected app's token.
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		case http.MethodPatch:
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			f.patches = append(f.patches, in)
			for k, v := range in {
				switch k {
				case "quantity":
					t.Quantity = int(v.(float64))
				case "name":
					t.Name, _ = v.(string)
				case "manufacturer":
					t.Manufacturer, _ = v.(string)
				case "model":
					t.Model, _ = v.(string)
				case "serial":
					t.Serial, _ = v.(string)
				case "notes":
					t.Notes, _ = v.(string)
				default:
					http.Error(w, "unknown "+k, http.StatusBadRequest)
					return
				}
			}
			send(t)
		case http.MethodDelete:
			delete(f.things, id)
			w.WriteHeader(http.StatusNoContent)
		}
	case len(parts) == 4 && parts[2] == "apps":
		t := thing(1)
		if t == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			t.app = ""
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if f.failLink {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		var l wherehouse.Link
		json.NewDecoder(r.Body).Decode(&l)
		if t.app != "" && t.app != l.ExternalID {
			http.Error(w, `{"error":"linked to another warranty"}`, http.StatusConflict)
			return
		}
		for _, o := range f.things {
			if o.app == l.ExternalID {
				o.app = ""
			}
		}
		t.app, t.link = l.ExternalID, l
		send(t)
	case len(parts) == 3 && parts[0] == "apps":
		for _, t := range f.things {
			if t.app == parts[2] && t.in() == f.householdOf(r) {
				send(t)
				return
			}
		}
		http.NotFound(w, r)
	case len(parts) == 2 && parts[0] == "homebox":
		t, ok := f.things[f.fromHomeBox[parts[1]]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		send(t)
	case len(parts) == 3 && parts[2] == "maintenance":
		var m wherehouse.Maintenance
		json.NewDecoder(r.Body).Decode(&m)
		f.last++
		m.ID = f.last
		t := thing(1)
		t.Maintenance = append(t.Maintenance, m)
		send(m)
	case parts[0] == "maintenance":
		for _, t := range f.things {
			for i, m := range t.Maintenance {
				if m.ID != id {
					continue
				}
				if r.Method == http.MethodDelete {
					t.Maintenance = append(t.Maintenance[:i], t.Maintenance[i+1:]...)
				} else {
					json.NewDecoder(r.Body).Decode(&t.Maintenance[i])
					t.Maintenance[i].ID = id
					f.entryWrites++
				}
				return
			}
		}
		http.NotFound(w, r)
	case len(parts) == 3 && parts[2] == "photos":
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		f.last++
		if f.photos == nil {
			f.photos = map[int64][]byte{}
		}
		f.photos[f.last] = data
		t := thing(1)
		t.Photos = append(t.Photos, map[string]int64{"id": f.last, "size": int64(len(data))})
		w.WriteHeader(http.StatusCreated)
		send([]map[string]int64{{"id": f.last}})
	case len(parts) == 2 && parts[0] == "photos" && r.Method == http.MethodGet:
		data, ok := f.photos[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(data)
	case len(parts) == 4 && parts[3] == "paperless":
		if f.noPaperless {
			http.Error(w, `{"error":"Paperless isn't set up"}`, http.StatusBadRequest)
			return
		}
		var in struct {
			DocumentIDs []int64 `json:"documentIds"`
		}
		var kind struct {
			Kind string `json:"kind"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &in)
		json.Unmarshal(body, &kind)
		t := thing(1)
		var added []map[string]any
		for _, d := range in.DocumentIDs {
			f.last++
			a := map[string]any{"id": f.last, "kind": kind.Kind, "storage": "paperless", "paperlessDocId": d,
				"filename": fmt.Sprintf("doc-%d.pdf", d), "mimeType": "application/pdf"}
			t.Attachments = append(t.Attachments, a)
			added = append(added, a)
		}
		w.WriteHeader(http.StatusCreated)
		send(added)
	case len(parts) == 2 && parts[0] == "photos" && r.Method == http.MethodDelete:
		for _, t := range f.things {
			for i, p := range t.Photos {
				if p["id"] == id {
					t.Photos = append(t.Photos[:i:i], t.Photos[i+1:]...)
					delete(f.photos, id)
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		http.NotFound(w, r)
	case parts[0] == "attachments":
		n, _ := strconv.ParseInt(parts[1], 10, 64)
		for _, t := range f.things {
			for i, a := range t.Attachments {
				if a["id"] != n {
					continue
				}
				if r.Method == http.MethodDelete {
					t.Attachments = append(t.Attachments[:i:i], t.Attachments[i+1:]...)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method == http.MethodPatch {
					var in map[string]string
					json.NewDecoder(r.Body).Decode(&in)
					a["kind"], a["filename"] = in["kind"], in["filename"]
					send(a)
					return
				}
				w.Header().Set("Content-Type", "application/pdf")
				w.Write(f.files[n])
				return
			}
		}
		http.NotFound(w, r)
	case len(parts) == 3 && parts[2] == "links":
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		t := thing(1)
		t.Links = append(t.Links, map[string]string{"url": in["url"]})
		w.WriteHeader(http.StatusCreated)
	case r.URL.Path == "/api/hook":
		if hook := f.onHook; hook != nil {
			// Let it reach this Wherehouse meanwhile.
			f.mu.Unlock()
			hook()
			f.mu.Lock()
		}
		if r.Method == http.MethodPut {
			writeJSON(w, 200, map[string]string{"secret": "hook-secret-" + r.Header.Get("Authorization")})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/token" && r.Method == http.MethodDelete:
		f.revoked = true
		f.rejectToken = true
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/api/household":
		members := []map[string]any{}
		for i := range max(f.members, 1) {
			members = append(members, map[string]any{"id": i + 1})
		}
		h := f.householdOf(r)
		name := f.householdNames[h]
		if name == "" {
			name = "Home"
		}
		send(map[string]any{"id": h, "name": name, "members": members})
	case r.URL.Path == "/api/connect/households":
		send([]map[string]any{{"id": 1, "name": "Home", "members": 1, "connected": true},
			{"id": 3, "name": "Cottage", "members": 1, "connected": false}})
	case r.URL.Path == "/api/paperless":
		if f.paperless == "" {
			http.NotFound(w, r)
			return
		}
		send(map[string]string{"url": f.paperless, "publicUrl": f.paperless})
	case r.URL.Path == "/api/changes":
		out := wherehouse.Changes{Until: "2026-01-01T00:00:00Z", Things: []wherehouse.Thing{}, Deleted: []int64{}}
		for _, cid := range f.changes {
			if t, ok := f.things[cid]; ok && t.in() == f.householdOf(r) {
				out.Things = append(out.Things, wherehouse.Thing{ID: t.ID, Name: t.Name, SoldDate: t.SoldDate})
			}
		}
		send(out)
	default:
		http.Error(w, "unknown "+r.Method+" "+r.URL.Path, http.StatusTeapot)
	}
}

func (f *fakeWherehouse) get(id int64) *fakeThing {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.things[id]
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

type wherehouseFixture struct {
	srv *Server
	wh  *fakeWherehouse
	// whServer serves wh; closing it makes Wherehouse unreachable.
	whServer *httptest.Server
	alice    *store.User
	// conn is Alice's connection, to household 1.
	conn int64
	// warrantyID has 2 pieces and a Paperless receipt Alice added.
	warrantyID int64
	db         *sql.DB
}

// saveConn connects someone to a household.
func saveConn(ctx context.Context, st *store.Store, uid, household int64, name, token string) error {
	_, err := st.SaveWherehouseConn(ctx, uid, household, name, token)
	return err
}

// client is Alice's Wherehouse connection.
func (f *wherehouseFixture) client(t *testing.T) *wherehouse.Client {
	t.Helper()
	wh, err := f.srv.pickWherehouse(context.Background(), f.alice.ID, f.conn)
	if err != nil || wh == nil {
		t.Fatalf("alice's connection: %v", err)
	}
	return wh
}

// Alice, connected to Wherehouse, with a warranty for two pieces.
func newWherehouseFixture(t *testing.T) *wherehouseFixture {
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
	s, _ := st.Settings(ctx, alice.ID)
	s.PaperlessURL, s.PaperlessPublicURL, s.PaperlessToken = "http://paperless:8000", "https://paperless.example.test", "alice-token"
	if err := st.SaveSettings(ctx, alice.ID, s); err != nil {
		t.Fatal(err)
	}
	conn, err := st.SaveWherehouseConn(ctx, alice.ID, 1, "Home", "wh-token")
	if err != nil {
		t.Fatal(err)
	}
	months := 24
	wid, err := st.CreateWarranty(ctx, alice.ID, &store.WarrantyInput{ProductName: "Lamp", PurchaseDate: "2025-03-01",
		WarrantyMonths: &months, Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	doc := int64(7)
	if err := st.CreateAttachment(ctx, &store.Attachment{WarrantyID: wid, Kind: "purchase", Storage: store.StoragePaperless,
		Filename: "receipt.pdf", MimeType: "application/pdf", PaperlessDocID: &doc, Status: store.StatusReady, AddedBy: &alice.ID}); err != nil {
		t.Fatal(err)
	}
	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wh := &fakeWherehouse{things: map[int64]*fakeThing{}, fromHomeBox: map[string]int64{}}
	whServer := httptest.NewServer(wh)
	t.Cleanup(whServer.Close)
	appURL, _ := url.Parse("https://warranty.example.test")
	cfg := &config.Config{AppURL: appURL, MaxUploadMB: 10, Wherehouse: config.Wherehouse{URL: whServer.URL, PublicURL: whServer.URL}}
	return &wherehouseFixture{srv: New(cfg, st, nil, nil, files, nil, "test"), wh: wh, whServer: whServer, alice: alice, conn: conn, warrantyID: wid, db: database}
}

// call runs a handler as Alice, with the warranty as its {id}.
func (f *wherehouseFixture) call(t *testing.T, h http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	r.SetPathValue("id", fmt.Sprint(f.warrantyID))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	h(w, r)
	f.srv.background.Wait()
	return w
}

func (f *wherehouseFixture) warranty(t *testing.T, id int64) *store.Warranty {
	t.Helper()
	wr, err := f.srv.store.GetWarranty(context.Background(), f.alice.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return wr
}

func (f *wherehouseFixture) addPhoto(t *testing.T) {
	t.Helper()
	path, size, err := f.srv.files.Save(strings.NewReader("\xff\xd8\xff photo"), ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.CreateAttachment(context.Background(), &store.Attachment{WarrantyID: f.warrantyID, Kind: "photo",
		Storage: store.StorageLocal, Filename: "lamp.jpg", MimeType: "image/jpeg", Size: size, LocalPath: path,
		Status: store.StatusReady, AddedBy: &f.alice.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestWherehouseAddSendsReceiptsAndPhotos(t *testing.T) {
	f := newWherehouseFixture(t)
	f.addPhoto(t)
	// Without Paperless in Wherehouse, the receipt is a link to it.
	f.wh.noPaperless = true
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	wr := f.warranty(t, f.warrantyID)
	if wr.Wherehouse == nil {
		t.Fatal("not linked")
	}
	th := f.wh.get(wr.Wherehouse.Thing)
	if th.app != fmt.Sprint(f.warrantyID) || th.Quantity != 2 || len(th.Photos) != 1 {
		t.Errorf("thing = %+v", th)
	}
	if len(th.Links) != 1 || th.Links[0]["url"] != "https://paperless.example.test/documents/7/details" {
		t.Errorf("receipt links = %+v", th.Links)
	}
	// Sending again adds nothing twice; a photo added later is sent.
	f.addPhoto(t)
	f.srv.sendToWherehouseLater(f.alice.ID, f.warrantyID)
	f.srv.background.Wait()
	if th := f.wh.get(wr.Wherehouse.Thing); len(th.Photos) != 2 || len(th.Links) != 1 {
		t.Errorf("after sending again: photos %d, links %+v", len(th.Photos), th.Links)
	}
}

func TestWherehouseReceiptsFromAnotherPaperless(t *testing.T) {
	for _, tc := range []struct {
		paperless   string
		docs, links int
	}{
		// The same Paperless: the receipt is a document there too.
		{"http://paperless:8000/", 1, 0},
		// Another one: its document 7 is something else, so a link.
		{"http://other-paperless:8000", 0, 1},
	} {
		f := newWherehouseFixture(t)
		f.wh.paperless = tc.paperless
		if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
			t.Fatalf("add = %d %s", w.Code, w.Body)
		}
		th := f.wh.get(f.warranty(t, f.warrantyID).Wherehouse.Thing)
		if len(th.Attachments) != tc.docs || len(th.Links) != tc.links {
			t.Errorf("%s: documents %+v, links %+v", tc.paperless, th.Attachments, th.Links)
		}
	}
}

func TestWherehouseAddLeavesNoThingBehind(t *testing.T) {
	f := newWherehouseFixture(t)
	f.wh.failLink = true
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code == 200 {
		t.Fatalf("add went through: %s", w.Body)
	}
	if len(f.wh.things) != 0 {
		t.Errorf("things left: %+v", f.wh.things)
	}
}

// A warranty deleted while its thing was being made never saw the thing, so
// the cleanup deletes it rather than leaving it behind.
func TestWherehouseDropThingOfDeletedWarranty(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	f.wh.mu.Lock()
	thing := f.wh.add("Lamp", nil)
	f.wh.mu.Unlock()
	if _, err := f.srv.store.DeleteWarranty(ctx, f.alice.ID, f.warrantyID); err != nil {
		t.Fatal(err)
	}
	f.srv.dropUnlinkedThing(f.alice.ID, f.warrantyID, f.client(t), thing.ID)
	f.srv.background.Wait()
	if th := f.wh.get(thing.ID); th != nil {
		t.Errorf("thing left behind: %+v", th)
	}
}

func TestWherehouseDeletedWarrantyLeaves(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	c, err := f.srv.store.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", Status: "open", ReportedOn: "2026-01-02"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.syncWherehouseClaim(ctx, f.alice.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	if th := f.wh.get(thing); len(th.Maintenance) != 1 {
		t.Fatalf("maintenance = %+v", th.Maintenance)
	}
	if w := f.call(t, f.srv.deleteWarranty, http.MethodDelete, ``); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if th := f.wh.get(thing); th == nil || th.app != "" || len(th.Maintenance) != 0 {
		t.Errorf("thing after the warranty was deleted = %+v", th)
	}
}

func TestWherehouseSplit(t *testing.T) {
	f := newWherehouseFixture(t)
	f.wh.mu.Lock()
	shelf := f.wh.add("Shelf", nil)
	f.wh.mu.Unlock()
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, fmt.Sprintf(`{"parentId":%d}`, shelf.ID)); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	orig := f.warranty(t, f.warrantyID).Wherehouse.Thing
	w := f.call(t, f.srv.splitWarranty, http.MethodPost, `{"pieces":1,"serialNumbers":[],"wherehouse":true}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("split = %d %s", w.Code, w.Body)
	}
	var out struct {
		Warranty        store.Warranty `json:"warranty"`
		WherehouseError string         `json:"wherehouseError"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.WherehouseError != "" || out.Warranty.Wherehouse == nil {
		t.Fatalf("split result = %+v", out)
	}
	split := f.wh.get(out.Warranty.Wherehouse.Thing)
	if split.Quantity != 1 || split.ParentID == nil || *split.ParentID != shelf.ID || split.app != fmt.Sprint(out.Warranty.ID) {
		t.Errorf("new thing = %+v", split)
	}
	if th := f.wh.get(orig); th.Quantity != 1 {
		t.Errorf("original quantity = %d, want 1", th.Quantity)
	}
}

func TestWherehouseArchivesAndBringsBack(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	sold := "2026-02-01"
	f.wh.mu.Lock()
	f.wh.things[thing].SoldDate = &sold
	f.wh.changes = []int64{thing}
	f.wh.mu.Unlock()
	if err := f.srv.syncWherehouseUser(ctx, f.alice.ID); err != nil {
		t.Fatal(err)
	}
	if f.warranty(t, f.warrantyID).ArchivedAt == nil {
		t.Fatal("not archived when sold")
	}
	f.wh.mu.Lock()
	f.wh.things[thing].SoldDate = nil
	f.wh.mu.Unlock()
	if err := f.srv.syncWherehouseUser(ctx, f.alice.ID); err != nil {
		t.Fatal(err)
	}
	if f.warranty(t, f.warrantyID).ArchivedAt != nil {
		t.Fatal("still archived after the sale was undone")
	}
	// Archived by hand, it stays archived.
	if err := f.srv.store.SetArchived(ctx, f.alice.ID, f.warrantyID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.syncWherehouseUser(ctx, f.alice.ID); err != nil {
		t.Fatal(err)
	}
	if f.warranty(t, f.warrantyID).ArchivedAt == nil {
		t.Error("brought back though it was archived by hand")
	}
}

func TestWherehouseAdopt(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	st := f.srv.store
	// The thing the warranty's HomeBox item became, with the claim already
	// in its maintenance log, and a thing the warranty is wrongly linked to.
	c, err := st.CreateClaim(ctx, f.alice.ID, f.warrantyID, &store.ClaimInput{Title: "Flickers", Status: "open", ReportedOn: "2026-01-02"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := st.Settings(ctx, f.alice.ID)
	title := f.srv.claimMaintenance(c, settings).Title
	f.wh.mu.Lock()
	moved := f.wh.add("Lamp", nil)
	moved.Maintenance = []wherehouse.Maintenance{{ID: 100, Title: title}}
	f.wh.fromHomeBox["hb-lamp"] = moved.ID
	other := f.wh.add("Kettle", nil)
	f.wh.mu.Unlock()
	if err := st.SetHomeBox(ctx, f.alice.ID, f.warrantyID, &store.HomeBoxLink{ID: "hb-lamp"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWherehouse(ctx, f.alice.ID, f.warrantyID, &store.WherehouseLink{Thing: other.ID}); err != nil {
		t.Fatal(err)
	}

	if err := f.srv.adoptWherehouse(ctx, f.alice.ID, f.client(t), true); err != nil {
		t.Fatal(err)
	}
	f.srv.background.Wait()
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse == nil || wr.Wherehouse.Thing != moved.ID {
		t.Fatalf("linked to %+v, want %d", wr.Wherehouse, moved.ID)
	}
	if th := f.wh.get(moved.ID); th.app != fmt.Sprint(f.warrantyID) || len(th.Maintenance) != 1 {
		t.Errorf("moved thing = %+v", th)
	}

	// Unlinked by hand, it isn't linked again.
	if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code != 200 {
		t.Fatalf("unlink = %d %s", w.Code, w.Body)
	}
	if err := f.srv.adoptWherehouse(ctx, f.alice.ID, f.client(t), true); err != nil {
		t.Fatal(err)
	}
	if wr := f.warranty(t, f.warrantyID); wr.Wherehouse != nil {
		t.Errorf("linked again: %+v", wr.Wherehouse)
	}
}

func TestWherehousePing(t *testing.T) {
	f := newWherehouseFixture(t)
	ctx := context.Background()
	if err := f.srv.store.SetWherehouseHookSecret(ctx, f.conn, "secret"); err != nil {
		t.Fatal(err)
	}
	ping := func(sig string) int {
		r := httptest.NewRequest(http.MethodPost, "/hooks/wherehouse", strings.NewReader(`{"event":"changed"}`))
		r.Header.Set("X-Wherehouse-Signature", sig)
		w := httptest.NewRecorder()
		f.srv.wherehousePing(w, r)
		f.srv.background.Wait()
		return w.Code
	}
	m := hmac.New(sha256.New, []byte("secret"))
	m.Write([]byte(`{"event":"changed"}`))
	if code := ping("sha256=" + hex.EncodeToString(m.Sum(nil))); code != http.StatusNoContent {
		t.Errorf("signed ping = %d", code)
	}
	if code := ping("sha256=00"); code != http.StatusUnauthorized {
		t.Errorf("badly signed ping = %d", code)
	}
}

func TestWherehouseDisconnect(t *testing.T) {
	for _, unlink := range []bool{false, true} {
		f := newWherehouseFixture(t)
		if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
			t.Fatalf("add = %d %s", w.Code, w.Body)
		}
		thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
		target := "/api/settings/wherehouse"
		if unlink {
			target += "?unlink=1"
		}
		r := httptest.NewRequest(http.MethodDelete, target, nil)
		r = r.WithContext(auth.WithUser(r.Context(), f.alice))
		w := httptest.NewRecorder()
		r.SetPathValue("id", fmt.Sprint(f.conn))
		f.srv.disconnectWherehouse(w, r)
		if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"revokeManually":false}` {
			t.Fatalf("disconnect = %d %s", w.Code, w.Body)
		}
		// The token is deleted there.
		f.wh.mu.Lock()
		revoked := f.wh.revoked
		f.wh.mu.Unlock()
		if !revoked {
			t.Error("the token is left in Wherehouse")
		}
		wr := f.warranty(t, f.warrantyID)
		th := f.wh.get(thing)
		if unlink && (wr.Wherehouse != nil || th.app != "") {
			t.Errorf("still linked after unlinking all: %+v, thing %+v", wr.Wherehouse, th)
		}
		if !unlink && (wr.Wherehouse == nil || th.app == "") {
			t.Errorf("unlinked though only disconnected: %+v, thing %+v", wr.Wherehouse, th)
		}
	}
}

// Disconnecting works with a Wherehouse that refuses the token, e.g. one
// set up afresh, or that can't be reached: what it can't be told is done
// here only.
func TestWherehouseDisconnectFromGone(t *testing.T) {
	for _, how := range []string{"token refused", "unreachable"} {
		for _, unlink := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s unlink=%v", how, unlink), func(t *testing.T) {
				f := newWherehouseFixture(t)
				f.photosOnly(t, true)
				f.addPhoto(t)
				if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
					t.Fatalf("add = %d %s", w.Code, w.Body)
				}
				if how == "token refused" {
					f.wh.mu.Lock()
					f.wh.rejectToken = true
					f.wh.mu.Unlock()
				} else {
					f.whServer.Close()
				}
				target := "/api/settings/wherehouse"
				if unlink {
					target += "?unlink=1"
				}
				r := httptest.NewRequest(http.MethodDelete, target, nil)
				r = r.WithContext(auth.WithUser(r.Context(), f.alice))
				w := httptest.NewRecorder()
				r.SetPathValue("id", fmt.Sprint(f.conn))
				f.srv.disconnectWherehouse(w, r)
				// A token refused is gone already; one Wherehouse can't
				// be told about is to be deleted there by hand.
				want := fmt.Sprintf(`{"revokeManually":%v}`, how == "unreachable")
				if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != want {
					t.Fatalf("disconnect = %d %s, want %s", w.Code, w.Body, want)
				}
				if conns, err := f.srv.store.WherehouseConns(context.Background(), f.alice.ID); err != nil || len(conns) != 0 {
					t.Errorf("still connected: %+v, %v", conns, err)
				}
				if wr := f.warranty(t, f.warrantyID); (wr.Wherehouse == nil) != unlink {
					t.Errorf("linked = %v, unlink = %v", wr.Wherehouse != nil, unlink)
				}
			})
		}
	}
}

// Connecting sends a PKCE challenge, and swaps the code with its verifier,
// which only this browser's cookie has.
func TestWherehouseConnectPKCE(t *testing.T) {
	f := newWherehouseFixture(t)
	w := f.call(t, f.srv.connectWherehouse, http.MethodPost, ``)
	if w.Code != 200 {
		t.Fatalf("connect = %d %s", w.Code, w.Body)
	}
	var out struct{ URL string }
	json.Unmarshal(w.Body.Bytes(), &out)
	u, err := url.Parse(out.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("state") == "" {
		t.Fatalf("connect URL = %s", out.URL)
	}
	cookies := w.Result().Cookies()
	var verifier string
	for _, c := range cookies {
		if c.Name == wherehouseVerifierCookie {
			verifier = c.Value
		}
	}
	if len(verifier) < 43 || len(verifier) > 128 || wherehouse.Challenge(verifier) != q.Get("code_challenge") {
		t.Fatalf("verifier %q doesn't match the challenge", verifier)
	}
	f.wh.mu.Lock()
	f.wh.challenge, f.wh.code = q.Get("code_challenge"), "the-code"
	f.wh.mu.Unlock()

	callback := func(drop string) string {
		r := httptest.NewRequest(http.MethodGet, "/wherehouse/callback?code=the-code&state="+q.Get("state"), nil)
		for _, c := range cookies {
			if c.Name != drop {
				r.AddCookie(c)
			}
		}
		r = r.WithContext(auth.WithUser(r.Context(), f.alice))
		w := httptest.NewRecorder()
		f.srv.wherehouseCallback(w, r)
		f.srv.background.Wait()
		return w.Header().Get("Location")
	}
	// Without the verifier, the code is worth nothing.
	if to := callback(wherehouseVerifierCookie); !strings.Contains(to, "wherehouse=state") {
		t.Errorf("without the verifier, back to %s", to)
	}
	if to := callback(""); !strings.Contains(to, "wherehouse=connected") {
		t.Fatalf("back to %s", to)
	}
	// Connecting to the same household again replaces its token.
	conns, err := f.srv.store.WherehouseConns(context.Background(), f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || conns[0].Token != "wh-token-2" || conns[0].HouseholdID != 1 {
		t.Errorf("connections = %+v", conns)
	}
}
