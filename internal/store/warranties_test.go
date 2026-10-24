package store

import (
	"testing"
	"time"

	"github.com/fhavrlent/warranteed/internal/dates"
)

func TestAddMonths(t *testing.T) {
	cases := map[string]struct {
		from   string
		months int
		want   string
	}{
		"simple":          {"2024-03-15", 24, "2026-03-15"},
		"clamp to feb":    {"2024-01-31", 1, "2024-02-29"},
		"clamp non leap":  {"2025-01-31", 1, "2025-02-28"},
		"year rollover":   {"2024-11-30", 3, "2025-02-28"},
		"end of month ok": {"2024-04-30", 12, "2025-04-30"},
	}
	for name, tc := range cases {
		from, _ := time.Parse(DateLayout, tc.from)
		if got := AddMonths(from, tc.months).Format(DateLayout); got != tc.want {
			t.Errorf("%s: got %s want %s", name, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	m := 24
	in := WarrantyInput{ProductName: " TV ", PurchaseDate: "2024-01-31", WarrantyMonths: &m, Currency: "eur", SerialNumbers: []string{" a ", "", "b"}}
	exp, err := in.Normalize()
	if err != nil || exp == nil || *exp != "2026-01-31" {
		t.Fatalf("got %v %v", exp, err)
	}
	if in.ProductName != "TV" || in.Currency != "EUR" || len(in.SerialNumbers) != 2 {
		t.Fatalf("not normalised: %+v", in)
	}

	life := WarrantyInput{ProductName: "Knife", PurchaseDate: "2024-01-01", IsLifetime: true, WarrantyMonths: &m}
	if exp, err := life.Normalize(); err != nil || exp != nil || life.WarrantyMonths != nil {
		t.Fatalf("lifetime: %v %v", exp, err)
	}

	end := "2023-01-01"
	bad := WarrantyInput{ProductName: "X", PurchaseDate: "2024-01-01", WarrantyEndDate: &end}
	if _, err := bad.Normalize(); err == nil {
		t.Fatal("end before purchase should fail")
	}
	none := WarrantyInput{ProductName: "X", PurchaseDate: "2024-01-01"}
	if _, err := none.Normalize(); err == nil {
		t.Fatal("missing duration should fail")
	}
}

func TestDateFormats(t *testing.T) {
	want := map[string]string{
		"yyyy-MM-dd":   "2024-03-05",
		"dd.MM.yyyy":   "05.03.2024",
		"d. M. yyyy":   "5. 3. 2024",
		"dd/MM/yyyy":   "05/03/2024",
		"MM/dd/yyyy":   "03/05/2024",
		"d MMM yyyy":   "5 Mar 2024",
		"MMM d, yyyy":  "Mar 5, 2024",
		"d MMMM yyyy":  "5 March 2024",
		"MMMM d, yyyy": "March 5, 2024",
	}
	for _, f := range dates.Formats {
		w, ok := want[f]
		if !ok {
			t.Errorf("no expectation for format %q", f)
			continue
		}
		if got := dates.Format("2024-03-05", f); got != w {
			t.Errorf("%s: got %q want %q", f, got, w)
		}
	}
}
