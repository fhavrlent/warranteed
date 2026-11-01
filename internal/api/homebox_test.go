package api

import (
	"testing"

	"github.com/fhavrlent/warranteed/internal/homebox"
	"github.com/fhavrlent/warranteed/internal/store"
)

func TestFillHomeBoxItem(t *testing.T) {
	months := 24
	expires := "2027-03-01"
	price := 17490.0
	wr := &store.Warranty{
		ProductName: "Dishwasher", Brand: "Bosch", Model: "SMS6", Vendor: "Datart", OrderNumber: "A-1",
		PurchaseDate: "2025-03-01", WarrantyMonths: &months, ExpiresOn: &expires, Price: &price,
		SerialNumbers: []string{"SN-1", "SN-2"},
	}
	st := &store.Settings{DateFormat: "dd.MM.yyyy", Language: "en"}

	// A new item gets everything.
	var fresh homebox.Item
	fillHomeBoxItem(&fresh, wr, st, false)
	if fresh.SerialNumber != "SN-1" || fresh.PurchasePrice != 17490 || fresh.WarrantyExpires != expires ||
		fresh.PurchaseFrom != "Datart" || fresh.Manufacturer != "Bosch" {
		t.Errorf("new item: %+v", fresh)
	}
	if want := "2 years\nOrder number: A-1\nSerial numbers: SN-1, SN-2"; fresh.WarrantyDetails != want {
		t.Errorf("details = %q, want %q", fresh.WarrantyDetails, want)
	}

	// An existing item keeps what it has; only empty fields are filled in.
	existing := homebox.Item{ModelNumber: "THEIRS", PurchasePrice: 15000, PurchaseDate: "2025-01-01"}
	fillHomeBoxItem(&existing, wr, st, true)
	if existing.ModelNumber != "THEIRS" || existing.PurchasePrice != 15000 || existing.PurchaseDate != "2025-01-01" {
		t.Errorf("existing values were overwritten: %+v", existing)
	}
	if existing.SerialNumber != "SN-1" || existing.WarrantyExpires != expires {
		t.Errorf("empty fields weren't filled: %+v", existing)
	}

	// Coverage is kept as a whole: a lifetime warranty doesn't overwrite an
	// item that already has an end date, nor its details.
	lifetime := &store.Warranty{ProductName: "Knife", IsLifetime: true}
	dated := homebox.Item{WarrantyExpires: "2026-01-01", WarrantyDetails: "Shop warranty"}
	fillHomeBoxItem(&dated, lifetime, st, true)
	if dated.LifetimeWarranty || dated.WarrantyExpires != "2026-01-01" || dated.WarrantyDetails != "Shop warranty" {
		t.Errorf("existing coverage was changed: %+v", dated)
	}
	var bare homebox.Item
	fillHomeBoxItem(&bare, lifetime, st, true)
	if !bare.LifetimeWarranty || bare.WarrantyDetails != "Lifetime" {
		t.Errorf("empty coverage wasn't filled: %+v", bare)
	}
	// A new item's lifetime warranty has no end date.
	fresh = homebox.Item{WarrantyExpires: "2026-01-01"}
	fillHomeBoxItem(&fresh, lifetime, st, false)
	if !fresh.LifetimeWarranty || fresh.WarrantyExpires != "" {
		t.Errorf("lifetime warranty kept an end date: %+v", fresh)
	}

	// Czech text for Czech users.
	var cs homebox.Item
	fillHomeBoxItem(&cs, wr, &store.Settings{DateFormat: "dd.MM.yyyy", Language: "cs"}, false)
	if want := "2 roky\nČíslo objednávky: A-1\nSériová čísla: SN-1, SN-2"; cs.WarrantyDetails != want {
		t.Errorf("Czech details = %q, want %q", cs.WarrantyDetails, want)
	}
}
