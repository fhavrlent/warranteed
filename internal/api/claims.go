package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/store"
)

// Claims are kept here and, for a warranty linked to a HomeBox item, copied
// to the item's maintenance log in the background.

func (s *Server) createClaim(w http.ResponseWriter, r *http.Request) {
	wid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in store.ClaimInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.store.CreateClaim(r.Context(), userID(r), wid, &in)
	if err != nil {
		fail(w, err)
		return
	}
	s.syncClaimLater(userID(r), c.ID)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateClaim(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in store.ClaimInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.store.UpdateClaim(r.Context(), userID(r), id, &in)
	if err != nil {
		fail(w, err)
		return
	}
	s.syncClaimLater(userID(r), c.ID)
	writeJSON(w, http.StatusOK, c)
}

// deleteClaim deletes a claim and its HomeBox entry; its documents stay on
// the warranty.
func (s *Server) deleteClaim(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	uid := userID(r)
	// Holding the Wherehouse lock, a copy finishing meanwhile can't save an
	// entry after it's read here.
	unlockWh := s.lockPhotos(claimKey(id) + ":wh")
	whEntry, _, _ := s.store.ClaimWherehouse(r.Context(), id)
	unlock := s.lockPhotos(claimKey(id))
	c, err := s.store.DeleteClaim(r.Context(), uid, id)
	unlock()
	unlockWh()
	if err != nil {
		fail(w, err)
		return
	}
	if c.HomeBoxEntry != "" {
		s.inBackground("remove claim from homebox", func(ctx context.Context) error {
			return s.removeClaimEntry(ctx, uid, c.WarrantyID, c.HomeBoxEntry)
		})
	}
	if whEntry != nil {
		s.inBackground("remove claim from wherehouse", func(ctx context.Context) error {
			clients, _, err := s.wherehouseClients(ctx, uid, c.WarrantyID)
			if err != nil {
				return err
			}
			for _, wh := range clients {
				if err = wh.DeleteMaintenance(ctx, *whEntry); err == nil {
					return nil
				}
			}
			return err
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

func claimKey(id int64) string { return "claim:" + strconv.FormatInt(id, 10) }

// inBackground runs fn after the request, with a time limit, logging failures.
func (s *Server) inBackground(what string, fn func(context.Context) error) {
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := fn(ctx); err != nil {
			slog.Warn(what, "err", err)
		}
	}()
}

func (s *Server) syncClaimLater(uid, claimID int64) {
	s.inBackground("copy claim to homebox", func(ctx context.Context) error {
		return s.syncClaim(ctx, uid, claimID)
	})
	s.inBackground("copy claim to wherehouse", func(ctx context.Context) error {
		return s.syncWherehouseClaim(ctx, uid, claimID)
	})
}

// claimClients are the HomeBox connections to update a warranty's item
// with: the user's first, then those of others who can see it.
func (s *Server) claimClients(ctx context.Context, uid, warrantyID int64) ([]*homebox.Client, error) {
	users := []int64{uid}
	others, err := s.store.HomeBoxUsers(ctx, warrantyID)
	if err != nil {
		return nil, err
	}
	for _, id := range others {
		if !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	return s.homeboxClientsOf(ctx, users)
}

// syncClaim writes the claim to its warranty's HomeBox item: a new entry in
// the maintenance log, or the one it has, written over.
func (s *Server) syncClaim(ctx context.Context, uid, claimID int64) error {
	defer s.lockPhotos(claimKey(claimID))()
	c, itemID, err := s.store.ClaimSync(ctx, claimID)
	if errors.Is(err, store.ErrNotFound) || itemID == "" {
		return nil // deleted meanwhile, or not in HomeBox
	}
	if err != nil {
		return err
	}
	clients, err := s.claimClients(ctx, uid, c.WarrantyID)
	if err != nil {
		return err
	}
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return err
	}
	entry := s.claimEntry(c, st)
	err = errors.New("no homebox key")
	for _, hb := range clients {
		if c.HomeBoxEntry != "" {
			if err = hb.UpdateMaintenance(ctx, c.HomeBoxEntry, entry); err == nil {
				return nil
			}
			var he *homebox.Error
			if !errors.As(err, &he) || !he.NotFound() {
				continue // try someone else's key
			}
			// Deleted in HomeBox: add it again.
		}
		var id string
		if id, err = hb.AddMaintenance(ctx, itemID, entry); err != nil {
			continue
		}
		saved, err := s.store.SetClaimEntry(ctx, claimID, itemID, id)
		if err != nil || saved {
			return err
		}
		// Unlinked or relinked meanwhile: the entry is in an item the
		// warranty no longer belongs to, so take it out again.
		return hb.DeleteMaintenance(ctx, id)
	}
	return err
}

// syncClaims copies a warranty's claims to its HomeBox item, e.g. once it's
// linked to one.
func (s *Server) syncClaims(ctx context.Context, uid, warrantyID int64) error {
	ids, err := s.store.ClaimIDs(ctx, warrantyID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.syncClaim(ctx, uid, id); err != nil {
			return err
		}
	}
	return nil
}

// removeClaimEntry deletes a deleted claim's entry from HomeBox.
func (s *Server) removeClaimEntry(ctx context.Context, uid, warrantyID int64, entry string) error {
	clients, err := s.claimClients(ctx, uid, warrantyID)
	if err != nil {
		return err
	}
	err = errors.New("no homebox key")
	for _, hb := range clients {
		if err = hb.DeleteMaintenance(ctx, entry); err == nil {
			return nil
		}
		var he *homebox.Error
		if errors.As(err, &he) && he.NotFound() {
			return nil
		}
	}
	return err
}

// claimEntry is how a claim reads in HomeBox's maintenance log, in the
// language of the person who changed it.
func (s *Server) claimEntry(c *store.Claim, st *store.Settings) homebox.MaintenanceEntry {
	lang := st.EffectiveLanguage()
	lines := []string{i18n.T(lang, "claims.status."+c.Status, nil)}
	if c.CaseNumber != "" {
		lines = append(lines, i18n.T(lang, "claims.homeboxCase", i18n.Args{"number": c.CaseNumber}))
	}
	if c.Notes != "" {
		lines = append(lines, "", c.Notes)
	}
	app := strings.TrimSuffix(s.cfg.AppURL.String(), "/")
	lines = append(lines, "", fmt.Sprintf("%s/w/%d", app, c.WarrantyID))
	e := homebox.MaintenanceEntry{
		Name:          i18n.T(lang, "claims.homeboxName", i18n.Args{"title": c.Title}),
		Description:   strings.Join(lines, "\n"),
		Cost:          "0",
		ScheduledDate: c.ReportedOn,
	}
	if c.Cost != nil {
		e.Cost = strconv.FormatFloat(*c.Cost, 'f', -1, 64)
	}
	if c.ResolvedOn != nil {
		e.CompletedDate = *c.ResolvedOn
	}
	return e
}
