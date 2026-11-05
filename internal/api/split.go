package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/store"
)

// splitWarranty moves some of a warranty's pieces to a new warranty, e.g.
// when one is sold or lent. With "homebox", a linked warranty's HomeBox
// item is split too: the new warranty gets its own item next to it.
func (s *Server) splitWarranty(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		store.SplitInput
		HomeBox    bool `json:"homebox"`
		Wherehouse bool `json:"wherehouse"`
	}
	if !decode(w, r, &in) {
		return
	}
	// One split of a warranty at a time, so each reads the pieces the last
	// one left, here and in HomeBox.
	defer s.lockPhotos(fmt.Sprintf("split:%d", id))()
	uid := userID(r)
	wr, err := s.store.GetWarranty(r.Context(), uid, id)
	if err != nil {
		fail(w, err)
		return
	}

	// Each warranty gets its own copy of the files stored here.
	copies := map[int64]string{}
	removeCopies := func() {
		for _, path := range copies {
			s.files.Remove(path)
		}
	}
	for _, a := range wr.Attachments {
		if a.Storage != store.StorageLocal {
			continue
		}
		f, err := s.files.Open(a.LocalPath)
		if err != nil {
			removeCopies()
			writeMsg(w, http.StatusInternalServerError, "server.splitFiles", nil)
			return
		}
		path, _, err := s.files.Save(f, filepath.Ext(a.LocalPath))
		f.Close()
		if err != nil {
			removeCopies()
			fail(w, err)
			return
		}
		copies[a.ID] = path
	}
	newID, err := s.store.SplitWarranty(r.Context(), uid, id, &in.SplitInput, copies)
	if err != nil {
		removeCopies()
		fail(w, err)
		return
	}
	s.syncWarrantyBacklinksLater(r.Context(), newID)

	var out struct {
		Warranty *store.Warranty `json:"warranty"`
		// HomeBoxError says why the HomeBox item couldn't be split; the
		// warranty is split either way.
		HomeBoxError string `json:"homeboxError,omitempty"`
		// WherehouseError, the same for the Wherehouse thing.
		WherehouseError string `json:"wherehouseError,omitempty"`
	}
	if in.HomeBox && wr.HomeBox != nil {
		if err := s.splitHomeBoxItem(r.Context(), uid, wr, newID); err != nil {
			out.HomeBoxError = err.Error()
		}
	}
	if in.Wherehouse && wr.Wherehouse != nil {
		if err := s.splitWherehouseThing(r.Context(), uid, wr, newID); err != nil {
			out.WherehouseError = err.Error()
		}
	}
	if out.Warranty, err = s.store.GetWarranty(r.Context(), uid, newID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// splitHomeBoxItem adds an item for the split-off warranty next to the
// original's, as adding it to HomeBox would, and makes the original item
// match what's left of its warranty. orig is the warranty before the split.
func (s *Server) splitHomeBoxItem(reqCtx context.Context, uid int64, orig *store.Warranty, newID int64) error {
	st, err := s.store.Settings(reqCtx, uid)
	if err != nil {
		return err
	}
	hb := s.homeboxFor(st)
	if hb == nil {
		return errors.New("no HomeBox key in your settings")
	}
	ctx, cancel := context.WithTimeout(reqCtx, 2*time.Minute)
	defer cancel()
	item, err := hb.Get(ctx, orig.HomeBox.ID)
	if err != nil {
		return err
	}
	parent := ""
	if item.Parent != nil {
		parent = item.Parent.ID
	}
	nw, err := s.store.GetWarranty(reqCtx, uid, newID)
	if err != nil {
		return err
	}
	created, err := hb.Create(ctx, displayName(nw), parent)
	if err != nil {
		return err
	}
	if err := s.joinHomeBoxItem(reqCtx, ctx, uid, st, hb, nw, created, false); err != nil {
		return err
	}
	if err := s.copyHomeBoxPhotos(ctx, hb, uid, nw, created.ID); err != nil {
		return err
	}
	rest, err := s.store.GetWarranty(reqCtx, uid, orig.ID)
	if err != nil {
		return err
	}
	defer s.lockPhotos("item:" + item.ID)()
	_, err = hb.Update(ctx, item.ID, func(it *homebox.Item) { shrinkHomeBoxItem(it, orig, rest, st) })
	return err
}

// shrinkHomeBoxItem updates the item of a warranty that had pieces split off,
// from before to rest. Only what still shows the warranty as it was before
// changes, so values set by hand in HomeBox stay.
func shrinkHomeBoxItem(it *homebox.Item, before, rest *store.Warranty, st *store.Settings) {
	if it.Quantity == before.Quantity {
		it.Quantity = rest.Quantity
	}
	if it.SerialNumber != "" && slices.Contains(before.SerialNumbers, it.SerialNumber) &&
		!slices.Contains(rest.SerialNumbers, it.SerialNumber) {
		it.SerialNumber = ""
		if len(rest.SerialNumbers) > 0 {
			it.SerialNumber = rest.SerialNumbers[0]
		}
	}
	if it.WarrantyDetails == warrantyDetails(before, st) {
		it.WarrantyDetails = warrantyDetails(rest, st)
	}
}

// copyHomeBoxPhotos puts the split-off warranty's photos stored in HomeBox,
// which are still on the original's item, on its own item too.
func (s *Server) copyHomeBoxPhotos(ctx context.Context, hb *homebox.Client, uid int64, wr *store.Warranty, itemID string) error {
	defer s.lockPhotos(wr.ID)()
	defer s.lockPhotos("item:" + itemID)()
	for _, a := range wr.Attachments {
		if a.Kind != "photo" || a.Storage != store.StorageHomeBox || a.HomeBoxItem == itemID {
			continue
		}
		// Read through the key of whoever stored it, as it's shown.
		src, err := s.attachmentHomeBox(ctx, &a, uid)
		if err != nil {
			return err
		}
		if src == nil {
			src = hb
		}
		res, err := src.OpenAttachment(ctx, a.HomeBoxItem, a.HomeBoxAttachment)
		if err != nil {
			return err
		}
		item, err := hb.Get(ctx, itemID)
		if err != nil {
			res.Body.Close()
			return err
		}
		id, err := hb.UploadPhoto(ctx, item, a.Filename, a.MimeType, res.Body, !item.HasPrimaryPhoto())
		res.Body.Close()
		if err != nil {
			return err
		}
		if err := s.store.MovedInHomeBox(ctx, a.ID, itemID, id); err != nil {
			return err
		}
	}
	return nil
}

// splitWherehouseThing adds a thing for the split-off warranty next to the
// original's, as adding it to Wherehouse would, and lowers the original
// thing's quantity by the pieces split off, if it still had the warranty's.
func (s *Server) splitWherehouseThing(reqCtx context.Context, uid int64, orig *store.Warranty, newID int64) error {
	st, err := s.store.Settings(reqCtx, uid)
	if err != nil {
		return err
	}
	wh := s.wherehouseFor(st)
	if wh == nil {
		return errors.New("not connected to Wherehouse")
	}
	ctx, cancel := context.WithTimeout(reqCtx, 2*time.Minute)
	defer cancel()
	t, err := wh.Get(ctx, orig.Wherehouse.Thing)
	if err != nil {
		return err
	}
	nw, err := s.store.GetWarranty(reqCtx, uid, newID)
	if err != nil {
		return err
	}
	created, err := wh.Create(ctx, newThing(nw, t.ParentID))
	if err != nil {
		return err
	}
	if err := s.joinWherehouse(ctx, uid, st, wh, nw, created.ID); err != nil {
		s.dropUnlinkedThing(uid, newID, wh, created.ID)
		return err
	}
	rest, err := s.store.GetWarranty(reqCtx, uid, orig.ID)
	if err != nil {
		return err
	}
	// A quantity changed by hand there stays.
	if t.Quantity == max(orig.Quantity, 1) {
		return wh.SetQuantity(ctx, t.ID, max(rest.Quantity, 1))
	}
	return nil
}
