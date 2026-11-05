package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

// A linked warranty and its thing show the same photos and documents.
//
// Photos kept only in Wherehouse are the thing's photos: one added there
// shows here, one deleted there goes here, and one deleted here is deleted
// there. Documents are on both: each remembers its counterpart on the thing
// (wherehouse_thing + wherehouse_attachment), and when either goes, so does
// the other. A Paperless document is the same document on both when both
// use the same Paperless; a file kept only in Wherehouse is read through it.

// whKind is the kind of a document in Wherehouse for a kind here.
func whKind(kind string) string {
	switch kind {
	case "purchase":
		return "receipt"
	case "terms", "card", "extended":
		return "warranty"
	case "manual":
		return "manual"
	}
	return "other"
}

// kindFromWherehouse is the kind here of a document added in Wherehouse.
func kindFromWherehouse(kind string) string {
	switch kind {
	case "receipt":
		return "purchase"
	case "warranty":
		return "card"
	case "manual":
		return "manual"
	}
	return "other"
}

// photoExt names a photo added in Wherehouse, which has no name there.
var photoExt = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp",
	"image/gif": ".gif", "image/avif": ".avif", "image/heic": ".heic"}

// mirrorPhotos makes the warranty's photos kept in Wherehouse the thing's
// photos t. Called with the warranty's photos settled, under their lock.
func (s *Server) mirrorPhotos(ctx context.Context, uid int64, wr *store.Warranty, t *wherehouse.Thing) error {
	there := map[int64]bool{}
	for _, p := range t.Photos {
		there[p.ID] = true
	}
	known := map[int64]bool{}
	for _, a := range wr.Attachments {
		if a.Kind != "photo" || a.Storage != store.StorageWherehouse || a.WherehousePhoto == nil ||
			a.WherehouseThing == nil || *a.WherehouseThing != t.ID {
			continue
		}
		if !there[*a.WherehousePhoto] {
			slog.Info("wherehouse: a photo was deleted there", "warranty", wr.ID, "attachment", a.ID)
			if err := s.dropAttachment(ctx, &a); err != nil {
				return err
			}
			continue
		}
		known[*a.WherehousePhoto] = true
	}
	for _, p := range t.Photos {
		if known[p.ID] {
			continue
		}
		thing, photo := t.ID, p.ID
		a := &store.Attachment{WarrantyID: wr.ID, Kind: "photo", Storage: store.StorageWherehouse,
			Filename: fmt.Sprintf("photo-%d%s", p.ID, photoExt[p.MimeType]), MimeType: p.MimeType, Size: p.Size,
			Status: store.StatusReady, AddedBy: &uid, WherehouseThing: &thing, WherehousePhoto: &photo}
		if a.MimeType == "" {
			a.MimeType = "image/jpeg"
		}
		if err := s.store.CreateAttachment(ctx, a); err != nil {
			return err
		}
		slog.Info("wherehouse: a photo was added there", "warranty", wr.ID, "attachment", a.ID)
	}
	return nil
}

// onThing reports whether a document's counterpart is the one on thing.
func onThing(a *store.Attachment, thing int64) bool {
	return a.WherehouseAttachment != nil && a.WherehouseThing != nil && *a.WherehouseThing == thing
}

// mirrorDocuments puts the warranty's documents on the thing t and the
// thing's documents on the warranty, and drops those whose counterpart was
// deleted. Claims' documents stay here. Called under the photos' lock.
func (s *Server) mirrorDocuments(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client,
	wr *store.Warranty, t *wherehouse.Thing) error {
	thing := t.ID
	byID := map[int64]*wherehouse.Attachment{}
	for i := range t.Attachments {
		byID[t.Attachments[i].ID] = &t.Attachments[i]
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
	same := func(owner int64) (bool, string, error) {
		web, err := s.userPaperlessWebURL(ctx, owner)
		if err != nil {
			return false, "", err
		}
		pc, err := s.userPaperless(ctx, owner)
		if err != nil {
			return false, "", err
		}
		return theirs != nil && pc != nil && (sameURL(pc.BaseURL, theirs.URL) || sameURL(web, theirs.PublicURL)), web, nil
	}
	claimed := map[int64]bool{}
	docsHere := map[int64]bool{}
	lang := st.EffectiveLanguage()
	for i := range wr.Attachments {
		a := &wr.Attachments[i]
		if a.Kind == "photo" || a.ClaimID != nil {
			continue
		}
		if a.PaperlessDocID != nil {
			docsHere[*a.PaperlessDocID] = true
		}
		if onThing(a, thing) {
			if byID[*a.WherehouseAttachment] != nil {
				claimed[*a.WherehouseAttachment] = true
				continue
			}
			slog.Info("wherehouse: a document was deleted there", "warranty", wr.ID, "attachment", a.ID)
			if err := s.dropAttachment(ctx, a); err != nil {
				return err
			}
			continue
		}
		if a.Storage != store.StoragePaperless || a.PaperlessDocID == nil || a.Status != store.StatusReady {
			continue
		}
		doc := *a.PaperlessDocID
		owner := uid
		if a.AddedBy != nil {
			owner = *a.AddedBy
		}
		ok, web, err := same(owner)
		if err != nil {
			return err
		}
		if ok {
			// Linked before, or added there too.
			var match *wherehouse.Attachment
			for j := range t.Attachments {
				ta := &t.Attachments[j]
				if ta.PaperlessDocID != nil && *ta.PaperlessDocID == doc && !claimed[ta.ID] {
					match = ta
					break
				}
			}
			if match == nil {
				added, err := wh.LinkPaperless(ctx, thing, []int64{doc}, whKind(a.Kind))
				if err != nil {
					slog.Warn("wherehouse: add a document", "warranty", wr.ID, "attachment", a.ID, "err", err)
				} else if len(added) > 0 {
					match = &added[0]
				}
			}
			if match != nil {
				claimed[match.ID] = true
				if err := s.store.OnWherehouseThing(ctx, a.ID, thing, match.ID); err != nil {
					return err
				}
				continue
			}
		}
		// Another Paperless: a receipt is linked to, so it can be found.
		link := fmt.Sprintf("%s/documents/%d/details", web, doc)
		if a.Kind != "purchase" || web == "" || haveLink[link] {
			continue
		}
		if err := wh.AddLink(ctx, thing, link, i18n.T(lang, "server.homeboxLinkReceipt", i18n.Args{"name": a.Filename})); err != nil {
			return err
		}
		haveLink[link] = true
	}
	// What was added there comes here.
	mine, _, err := same(uid)
	if err != nil {
		return err
	}
	for i := range t.Attachments {
		ta := &t.Attachments[i]
		if claimed[ta.ID] {
			continue
		}
		thingID, attID := thing, ta.ID
		a := &store.Attachment{WarrantyID: wr.ID, Kind: kindFromWherehouse(ta.Kind), Filename: ta.Filename,
			MimeType: ta.MimeType, Size: ta.Size, Status: store.StatusReady, AddedBy: &uid,
			WherehouseThing: &thingID, WherehouseAttachment: &attID}
		if a.MimeType == "" {
			a.MimeType = "application/octet-stream"
		}
		paperlessDoc := ta.Storage == "paperless" && ta.PaperlessDocID != nil && mine
		if paperlessDoc {
			if docsHere[*ta.PaperlessDocID] {
				// Here already, but not yet ready or not to be sent there.
				continue
			}
			a.Storage, a.PaperlessDocID = store.StoragePaperless, ta.PaperlessDocID
		} else {
			a.Storage = store.StorageWherehouse
		}
		if err := s.store.CreateAttachment(ctx, a); err != nil {
			return err
		}
		slog.Info("wherehouse: a document was added there", "warranty", wr.ID, "attachment", a.ID)
		if paperlessDoc {
			if doc, err := s.store.AttachmentPaperlessDoc(ctx, a.ID); err == nil && doc != nil {
				s.syncBacklinksLater(*doc)
			}
		}
	}
	return nil
}

// dropAttachment deletes a photo or document here whose counterpart in
// Wherehouse is gone.
func (s *Server) dropAttachment(ctx context.Context, a *store.Attachment) error {
	doc, err := s.store.AttachmentPaperlessDoc(ctx, a.ID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteAttachment(ctx, a.ID); err != nil {
		return err
	}
	if doc != nil {
		s.syncBacklinksLater(*doc)
	}
	if a.Storage == store.StorageLocal {
		if used, err := s.store.FileInUse(ctx, a.LocalPath); err != nil || used {
			return err
		}
	}
	s.removeFiles(a)
	return nil
}

// deleteOnThing deletes in Wherehouse what an attachment about to be deleted
// here is there: the photo it's kept as, or its document on the thing the
// warranty is linked to. A copy of a photo stays.
func (s *Server) deleteOnThing(ctx context.Context, uid int64, a *store.Attachment) error {
	photo := a.Storage == store.StorageWherehouse && a.WherehousePhoto != nil
	if !photo && a.WherehouseAttachment == nil {
		return nil
	}
	if !photo {
		wr, err := s.store.GetWarranty(ctx, uid, a.WarrantyID)
		if err != nil {
			return err
		}
		if wr.Wherehouse == nil || !onThing(a, wr.Wherehouse.Thing) {
			return nil
		}
	}
	clients, _, err := s.wherehouseClients(ctx, uid, a.WarrantyID)
	if err != nil || len(clients) == 0 {
		return err // nothing connected to delete it with
	}
	return firstWherehouse(clients, func(wh *wherehouse.Client) error {
		var err error
		if photo {
			err = wh.DeletePhoto(ctx, *a.WherehousePhoto)
		} else {
			err = wh.DeleteAttachment(ctx, *a.WherehouseAttachment)
		}
		if wherehouseNotFound(err) {
			return nil
		}
		return err
	})
}
