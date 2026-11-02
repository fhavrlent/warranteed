package homebox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The current item as HomeBox returns it, with related records as objects.
const storedItem = `{
	"id": "item-1", "assetId": "000-007", "name": "Dishwasher", "quantity": 1, "notes": "kept",
	"serialNumber": "", "purchaseDate": "", "purchasePrice": 0, "locationId": "loc-1",
	"parent": {"id": "loc-1", "name": "Kitchen"},
	"entityType": {"id": "type-item", "name": "Item"},
	"tags": [{"id": "tag-a"}, {"id": "tag-b"}],
	"fields": [{"name": "Colour", "type": "text", "textValue": "white"}],
	"attachments": []
}`

func TestUpdateKeepsOtherFields(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(storedItem))
		case http.MethodPut:
			json.NewDecoder(r.Body).Decode(&sent)
			w.Write([]byte(storedItem))
		}
	}))
	defer srv.Close()

	_, err := New(srv.URL, "key").Update(context.Background(), "item-1", func(it *Item) {
		it.SerialNumber = "SN-1"
		it.PurchaseDate = "2025-03-01"
		it.PurchasePrice = 17490
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"serialNumber": "SN-1", "purchaseDate": "2025-03-01", "purchasePrice": 17490.0,
		"notes": "kept", "assetId": "000-007", "locationId": "loc-1",
		"parentId": "loc-1", "entityTypeId": "type-item",
	}
	for k, v := range want {
		if sent[k] != v {
			t.Errorf("%s = %v, want %v", k, sent[k], v)
		}
	}
	if tags, _ := sent["tagIds"].([]any); len(tags) != 2 || tags[0] != "tag-a" {
		t.Errorf("tagIds = %v", sent["tagIds"])
	}
	if fields, _ := sent["fields"].([]any); len(fields) != 1 {
		t.Errorf("custom fields were dropped: %v", sent["fields"])
	}
}

func TestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/entities/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var he *Error
	_, err := New(srv.URL, "expired").Self(context.Background())
	if !errors.As(err, &he) || !he.Unauthorized() {
		t.Errorf("expected an unauthorized error, got %v", err)
	}
	_, err = New(srv.URL, "key").Get(context.Background(), "missing")
	if !errors.As(err, &he) || !he.NotFound() {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestSearchFindsLocationsToo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("isLocation") == "true" {
			w.Write([]byte(`{"items":[{"id":"loc-1","name":"Drawer organizer","entityType":{"isLocation":true}},
				{"id":"item-1","name":"Cable organizer"}]}`))
			return
		}
		w.Write([]byte(`{"items":[{"id":"item-1","name":"Cable organizer","entityType":{"isLocation":false}}]}`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, "key").Search(context.Background(), "organizer")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "item-1" || got[0].IsLocation() || got[1].ID != "loc-1" || !got[1].IsLocation() {
		t.Errorf("got %+v, want the item, then the location", got)
	}
}

func TestPlaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/entities/tree" || r.URL.Query().Get("withItems") != "true" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[{"id":"office","name":"Office","type":"location","children":[
			{"id":"drawer","name":"Drawer","type":"location","children":[]},
			{"id":"organizer","name":"Cable organizer","type":"item","children":[
				{"id":"cable","name":"USB cable","type":"item","children":[]}]},
			{"id":"lamp","name":"Lamp","type":"item","children":[]}]},
			{"id":"garage","name":"Garage","type":"location"}]`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, "key").Places(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range got {
		names = append(names, strings.Join(append(slices.Clip(p.Path), p.Name), " › "))
	}
	want := "Office, Office › Drawer, Office › Cable organizer, Garage"
	if strings.Join(names, ", ") != want {
		t.Errorf("places = %s, want %s", strings.Join(names, ", "), want)
	}
}

func TestWithFieldReadsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != LinkField+"=5-abc" {
			http.Error(w, "bad filter", http.StatusBadRequest)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		var items []map[string]string
		for i := (page - 1) * size; i < min(page*size, 230); i++ {
			items = append(items, map[string]string{"id": fmt.Sprint("item-", i)})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer srv.Close()
	ids, err := New(srv.URL, "key").WithField(context.Background(), LinkField, "5-abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 230 || ids[229] != "item-229" {
		t.Errorf("got %d items", len(ids))
	}
}
