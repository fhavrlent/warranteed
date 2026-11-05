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
	for _, a := range wr.Attachments {
		if a.Kind != "photo" {
			continue
		}
		switch {
		case a.Storage == store.StorageWherehouse && (!there || a.WherehouseThing == nil || *a.WherehouseThing != thing):
			if readers == nil {
				clients, _, err := s.wherehouseClients(ctx, uid, wr.ID)
				if err != nil {
					return err
				}
				readers = clients
				if wh != nil {
					readers = append([]*wherehouse.Client{wh}, readers...)
				}
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
				if !haveOK {
					var err error
					if have, err = thingPhotos(ctx, wh, thing); err != nil {
						return err
					}
					haveOK = true
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
		}
	}
	return nil
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
	var mine *[32]byte
	for i := range have {
		p := &have[i]
		if p.size != a.Size {
			continue
		}
		if mine == nil {
			f, err := s.files.Open(a.LocalPath)
			if err != nil {
				return 0, have, err
			}
			h := sha256.New()
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return 0, have, err
			}
			mine = (*[32]byte)(h.Sum(nil))
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

// proxyWherehousePhoto streams a photo kept in Wherehouse, or its preview,
// through the viewer's connection or another one that can see the warranty.
func (s *Server) proxyWherehousePhoto(w http.ResponseWriter, r *http.Request, a *store.Attachment, thumb, download bool) {
	clients, _, err := s.wherehouseClients(r.Context(), userID(r), a.WarrantyID)
	if err != nil {
		fail(w, err)
		return
	}
	if len(clients) == 0 || a.WherehousePhoto == nil {
		writeMsg(w, http.StatusBadGateway, "server.wherehousePhotoNoConnection", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	var res *http.Response
	for _, c := range clients {
		if res, err = c.OpenPhoto(ctx, *a.WherehousePhoto, thumb); err == nil {
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
