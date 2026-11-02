package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fhavrlent/warranteed/internal/store"
)

func TestClaims(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	ann, bob := user(t, st, "ann"), user(t, st, "bob")
	wid := add(t, st, ann, "Kettle", nil) // household: Bob sees it too
	priv, _ := spaces(t, st, ann)
	private := add(t, st, ann, "Diary", &priv)

	cost := 12.5
	c, err := st.CreateClaim(ctx, bob, wid, &store.ClaimInput{
		Title: "  Doesn't heat ", ReportedOn: "2025-06-01", CaseNumber: " RMA-1 ", Cost: &cost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Doesn't heat" || c.Status != "open" || c.CaseNumber != "RMA-1" || c.AddedBy.ID != bob {
		t.Fatalf("created: %+v", c)
	}
	// Someone who can't see the warranty can't add a claim to it.
	if _, err := st.CreateClaim(ctx, bob, private, &store.ClaimInput{Title: "X", ReportedOn: "2025-06-01"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("claim on a private warranty: %v", err)
	}

	// Resolving it.
	done := "2025-06-20"
	c, err = st.UpdateClaim(ctx, ann, c.ID, &store.ClaimInput{
		Title: c.Title, Status: "repaired", ReportedOn: c.ReportedOn, ResolvedOn: &done,
	})
	if err != nil || c.Status != "repaired" || c.ResolvedOn == nil || *c.ResolvedOn != done {
		t.Fatalf("updated: %+v %v", c, err)
	}
	// An open claim has no resolution date.
	c, _ = st.UpdateClaim(ctx, ann, c.ID, &store.ClaimInput{Title: c.Title, ReportedOn: c.ReportedOn, ResolvedOn: &done})
	if c.ResolvedOn != nil {
		t.Fatalf("open claim kept its resolution date")
	}

	for name, in := range map[string]store.ClaimInput{
		"no title":        {ReportedOn: "2025-06-01"},
		"bad status":      {Title: "X", Status: "lost", ReportedOn: "2025-06-01"},
		"bad date":        {Title: "X", ReportedOn: "June"},
		"resolved before": {Title: "X", Status: "rejected", ReportedOn: "2025-06-01", ResolvedOn: ptr("2025-05-01")},
		"negative cost":   {Title: "X", ReportedOn: "2025-06-01", Cost: ptr(-1.0)},
	} {
		if _, err := st.UpdateClaim(ctx, ann, c.ID, &in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// The warranty lists its claims.
	w, _ := st.GetWarranty(ctx, ann, wid)
	if len(w.Claims) != 1 || w.Claims[0].ID != c.ID {
		t.Fatalf("warranty claims: %+v", w.Claims)
	}

	// A document can belong to the claim, and stays when the claim goes.
	a := &store.Attachment{WarrantyID: wid, Kind: "claim", Storage: store.StorageLocal, Filename: "letter.pdf",
		MimeType: "application/pdf", Status: store.StatusReady, ClaimID: &c.ID}
	if err := st.CreateAttachment(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimOf(ctx, wid, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimOf(ctx, private, c.ID); err == nil {
		t.Fatal("claim matched another warranty")
	}

	// Linking to another HomeBox item forgets the entry in the old one.
	st.SetClaimEntry(ctx, c.ID, "entry-1")
	if err := st.SetHomeBox(ctx, ann, wid, &store.HomeBoxLink{ID: "item-2"}); err != nil {
		t.Fatal(err)
	}
	if got, item, _ := st.ClaimSync(ctx, c.ID); got.HomeBoxEntry != "" || item != "item-2" {
		t.Fatalf("after relinking: entry %q, item %q", got.HomeBoxEntry, item)
	}

	if _, err := st.DeleteClaim(ctx, bob, c.ID); err != nil {
		t.Fatal(err)
	}
	w, _ = st.GetWarranty(ctx, ann, wid)
	if len(w.Claims) != 0 || len(w.Attachments) != 1 || w.Attachments[0].ClaimID != nil {
		t.Fatalf("after deleting the claim: %+v %+v", w.Claims, w.Attachments)
	}
}

func ptr[T any](v T) *T { return &v }
