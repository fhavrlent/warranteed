package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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
// deleted. A type or name changed there is changed here. Called under the
// photos' lock.
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
		if a.Kind == "photo" {
			continue
		}
		if a.PaperlessDocID != nil {
			docsHere[*a.PaperlessDocID] = true
		}
		if onThing(a, thing) {
			if ta := byID[*a.WherehouseAttachment]; ta != nil {
				claimed[ta.ID] = true
				if err := s.followDocument(ctx, a, ta); err != nil {
					return err
				}
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
					// Named there as here.
					if match.Filename != a.Filename {
						if err := wh.UpdateAttachment(ctx, match.ID, match.Kind, a.Filename); err != nil {
							slog.Warn("wherehouse: name a document", "warranty", wr.ID, "attachment", a.ID, "err", err)
						} else {
							match.Filename = a.Filename
						}
					}
				}
			}
			if match != nil {
				claimed[match.ID] = true
				if err := s.store.OnWherehouseThing(ctx, a.ID, thing, match.ID, seenAs(match)); err != nil {
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
			WherehouseThing: &thingID, WherehouseAttachment: &attID, WherehouseSeen: seenAs(ta)}
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

// seenAs is how a document's type and name there are remembered.
func seenAs(ta *wherehouse.Attachment) string { return ta.Kind + "\x00" + ta.Filename }

// followDocument changes a document's type and name as they were changed on
// its counterpart ta since last seen.
func (s *Server) followDocument(ctx context.Context, a *store.Attachment, ta *wherehouse.Attachment) error {
	seen := seenAs(ta)
	if a.WherehouseSeen == seen {
		return nil
	}
	if a.WherehouseSeen != "" {
		// Their kinds are fewer: one that still means the same stays.
		kind := a.Kind
		if whKind(kind) != ta.Kind {
			kind = kindFromWherehouse(ta.Kind)
		}
		if kind != a.Kind || ta.Filename != a.Filename {
			if err := s.store.UpdateAttachment(ctx, a.ID, kind, ta.Filename); err != nil {
				return err
			}
			slog.Info("wherehouse: a document was changed there", "warranty", a.WarrantyID, "attachment", a.ID)
		}
	}
	return s.store.SeenOnWherehouse(ctx, a.ID, seen)
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
	// Not found by any: gone there already.
	_, err = foundOnWherehouse(clients, func(wh *wherehouse.Client) error {
		if photo {
			return wh.DeletePhoto(ctx, *a.WherehousePhoto)
		}
		return wh.DeleteAttachment(ctx, *a.WherehouseAttachment)
	})
	return err
}

// updateOnThing changes a document's type and name on its thing too, before
// they're changed here, so a change there isn't taken for this one. It
// returns how the document is seen there now, or "" if it isn't there.
func (s *Server) updateOnThing(ctx context.Context, uid int64, a *store.Attachment, kind, filename string) (string, error) {
	if a.WherehouseAttachment == nil || a.Kind == "photo" {
		return "", nil
	}
	wr, err := s.store.GetWarranty(ctx, uid, a.WarrantyID)
	if err != nil {
		return "", err
	}
	if wr.Wherehouse == nil || !onThing(a, wr.Wherehouse.Thing) {
		return "", nil
	}
	clients, _, err := s.wherehouseClients(ctx, uid, a.WarrantyID)
	if err != nil || len(clients) == 0 {
		return "", err
	}
	ta := &wherehouse.Attachment{ID: *a.WherehouseAttachment, Kind: whKind(kind), Filename: filename}
	found, err := foundOnWherehouse(clients, func(wh *wherehouse.Client) error {
		return wh.UpdateAttachment(ctx, ta.ID, ta.Kind, ta.Filename)
	})
	if err != nil || !found {
		return "", err // not found: gone there, and the next sync drops it here
	}
	return seenAs(ta), nil
}

// ---- details both have ----

// detailKeys are the details a warranty and its thing share, by their names
// in Wherehouse.
var detailKeys = []string{"name", "manufacturer", "model", "serial", "notes", "quantity"}

// detailsSeen is what both sides had when last in step.
type detailsSeen struct {
	Here  map[string]string `json:"here"`
	There map[string]string `json:"there"`
}

func sharedDetails(wr *store.Warranty) map[string]string {
	return map[string]string{"name": displayName(wr), "manufacturer": wr.Brand, "model": wr.Model,
		"serial": strings.Join(wr.SerialNumbers, ", "), "notes": wr.Notes, "quantity": strconv.Itoa(max(wr.Quantity, 1))}
}

func thingDetails(t *wherehouse.Thing) map[string]string {
	return map[string]string{"name": t.Name, "manufacturer": t.Manufacturer, "model": t.Model,
		"serial": t.Serial, "notes": t.Notes, "quantity": strconv.Itoa(t.Quantity)}
}

// mirrorDetails brings the details the warranty and its thing t share in
// step: one changed on one side since last seen is changed on the other,
// and when both changed, the warranty's wins. Details that differed when
// first seen stay as they are until one side changes. Called under the
// photos' lock.
func (s *Server) mirrorDetails(ctx context.Context, wh *wherehouse.Client, wr *store.Warranty, t *wherehouse.Thing) error {
	here, there := sharedDetails(wr), thingDetails(t)
	raw, err := s.store.WherehouseFields(ctx, wr.ID)
	if err != nil {
		return err
	}
	var seen detailsSeen
	if raw == "" || json.Unmarshal([]byte(raw), &seen) != nil || seen.Here == nil || seen.There == nil {
		return s.saveDetailsSeen(ctx, wr.ID, here, there)
	}
	toThing := map[string]any{}
	toHere := map[string]string{}
	for _, k := range detailKeys {
		switch {
		case here[k] == there[k]:
		case here[k] != seen.Here[k]:
			toThing[k] = here[k]
		case there[k] != seen.There[k]:
			toHere[k] = there[k]
		}
	}
	if len(toThing) > 0 {
		if q, ok := toThing["quantity"].(string); ok {
			toThing["quantity"], _ = strconv.Atoi(q)
		}
		if err := wh.SetDetails(ctx, t.ID, toThing); err != nil {
			return err
		}
		for k := range toThing {
			there[k] = here[k]
		}
		slog.Info("wherehouse: details sent to the thing", "warranty", wr.ID, "details", len(toThing))
	}
	if len(toHere) > 0 {
		d := store.ThingDetails{ProductName: wr.ProductName, Brand: wr.Brand, Model: wr.Model, Notes: wr.Notes,
			SerialNumbers: wr.SerialNumbers, Quantity: max(wr.Quantity, 1)}
		for k, v := range toHere {
			switch k {
			case "name":
				if strings.TrimSpace(v) != "" {
					d.ProductName = v
				}
			case "manufacturer":
				d.Brand = v
			case "model":
				d.Model = v
			case "serial":
				d.SerialNumbers = nil
				for _, sn := range strings.Split(v, ",") {
					if sn = strings.TrimSpace(sn); sn != "" {
						d.SerialNumbers = append(d.SerialNumbers, sn)
					}
				}
			case "notes":
				d.Notes = v
			case "quantity":
				if q, err := strconv.Atoi(v); err == nil && q >= 1 && q <= store.MaxQuantity {
					d.Quantity = q
				}
			}
		}
		if err := s.store.SetThingDetails(ctx, wr.ID, d); err != nil {
			return err
		}
		slog.Info("wherehouse: details changed on the thing", "warranty", wr.ID, "details", len(toHere))
		wr.ProductName, wr.Brand, wr.Model, wr.Notes, wr.SerialNumbers, wr.Quantity =
			d.ProductName, d.Brand, d.Model, d.Notes, d.SerialNumbers, d.Quantity
		here = sharedDetails(wr)
	}
	return s.saveDetailsSeen(ctx, wr.ID, here, there)
}

func (s *Server) saveDetailsSeen(ctx context.Context, id int64, here, there map[string]string) error {
	b, err := json.Marshal(detailsSeen{Here: here, There: there})
	if err != nil {
		return err
	}
	return s.store.SetWherehouseFields(ctx, id, string(b))
}

// ---- claims' entries ----

// entrySeen is how a claim's maintenance entry is remembered.
func entrySeen(m wherehouse.Maintenance) string {
	m.ID = 0
	b, _ := json.Marshal(m)
	return string(b)
}

// mirrorClaims follows what was done in Wherehouse to the claims' entries in
// the thing t's maintenance log: an entry changed there since last seen
// changes its claim, and one deleted there deletes its claim. Claims changed
// here are sent there as they change. Called under the photos' lock.
func (s *Server) mirrorClaims(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client, wr *store.Warranty, t *wherehouse.Thing) error {
	ids, err := s.store.ClaimIDs(ctx, wr.ID)
	if err != nil {
		return err
	}
	for _, cid := range ids {
		if err := s.mirrorClaim(ctx, uid, st, wh, t, cid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) mirrorClaim(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client,
	t *wherehouse.Thing, cid int64) error {
	// No copy there can be under way meanwhile.
	defer s.lockPhotos(claimKey(cid) + ":wh")()
	entry, thing, err := s.store.ClaimWherehouse(ctx, cid)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil || entry == nil || thing == nil || *thing != t.ID {
		return err
	}
	seen, err := s.store.ClaimWherehouseSeen(ctx, cid)
	if err != nil {
		return err
	}
	m := entryOn(t, *entry)
	if m == nil || entrySeen(*m) != seen {
		// t was read before this lock was taken, and a copy sent since
		// may have added or changed the entry: what's there now decides.
		now, err := wh.Get(ctx, t.ID)
		if wherehouseNotFound(err) {
			return nil // the thing is gone, which is seen to elsewhere
		}
		if err != nil {
			return err
		}
		m = entryOn(now, *entry)
	}
	if m == nil && seen == "" {
		// Never seen there: it went missing some other way, e.g. with a
		// Wherehouse set up afresh, so it's added again.
		defer s.syncClaimLater(uid, cid)
		return nil
	}
	if m == nil {
		slog.Info("wherehouse: a claim's entry was deleted there", "claim", cid)
		unlock := s.lockPhotos(claimKey(cid))
		c, err := s.store.DeleteClaim(ctx, uid, cid)
		unlock()
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if c.HomeBoxEntry != "" {
			s.inBackground("remove claim from homebox", func(ctx context.Context) error {
				return s.removeClaimEntry(ctx, uid, c.WarrantyID, c.HomeBoxEntry)
			})
		}
		return nil
	}
	now := entrySeen(*m)
	if seen == now {
		return nil
	}
	if seen != "" {
		c, _, err := s.store.ClaimSync(ctx, cid)
		if err != nil {
			return err
		}
		in := s.claimFromEntry(c, *m)
		if changed := in.Title != c.Title || in.Status != c.Status || in.ReportedOn != c.ReportedOn ||
			!sameDate(in.ResolvedOn, c.ResolvedOn) || in.CaseNumber != c.CaseNumber || !sameCost(in.Cost, c.Cost) ||
			in.Notes != c.Notes; changed {
			if _, err := s.store.UpdateClaim(ctx, uid, cid, &in); err != nil {
				// Something it can't be, e.g. no title: it's left as it is.
				slog.Warn("wherehouse: a claim's entry changed there", "claim", cid, "err", err)
			} else {
				slog.Info("wherehouse: a claim's entry was changed there", "claim", cid)
				// Written back as entries read, and to HomeBox.
				defer s.syncClaimLater(uid, cid)
			}
		}
	}
	return s.store.SetClaimWherehouseSeen(ctx, cid, now)
}

// entryOn is the entry with the ID on t, nil if it isn't there.
func entryOn(t *wherehouse.Thing, id int64) *wherehouse.Maintenance {
	for i := range t.Maintenance {
		if t.Maintenance[i].ID == id {
			return &t.Maintenance[i]
		}
	}
	return nil
}

func sameDate(a, b *string) bool  { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
func sameCost(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// claimFromEntry is the claim c as its entry m reads: the entry's title
// without "Warranty claim: ", its date as the claim's (resolved, else
// reported), its cost, and its notes: the status, case number and notes,
// as claimEntry writes them, without the link back.
func (s *Server) claimFromEntry(c *store.Claim, m wherehouse.Maintenance) store.ClaimInput {
	in := store.ClaimInput{Title: c.Title, Status: c.Status, ReportedOn: c.ReportedOn, ResolvedOn: c.ResolvedOn,
		CaseNumber: c.CaseNumber, Cost: m.Cost, Notes: c.Notes}
	title := strings.TrimSpace(m.Title)
	for _, lang := range i18n.Languages() {
		if rest, ok := strings.CutPrefix(title, i18n.T(lang, "claims.homeboxName", i18n.Args{"title": ""})); ok {
			title = strings.TrimSpace(rest)
			break
		}
	}
	if title != "" {
		in.Title = title
	}
	if m.DoneOn != nil && *m.DoneOn != "" {
		if c.ResolvedOn != nil {
			in.ResolvedOn = m.DoneOn
		} else {
			in.ReportedOn = *m.DoneOn
		}
	}
	lines := strings.Split(strings.ReplaceAll(m.Notes, "\r\n", "\n"), "\n")
	trim := func() {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}
	trim()
	link := fmt.Sprintf("%s/w/%d", strings.TrimSuffix(s.cfg.AppURL.String(), "/"), c.WarrantyID)
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == link {
		lines = lines[:len(lines)-1]
		trim()
	}
	if len(lines) > 0 {
		if status, ok := claimStatusNamed(strings.TrimSpace(lines[0])); ok {
			in.Status, in.CaseNumber = status, ""
			lines = lines[1:]
			if len(lines) > 0 {
				if number, ok := caseNumberIn(strings.TrimSpace(lines[0])); ok {
					in.CaseNumber = number
					lines = lines[1:]
				}
			}
		}
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	in.Notes = strings.Join(lines, "\n")
	if in.Status == "open" {
		in.ResolvedOn = nil
	}
	return in
}

// claimStatusNamed finds the status a line names, in any language.
func claimStatusNamed(line string) (string, bool) {
	for _, lang := range i18n.Languages() {
		for _, status := range store.ClaimStatuses {
			if strings.EqualFold(line, i18n.T(lang, "claims.status."+status, nil)) {
				return status, true
			}
		}
	}
	return "", false
}

// caseNumberIn reads "Case number: …" in any language.
func caseNumberIn(line string) (string, bool) {
	for _, lang := range i18n.Languages() {
		if rest, ok := strings.CutPrefix(line, i18n.T(lang, "claims.homeboxCase", i18n.Args{"number": ""})); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}
