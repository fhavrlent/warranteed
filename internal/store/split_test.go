package store_test

import (
	"context"
	"testing"

	"github.com/fhavrlent/warranteed/internal/store"
)

func TestSplitWarranty(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	ann := user(t, st, "ann")
	m := 24
	price := 349.0
	id, err := st.CreateWarranty(ctx, ann, &store.WarrantyInput{
		ProductName: "Bulb", PurchaseDate: "2025-01-01", WarrantyMonths: &m, Price: &price, Quantity: 3,
		SerialNumbers: []string{"A", "B", "C"}, Tags: []string{"Lights"},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := int64(5)
	local := &store.Attachment{WarrantyID: id, Kind: "purchase", Storage: store.StorageLocal, Filename: "r.pdf",
		MimeType: "application/pdf", LocalPath: "orig.pdf", Status: store.StatusReady, HomeBoxItem: "item-1"}
	paper := &store.Attachment{WarrantyID: id, Kind: "manual", Storage: store.StoragePaperless, Filename: "m.pdf",
		MimeType: "application/pdf", PaperlessDocID: &doc, Status: store.StatusReady}
	for _, a := range []*store.Attachment{local, paper} {
		if err := st.CreateAttachment(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	c, err := st.CreateClaim(ctx, ann, id, &store.ClaimInput{Title: "Flickers", ReportedOn: "2025-02-01"})
	if err != nil {
		t.Fatal(err)
	}

	// The copy of each file stored here is required.
	if _, err := st.SplitWarranty(ctx, ann, id, &store.SplitInput{Pieces: 1}, nil); err == nil {
		t.Fatal("split without copies of the files")
	}
	for _, bad := range []store.SplitInput{{Pieces: 0}, {Pieces: 3}, {Pieces: 1, SerialNumbers: []string{"Z"}}} {
		if _, err := st.SplitWarranty(ctx, ann, id, &bad, map[int64]string{local.ID: "copy.pdf"}); err == nil {
			t.Errorf("split %+v was accepted", bad)
		}
	}

	newID, err := st.SplitWarranty(ctx, ann, id, &store.SplitInput{Pieces: 1, SerialNumbers: []string{"B"}},
		map[int64]string{local.ID: "copy.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := st.GetWarranty(ctx, ann, id)
	split, _ := st.GetWarranty(ctx, ann, newID)
	if orig.Quantity != 2 || split.Quantity != 1 {
		t.Errorf("quantities %d and %d, want 2 and 1", orig.Quantity, split.Quantity)
	}
	if len(orig.SerialNumbers) != 2 || orig.SerialNumbers[0] != "A" || orig.SerialNumbers[1] != "C" ||
		len(split.SerialNumbers) != 1 || split.SerialNumbers[0] != "B" {
		t.Errorf("serials %v and %v", orig.SerialNumbers, split.SerialNumbers)
	}
	if *split.ExpiresOn != *orig.ExpiresOn || *split.Price != 349 || split.PurchaseDate != "2025-01-01" ||
		len(split.Tags) != 1 || split.Tags[0].Name != "Lights" {
		t.Errorf("split copy: %+v", split)
	}
	if len(split.Attachments) != 2 || len(orig.Attachments) != 2 {
		t.Fatalf("attachments %d and %d, want 2 each", len(orig.Attachments), len(split.Attachments))
	}
	for _, a := range split.Attachments {
		if a.Storage == store.StoragePaperless && *a.PaperlessDocID != 5 {
			t.Errorf("paperless document %v", *a.PaperlessDocID)
		}
	}
	// Claims stay with the original.
	if len(orig.Claims) != 1 || orig.Claims[0].ID != c.ID || len(split.Claims) != 0 {
		t.Errorf("claims %d and %d", len(orig.Claims), len(split.Claims))
	}
	// The new warranty's local file is its own copy, not yet in HomeBox.
	got, _ := st.GetAttachment(ctx, ann, split.Attachments[0].ID)
	if got.LocalPath != "copy.pdf" || got.HomeBoxItem != "" {
		t.Errorf("copied file: %q, in %q", got.LocalPath, got.HomeBoxItem)
	}
}
