package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

// wherehouseFor returns a Wherehouse client with the person's token, or nil
// when Wherehouse isn't set up here or they haven't connected.
func (s *Server) wherehouseFor(st *store.Settings) *wherehouse.Client {
	if s.cfg.Wherehouse.URL == "" || st.WherehouseToken == "" {
		return nil
	}
	return wherehouse.New(s.cfg.Wherehouse.URL, st.WherehouseToken)
}

// wherehouseFail reports an error from Wherehouse: a token it no longer
// takes or a thing that's gone in the reader's language, anything else as
// Wherehouse put it.
func wherehouseFail(w http.ResponseWriter, err error) {
	var we *wherehouse.Error
	switch {
	case errors.As(err, &we) && we.Unauthorized():
		writeMsg(w, http.StatusBadGateway, "server.wherehouseTokenRejected", nil)
	case errors.As(err, &we) && we.NotFound():
		writeMsg(w, http.StatusNotFound, "server.wherehouseThingGone", nil)
	default:
		writeErr(w, http.StatusBadGateway, err)
	}
}

func (s *Server) userWherehouse(w http.ResponseWriter, r *http.Request) (*store.Settings, *wherehouse.Client) {
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return nil, nil
	}
	wh := s.wherehouseFor(st)
	if wh == nil {
		writeMsg(w, http.StatusBadRequest, "server.wherehouseNotConnected", nil)
		return nil, nil
	}
	return st, wh
}

// ---- connecting ----

const wherehouseStateCookie = "warranteed_wherehouse_state"

func (s *Server) wherehouseCallbackURL() string {
	return strings.TrimSuffix(s.cfg.AppURL.String(), "/") + "/wherehouse/callback"
}

// connectWherehouse answers where to send the browser to connect: Wherehouse's
// page where the person allows Warranteed.
func (s *Server) connectWherehouse(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Wherehouse.URL == "" {
		writeMsg(w, http.StatusBadRequest, "server.wherehouseNotConfigured", nil)
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: wherehouseStateCookie, Value: state, Path: "/wherehouse/", MaxAge: 600,
		HttpOnly: true, Secure: s.cfg.Secure(), SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]string{"url": wherehouse.ConnectURL(s.cfg.Wherehouse.PublicURL, s.wherehouseCallbackURL(), state)})
}

// wherehouseCallback is where Wherehouse sends the browser back with a code,
// which is swapped for the person's token.
func (s *Server) wherehouseCallback(w http.ResponseWriter, r *http.Request) {
	done := func(result string) {
		http.SetCookie(w, &http.Cookie{Name: wherehouseStateCookie, Path: "/wherehouse/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.Secure()})
		http.Redirect(w, r, "/settings?wherehouse="+result+"#wherehouse", http.StatusSeeOther)
	}
	q := r.URL.Query()
	c, err := r.Cookie(wherehouseStateCookie)
	if err != nil || c.Value == "" || c.Value != q.Get("state") {
		done("state")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		done("denied")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	got, err := wherehouse.New(s.cfg.Wherehouse.URL, "").Exchange(ctx, q.Get("code"), s.wherehouseCallbackURL())
	if err != nil {
		slog.Warn("wherehouse connect", "err", err)
		done("failed")
		return
	}
	if err := s.store.SetWherehouseConnection(r.Context(), userID(r), got.Token, got.Household.Name); err != nil {
		fail(w, err)
		return
	}
	// Pings make changes there show here at once; without them, the
	// regular reads still catch everything.
	hook := strings.TrimSuffix(s.cfg.AppURL.String(), "/") + "/hooks/wherehouse"
	if secret, err := wherehouse.New(s.cfg.Wherehouse.URL, got.Token).SetHook(ctx, hook); err != nil {
		slog.Warn("wherehouse pings", "err", err)
	} else if err := s.store.SetWherehouseHookSecret(r.Context(), userID(r), secret); err != nil {
		fail(w, err)
		return
	}
	// Links from before, and warranties linked to HomeBox, are put in step
	// with this Wherehouse. That can take a while with many warranties.
	uid := userID(r)
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if err := s.adoptWherehouse(ctx, uid, true); err != nil {
			slog.Warn("wherehouse: put warranties in step", "err", err)
		}
		// Photos left in Wherehouse by an earlier disconnect come back,
		// or stay there, as the setting says.
		if err := s.settleAllWherehousePhotos(ctx, uid, false); err != nil {
			slog.Warn("wherehouse: settle photos", "err", err)
		}
	}()
	done("connected")
}

// disconnectWherehouse forgets the person's connection. Links to things stay,
// for whenever someone connects again.
func (s *Server) disconnectWherehouse(w http.ResponseWriter, r *http.Request) {
	// With ?unlink=1, warranties are unlinked too, both here and there,
	// while the token still works.
	unlink := r.URL.Query().Get("unlink") == "1"
	uid := userID(r)
	st, err := s.store.Settings(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	if wh := s.wherehouseFor(st); wh != nil {
		// Photos kept only in Wherehouse come back here while the
		// connection still reads them. Ones Wherehouse can't hand over
		// stay as they are, shown through anyone else's connection, and
		// come back after connecting again.
		photoCtx, cancelPhotos := context.WithTimeout(r.Context(), 5*time.Minute)
		if err := s.settleAllWherehousePhotos(photoCtx, uid, true); err != nil {
			slog.Warn("wherehouse: photos back on disconnecting", "err", err)
		}
		cancelPhotos()
		ctx := r.Context()
		if unlink {
			if err := s.unlinkAllWherehouse(ctx, uid, wh); err != nil {
				fail(w, err)
				return
			}
		}
		// Stop the pings; if Wherehouse can't be reached they're refused
		// here anyway, as the secret goes with the connection.
		if err := wh.RemoveHook(r.Context()); err != nil {
			slog.Warn("wherehouse pings", "err", err)
		}
	}
	if err := s.store.SetWherehouseConnection(r.Context(), uid, "", ""); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unlinkAllWherehouse unlinks the warranties someone can see from their
// things in Wherehouse, which keep their details as their own. A warranty
// someone else still reaches through their own connection stays linked.
// Unlinked warranties aren't linked again by themselves.
func (s *Server) unlinkAllWherehouse(ctx context.Context, uid int64, wh *wherehouse.Client) error {
	states, err := s.store.WherehouseStates(ctx, uid)
	if err != nil {
		return err
	}
	for _, ws := range states {
		if ws.Thing == nil {
			continue
		}
		unlock := s.lockPhotos(wherehousePushKey(ws.ID))
		gone, err := s.unlinkForDisconnect(ctx, uid, wh, ws.ID, *ws.Thing)
		unlock()
		if err != nil {
			return err
		}
		// A Wherehouse that refuses the token or can't be reached isn't
		// asked again for every warranty.
		if gone {
			wh = nil
		}
	}
	return nil
}

// unlinkForDisconnect unlinks one warranty, there through wh if it's not
// nil, and here. gone reports that wh refused the token or couldn't be reached.
func (s *Server) unlinkForDisconnect(ctx context.Context, uid int64, wh *wherehouse.Client, id, thing int64) (gone bool, err error) {
	clients, _, err := s.wherehouseClients(ctx, uid, id)
	if err != nil {
		return false, err
	}
	for _, other := range clients {
		if wh == nil || other == wh || other.Token == wh.Token {
			continue
		}
		if t, err := other.LinkedThing(ctx, id); err == nil && t.ID == thing {
			return false, nil // someone else still uses it
		}
	}
	// Only a thing that really is linked to this warranty is unlinked there.
	// If Wherehouse can't be told (it's gone, set up afresh or out of
	// reach), it's unlinked here anyway: disconnecting never gets stuck.
	if wh != nil {
		t, err := wh.LinkedThing(ctx, id)
		if err == nil && t.ID == thing {
			err = wh.RemoveLink(ctx, thing)
		}
		if err != nil && !wherehouseNotFound(err) {
			slog.Warn("wherehouse: unlink there on disconnecting", "warranty", id, "err", err)
			var we *wherehouse.Error
			gone = !errors.As(err, &we) || we.Unauthorized()
		}
	}
	if err := s.store.SetWherehouse(ctx, uid, id, nil); err != nil {
		return gone, err
	}
	return gone, s.store.SetWherehouseUnlinked(ctx, id)
}

// ---- finding things ----

type wherehouseThing struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	// Path is where it is, e.g. "House › Garage".
	Path    string `json:"path"`
	IsPlace bool   `json:"isPlace"`
}

func toWherehouseThings(list []wherehouse.Thing) []wherehouseThing {
	out := make([]wherehouseThing, 0, len(list))
	for _, t := range list {
		out = append(out, wherehouseThing{ID: t.ID, Name: t.Name, Label: t.Label, Path: t.PathText(),
			IsPlace: t.Kind.IsPlace || t.Contains > 0})
	}
	return out
}

func (s *Server) wherehousePlaces(w http.ResponseWriter, r *http.Request) {
	_, wh := s.userWherehouse(w, r)
	if wh == nil {
		return
	}
	list, err := wh.Places(r.Context())
	if err != nil {
		wherehouseFail(w, err)
		return
	}
	writeJSON(w, 200, toWherehouseThings(list))
}

func (s *Server) searchWherehouse(w http.ResponseWriter, r *http.Request) {
	_, wh := s.userWherehouse(w, r)
	if wh == nil {
		return
	}
	list, err := wh.Search(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		wherehouseFail(w, err)
		return
	}
	writeJSON(w, 200, toWherehouseThings(list))
}

// ---- linking ----

// addToWherehouse makes a thing for the warranty's product in Wherehouse,
// inside parentId (null: not put anywhere yet), and links them.
func (s *Server) addToWherehouse(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ParentID *int64 `json:"parentId"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.linkWherehouse(w, r, true, func(ctx context.Context, wh *wherehouse.Client, wr *store.Warranty) (int64, error) {
		t, err := wh.Create(ctx, newThing(wr, in.ParentID))
		if err != nil {
			return 0, err
		}
		return t.ID, nil
	})
}

// linkWherehouseThing links the warranty to a thing already in Wherehouse.
func (s *Server) linkWherehouseThing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ThingID int64 `json:"thingId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.ThingID <= 0 {
		writeMsg(w, http.StatusBadRequest, "server.wherehouseNeedsThing", nil)
		return
	}
	s.linkWherehouse(w, r, false, func(context.Context, *wherehouse.Client, *store.Warranty) (int64, error) { return in.ThingID, nil })
}

func newThing(wr *store.Warranty, parent *int64) wherehouse.NewThing {
	t := wherehouse.NewThing{ParentID: parent, Name: displayName(wr), Quantity: max(wr.Quantity, 1),
		Manufacturer: wr.Brand, Model: wr.Model}
	if len(wr.SerialNumbers) > 0 {
		t.Serial = wr.SerialNumbers[0]
	}
	return t
}

// linkWherehouse links the warranty to the thing pick finds, or makes when
// created is true.
func (s *Server) linkWherehouse(w http.ResponseWriter, r *http.Request, created bool,
	pick func(context.Context, *wherehouse.Client, *store.Warranty) (int64, error)) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, busy := s.homeboxBusy.LoadOrStore(wherehouseKey(id), true); busy {
		writeMsg(w, http.StatusConflict, "server.homeboxBusy", nil)
		return
	}
	defer s.homeboxBusy.Delete(wherehouseKey(id))
	uid := userID(r)
	st, wh := s.userWherehouse(w, r)
	if wh == nil {
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	if wr.Wherehouse != nil {
		writeMsg(w, http.StatusConflict, "server.wherehouseAlreadyLinked", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	thing, err := pick(ctx, wh, wr)
	if err != nil {
		wherehouseFail(w, err)
		return
	}
	if err := s.joinWherehouse(ctx, uid, st, wh, wr, thing); err != nil {
		if created {
			s.dropUnlinkedThing(uid, id, wh, thing)
		}
		var ve *store.ValidationError
		if errors.Is(err, store.ErrNotFound) || errors.As(err, &ve) {
			fail(w, err)
		} else {
			wherehouseFail(w, err)
		}
		return
	}
	if wr, err = s.store.GetWarranty(r.Context(), uid, id); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

// dropUnlinkedThing deletes a thing made for a warranty that couldn't be
// linked to it after all, so trying again doesn't leave one more behind.
func (s *Server) dropUnlinkedThing(uid, id int64, wh *wherehouse.Client, thing int64) {
	s.inBackground("wherehouse: delete a thing that wasn't linked", func(ctx context.Context) error {
		// A warranty deleted meanwhile never saw the thing, so it goes too.
		wr, err := s.store.GetWarranty(ctx, uid, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil && wr.Wherehouse != nil {
			return nil
		}
		if err := wh.Delete(ctx, thing); err != nil && !wherehouseNotFound(err) {
			return err
		}
		return nil
	})
}

// joinWherehouse links a warranty to a thing: the thing gets its purchase and
// warranty details, which Warranteed keeps from then on, the warranty's
// photos if it has none, and its claims in the maintenance log.
func (s *Server) joinWherehouse(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client,
	wr *store.Warranty, thing int64) error {
	t, err := wh.SetLink(ctx, thing, s.wherehouseLink(wr, st))
	if err != nil {
		return err
	}
	if err := s.store.SetWherehouse(ctx, uid, wr.ID, &store.WherehouseLink{Thing: t.ID, Label: t.Label, Path: t.PathText()}); err != nil {
		// Not linked here, so not there either.
		if err := wh.RemoveLink(ctx, t.ID); err != nil {
			slog.Warn("wherehouse: remove a link not saved here", "thing", t.ID, "err", err)
		}
		return err
	}
	// An edit made meanwhile found the warranty not linked yet and sent
	// nothing; this sends the warranty as it is now.
	s.pushWherehouseLater(uid, wr.ID)
	// A thing with photos of its own keeps them; the warranty's are taken
	// as being there, and only photos added later are sent.
	if len(t.Photos) > 0 {
		photos, err := s.store.WherehousePhotosToSend(ctx, wr.ID, t.ID)
		if err != nil {
			return err
		}
		ids := make([]int64, len(photos))
		for i, a := range photos {
			ids[i] = a.ID
		}
		if err := s.store.SentToWherehouse(ctx, t.ID, ids...); err != nil {
			return err
		}
	}
	if err := s.sendToWherehouse(ctx, uid, wh, wr.ID); err != nil {
		slog.Warn("photos and receipts to wherehouse", "warranty", wr.ID, "err", err)
	}
	ids, err := s.store.ClaimIDs(ctx, wr.ID)
	if err != nil {
		return err
	}
	if err := s.matchClaimEntries(ctx, st, t, ids); err != nil {
		return err
	}
	for _, c := range ids {
		if err := s.syncWherehouseClaim(ctx, uid, c); err != nil {
			slog.Warn("claim to wherehouse", "claim", c, "err", err)
		}
	}
	return nil
}

// wherehouseLink is what a linked thing is told of its warranty.
func (s *Server) wherehouseLink(wr *store.Warranty, st *store.Settings) wherehouse.Link {
	lang := st.EffectiveLanguage()
	p := &wherehouse.Purchase{
		PurchasePrice:    wr.Price,
		PurchaseFrom:     wr.Vendor,
		Currency:         strings.ToUpper(wr.Currency),
		LifetimeWarranty: wr.IsLifetime,
		WarrantyDetails:  warrantyDetails(wr, st),
	}
	if wr.PurchaseDate != "" {
		d := wr.PurchaseDate
		p.PurchaseDate = &d
	}
	if !wr.IsLifetime && wr.ExpiresOn != nil {
		d := *wr.ExpiresOn
		p.WarrantyUntil = &d
	}
	title := i18n.T(lang, "server.wherehouseTitle", nil)
	switch {
	case wr.VoidedAt != nil:
		title = i18n.T(lang, "server.wherehouseTitleVoid", nil)
	case wr.IsLifetime:
		title = i18n.T(lang, "server.wherehouseTitleLifetime", nil)
	case wr.ExpiresOn != nil:
		title = i18n.T(lang, "server.wherehouseTitleUntil", i18n.Args{"date": *wr.ExpiresOn})
	}
	return wherehouse.Link{
		ExternalID: strconv.FormatInt(wr.ID, 10),
		URL:        fmt.Sprintf("%s/w/%d", strings.TrimSuffix(s.cfg.AppURL.String(), "/"), wr.ID),
		Title:      title,
		Purchase:   p,
	}
}

// wherehouseClients are the Wherehouse connections to update a warranty's
// thing with: the person's first, then those of others who can see it.
func (s *Server) wherehouseClients(ctx context.Context, uid, warrantyID int64) ([]*wherehouse.Client, *store.Settings, error) {
	users := []int64{uid}
	others, err := s.store.WherehouseUsers(ctx, warrantyID)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range others {
		if !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	var out []*wherehouse.Client
	var first *store.Settings
	for _, u := range users {
		st, err := s.store.Settings(ctx, u)
		if err != nil {
			return nil, nil, err
		}
		if first == nil {
			first = st
		}
		if c := s.wherehouseFor(st); c != nil {
			out = append(out, c)
		}
	}
	return out, first, nil
}

// wherehouseKey marks a warranty being added to or linked with Wherehouse.
func wherehouseKey(id int64) string { return "wh:" + strconv.FormatInt(id, 10) }

// wherehousePushKey locks a warranty while its details are sent to its thing
// or it's unlinked.
func wherehousePushKey(id int64) string { return "wh-push:" + strconv.FormatInt(id, 10) }

// pushWherehouseLater sends a linked warranty's changed details to its thing.
// Pushes take turns and each reads the warranty once it's its turn, so an
// older one can't land after a newer one.
func (s *Server) pushWherehouseLater(uid, warrantyID int64) {
	s.inBackground("update wherehouse", func(ctx context.Context) error {
		defer s.lockPhotos(wherehousePushKey(warrantyID))()
		wr, err := s.store.GetWarranty(ctx, uid, warrantyID)
		if err != nil || wr.Wherehouse == nil {
			return err
		}
		clients, st, err := s.wherehouseClients(ctx, uid, warrantyID)
		if err != nil {
			return err
		}
		for _, wh := range clients {
			if _, err = wh.SetLink(ctx, wr.Wherehouse.Thing, s.wherehouseLink(wr, st)); err == nil {
				return nil
			}
		}
		return err
	})
}

func (s *Server) unlinkWherehouse(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// A push waiting meanwhile would link the thing again after it's removed.
	defer s.lockPhotos(wherehousePushKey(id))()
	uid := userID(r)
	wr, err := s.store.GetWarranty(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	if wr.Wherehouse == nil {
		writeJSON(w, 200, wr)
		return
	}
	// The thing there keeps its details, which become Wherehouse's own. If
	// Wherehouse can't be told, the link stays so unlinking can be tried
	// again; with no connection left at all, there's no one to tell.
	clients, _, err := s.wherehouseClients(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	for _, wh := range clients {
		var we *wherehouse.Error
		if err = wh.RemoveLink(r.Context(), wr.Wherehouse.Thing); err == nil || (errors.As(err, &we) && we.NotFound()) {
			err = nil
			break
		}
	}
	if err != nil {
		wherehouseFail(w, err)
		return
	}
	if err := s.store.SetWherehouse(r.Context(), uid, id, nil); err != nil {
		fail(w, err)
		return
	}
	if err := s.store.SetWherehouseUnlinked(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	// Photos kept on the thing come back here; the thing keeps them too.
	s.settleWherehousePhotosLater(uid, id)
	if wr, err = s.store.GetWarranty(r.Context(), uid, id); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

// ---- claims ----

// syncWherehouseClaim writes a claim to its warranty's thing: a new entry in
// the maintenance log, or the one it has, written over.
func (s *Server) syncWherehouseClaim(ctx context.Context, uid, claimID int64) error {
	defer s.lockPhotos(claimKey(claimID) + ":wh")()
	entry, thing, err := s.store.ClaimWherehouse(ctx, claimID)
	if errors.Is(err, store.ErrNotFound) || thing == nil {
		return nil
	}
	if err != nil {
		return err
	}
	c, _, err := s.store.ClaimSync(ctx, claimID)
	if err != nil {
		return err
	}
	clients, st, err := s.wherehouseClients(ctx, uid, c.WarrantyID)
	if err != nil {
		return err
	}
	m := s.claimMaintenance(c, st)
	err = errors.New("no wherehouse connection")
	for _, wh := range clients {
		if entry != nil {
			if err = wh.UpdateMaintenance(ctx, *entry, m); err == nil {
				return nil
			}
			var we *wherehouse.Error
			if !errors.As(err, &we) || !we.NotFound() {
				continue
			}
		}
		var id int64
		if id, err = wh.AddMaintenance(ctx, *thing, m); err != nil {
			continue
		}
		saved, err := s.store.SetClaimWherehouseEntry(ctx, claimID, *thing, id)
		if err != nil || saved {
			return err
		}
		return wh.DeleteMaintenance(ctx, id)
	}
	return err
}

// claimMaintenance is how a claim reads in Wherehouse's maintenance log.
func (s *Server) claimMaintenance(c *store.Claim, st *store.Settings) wherehouse.Maintenance {
	e := s.claimEntry(c, st)
	m := wherehouse.Maintenance{Title: e.Name, Notes: e.Description, Cost: c.Cost}
	if c.ResolvedOn != nil {
		m.DoneOn = c.ResolvedOn
	} else {
		reported := c.ReportedOn
		m.DoneOn = &reported
	}
	return m
}

// ---- keeping in step ----

// wherehousePing is where Wherehouse pings when something changed: the
// signature says whose connection it is, and that person's changes are read
// at once.
func (s *Server) wherehousePing(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("X-Wherehouse-Signature")
	hooks, err := s.store.WherehouseHooks(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	for _, h := range hooks {
		m := hmac.New(sha256.New, []byte(h.Secret))
		m.Write(body)
		if hmac.Equal([]byte(sig), []byte("sha256="+hex.EncodeToString(m.Sum(nil)))) {
			s.syncWherehouseSoon(h.UserID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	w.WriteHeader(http.StatusUnauthorized)
}

// syncWherehouseSoon reads a person's Wherehouse changes now, once at a
// time: a ping while reading reads again after.
func (s *Server) syncWherehouseSoon(uid int64) {
	if _, running := s.wherehouseSyncing.LoadOrStore(uid, false); running {
		s.wherehouseSyncing.Store(uid, true) // read again once done
		return
	}
	s.inBackground("wherehouse sync", func(ctx context.Context) error {
		for {
			if err := s.syncWherehouseUser(ctx, uid); err != nil {
				s.wherehouseSyncing.Delete(uid)
				return err
			}
			if again, _ := s.wherehouseSyncing.Load(uid); again != true {
				s.wherehouseSyncing.Delete(uid)
				return nil
			}
			s.wherehouseSyncing.Store(uid, false)
		}
	})
}

// SyncWherehouse reads each connected person's Wherehouse changes every few
// minutes: where linked things are now, and which were sold, archived or
// deleted there. A sold or archived thing archives its warranty; a deleted
// one is unlinked.
func (s *Server) SyncWherehouse(ctx context.Context) {
	if s.cfg.Wherehouse.URL == "" {
		return
	}
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		users, err := s.store.WherehouseConnected(ctx)
		if err != nil {
			slog.Warn("wherehouse sync", "err", err)
		}
		for _, uid := range users {
			if err := s.syncWherehouseUser(ctx, uid); err != nil {
				slog.Warn("wherehouse sync", "user", uid, "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) syncWherehouseUser(ctx context.Context, uid int64) error {
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return err
	}
	wh := s.wherehouseFor(st)
	if wh == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// Many changes come a page at a time.
	since := st.WherehouseSince
	for range 50 {
		ch, err := wh.Changes(cctx, since)
		if err != nil {
			return err
		}
		if err := s.applyWherehouseChanges(ctx, uid, ch); err != nil {
			return err
		}
		if err := s.store.SetWherehouseSince(ctx, uid, ch.Until); err != nil {
			return err
		}
		if since = ch.Until; !ch.More {
			break
		}
	}
	// Things copied from HomeBox since are picked up.
	return s.adoptWherehouse(cctx, uid, false)
}

// applyWherehouseChanges brings the warranties linked to things that changed
// in Wherehouse in step with them.
func (s *Server) applyWherehouseChanges(ctx context.Context, uid int64, ch *wherehouse.Changes) error {
	for _, t := range ch.Things {
		ids, err := s.store.LinkedToThing(ctx, uid, t.ID)
		if err != nil {
			return err
		}
		for _, wid := range ids {
			if err := s.store.SetWherehouseSeen(ctx, wid, t.Label, t.PathText()); err != nil {
				return err
			}
			gone := t.SoldDate != nil || t.ArchivedAt != nil
			changed, err := s.store.SetWherehouseArchived(ctx, wid, gone)
			if err != nil {
				return err
			}
			switch {
			case changed && gone:
				slog.Info("warranty archived: its thing was sold or archived in Wherehouse", "warranty", wid)
			case changed:
				slog.Info("warranty back from the archive: its thing is back in Wherehouse", "warranty", wid)
			}
		}
	}
	for _, thing := range ch.Deleted {
		ids, err := s.store.LinkedToThing(ctx, uid, thing)
		if err != nil {
			return err
		}
		for _, wid := range ids {
			if err := s.store.SetWherehouse(ctx, uid, wid, nil); err != nil {
				return err
			}
			// Photos kept on it went with it.
			if err := s.store.ForgetWherehousePhotos(ctx, wid, thing); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- moving over ----

// adoptWherehouse brings a person's warranties in step with the Wherehouse
// they connected to. A link the thing there doesn't have, because that
// Wherehouse was set up afresh or the link was removed there, is dropped.
// With all is false, only that is skipped: a warranty linked to a HomeBox
// item that was copied into Wherehouse is linked to the thing it became.
func (s *Server) adoptWherehouse(ctx context.Context, uid int64, all bool) error {
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return err
	}
	wh := s.wherehouseFor(st)
	if wh == nil {
		return nil
	}
	states, err := s.store.WherehouseStates(ctx, uid)
	if err != nil {
		return err
	}
	for _, ws := range states {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ws.Thing != nil {
			if !all {
				continue
			}
			stale, moved, err := s.wherehouseLinkStale(ctx, uid, ws.ID, *ws.Thing)
			if err != nil {
				slog.Warn("check wherehouse link", "warranty", ws.ID, "err", err)
				continue
			}
			if moved != nil {
				// Wherehouse has the warranty on another thing, e.g. after
				// restoring a backup there: the link follows it.
				if err := s.followMovedThing(ctx, uid, ws.ID, *ws.Thing, moved); err != nil {
					slog.Warn("wherehouse: follow a moved link", "warranty", ws.ID, "err", err)
				} else {
					slog.Info("wherehouse link moved to the thing linked there", "warranty", ws.ID, "thing", moved.ID)
				}
				continue
			}
			if stale {
				slog.Info("wherehouse link dropped: the thing there isn't linked to the warranty", "warranty", ws.ID)
				if err := s.store.SetWherehouse(ctx, uid, ws.ID, nil); err != nil {
					return err
				}
				ws.Thing = nil
				if err := s.settleWherehousePhotos(ctx, uid, ws.ID, false); err != nil {
					slog.Warn("wherehouse: photos of a dropped link", "warranty", ws.ID, "err", err)
				}
			}
		}
		if ws.Thing != nil || ws.HomeBoxID == "" {
			continue
		}
		t, err := wh.FromHomeBox(ctx, ws.HomeBoxID)
		var we *wherehouse.Error
		if errors.As(err, &we) && we.NotFound() {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.moveFromHomeBox(ctx, uid, st, wh, ws.ID, t); err != nil {
			slog.Warn("link warranty to wherehouse", "warranty", ws.ID, "err", err)
			continue
		}
		slog.Info("warranty linked to the wherehouse thing its homebox item became", "warranty", ws.ID, "thing", t.ID)
	}
	return nil
}

// wherehouseLinkStale reports whether a warranty's link is gone from
// Wherehouse: no connection of those who can see it finds the thing linked
// to it there. People sharing a warranty may be connected to different
// Wherehouse households, so one of them not finding it isn't enough. A
// connection that can't be asked leaves it undecided, as an error.
// If a connection finds the warranty linked to another thing, that thing
// is returned as moved.
func (s *Server) wherehouseLinkStale(ctx context.Context, uid, warrantyID, thing int64) (stale bool, moved *wherehouse.Thing, err error) {
	clients, _, err := s.wherehouseClients(ctx, uid, warrantyID)
	if err != nil {
		return false, nil, err
	}
	for _, wh := range clients {
		t, err := wh.LinkedThing(ctx, warrantyID)
		var we *wherehouse.Error
		switch {
		case err == nil && t.ID == thing:
			return false, nil, nil
		case err == nil:
			if moved == nil {
				moved = t
			}
		case errors.As(err, &we) && (we.NotFound() || we.Unauthorized()):
			// Not there, or a token that Wherehouse no longer takes.
		default:
			return false, nil, err
		}
	}
	if moved != nil {
		return false, moved, nil
	}
	return true, nil, nil
}

// followMovedThing links a warranty to the thing Wherehouse has it on now
// instead of old. Its photos kept on old are found on that thing by size,
// as a restored thing has the same photos under new IDs.
func (s *Server) followMovedThing(ctx context.Context, uid, wid, old int64, t *wherehouse.Thing) error {
	defer s.lockPhotos(wherehousePushKey(wid))()
	defer s.lockPhotos(wherehousePhotosKey(wid))()
	wr, err := s.store.GetWarranty(ctx, uid, wid)
	if err != nil {
		return err
	}
	claimed := map[int64]bool{}
	for _, a := range wr.Attachments {
		if a.Storage != store.StorageWherehouse || a.WherehouseThing == nil || *a.WherehouseThing != old {
			continue
		}
		for _, p := range t.Photos {
			if p.Size == a.Size && !claimed[p.ID] {
				claimed[p.ID] = true
				if err := s.store.StoredInWherehouse(ctx, a.ID, t.ID, p.ID); err != nil {
					return err
				}
				break
			}
		}
	}
	return s.store.SetWherehouse(ctx, uid, wid, &store.WherehouseLink{Thing: t.ID, Label: t.Label, Path: t.PathText()})
}

// moveFromHomeBox links a warranty to the thing its HomeBox item was copied
// into.
func (s *Server) moveFromHomeBox(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client,
	id int64, t *wherehouse.Thing) error {
	if _, busy := s.homeboxBusy.LoadOrStore(wherehouseKey(id), true); busy {
		return nil
	}
	defer s.homeboxBusy.Delete(wherehouseKey(id))
	wr, err := s.store.GetWarranty(ctx, uid, id)
	if err != nil || wr.Wherehouse != nil {
		return err
	}
	return s.joinWherehouse(ctx, uid, st, wh, wr, t.ID)
}

// matchClaimEntries records the thing's maintenance entries that already
// are the claims, by title, so they're written over instead of added
// again: a maintenance log copied from HomeBox has them, as does a thing
// linked before.
func (s *Server) matchClaimEntries(ctx context.Context, st *store.Settings, t *wherehouse.Thing, claims []int64) error {
	taken := map[int64]bool{}
	for _, cid := range claims {
		c, _, err := s.store.ClaimSync(ctx, cid)
		if err != nil {
			return err
		}
		title := s.claimMaintenance(c, st).Title
		for _, m := range t.Maintenance {
			if m.Title == title && !taken[m.ID] {
				taken[m.ID] = true
				if _, err := s.store.SetClaimWherehouseEntry(ctx, cid, t.ID, m.ID); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// ---- photos and receipts ----

// sendToWherehouseLater sends a linked warranty's new photos and receipts
// to its thing in the background.
func (s *Server) sendToWherehouseLater(uid, wid int64) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		clients, _, err := s.wherehouseClients(ctx, uid, wid)
		if err == nil && len(clients) > 0 {
			err = firstWherehouse(clients, func(wh *wherehouse.Client) error { return s.sendToWherehouse(ctx, uid, wh, wid) })
		}
		if err != nil {
			slog.Warn("photos and receipts to wherehouse", "warranty", wid, "err", err)
		}
	}()
}

// sendToWherehouse sends the warranty's photos stored here that its thing
// hasn't got, and adds its Paperless receipts to the thing: as Paperless
// documents if Wherehouse links them from the same Paperless and can read
// them, else as links to them.
func (s *Server) sendToWherehouse(ctx context.Context, uid int64, wh *wherehouse.Client, wid int64) error {
	// One at a time per warranty, so a photo isn't sent twice.
	defer s.lockPhotos(wherehousePhotosKey(wid))()
	wr, err := s.store.GetWarranty(ctx, uid, wid)
	if err != nil {
		return err
	}
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return err
	}
	// Photos kept only there go there first; what stays here is copied.
	if err := s.settlePhotos(ctx, uid, st, wh, wr); err != nil {
		return err
	}
	if wr.Wherehouse == nil {
		return nil
	}
	thing := wr.Wherehouse.Thing
	photos, err := s.store.WherehousePhotosToSend(ctx, wid, thing)
	if err != nil {
		return err
	}
	for _, a := range photos {
		f, err := s.files.Open(a.LocalPath)
		if err != nil {
			return err
		}
		_, err = wh.UploadPhoto(ctx, thing, a.Filename, a.MimeType, f)
		f.Close()
		if err != nil {
			return err
		}
		if err := s.store.SentToWherehouse(ctx, thing, a.ID); err != nil {
			return err
		}
	}
	t, err := wh.Get(ctx, thing)
	if err != nil {
		return err
	}
	haveDoc := map[int64]bool{}
	for _, a := range t.Attachments {
		if a.PaperlessDocID != nil {
			haveDoc[*a.PaperlessDocID] = true
		}
	}
	haveLink := map[string]bool{}
	for _, l := range t.Links {
		haveLink[l.URL] = true
	}
	// A document ID means the same document only in the same Paperless.
	theirs, err := wh.Paperless(ctx)
	if err != nil {
		return err
	}
	lang := st.EffectiveLanguage()
	for _, a := range wr.Attachments {
		if a.Kind != "purchase" || a.Storage != store.StoragePaperless || a.PaperlessDocID == nil || a.Status != store.StatusReady {
			continue
		}
		doc := *a.PaperlessDocID
		owner := uid
		if a.AddedBy != nil {
			owner = *a.AddedBy
		}
		web, err := s.userPaperlessWebURL(ctx, owner)
		if err != nil {
			return err
		}
		pc, err := s.userPaperless(ctx, owner)
		if err != nil {
			return err
		}
		same := theirs != nil && pc != nil && (sameURL(pc.BaseURL, theirs.URL) || sameURL(web, theirs.PublicURL))
		link := fmt.Sprintf("%s/documents/%d/details", web, doc)
		if (same && haveDoc[doc]) || (web != "" && haveLink[link]) {
			continue
		}
		if same {
			if err := wh.LinkPaperless(ctx, thing, []int64{doc}, "receipt"); err == nil {
				continue
			}
		}
		if web == "" {
			continue
		}
		if err := wh.AddLink(ctx, thing, link, i18n.T(lang, "server.homeboxLinkReceipt", i18n.Args{"name": a.Filename})); err != nil {
			return err
		}
	}
	return nil
}

// sameURL reports whether two addresses are the same, but for case and a
// trailing slash.
func sameURL(a, b string) bool {
	a, b = strings.TrimSuffix(a, "/"), strings.TrimSuffix(b, "/")
	return a != "" && strings.EqualFold(a, b)
}

// ---- deleting ----

// forgetInventory prepares to take a warranty that's about to be deleted
// out of Wherehouse and HomeBox: its claims' maintenance entries, and in
// Wherehouse its link, whose purchase details become the thing's own. The
// returned function does it once the warranty is gone.
func (s *Server) forgetInventory(ctx context.Context, uid, id int64) (func(context.Context) error, error) {
	wr, err := s.store.GetWarranty(ctx, uid, id)
	if err != nil {
		return nil, err
	}
	ids, err := s.store.ClaimIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	var whEntries []int64
	var hbEntries []string
	for _, cid := range ids {
		entry, _, err := s.store.ClaimWherehouse(ctx, cid)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			whEntries = append(whEntries, *entry)
		}
		c, _, err := s.store.ClaimSync(ctx, cid)
		if err != nil {
			return nil, err
		}
		if c.HomeBoxEntry != "" {
			hbEntries = append(hbEntries, c.HomeBoxEntry)
		}
	}
	var whClients []*wherehouse.Client
	if wr.Wherehouse != nil {
		if whClients, _, err = s.wherehouseClients(ctx, uid, id); err != nil {
			return nil, err
		}
	}
	var hbClients []*homebox.Client
	if len(hbEntries) > 0 {
		if hbClients, err = s.claimClients(ctx, uid, id); err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context) error {
		var errs []error
		if wr.Wherehouse != nil {
			errs = append(errs, firstWherehouse(whClients, func(wh *wherehouse.Client) error {
				for _, e := range whEntries {
					if err := wh.DeleteMaintenance(ctx, e); err != nil && !wherehouseNotFound(err) {
						return err
					}
				}
				if err := wh.RemoveLink(ctx, wr.Wherehouse.Thing); err != nil && !wherehouseNotFound(err) {
					return err
				}
				return nil
			}))
		}
		for _, e := range hbEntries {
			err := errors.New("no homebox key")
			for _, hb := range hbClients {
				var he *homebox.Error
				if err = hb.DeleteMaintenance(ctx, e); err == nil || (errors.As(err, &he) && he.NotFound()) {
					err = nil
					break
				}
			}
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}, nil
}

// firstWherehouse runs fn with each connection in turn until one manages.
func firstWherehouse(clients []*wherehouse.Client, fn func(*wherehouse.Client) error) error {
	err := errors.New("no wherehouse connection")
	for _, wh := range clients {
		if err = fn(wh); err == nil {
			return nil
		}
	}
	return err
}

func wherehouseNotFound(err error) bool {
	var we *wherehouse.Error
	return errors.As(err, &we) && we.NotFound()
}
