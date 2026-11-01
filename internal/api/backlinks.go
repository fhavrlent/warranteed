package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/i18n"
	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// Links back from Paperless: users who turn this on get two URL custom
// fields on each of their documents in Paperless, pointing to the warranty
// the document belongs to and to that warranty's HomeBox item.

// syncBacklinks brings the link fields of one document up to date. They point
// to the oldest warranty the document is attached to and the first HomeBox
// item among its warranties, and are removed when there is none. A value
// Warranteed didn't write, such as one a user typed in, is left alone.
func (s *Server) syncBacklinks(ctx context.Context, doc store.PaperlessDoc) error {
	st, err := s.store.Settings(ctx, doc.Owner)
	if err != nil || !st.PaperlessBacklinks {
		return err
	}
	pc, err := s.userPaperless(ctx, doc.Owner)
	if err != nil || pc == nil {
		return err
	}
	warranties, err := s.store.DocumentWarranties(ctx, doc)
	if err != nil {
		return err
	}

	app := strings.TrimSuffix(s.cfg.AppURL.String(), "/")
	wantWarranty, wantHomeBox := "", ""
	if len(warranties) > 0 {
		wantWarranty = fmt.Sprintf("%s/w/%d", app, warranties[0].WarrantyID)
	}
	type linkField struct{ name, want, prefix string }
	fields := []linkField{{s.cfg.Paperless.WarrantyField, wantWarranty, app + "/w/"}}
	if hb := s.cfg.HomeBox.PublicURL; hb != "" {
		for _, w := range warranties {
			if w.HomeBoxID != "" {
				wantHomeBox = hb + "/item/" + url.PathEscape(w.HomeBoxID)
				break
			}
		}
		fields = append(fields, linkField{s.cfg.Paperless.HomeBoxField, wantHomeBox, hb + "/item/"})
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
		current, c = setLinkField(current, id, f.want, f.prefix)
		changed = changed || c
	}
	if !changed {
		return nil
	}
	return pc.SetDocumentCustomFields(ctx, doc.DocID, current)
}

// setLinkField sets field id to want, or removes it when want is empty,
// unless it holds a value that doesn't start with prefix. It reports whether
// anything changed.
func setLinkField(fields []paperless.CustomField, id int64, want, prefix string) ([]paperless.CustomField, bool) {
	for i, f := range fields {
		if f.Field != id {
			continue
		}
		cur, _ := f.Value.(string)
		if cur != "" && !strings.HasPrefix(cur, prefix) {
			return fields, false
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
	if errors.As(err, &pe) && (pe.Status == http.StatusForbidden || pe.Status == http.StatusUnauthorized) {
		return &apiError{Error: i18n.T(i18n.Fallback, "server.paperlessCantEdit", nil), Code: "server.paperlessCantEdit"}
	}
	return &apiError{Error: err.Error()}
}
