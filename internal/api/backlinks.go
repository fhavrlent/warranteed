package api

import (
	"context"
	"crypto/sha256"
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
		wantHomeBox = s.homeboxBacklink(ctx, hb, pc.BaseURL, doc.DocID, warranties)
		fields = append(fields, linkField{s.cfg.Paperless.HomeBoxField, wantHomeBox, []string{hb + "/item/", homebox.ListPrefix(hb)}})
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

// homeboxBacklink is the link to the HomeBox items of a document's
// warranties: the item itself when there is one, or else HomeBox's list of
// the items whose link field holds the document's value, which keeps the
// link short however many there are. Items that no longer belong lose the
// value, found with every HomeBox key. When an item can't be marked, the link falls back to the first one.
func (s *Server) homeboxBacklink(ctx context.Context, hb, paperlessURL string, docID int64, warranties []store.DocumentRef) string {
	var items []string
	for _, w := range warranties {
		if w.HomeBoxID != "" && !slices.Contains(items, w.HomeBoxID) {
			items = append(items, w.HomeBoxID)
		}
	}
	if len(items) == 0 {
		return ""
	}
	clients, err := s.homeboxClients(ctx, warranties)
	if err != nil {
		slog.Warn("read homebox keys", "doc", docID, "err", err)
	}
	value := receiptValue(paperlessURL, docID)
	want := hb + "/item/" + url.PathEscape(items[0])
	marked := []string{}
	if len(items) > 1 {
		for _, id := range items {
			// An item ID left by the previous version stays: another receipt's
			// link may still list the item by it until that one is updated.
			if err := s.updateHomeBoxItem(ctx, clients, id, func(it *homebox.Item) {
				it.AddField(homebox.LinkField, value)
			}, func(it *homebox.Item) bool { return it.HasField(homebox.LinkField, value) }); err != nil {
				slog.Warn("mark homebox item", "item", id, "err", err)
				break
			}
			marked = append(marked, id)
		}
		if len(marked) == len(items) {
			want = homebox.ListURL(hb, value)
		}
	}
	// Items taken off the receipt may be reachable only with the key of
	// someone it no longer concerns, so every key looks for them.
	keyHolders, err := s.store.HomeBoxKeyHolders(ctx)
	if err != nil {
		slog.Warn("read homebox keys", "doc", docID, "err", err)
	}
	everyone, err := s.homeboxClientsOf(ctx, keyHolders)
	if err != nil {
		slog.Warn("read homebox keys", "doc", docID, "err", err)
	}
	for _, c := range everyone {
		ids, err := c.WithField(ctx, homebox.LinkField, value)
		if err != nil {
			slog.Warn("find homebox items", "doc", docID, "err", err)
			continue
		}
		for _, id := range ids {
			if slices.Contains(marked, id) {
				continue
			}
			if err := s.updateHomeBoxItem(ctx, []*homebox.Client{c}, id, func(it *homebox.Item) {
				it.RemoveFields(homebox.LinkField, func(v string) bool { return v == value })
			}, func(it *homebox.Item) bool { return !it.HasField(homebox.LinkField, value) }); err != nil {
				slog.Warn("unmark homebox item", "item", id, "err", err)
			}
		}
	}
	return want
}

// receiptValue is what the items of Paperless document docID hold in their
// link field: its number, and a little of its Paperless's address to tell
// documents with the same number in another Paperless apart.
func receiptValue(paperlessURL string, docID int64) string {
	sum := sha256.Sum256([]byte(paperlessURL))
	return fmt.Sprintf("%d-%x", docID, sum[:3])
}

// homeboxClients are the HomeBox keys of everyone sharing the warranties,
// whose owners come first: whoever linked an item may not be whoever added
// the receipt.
func (s *Server) homeboxClients(ctx context.Context, warranties []store.DocumentRef) ([]*homebox.Client, error) {
	var users []int64
	for _, w := range warranties {
		if !slices.Contains(users, w.Owner) {
			users = append(users, w.Owner)
		}
	}
	for _, w := range warranties {
		ids, err := s.store.HomeBoxUsers(ctx, w.WarrantyID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !slices.Contains(users, id) {
				users = append(users, id)
			}
		}
	}
	return s.homeboxClientsOf(ctx, users)
}

// homeboxClientsOf returns HomeBox clients for those of users with a key.
func (s *Server) homeboxClientsOf(ctx context.Context, users []int64) ([]*homebox.Client, error) {
	var out []*homebox.Client
	for _, uid := range users {
		st, err := s.store.Settings(ctx, uid)
		if err != nil {
			return out, err
		}
		if c := s.homeboxFor(st); c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}

// updateHomeBoxItem changes an item with the first of clients that can read
// it, unless done says it needs nothing.
func (s *Server) updateHomeBoxItem(ctx context.Context, clients []*homebox.Client, id string, change func(*homebox.Item), done func(*homebox.Item) bool) error {
	defer s.lockPhotos("item:" + id)()
	err := errors.New("no homebox key")
	for _, c := range clients {
		var it *homebox.Item
		if it, err = c.Get(ctx, id); err != nil {
			continue
		}
		if done(it) {
			return nil
		}
		_, err = c.Update(ctx, id, change)
		return err
	}
	return err
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
