package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/store"
)

func (f *homeboxFixture) claimRequest(t *testing.T, h http.HandlerFunc, who *store.User, id int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	h(w, r.WithContext(auth.WithUser(r.Context(), who)))
	f.srv.background.Wait()
	return w
}

func (f *homeboxFixture) onlyEntry(t *testing.T) homebox.MaintenanceEntry {
	t.Helper()
	f.hb.mu.Lock()
	defer f.hb.mu.Unlock()
	if len(f.hb.entries) != 1 {
		t.Fatalf("%d maintenance entries, want 1: %+v", len(f.hb.entries), f.hb.entries)
	}
	for _, e := range f.hb.entries {
		return e
	}
	return homebox.MaintenanceEntry{}
}

func TestClaimsInHomeBox(t *testing.T) {
	f := newHomeBoxFixture(t)

	// Alice reports a problem before the warranty is in HomeBox.
	w := f.claimRequest(t, f.srv.createClaim, f.alice, f.warrantyID,
		`{"title":"Doesn't heat","reportedOn":"2025-06-01","caseNumber":"RMA-1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var c store.Claim
	json.NewDecoder(w.Body).Decode(&c)
	if len(f.hb.entries) != 0 {
		t.Fatal("a claim went to HomeBox before the warranty was linked")
	}

	// Linking copies it to the item's maintenance log.
	if w := f.link(t); w.Code != 200 {
		t.Fatalf("link: %d %s", w.Code, w.Body)
	}
	e := f.onlyEntry(t)
	if e.Name != "Warranty claim: Doesn't heat" || e.ScheduledDate != "2025-06-01" || e.CompletedDate != "" ||
		!strings.Contains(e.Description, "Case number: RMA-1") ||
		!strings.Contains(e.Description, fmt.Sprintf("https://warranty.example.test/w/%d", f.warrantyID)) {
		t.Errorf("entry: %+v", e)
	}

	// Alice has no HomeBox key; her change goes through Bob's.
	w = f.claimRequest(t, f.srv.updateClaim, f.alice, c.ID,
		`{"title":"Doesn't heat","status":"repaired","reportedOn":"2025-06-01","resolvedOn":"2025-06-20","cost":12.5}`)
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	if e = f.onlyEntry(t); e.CompletedDate != "2025-06-20" || e.Cost != "12.5" || !strings.HasPrefix(e.Description, "Repaired") {
		t.Errorf("updated entry: %+v", e)
	}

	// Removed in HomeBox: the next change adds it again.
	f.hb.mu.Lock()
	clear(f.hb.entries)
	f.hb.mu.Unlock()
	f.claimRequest(t, f.srv.updateClaim, f.bob, c.ID,
		`{"title":"Still doesn't heat","status":"repaired","reportedOn":"2025-06-01","resolvedOn":"2025-06-20"}`)
	if e = f.onlyEntry(t); e.Name != "Warranty claim: Still doesn't heat" {
		t.Errorf("re-added entry: %+v", e)
	}

	// A document can go with the claim, but not with another warranty's.
	upload := func(claimID string) int {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		mw.WriteField("kind", "claim")
		mw.WriteField("claimId", claimID)
		part, _ := mw.CreateFormFile("file", "letter.pdf")
		part.Write([]byte("%PDF-1.4 letter"))
		mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.SetPathValue("id", fmt.Sprint(f.warrantyID))
		w := httptest.NewRecorder()
		f.srv.uploadAttachment(w, r.WithContext(auth.WithUser(r.Context(), f.bob)))
		return w.Code
	}
	if code := upload(fmt.Sprint(c.ID)); code != http.StatusCreated {
		t.Fatalf("upload with the claim: %d", code)
	}
	if code := upload("999"); code != http.StatusBadRequest {
		t.Errorf("upload with someone else's claim: %d, want 400", code)
	}
	wr, _ := f.srv.store.GetWarranty(t.Context(), f.bob.ID, f.warrantyID)
	var withClaim int
	for _, a := range wr.Attachments {
		if a.ClaimID != nil && *a.ClaimID == c.ID {
			withClaim++
		}
	}
	if withClaim != 1 {
		t.Errorf("%d documents with the claim, want 1", withClaim)
	}

	// Deleting the claim deletes its entry.
	if w := f.claimRequest(t, f.srv.deleteClaim, f.alice, c.ID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	if len(f.hb.entries) != 0 {
		t.Errorf("entry left in HomeBox: %+v", f.hb.entries)
	}
}
