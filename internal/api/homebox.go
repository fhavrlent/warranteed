package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fhavrlent/warranteed/internal/dates"
	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/store"
)

// homeboxFor returns a HomeBox client with the user's API key, or nil when
// HomeBox isn't set up on the server or the user has no key. The address
// only ever comes from the server configuration.
func (s *Server) homeboxFor(st *store.Settings) *homebox.Client {
	if s.cfg.HomeBox.URL == "" || st.HomeBoxToken == "" {
		return nil
	}
	return homebox.New(s.cfg.HomeBox.URL, st.HomeBoxToken)
}

// homeboxFail reports an error from HomeBox: a rejected key or a missing item
// in the user's language, anything else as HomeBox put it.
func homeboxFail(w http.ResponseWriter, err error) {
	var he *homebox.Error
	switch {
	case errors.As(err, &he) && he.Unauthorized():
		writeMsg(w, http.StatusBadGateway, "server.homeboxKeyRejected", nil)
	case errors.As(err, &he) && he.NotFound():
		writeMsg(w, http.StatusNotFound, "server.homeboxItemGone", nil)
	default:
		writeErr(w, http.StatusBadGateway, err)
	}
}

// userHomeBox loads the user's settings and HomeBox client, or answers with
// an error and returns nil.
func (s *Server) userHomeBox(w http.ResponseWriter, r *http.Request) (*store.Settings, *homebox.Client) {
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return nil, nil
	}
	hb := s.homeboxFor(st)
	if hb == nil {
		writeMsg(w, http.StatusBadRequest, "server.homeboxNotConfigured", nil)
		return nil, nil
	}
	return st, hb
}

type homeboxItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	AssetID  string `json:"assetId"`
	Location string `json:"location"`
	// IsLocation marks a location rather than an item.
	IsLocation bool `json:"isLocation"`
}

func toHomeBoxItems(list []homebox.Summary) []homeboxItem {
	out := make([]homeboxItem, 0, len(list))
	for _, it := range list {
		loc := ""
		if it.Parent != nil {
			loc = it.Parent.Name
		}
		out = append(out, homeboxItem{ID: it.ID, Name: it.Name, AssetID: it.AssetID, Location: loc, IsLocation: it.IsLocation()})
	}
	return out
}

func (s *Server) searchHomeBox(w http.ResponseWriter, r *http.Request) {
	_, hb := s.userHomeBox(w, r)
	if hb == nil {
		return
	}
	list, err := hb.Search(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		homeboxFail(w, err)
		return
	}
	writeJSON(w, 200, toHomeBoxItems(list))
}

// homeboxLocations lists where a new item can be put: locations, and items
// that hold other items, with what they're in.
func (s *Server) homeboxLocations(w http.ResponseWriter, r *http.Request) {
	_, hb := s.userHomeBox(w, r)
	if hb == nil {
		return
	}
	places, err := hb.Places(r.Context())
	if err != nil {
		homeboxFail(w, err)
		return
	}
	out := make([]homeboxItem, 0, len(places))
	for _, p := range places {
		out = append(out, homeboxItem{ID: p.ID, Name: p.Name, Location: strings.Join(p.Path, " › ")})
	}
	writeJSON(w, 200, out)
}

// testHomeBox checks an API key before it is saved; without one it checks
// the stored key.
func (s *Server) testHomeBox(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token *string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.store.Settings(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	probe := *st
	if in.Token != nil && strings.TrimSpace(*in.Token) != "" {
		probe.HomeBoxToken = strings.TrimSpace(*in.Token)
	}
	hb := s.homeboxFor(&probe)
	if hb == nil {
		key := "server.homeboxNeedsToken"
		if s.cfg.HomeBox.URL == "" {
			key = "server.homeboxNotConfigured"
		}
		writeMsg(w, http.StatusBadRequest, key, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	user, err := hb.Self(ctx)
	if err != nil {
		homeboxFail(w, err)
		return
	}
	writeJSON(w, 200, user)
}

// createHomeBoxItem adds the warranty's product to HomeBox and links them.
func (s *Server) createHomeBoxItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LocationID string `json:"locationId"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.linkHomeBox(w, r, func(ctx context.Context, hb *homebox.Client, wr *store.Warranty) (*homebox.Item, bool, error) {
		item, err := hb.Create(ctx, displayName(wr), strings.TrimSpace(in.LocationID))
		return item, false, err
	})
}

// linkHomeBoxItem links the warranty to an item already in HomeBox. Only the
// item's empty fields are filled in.
func (s *Server) linkHomeBoxItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ItemID string `json:"itemId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ItemID) == "" {
		writeMsg(w, http.StatusBadRequest, "server.homeboxNeedsItem", nil)
		return
	}
	s.linkHomeBox(w, r, func(ctx context.Context, hb *homebox.Client, _ *store.Warranty) (*homebox.Item, bool, error) {
		item, err := hb.Get(ctx, strings.TrimSpace(in.ItemID))
		return item, true, err
	})
}

// linkHomeBox gets an item from pick, links it to the warranty, then fills in
// its purchase and warranty details and adds links back to the warranty and
// its receipts in Paperless.
func (s *Server) linkHomeBox(w http.ResponseWriter, r *http.Request,
	pick func(context.Context, *homebox.Client, *store.Warranty) (item *homebox.Item, existing bool, err error),
) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// One add or link per warranty at a time, so two people (or tabs) can't
	// both see it unlinked and create two items.
	if _, busy := s.homeboxBusy.LoadOrStore(id, true); busy {
		writeMsg(w, http.StatusConflict, "server.homeboxBusy", nil)
		return
	}
	defer s.homeboxBusy.Delete(id)
	uid := userID(r)
	st, hb := s.userHomeBox(w, r)
	if hb == nil {
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	if wr.HomeBox != nil {
		writeMsg(w, http.StatusConflict, "server.homeboxAlreadyLinked", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	item, existing, err := pick(ctx, hb, wr)
	if err != nil {
		homeboxFail(w, err)
		return
	}
	// Link first: from here on the item exists, even if filling it in fails.
	if err := s.store.SetHomeBox(r.Context(), uid, id, &store.HomeBoxLink{ID: item.ID, AssetID: item.AssetID}); err != nil {
		fail(w, err)
		return
	}
	// Once the item is filled in, so the two updates don't overlap.
	defer s.syncWarrantyBacklinksLater(r.Context(), id)

	lang := st.EffectiveLanguage()
	// Taking turns with other changes to the item, such as receipts' values.
	unlock := s.lockPhotos("item:" + item.ID)
	item, err = hb.Update(ctx, item.ID, func(it *homebox.Item) { fillHomeBoxItem(it, wr, st, existing) })
	unlock()
	if err != nil {
		homeboxFail(w, err)
		return
	}
	have := map[string]bool{}
	for _, a := range item.Attachments {
		have[a.Path] = true
	}
	type link struct{ url, title, kind string }
	links := []link{{
		url:   strings.TrimSuffix(s.cfg.AppURL.String(), "/") + fmt.Sprintf("/w/%d", wr.ID),
		title: i18n.T(lang, "server.homeboxLinkWarranty", nil),
		kind:  "warranty",
	}}
	// A receipt lives in the Paperless of whoever added it, which in a shared
	// household may not be the person linking.
	webURLs := map[int64]string{}
	for _, a := range wr.Attachments {
		if a.Kind != "purchase" || a.Storage != store.StoragePaperless || a.PaperlessDocID == nil {
			continue
		}
		owner := uid
		if a.AddedBy != nil {
			owner = *a.AddedBy
		}
		web, ok := webURLs[owner]
		if !ok {
			if web, err = s.userPaperlessWebURL(r.Context(), owner); err != nil {
				fail(w, err)
				return
			}
			webURLs[owner] = web
		}
		if web == "" {
			continue
		}
		links = append(links, link{
			url:   fmt.Sprintf("%s/documents/%d/details", web, *a.PaperlessDocID),
			title: i18n.T(lang, "server.homeboxLinkReceipt", i18n.Args{"name": a.Filename}),
			kind:  "receipt",
		})
	}
	for _, l := range links {
		if have[l.url] {
			continue
		}
		if err := hb.AddLink(ctx, item.ID, l.url, l.title, l.kind); err != nil {
			homeboxFail(w, err)
			return
		}
	}
	photoCtx, cancelPhotos := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancelPhotos()
	if err := s.sendHomeBoxPhotos(photoCtx, hb, uid, id); err != nil {
		homeboxFail(w, err)
		return
	}
	// The item is linked either way; a claim that didn't make it is copied
	// again when it's next changed.
	if err := s.syncClaims(photoCtx, uid, id); err != nil {
		slog.Warn("copy claims to homebox", "warranty", id, "err", err)
	}

	wr, err = s.store.GetWarranty(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

// sendHomeBoxPhotos uploads the warranty's product photos that its HomeBox
// item doesn't have from Warranteed yet. The first becomes the item's main
// photo if it has none.
func (s *Server) sendHomeBoxPhotos(ctx context.Context, hb *homebox.Client, uid, wid int64) error {
	// One at a time per warranty, so a photo uploaded while linking isn't sent twice.
	defer s.lockPhotos(wid)()

	wr, err := s.store.GetWarranty(ctx, uid, wid)
	if err != nil || wr.HomeBox == nil {
		return err
	}
	var photos []store.Attachment
	for _, a := range wr.Attachments {
		if a.Kind == "photo" && a.Storage == store.StorageLocal && a.HomeBoxItem != wr.HomeBox.ID {
			photos = append(photos, a)
		}
	}
	if len(photos) == 0 {
		return nil
	}
	defer s.lockPhotos("item:" + wr.HomeBox.ID)()
	item, err := hb.Get(ctx, wr.HomeBox.ID)
	if err != nil {
		return err
	}
	primary := !item.HasPrimaryPhoto()
	for _, a := range photos {
		f, err := s.files.Open(a.LocalPath)
		if err != nil {
			return err
		}
		_, err = hb.UploadPhoto(ctx, item, a.Filename, a.MimeType, f, primary)
		f.Close()
		if err != nil {
			return err
		}
		primary = false
		if err := s.store.SentToHomeBox(ctx, a.ID, item.ID); err != nil {
			return err
		}
	}
	return nil
}

// lockPhotos holds back other photo uploads for a warranty ID or an
// "item:<id>" key until the returned function is called, so they agree on
// the main photo.
func (s *Server) lockPhotos(key any) (unlock func()) {
	lock, _ := s.photoLocks.LoadOrStore(key, new(sync.Mutex))
	lock.(*sync.Mutex).Lock()
	return lock.(*sync.Mutex).Unlock
}

// storeHomeBoxPhoto uploads a product photo to the warranty's HomeBox item
// instead of keeping it here, and records where it is. It becomes the item's
// main photo if it has none.
func (s *Server) storeHomeBoxPhoto(ctx context.Context, hb *homebox.Client, itemID string, a *store.Attachment, photo io.Reader) error {
	defer s.lockPhotos(a.WarrantyID)()
	// Warranties linked to the same item take turns too, so each upload
	// finds its own photo among the item's attachments.
	defer s.lockPhotos("item:" + itemID)()
	item, err := hb.Get(ctx, itemID)
	if err != nil {
		return err
	}
	id, err := hb.UploadPhoto(ctx, item, a.Filename, a.MimeType, photo, !item.HasPrimaryPhoto())
	if err != nil {
		return err
	}
	a.Storage, a.HomeBoxItem, a.HomeBoxAttachment = store.StorageHomeBox, item.ID, id
	return s.store.CreateAttachment(ctx, a)
}

// attachmentHomeBox returns the HomeBox connection of whoever stored a photo
// there, so the household sees it through theirs, like Paperless documents.
func (s *Server) attachmentHomeBox(ctx context.Context, a *store.Attachment, viewer int64) (*homebox.Client, error) {
	owner := viewer
	if a.AddedBy != nil {
		owner = *a.AddedBy
	}
	st, err := s.store.Settings(ctx, owner)
	if err != nil {
		return nil, err
	}
	return s.homeboxFor(st), nil
}

// sendHomeBoxPhotosLater sends a linked warranty's new photos to HomeBox in
// the background, with the HomeBox key of the person who added them.
func (s *Server) sendHomeBoxPhotosLater(uid, wid int64) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		st, err := s.store.Settings(ctx, uid)
		if err != nil {
			slog.Warn("send photos to homebox", "warranty", wid, "err", err)
			return
		}
		hb := s.homeboxFor(st)
		if hb == nil {
			return // they haven't connected HomeBox
		}
		if err := s.sendHomeBoxPhotos(ctx, hb, uid, wid); err != nil {
			slog.Warn("send photos to homebox", "warranty", wid, "err", err)
		}
	}()
}

// userPaperlessWebURL is where a user's Paperless opens in the browser, or "".
func (s *Server) userPaperlessWebURL(ctx context.Context, userID int64) (string, error) {
	st, err := s.store.Settings(ctx, userID)
	if err != nil {
		return "", err
	}
	admin, err := s.store.IsAdmin(ctx, userID)
	if err != nil {
		return "", err
	}
	return s.paperlessWebURL(st, admin), nil
}

// unlinkHomeBox forgets the link; the item stays in HomeBox.
func (s *Server) unlinkHomeBox(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// Updating the documents' links also takes the item off their lists.
	if err := s.store.SetHomeBox(r.Context(), userID(r), id, nil); err != nil {
		fail(w, err)
		return
	}
	s.syncWarrantyBacklinksLater(r.Context(), id)
	wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

// fillHomeBoxItem copies the warranty's details onto the item. For an item
// that was already in HomeBox only empty fields are filled in.
func fillHomeBoxItem(it *homebox.Item, wr *store.Warranty, st *store.Settings, onlyEmpty bool) {
	set := func(dst *string, v string) {
		if v != "" && (!onlyEmpty || *dst == "") {
			*dst = v
		}
	}
	set(&it.Manufacturer, wr.Brand)
	set(&it.ModelNumber, wr.Model)
	if len(wr.SerialNumbers) > 0 {
		set(&it.SerialNumber, wr.SerialNumbers[0])
	}
	set(&it.PurchaseDate, wr.PurchaseDate)
	set(&it.PurchaseFrom, wr.Vendor)
	if wr.Price != nil && (!onlyEmpty || it.PurchasePrice == 0) {
		it.PurchasePrice = *wr.Price
	}
	// HomeBox gives every item a quantity of 1, so that counts as empty.
	if wr.Quantity > 1 && (!onlyEmpty || it.Quantity <= 1) {
		it.Quantity = wr.Quantity
	}
	// Coverage is one thing: an item that already has an end date or a
	// lifetime warranty keeps it, along with its warranty details.
	if onlyEmpty && (it.LifetimeWarranty || it.WarrantyExpires != "" || it.WarrantyDetails != "") {
		return
	}
	switch {
	case wr.IsLifetime:
		it.LifetimeWarranty, it.WarrantyExpires = true, ""
	case wr.ExpiresOn != nil:
		it.LifetimeWarranty, it.WarrantyExpires = false, *wr.ExpiresOn
	}
	it.WarrantyDetails = warrantyDetails(wr, st)
}

// warrantyDetails describes the coverage for HomeBox's warranty notes, e.g.
// "2 years" plus the order number and any further serial numbers.
func warrantyDetails(wr *store.Warranty, st *store.Settings) string {
	lang := st.EffectiveLanguage()
	var lines []string
	switch {
	case wr.IsLifetime:
		lines = append(lines, i18n.T(lang, "detail.lifetime", nil))
	case wr.WarrantyMonths != nil:
		m := *wr.WarrantyMonths
		switch {
		case m%12 == 0:
			lines = append(lines, i18n.T(lang, "duration.years", i18n.Args{"count": m / 12}))
		case m > 12:
			lines = append(lines, i18n.T(lang, "duration.yearsMonths", i18n.Args{"years": m / 12, "months": m % 12}))
		default:
			lines = append(lines, i18n.T(lang, "duration.months", i18n.Args{"count": m}))
		}
	case wr.WarrantyEndDate != nil:
		lines = append(lines, i18n.T(lang, "detail.until", i18n.Args{"date": dates.Format(*wr.WarrantyEndDate, st.DateFormat, lang)}))
	}
	if wr.OrderNumber != "" {
		lines = append(lines, i18n.T(lang, "server.homeboxOrder", i18n.Args{"number": wr.OrderNumber}))
	}
	if len(wr.SerialNumbers) > 1 {
		lines = append(lines, i18n.T(lang, "server.homeboxSerials", i18n.Args{"list": strings.Join(wr.SerialNumbers, ", ")}))
	}
	return strings.Join(lines, "\n")
}

// displayName is the product as the web app shows it, e.g. "Bosch Dishwasher".
func displayName(wr *store.Warranty) string {
	if wr.Brand != "" && !strings.Contains(strings.ToLower(wr.ProductName), strings.ToLower(wr.Brand)) {
		return wr.Brand + " " + wr.ProductName
	}
	return wr.ProductName
}
