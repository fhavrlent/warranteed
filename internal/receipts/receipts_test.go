package receipts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDetails(t *testing.T) {
	var a answer
	if err := json.Unmarshal([]byte(`{"vendor":"Obchod","purchaseDate":"2026-10-31","currency":"CZK","orderNumber":"1001",
		"items":[{"productName":"Rychlovarná konvice","brand":"Značka","model":"K100","price":799,"warrantyMonths":null,"serialNumbers":[],"extension":"","extendedMonths":null},
		{"productName":"Toustovač","brand":"Značka","model":"T200","price":599,"warrantyMonths":24,"serialNumbers":["A1"],"extension":"Prodloužená záruka","extendedMonths":12}]}`), &a); err != nil {
		t.Fatal(err)
	}
	got := a.details()
	if len(got) != 2 {
		t.Fatalf("got %d products, want 2", len(got))
	}
	for _, d := range got {
		if d.Vendor != "Obchod" || d.PurchaseDate != "2026-10-31" || d.Currency != "CZK" || d.OrderNumber != "1001" {
			t.Errorf("%s lacks the purchase's details: %+v", d.Model, d)
		}
	}
	if got[0].Model != "K100" || *got[0].Price != 799 || got[0].WarrantyMonths != nil {
		t.Errorf("first product: %+v", got[0])
	}
	if got[1].Model != "T200" || *got[1].WarrantyMonths != 24 || got[1].SerialNumbers[0] != "A1" ||
		got[1].Extension != "Prodloužená záruka" || *got[1].ExtendedMonths != 12 {
		t.Errorf("second product: %+v", got[1])
	}
}

func TestDetailsWithoutItems(t *testing.T) {
	a := answer{Vendor: "Obchod", OrderNumber: "1001"}
	got := a.details()
	if len(got) != 1 || got[0].Vendor != "Obchod" || got[0].OrderNumber != "1001" {
		t.Errorf("got %+v, want the purchase alone", got)
	}
}

func TestDetailsAtMost(t *testing.T) {
	items := make([]string, MaxItems+5)
	for i := range items {
		items[i] = fmt.Sprintf(`{"productName":"P%d","brand":"","model":"","price":null,"warrantyMonths":null,"serialNumbers":[]}`, i)
	}
	var a answer
	if err := json.Unmarshal([]byte(`{"items":[`+strings.Join(items, ",")+`]}`), &a); err != nil {
		t.Fatal(err)
	}
	if got := a.details(); len(got) != MaxItems {
		t.Errorf("got %d products, want %d", len(got), MaxItems)
	}
}

func TestReceivedDate(t *testing.T) {
	for received, want := range map[string]string{"2026-11-02": "2026-11-02", "2026-10-31": "", "": ""} {
		a := answer{PurchaseDate: "2026-10-31", ReceivedDate: received}
		if got := a.details()[0].ReceivedDate; got != want {
			t.Errorf("received %q: got %q, want %q", received, got, want)
		}
	}
}

func TestPromptFollowsTheChoice(t *testing.T) {
	if p := prompt(false); !strings.Contains(p, "worth tracking a warranty for") || strings.Contains(p, "%!") {
		t.Errorf("worthy prompt: %s", p)
	}
	if p := prompt(true); !strings.Contains(p, "every product") || strings.Contains(p, "worth tracking") || strings.Contains(p, "%!") {
		t.Errorf("every-product prompt: %s", p)
	}
}
