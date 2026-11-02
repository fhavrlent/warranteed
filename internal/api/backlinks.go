package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// Links back from Paperless: users who turn this on get two URL custom
// fields on each of their documents in Paperless, pointing to the warranty
// the document belongs to and to that warranty's HomeBox item.

// syncBacklinks brings the link fields of one document up to date. They point
// to the warranty the document is attached to, or to the list of them when
// there are several, and to their HomeBox item or items, and are removed
// when there is none. A value
// Warranteed didn't write, such as one a user typed in, is left alone.
//
// A document can be attached by several people using the same Paperless, so
// every attachment through that instance counts, and the fields are written
// with the credentials of someone on that Paperless who turned links on,
// preferably doc.Owner, who triggered the update, then whoever has it
// attached, then anyone else.
func (s *Server) syncBacklinks(ctx context.Context, doc store.PaperlessDoc) error {
	pc, err := s.userPaperless(ctx, doc.Owner)
	if err != nil || pc == nil {
		return err
	}
	// One update per document at a time, reading the latest state once it's
	// its turn, so an older update can't finish last and win.
	lock, _ := s.docLocks.LoadOrStore(fmt.Sprintf("%s\x00%d", pc.BaseURL, doc.DocID), new(sync.Mutex))
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	refs, err := s.store.DocumentRefs(ctx, doc.DocID)
	if err != nil {
		return err
	}
	// Owners whose Paperless is this one, and whether they turned links on.
	type ownerInfo struct {
		client *paperless.Client
		on     bool
	}
	owners := map[int64]*ownerInfo{}
	owner := func(uid int64) (*ownerInfo, error) {
		if o, ok := owners[uid]; ok {
			return o, nil
		}
		o := &ownerInfo{}
		st, err := s.store.Settings(ctx, uid)
		if err != nil {
			return nil, err
		}
		admin, err := s.store.IsAdmin(ctx, uid)
		if err != nil {
			return nil, err
		}
		if c := s.paperlessFor(st, admin); c != nil && c.BaseURL == pc.BaseURL {
			o.client, o.on = c, st.PaperlessBacklinks
		}
		owners[uid] = o
		return o, nil
	}
	var warranties []store.DocumentRef
	writer, err := owner(doc.Owner)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		o, err := owner(ref.Owner)
		if err != nil {
			return err
		}
		if o.client == nil {
			continue // the same ID in someone else's Paperless
		}
		warranties = append(warranties, ref)
		if !writer.on && o.on {
			writer = o
		}
	}
	if !writer.on {
		// Nobody with links on has it attached any more, but someone may have
		// written links earlier that now need correcting or removing.
		ids, err := s.store.BacklinkUsers(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if o, err := owner(id); err != nil {
				return err
			} else if o.on {
				writer = o
				break
			}
		}
	}
	if !writer.on {
		return nil
	}
	pc = writer.client

	app := strings.TrimSuffix(s.cfg.AppURL.String(), "/")
	wantWarranty, wantHomeBox := "", ""
	// The warranties by ID, which also tells documents with the same ID in
	// other people's Paperless apart.
	var ids []string
	for _, w := range warranties {
		if id := strconv.FormatInt(w.WarrantyID, 10); !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	switch {
	case len(ids) > 1:
		wantWarranty = fmt.Sprintf("%s/?warranties=%s&filter=all", app, strings.Join(ids, ","))
	case len(ids) == 1:
		wantWarranty = fmt.Sprintf("%s/w/%s", app, ids[0])
	}
	type linkField struct {
		name, want string
		ours       []string // how values Warranteed wrote start
	}
	fields := []linkField{{s.cfg.Paperless.WarrantyField, wantWarranty, []string{app + "/w/", app + "/?warranties="}}}
	if hb := s.cfg.HomeBox.PublicURL; hb != "" {
		wantHomeBox = s.homeboxBacklink(ctx, hb, warranties)
		fields = append(fields, linkField{s.cfg.Paperless.HomeBoxField, wantHomeBox, []string{hb + "/item/", hb + "/items?"}})
	}

	current, err := pc.DocumentCustomFields(ctx, doc.DocID)
	if err != nil {
		return err
	}
	changed := false
	for _, f := range fields {
		// A field is only created when there is something to put in it.
		id, err := pc.CustomFieldID(ctx, f.name, "url", f.want != "")
		if err != nil {
			return err
		}
		if id == 0 {
			continue
		}
		var c bool
		current, c = setLinkField(current, id, f.want, f.ours...)
		changed = changed || c
	}
	if !changed {
		return nil
	}
	return pc.SetDocumentCustomFields(ctx, doc.DocID, current)
}

// homeboxBacklink is the link to the HomeBox items of warranties: the item
// itself when there is one, or else HomeBox's list of the items by their link
// field, which items linked before it existed get here. When an item can't
// be read for that, the link falls back to the first item.
func (s *Server) homeboxBacklink(ctx context.Context, hb string, warranties []store.DocumentRef) string {
	var items []store.DocumentRef
	for _, w := range warranties {
		if w.HomeBoxID != "" && !slices.ContainsFunc(items, func(i store.DocumentRef) bool { return i.HomeBoxID == w.HomeBoxID }) {
			items = append(items, w)
		}
	}
	switch len(items) {
	case 0:
		return ""
	case 1:
		return hb + "/item/" + url.PathEscape(items[0].HomeBoxID)
	}
	first := hb + "/item/" + url.PathEscape(items[0].HomeBoxID)
	var values []string
	for _, w := range items {
		v, err := s.homeboxLinkValue(ctx, w)
		if err != nil {
			slog.Warn("read homebox link field", "item", w.HomeBoxID, "err", err)
			return first
		}
		if !slices.Contains(values, v) {
			values = append(values, v)
		}
	}
	return homebox.ListURL(hb, values)
}

// homeboxLinkValue reads the link field of the warranty's item, using its
// owner's HomeBox key, and fills it in when it's empty.
func (s *Server) homeboxLinkValue(ctx context.Context, w store.DocumentRef) (string, error) {
	st, err := s.store.Settings(ctx, w.Owner)
	if err != nil {
		return "", err
	}
	hb := s.homeboxFor(st)
	if hb == nil {
		return "", errors.New("no homebox key")
	}
	it, err := hb.Get(ctx, w.HomeBoxID)
	if err != nil {
		return "", err
	}
	if v := it.Field(homebox.LinkField); v != "" {
		return v, nil
	}
	v := strconv.FormatInt(w.WarrantyID, 10)
	_, err = hb.Update(ctx, w.HomeBoxID, func(it *homebox.Item) {
		if it.Field(homebox.LinkField) == "" {
			it.SetField(homebox.LinkField, v)
		}
	})
	return v, err
}

// setLinkField sets field id to want, or removes it when want is empty,
// unless it holds a value that doesn't start with one of ours. It reports
// whether anything changed.
func setLinkField(fields []paperless.CustomField, id int64, want string, ours ...string) ([]paperless.CustomField, bool) {
	for i, f := range fields {
		if f.Field != id {
			continue
		}
		cur, isText := f.Value.(string)
		mine := cur == ""
		for _, p := range ours {
			mine = mine || strings.HasPrefix(cur, p)
		}
		if (f.Value != nil && !isText) || !mine {
			return fields, false // someone else's value
		}
		if want == "" {
			return append(fields[:i:i], fields[i+1:]...), true
		}
		if cur == want {
			return fields, false
		}
		out := append([]paperless.CustomField(nil), fields...)
		out[i].Value = want
		return out, true
	}
	if want == "" {
		return fields, false
	}
	return append(fields, paperless.CustomField{Field: id, Value: want}), true
}

// syncBacklinksLater updates documents' link fields in the background, so a
// slow or refusing Paperless doesn't hold up the request that changed them.
func (s *Server) syncBacklinksLater(docs ...store.PaperlessDoc) {
	if len(docs) == 0 {
		return
	}
	s.backlinks.Add(1)
	go func() {
		defer s.backlinks.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, d := range docs {
			if err := s.syncBacklinks(ctx, d); err != nil {
				slog.Warn("update links in paperless", "user", d.Owner, "doc", d.DocID, "err", err)
			}
		}
	}()
}

// syncWarrantyBacklinksLater updates the documents attached to a warranty.
func (s *Server) syncWarrantyBacklinksLater(ctx context.Context, warrantyID int64) {
	docs, err := s.store.WarrantyPaperlessDocs(ctx, warrantyID)
	if err != nil {
		slog.Warn("list paperless documents", "warranty", warrantyID, "err", err)
		return
	}
	s.syncBacklinksLater(docs...)
}

// syncAllBacklinks updates every document attached through the user's
// Paperless, e.g. right after they turn the links on, and reports how it went.
func (s *Server) syncAllBacklinks(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.OwnerPaperlessDocs(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	out := struct {
		Documents int       `json:"documents"`
		Failed    int       `json:"failed"`
		Error     *apiError `json:"error,omitempty"`
	}{Documents: len(docs)}
	for _, d := range docs {
		if err := s.syncBacklinks(ctx, d); err != nil {
			out.Failed++
			if out.Error == nil {
				out.Error = backlinkError(err)
			}
		}
	}
	writeJSON(w, 200, out)
}

// backlinkError explains why links couldn't be written; Paperless refusing
// is almost always a token that may read but not edit documents.
func backlinkError(err error) *apiError {
	var pe *paperless.Error
	var fe *paperless.FieldTypeError
	if errors.As(err, &fe) {
		args := i18n.Args{"name": fe.Name}
		return &apiError{Error: i18n.T(i18n.Fallback, "server.paperlessFieldType", args), Code: "server.paperlessFieldType", Params: args}
	}
	if errors.As(err, &pe) && (pe.Status == http.StatusForbidden || pe.Status == http.StatusUnauthorized) {
		return &apiError{Error: i18n.T(i18n.Fallback, "server.paperlessCantEdit", nil), Code: "server.paperlessCantEdit"}
	}
	return &apiError{Error: err.Error()}
}
