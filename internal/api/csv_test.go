package api

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/store"
)

const sampleCSV = `product_name,brand,model,vendor,order_number,purchase_date,warranty_months,warranty_end_date,lifetime,price,currency,serial_numbers,tags,product_url,notes,archived
Washing Machine,Bosch,WM-900,Datart,A-1,2023-06-01,30,,false,499.99,CZK,SN-1;SN-2,Home;Laundry,example.com/wm,Keep the box,false
Knife,,,,,2024-01-15,,,true,,,,,,,false
Old Phone,,,,,15.03.2022,,2024-03-15,false,,,,Electronics,,,true
Broken,,,,,2024-01-01,,,false,,,,,,,false
`

func parseRows(t *testing.T) []func(string) string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(sampleCSV)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	var out []func(string) string
	for _, rec := range recs[1:] {
		rec := rec
		out = append(out, func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		})
	}
	return out
}

func TestImportRows(t *testing.T) {
	st := &store.Settings{DateFormat: "dd.MM.yyyy", Currency: "EUR"}
	rows := parseRows(t)

	wm, archived, msg := rowToInput(rows[0], st)
	if msg != "" || archived {
		t.Fatal(msg)
	}
	if *wm.WarrantyMonths != 30 || wm.Currency != "CZK" || wm.Model != "WM-900" ||
		len(wm.SerialNumbers) != 2 || len(wm.Tags) != 2 || *wm.Price != 499.99 {
		t.Fatalf("washing machine parsed wrong: %+v", wm)
	}
	exp, err := wm.Normalize()
	if err != nil || *exp != "2025-12-01" || wm.ProductURL != "https://example.com/wm" {
		t.Fatalf("expiry %v url %q err %v", exp, wm.ProductURL, err)
	}

	knife, _, msg := rowToInput(rows[1], st)
	if msg != "" || !knife.IsLifetime || knife.Currency != "EUR" {
		t.Fatalf("knife: %+v %s", knife, msg)
	}

	phone, archived, msg := rowToInput(rows[2], st)
	if msg != "" || !archived || phone.PurchaseDate != "2022-03-15" || *phone.WarrantyEndDate != "2024-03-15" {
		t.Fatalf("phone: %+v %s", phone, msg)
	}

	if _, _, msg := rowToInput(rows[3], st); msg == "" {
		t.Fatal("row without any warranty length should be rejected")
	}
}

// What export writes must import back unchanged.
func TestExportHeaderRoundTrip(t *testing.T) {
	recs, _ := csv.NewReader(strings.NewReader(sampleCSV)).Read()
	if strings.Join(recs, ",") != strings.Join(csvHeader, ",") {
		t.Fatalf("sample header out of sync with export header")
	}
}

func TestFormulaEscaping(t *testing.T) {
	for _, v := range []string{"=HYPERLINK(\"http://x\",\"y\")", "+1", "-5 off", "@SUM(A1)", "\tx", "plain", "", "'quoted", "'=already escaped"} {
		esc := escapeCell(v)
		if esc != "" && strings.ContainsRune(formulaChars, rune(esc[0])) {
			t.Errorf("%q exported as %q, still starts with a formula character", v, esc)
		}
		if got := unescapeCell(esc); got != v {
			t.Errorf("%q round-tripped to %q", v, got)
		}
	}
}
