package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
)

const photoBytes = "\xff\xd8\xff photo"

// photosOnly turns "Keep product photos only in Wherehouse" on or off as Alice.
func (f *wherehouseFixture) photosOnly(t *testing.T, on bool) {
	t.Helper()
	ctx := context.Background()
	st, err := f.srv.store.Settings(ctx, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(st)
	var in map[string]any
	json.Unmarshal(body, &in)
	in["wherehousePhotosOnly"] = on
	body, _ = json.Marshal(in)
	r := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(string(body)))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	f.srv.putSettings(w, r)
	if w.Code != 200 {
		t.Fatalf("settings = %d %s", w.Code, w.Body)
	}
	f.srv.background.Wait()
}

// photos returns the warranty's photos.
func (f *wherehouseFixture) photos(t *testing.T, wid int64) []store.Attachment {
	t.Helper()
	var out []store.Attachment
	for _, a := range f.warranty(t, wid).Attachments {
		if a.Kind == "photo" {
			out = append(out, a)
		}
	}
	return out
}

// photoFile reads a photo the way the browser does.
func (f *wherehouseFixture) photoFile(t *testing.T, id int64, thumb bool) (int, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", fmt.Sprint(id))
	r = r.WithContext(auth.WithUser(r.Context(), f.alice))
	w := httptest.NewRecorder()
	if thumb {
		f.srv.attachmentThumb(w, r)
	} else {
		f.srv.attachmentFile(w, r)
	}
	return w.Code, w.Body.String()
}

// fileThere reports whether a photo's file is still kept here.
func (f *wherehouseFixture) fileThere(name string) bool {
	if name == "" {
		return false
	}
	file, err := f.srv.files.Open(name)
	if err == nil {
		file.Close()
	}
	return err == nil
}

func TestWherehousePhotosOnlyOneCopy(t *testing.T) {
	f := newWherehouseFixture(t)
	f.photosOnly(t, true)
	f.addPhoto(t)
	local := f.photos(t, f.warrantyID)[0].LocalPath
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageWherehouse || *ps[0].WherehouseThing != thing {
		t.Fatalf("photos = %+v", ps)
	}
	if f.fileThere(local) {
		t.Error("the photo's file is still kept here")
	}
	if th := f.wh.get(thing); len(th.Photos) != 1 {
		t.Errorf("thing photos = %+v", th.Photos)
	}
	// It's shown from Wherehouse.
	if code, body := f.photoFile(t, ps[0].ID, false); code != 200 || body != photoBytes {
		t.Errorf("photo = %d %q", code, body)
	}
	if code, _ := f.photoFile(t, ps[0].ID, true); code != 200 {
		t.Errorf("preview = %d", code)
	}
	// A photo added later goes there too, once.
	f.addPhoto(t)
	f.srv.sendToWherehouseLater(f.alice.ID, f.warrantyID)
	f.srv.background.Wait()
	for _, a := range f.photos(t, f.warrantyID) {
		if a.Storage != store.StorageWherehouse {
			t.Errorf("photo kept here: %+v", a)
		}
	}
	if th := f.wh.get(thing); len(th.Photos) != 2 {
		t.Errorf("thing photos after another = %d, want 2", len(th.Photos))
	}
}

// Copies sent before the setting was turned on are the ones already there:
// nothing is sent twice, and the files here go.
func TestWherehousePhotosOnlyTurnedOn(t *testing.T) {
	f := newWherehouseFixture(t)
	f.addPhoto(t)
	if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
		t.Fatalf("add = %d %s", w.Code, w.Body)
	}
	thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
	local := f.photos(t, f.warrantyID)[0].LocalPath
	// The thing has a photo of its own of the same size, which isn't it.
	f.wh.mu.Lock()
	f.wh.last++
	f.wh.photos[f.wh.last] = []byte(strings.Repeat("x", len(photoBytes)))
	th := f.wh.things[thing]
	th.Photos = append([]map[string]int64{{"id": f.wh.last, "size": int64(len(photoBytes))}}, th.Photos...)
	f.wh.mu.Unlock()

	f.photosOnly(t, true)
	ps := f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageWherehouse || *ps[0].WherehousePhoto != th.Photos[1]["id"] {
		t.Fatalf("photos = %+v, thing's %+v", ps, th.Photos)
	}
	if len(f.wh.get(thing).Photos) != 2 {
		t.Errorf("sent again: %+v", f.wh.get(thing).Photos)
	}
	if f.fileThere(local) {
		t.Error("the copy here is still kept")
	}

	// Turned off, they come back here, and aren't sent there again.
	f.photosOnly(t, false)
	ps = f.photos(t, f.warrantyID)
	if len(ps) != 1 || ps[0].Storage != store.StorageLocal || !f.fileThere(ps[0].LocalPath) {
		t.Fatalf("photos after turning off = %+v", ps)
	}
	if code, body := f.photoFile(t, ps[0].ID, false); code != 200 || body != photoBytes {
		t.Errorf("photo back here = %d %q", code, body)
	}
	f.srv.sendToWherehouseLater(f.alice.ID, f.warrantyID)
	f.srv.background.Wait()
	if len(f.wh.get(thing).Photos) != 2 {
		t.Errorf("sent again after coming back: %+v", f.wh.get(thing).Photos)
	}
}

// Unlinking or disconnecting brings the photos back; the thing keeps them.
func TestWherehousePhotosComeBack(t *testing.T) {
	for _, how := range []string{"unlink", "disconnect", "disconnect and unlink"} {
		t.Run(how, func(t *testing.T) {
			f := newWherehouseFixture(t)
			f.photosOnly(t, true)
			f.addPhoto(t)
			if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
				t.Fatalf("add = %d %s", w.Code, w.Body)
			}
			thing := f.warranty(t, f.warrantyID).Wherehouse.Thing
			if f.photos(t, f.warrantyID)[0].Storage != store.StorageWherehouse {
				t.Fatal("not moved to Wherehouse")
			}
			switch how {
			case "unlink":
				if w := f.call(t, f.srv.unlinkWherehouse, http.MethodDelete, ``); w.Code != 200 {
					t.Fatalf("unlink = %d %s", w.Code, w.Body)
				}
			default:
				target := "/api/settings/wherehouse"
				if how == "disconnect and unlink" {
					target += "?unlink=1"
				}
				r := httptest.NewRequest(http.MethodDelete, target, nil)
				r = r.WithContext(auth.WithUser(r.Context(), f.alice))
				w := httptest.NewRecorder()
				f.srv.disconnectWherehouse(w, r)
				if w.Code != http.StatusNoContent {
					t.Fatalf("disconnect = %d %s", w.Code, w.Body)
				}
				f.srv.background.Wait()
			}
			ps := f.photos(t, f.warrantyID)
			if len(ps) != 1 || ps[0].Storage != store.StorageLocal || !f.fileThere(ps[0].LocalPath) {
				t.Fatalf("photos = %+v", ps)
			}
			file, _ := f.srv.files.Open(ps[0].LocalPath)
			data, _ := io.ReadAll(file)
			file.Close()
			if string(data) != photoBytes {
				t.Errorf("photo back here = %q", data)
			}
			if len(f.wh.get(thing).Photos) != 1 {
				t.Errorf("thing's photos = %+v", f.wh.get(thing).Photos)
			}
		})
	}
}

// A split-off warranty gets the photos on its own thing, or here.
func TestWherehousePhotosSplit(t *testing.T) {
	for _, toWherehouse := range []bool{true, false} {
		t.Run(fmt.Sprint("wherehouse=", toWherehouse), func(t *testing.T) {
			f := newWherehouseFixture(t)
			f.photosOnly(t, true)
			f.addPhoto(t)
			if w := f.call(t, f.srv.addToWherehouse, http.MethodPost, `{}`); w.Code != 200 {
				t.Fatalf("add = %d %s", w.Code, w.Body)
			}
			orig := f.warranty(t, f.warrantyID).Wherehouse.Thing
			w := f.call(t, f.srv.splitWarranty, http.MethodPost,
				fmt.Sprintf(`{"pieces":1,"serialNumbers":[],"wherehouse":%v}`, toWherehouse))
			if w.Code != http.StatusCreated {
				t.Fatalf("split = %d %s", w.Code, w.Body)
			}
			var out struct {
				Warranty store.Warranty `json:"warranty"`
			}
			json.Unmarshal(w.Body.Bytes(), &out)
			ps := f.photos(t, out.Warranty.ID)
			if len(ps) != 1 {
				t.Fatalf("split-off photos = %+v", ps)
			}
			if toWherehouse {
				own := f.warranty(t, out.Warranty.ID).Wherehouse.Thing
				if ps[0].Storage != store.StorageWherehouse || *ps[0].WherehouseThing != own || len(f.wh.get(own).Photos) != 1 {
					t.Errorf("split-off photo = %+v, its thing's %+v", ps[0], f.wh.get(own).Photos)
				}
			} else if ps[0].Storage != store.StorageLocal || !f.fileThere(ps[0].LocalPath) {
				t.Errorf("split-off photo = %+v", ps[0])
			}
			if o := f.photos(t, f.warrantyID); len(o) != 1 || o[0].Storage != store.StorageWherehouse || *o[0].WherehouseThing != orig {
				t.Errorf("original's photos = %+v", o)
			}
			if code, body := f.photoFile(t, ps[0].ID, false); code != 200 || body != photoBytes {
				t.Errorf("split-off photo = %d %q", code, body)
			}
		})
	}
}
