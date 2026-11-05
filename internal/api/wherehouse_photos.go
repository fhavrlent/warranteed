package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/internal/wherehouse"
)

// Product photos kept only in Wherehouse.
//
// With "Keep product photos only in Wherehouse" on, a linked warranty's
// photos live on its thing and nowhere here: a copy sent there before is
// matched with the one there and its file here goes. When the warranty is
// unlinked, the person disconnects or turns the setting off, the photos come
// back here, so the warranty never loses them.

// wherehousePhotosKey makes the photos of a warranty take turns between
// Wherehouse and here.
func wherehousePhotosKey(wid int64) string { return "wh-files:" + strconv.FormatInt(wid, 10) }

// settleWherehousePhotosLater puts the warranty's photos where they belong,
// in the background.
func (s *Server) settleWherehousePhotosLater(uid, wid int64) {
	s.inBackground("wherehouse: settle photos", func(ctx context.Context) error {
		return s.settleWherehousePhotos(ctx, uid, wid, false)
	})
}

// settleWherehousePhotos puts the warranty's photos where they belong: only
// on its thing if uid keeps them only in Wherehouse, else here. With
// leaving, uid is disconnecting, so they all come back here.
func (s *Server) settleWherehousePhotos(ctx context.Context, uid, wid int64, leaving bool) error {
	defer s.lockPhotos(wherehousePhotosKey(wid))()
	wr, err := s.store.GetWarranty(ctx, uid, wid)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	st, err := s.store.Settings(ctx, uid)
	if err != nil {
		return err
	}
	var wh *wherehouse.Client
	if !leaving {
		wh = s.wherehouseFor(st)
	}
	return s.settlePhotos(ctx, uid, st, wh, wr)
}

// settlePhotos does settleWherehousePhotos' work while holding its lock. wh
// is uid's connection, nil if there's none to keep photos with.
func (s *Server) settlePhotos(ctx context.Context, uid int64, st *store.Settings, wh *wherehouse.Client, wr *store.Warranty) error {
	var thing int64
	if wr.Wherehouse != nil {
		thing = wr.Wherehouse.Thing
	}
	there := wh != nil && thing != 0 && st.WherehousePhotosOnly
	var (
		readers []*wherehouse.Client
		have    []photoThere // the thing's photos, read once
		haveOK  bool
	)
	getReaders := func() ([]*wherehouse.Client, error) {
		if readers == nil {
			clients, _, err := s.wherehouseClients(ctx, uid, wr.ID)
			if err != nil {
				return nil, err
			}
			readers = clients
			if wh != nil {
				readers = append([]*wherehouse.Client{wh}, readers...)
			}
		}
		return readers, nil
	}
	getHave := func() ([]photoThere, error) {
		if !haveOK {
			var err error
			if have, err = freePhotos(ctx, wh, wr, thing); err != nil {
				return nil, err
			}
			haveOK = true
		}
		return have, nil
	}
	for _, a := range wr.Attachments {
		if a.Kind != "photo" {
			// A document kept on a thing it's no longer linked to comes here.
			if a.Storage == store.StorageWherehouse && a.WherehouseThing != nil && a.WherehouseAttachment != nil &&
				(wh == nil || *a.WherehouseThing != thing) {
				readers, err := getReaders()
				if err != nil {
					return err
				}
				if err := s.documentBack(ctx, readers, wr, &a, thing); err != nil {
					return err
				}
			} else if a.Storage != store.StorageWherehouse && a.WherehouseAttachment != nil &&
				(a.WherehouseThing == nil || *a.WherehouseThing != thing) {
				// The thing it's on is no longer this warranty's.
				if err := s.store.NotOnWherehouseThing(ctx, a.ID); err != nil {
					return err
				}
			}
			continue
		}
		switch {
		case a.Storage == store.StorageWherehouse && a.WherehouseThing != nil && a.WherehousePhoto != nil &&
			(!there || *a.WherehouseThing != thing):
			readers, err := getReaders()
			if err != nil {
				return err
			}
			// A thing deleted, or a Wherehouse set up afresh, took the photo
			// with it; an ID there may even be another photo's now.
			if on, err := photoOnThing(ctx, readers, *a.WherehouseThing, *a.WherehousePhoto); err != nil {
				return err
			} else if !on {
				slog.Info("wherehouse: a photo kept there is gone", "warranty", wr.ID, "attachment", a.ID)
				if err := s.store.DeleteAttachment(ctx, a.ID); err != nil {
					return err
				}
				continue
			}
			data, err := readWherehousePhoto(ctx, readers, *a.WherehousePhoto)
			if err != nil {
				return err
			}
			if there {
				// Linked to another thing since: it moves there.
				id, err := wh.UploadPhoto(ctx, thing, a.Filename, a.MimeType, bytes.NewReader(data))
				if err != nil {
					return err
				}
				if err := s.store.StoredInWherehouse(ctx, a.ID, thing, id); err != nil {
					return err
				}
				continue
			}
			name, size, err := s.files.Save(bytes.NewReader(data), strings.ToLower(filepath.Ext(a.Filename)))
			if err != nil {
				return err
			}
			// The thing that held it keeps it, so it isn't sent there again.
			var sentTo *int64
			if thing != 0 && a.WherehouseThing != nil && *a.WherehouseThing == thing {
				sentTo = &thing
			}
			if err := s.store.BackFromWherehouse(ctx, a.ID, name, size, sentTo); err != nil {
				s.files.Remove(name)
				return err
			}
		case a.Storage == store.StorageLocal && there:
			var id int64
			if a.WherehouseThing != nil && *a.WherehouseThing == thing {
				// Sent there before: the one there is it.
				if _, err := getHave(); err != nil {
					return err
				}
				var err error
				if id, have, err = s.matchPhoto(ctx, wh, &a, have); err != nil {
					return err
				}
			}
			if id == 0 {
				f, err := s.files.Open(a.LocalPath)
				if err != nil {
					return err
				}
				id, err = wh.UploadPhoto(ctx, thing, a.Filename, a.MimeType, f)
				f.Close()
				if err != nil {
					return err
				}
			}
			if err := s.store.StoredInWherehouse(ctx, a.ID, thing, id); err != nil {
				return err
			}
			// A split-off warranty's copy has a file of its own; this checks anyway.
			if used, err := s.store.FileInUse(ctx, a.LocalPath); err != nil {
				slog.Warn("wherehouse: check a photo's file", "file", a.LocalPath, "err", err)
			} else if !used {
				s.files.Remove(a.LocalPath)
			}
		case a.Storage == store.StorageHomeBox && there:
			// Copied from HomeBox into Wherehouse, or not yet: it's kept
			// there from now on, and HomeBox keeps its copy.
			data, err := s.readHomeBoxPhoto(ctx, uid, &a)
			if err != nil {
				slog.Warn("wherehouse: read a photo kept in HomeBox", "warranty", wr.ID, "attachment", a.ID, "err", err)
				continue
			}
			if _, err := getHave(); err != nil {
				return err
			}
			var id int64
			if id, have, err = matchContents(ctx, wh, int64(len(data)), func() (*[32]byte, error) {
				sum := sha256.Sum256(data)
				return &sum, nil
			}, have); err != nil {
				return err
			}
			if id == 0 {
				if id, err = wh.UploadPhoto(ctx, thing, a.Filename, a.MimeType, bytes.NewReader(data)); err != nil {
					return err
				}
			}
			if err := s.store.StoredInWherehouse(ctx, a.ID, thing, id); err != nil {
				return err
			}
			if name := homeboxThumbName(a.HomeBoxAttachment); name != "" {
				s.files.Remove(name)
			}
		}
	}
	return nil
}

// readHomeBoxPhoto reads a photo kept in HomeBox.
func (s *Server) readHomeBoxPhoto(ctx context.Context, uid int64, a *store.Attachment) ([]byte, error) {
	hb, err := s.attachmentHomeBox(ctx, a, uid)
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, errors.New("no HomeBox key")
	}
	res, err := hb.OpenAttachment(ctx, a.HomeBoxItem, a.HomeBoxAttachment)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxWherehousePhoto+1))
	if err == nil && len(data) > maxWherehousePhoto {
		err = errors.New("photo in HomeBox is too large")
	}
	return data, err
}

// documentBack brings a document kept on a thing back here, as the warranty
// is no longer linked to it, or drops it if it was deleted there. Still
// linked to it, it stays the same document as the one there.
func (s *Server) documentBack(ctx context.Context, readers []*wherehouse.Client, wr *store.Warranty, a *store.Attachment, thing int64) error {
	from, att := *a.WherehouseThing, *a.WherehouseAttachment
	on, err := onThingThere(ctx, readers, from, func(t *wherehouse.Thing) bool {
		for _, ta := range t.Attachments {
			if ta.ID == att {
				return true
			}
		}
		return false
	})
	if err != nil {
		return err
	}
	if !on {
		slog.Info("wherehouse: a document kept there is gone", "warranty", wr.ID, "attachment", a.ID)
		return s.dropAttachment(ctx, a)
	}
	limit := max(s.cfg.MaxUploadMB<<20, maxWherehousePhoto)
	var data []byte
	err = errors.New("not connected to Wherehouse")
	for _, c := range readers {
		var res *http.Response
		if res, err = c.OpenAttachment(ctx, att, true); err != nil {
			continue
		}
		data, err = io.ReadAll(io.LimitReader(res.Body, limit+1))
		res.Body.Close()
		if err == nil && int64(len(data)) > limit {
			err = errors.New("document in Wherehouse is too large")
		}
		if err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	name, size, err := s.files.Save(bytes.NewReader(data), strings.ToLower(filepath.Ext(a.Filename)))
	if err != nil {
		return err
	}
	var sentTo *int64
	if from == thing {
		sentTo = &thing
	}
	if err := s.store.BackFromWherehouse(ctx, a.ID, name, size, sentTo); err != nil {
		s.files.Remove(name)
		return err
	}
	if from == thing {
		return s.store.OnWherehouseThing(ctx, a.ID, thing, att, a.WherehouseSeen)
	}
	return nil
}

// freePhotos are the thing's photos no photo of the warranty is kept as.
func freePhotos(ctx context.Context, wh *wherehouse.Client, wr *store.Warranty, thing int64) ([]photoThere, error) {
	all, err := thingPhotos(ctx, wh, thing)
	if err != nil {
		return nil, err
	}
	taken := map[int64]bool{}
	for _, a := range wr.Attachments {
		if a.Storage == store.StorageWherehouse && a.WherehousePhoto != nil && a.WherehouseThing != nil && *a.WherehouseThing == thing {
			taken[*a.WherehousePhoto] = true
		}
	}
	var out []photoThere
	for _, p := range all {
		if !taken[p.id] {
			out = append(out, p)
		}
	}
	return out, nil
}

// photoThere is one of a thing's photos, with its contents' hash once read.
type photoThere struct {
	id   int64
	size int64
	sum  *[32]byte
}

func thingPhotos(ctx context.Context, wh *wherehouse.Client, thing int64) ([]photoThere, error) {
	t, err := wh.Get(ctx, thing)
	if err != nil {
		return nil, err
	}
	out := make([]photoThere, len(t.Photos))
	for i, p := range t.Photos {
		out[i] = photoThere{id: p.ID, size: p.Size}
	}
	return out, nil
}

// matchPhoto finds the thing's photo with the same contents as a, which was
// sent there before, and returns its ID (0 if there's none) and the photos
// not matched yet.
func (s *Server) matchPhoto(ctx context.Context, wh *wherehouse.Client, a *store.Attachment, have []photoThere) (int64, []photoThere, error) {
	return matchContents(ctx, wh, a.Size, func() (*[32]byte, error) {
		f, err := s.files.Open(a.LocalPath)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return nil, err
		}
		return (*[32]byte)(h.Sum(nil)), nil
	}, have)
}

// matchContents finds the thing's photo of size whose contents hash to
// what sum returns, read only if needed; see matchPhoto.
func matchContents(ctx context.Context, wh *wherehouse.Client, size int64, sum func() (*[32]byte, error), have []photoThere) (int64, []photoThere, error) {
	var mine *[32]byte
	for i := range have {
		p := &have[i]
		if p.size != size {
			continue
		}
		if mine == nil {
			var err error
			if mine, err = sum(); err != nil {
				return 0, have, err
			}
		}
		if p.sum == nil {
			data, err := readWherehousePhoto(ctx, []*wherehouse.Client{wh}, p.id)
			if err != nil {
				return 0, have, err
			}
			sum := sha256.Sum256(data)
			p.sum = &sum
		}
		if *p.sum == *mine {
			id := p.id
			return id, append(have[:i:i], have[i+1:]...), nil
		}
	}
	return 0, have, nil
}

// maxWherehousePhoto is the most of a photo read from Wherehouse.
const maxWherehousePhoto = 64 << 20

// readWherehousePhoto reads a photo through the first connection that can.
func readWherehousePhoto(ctx context.Context, clients []*wherehouse.Client, id int64) ([]byte, error) {
	err := errors.New("not connected to Wherehouse")
	for _, c := range clients {
		var res *http.Response
		if res, err = c.OpenPhoto(ctx, id, false); err != nil {
			continue
		}
		var data []byte
		data, err = io.ReadAll(io.LimitReader(res.Body, maxWherehousePhoto+1))
		res.Body.Close()
		if err == nil && len(data) > maxWherehousePhoto {
			err = errors.New("photo in Wherehouse is too large")
		}
		if err == nil {
			return data, nil
		}
	}
	return nil, err
}

// proxyWherehousePhoto streams a photo or document kept in Wherehouse, or a
// photo's preview,
// through the viewer's connection or another one that can see the warranty.
func (s *Server) proxyWherehousePhoto(w http.ResponseWriter, r *http.Request, a *store.Attachment, thumb, download bool) {
	clients, _, err := s.wherehouseClients(r.Context(), userID(r), a.WarrantyID)
	if err != nil {
		fail(w, err)
		return
	}
	if len(clients) == 0 || (a.WherehousePhoto == nil && a.WherehouseAttachment == nil) {
		writeMsg(w, http.StatusBadGateway, "server.wherehousePhotoNoConnection", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	var res *http.Response
	for _, c := range clients {
		if a.WherehousePhoto != nil {
			res, err = c.OpenPhoto(ctx, *a.WherehousePhoto, thumb)
		} else {
			// A document has no preview there.
			thumb = false
			res, err = c.OpenAttachment(ctx, *a.WherehouseAttachment, download)
		}
		if err == nil {
			break
		}
	}
	if err != nil {
		wherehouseFail(w, err)
		return
	}
	defer res.Body.Close()
	if cl := res.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	// The type is the one checked on upload, or a preview's, which is an
	// image too, so Wherehouse can't make it active content.
	mimeType := a.MimeType
	if ct := res.Header.Get("Content-Type"); thumb && strings.HasPrefix(ct, "image/") && ct != "image/svg+xml" {
		mimeType = ct
	}
	w.Header().Set("Content-Type", mimeType)
	setDisposition(w, a.Filename, a.MimeType, download)
	io.Copy(w, res.Body)
}

// settleAllWherehousePhotos settles the photos of every warranty uid can see
// that is linked or has photos in Wherehouse, e.g. after the setting changed.
func (s *Server) settleAllWherehousePhotos(ctx context.Context, uid int64, leaving bool) error {
	states, err := s.store.WherehouseStates(ctx, uid)
	if err != nil {
		return err
	}
	ids, err := s.store.WherehousePhotoWarranties(ctx, uid)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, st := range states {
		if st.Thing != nil {
			ids = append(ids, st.ID)
		}
	}
	var errs []error
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := s.settleWherehousePhotos(ctx, uid, id, leaving); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// photoOnThing reports whether a photo is still on the thing that held it,
// through any connection that can see the thing.
func photoOnThing(ctx context.Context, clients []*wherehouse.Client, thing, photo int64) (bool, error) {
	return onThingThere(ctx, clients, thing, func(t *wherehouse.Thing) bool {
		for _, p := range t.Photos {
			if p.ID == photo {
				return true
			}
		}
		return false
	})
}

// onThingThere reports whether has holds for the thing, through any
// connection that can see it; false if it's gone.
func onThingThere(ctx context.Context, clients []*wherehouse.Client, thing int64, has func(*wherehouse.Thing) bool) (bool, error) {
	var failed error
	for _, c := range clients {
		t, err := c.Get(ctx, thing)
		var we *wherehouse.Error
		switch {
		case err == nil:
			return has(t), nil
		case errors.As(err, &we) && we.NotFound():
			// Not this one's, or gone.
		default:
			// A refused token or no answer says nothing about it.
			failed = err
		}
	}
	return false, failed
}
