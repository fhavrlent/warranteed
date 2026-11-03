package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
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
		HomeBox bool `json:"homebox"`
	}
	if !decode(w, r, &in) {
		return
	}
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
	}
	if in.HomeBox && wr.HomeBox != nil {
		if err := s.splitHomeBoxItem(r.Context(), uid, wr, newID, in.Pieces); err != nil {
			out.HomeBoxError = err.Error()
		}
	}
	if out.Warranty, err = s.store.GetWarranty(r.Context(), uid, newID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// splitHomeBoxItem adds an item for the split-off warranty next to the
// original's, as adding it to HomeBox would, and lowers the original item's
// quantity to match its warranty.
func (s *Server) splitHomeBoxItem(reqCtx context.Context, uid int64, orig *store.Warranty, newID int64, pieces int) error {
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
	// Only when the item counted the same pieces, so a quantity set by hand
	// in HomeBox stays.
	defer s.lockPhotos("item:" + item.ID)()
	_, err = hb.Update(ctx, item.ID, func(it *homebox.Item) {
		if it.Quantity == orig.Quantity {
			it.Quantity = orig.Quantity - pieces
		}
	})
	return err
}
