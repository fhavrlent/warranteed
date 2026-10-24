package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return store.New(database)
}

func user(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	u, err := st.UpsertUser(context.Background(), store.UpsertUser{Issuer: "test", Subject: name, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func spaces(t *testing.T, st *store.Store, uid int64) (personal, household int64) {
	t.Helper()
	list, err := st.ListSpaces(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range list {
		switch sp.Kind {
		case store.SpacePersonal:
			personal = sp.ID
		case store.SpaceHousehold:
			household = sp.ID
		}
	}
	return
}

func add(t *testing.T, st *store.Store, uid int64, name string, space *int64, tags ...string) int64 {
	t.Helper()
	m := 24
	id, err := st.CreateWarranty(context.Background(), uid, &store.WarrantyInput{
		ProductName: name, PurchaseDate: "2025-01-01", WarrantyMonths: &m, SpaceID: space, Tags: tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func names(t *testing.T, st *store.Store, uid int64) map[string]bool {
	t.Helper()
	list, err := st.ListWarranties(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, w := range list {
		out[w.ProductName] = true
	}
	return out
}

func TestSharingRules(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	me, partner := user(t, st, "me"), user(t, st, "partner")
	myPersonal, household := spaces(t, st, me)
	partnerPersonal, partnerHousehold := spaces(t, st, partner)
	if household == 0 || household != partnerHousehold || myPersonal == partnerPersonal {
		t.Fatalf("expected one shared household and separate personal spaces: %d %d %d %d",
			myPersonal, household, partnerPersonal, partnerHousehold)
	}

	fridge := add(t, st, me, "Fridge", nil, "Kitchen")          // default: household
	diary := add(t, st, me, "Diary", &myPersonal)               // private
	bike := add(t, st, partner, "Bike", &partnerPersonal)       // partner's private
	add(t, st, partner, "Kettle", &partnerHousehold, "Kitchen") // shared, same tag name

	if got := names(t, st, me); !got["Fridge"] || !got["Diary"] || !got["Kettle"] || got["Bike"] {
		t.Errorf("me sees %v", got)
	}
	if got := names(t, st, partner); !got["Fridge"] || got["Diary"] || !got["Kettle"] || !got["Bike"] {
		t.Errorf("partner sees %v", got)
	}

	// Shared tags: both household warranties use the same "Kitchen" tag.
	tags, _ := st.ListTags(ctx, partner)
	kitchen := 0
	for _, tg := range tags {
		if tg.Name == "Kitchen" {
			kitchen++
			if tg.Count != 2 {
				t.Errorf("Kitchen used %d times, want 2", tg.Count)
			}
		}
	}
	if kitchen != 1 {
		t.Errorf("want one shared Kitchen tag, got %d", kitchen)
	}

	twoYears := 24
	// Private stays private, for every kind of access.
	if _, err := st.GetWarranty(ctx, partner, diary); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("partner read my private warranty: %v", err)
	}
	if err := st.SetArchived(ctx, partner, diary, true); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("partner archived my private warranty: %v", err)
	}
	if _, err := st.DeleteWarranty(ctx, me, bike); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("I deleted partner's private warranty: %v", err)
	}
	if _, err := st.CreateWarranty(ctx, partner, &store.WarrantyInput{
		ProductName: "Sneaky", PurchaseDate: "2025-01-01", WarrantyMonths: &twoYears, SpaceID: &myPersonal,
	}); err == nil || err.Error() != "You can't add warranties there" {
		t.Errorf("partner adding to my personal space: %v", err)
	}

	// Shared: anyone edits and archives; only the creator deletes or makes it private.
	w, _ := st.GetWarranty(ctx, partner, fridge)
	if w.AddedBy.ID != me || w.SpaceKind != store.SpaceHousehold {
		t.Fatalf("fridge: added by %d in %s", w.AddedBy.ID, w.SpaceKind)
	}
	m := 36
	edit := &store.WarrantyInput{ProductName: "Fridge", PurchaseDate: "2025-01-01", WarrantyMonths: &m, Tags: []string{"Kitchen"}}
	if err := st.UpdateWarranty(ctx, partner, fridge, edit); err != nil {
		t.Errorf("partner can't edit shared warranty: %v", err)
	}
	if err := st.SetArchived(ctx, partner, fridge, true); err != nil {
		t.Errorf("partner can't archive shared warranty: %v", err)
	}
	if _, err := st.DeleteWarranty(ctx, partner, fridge); err == nil {
		t.Error("partner deleted a shared warranty they didn't add")
	}
	edit.SpaceID = &partnerPersonal
	if err := st.UpdateWarranty(ctx, partner, fridge, edit); err == nil {
		t.Error("partner moved my shared warranty into their personal space")
	}
	edit.SpaceID = &myPersonal
	if err := st.UpdateWarranty(ctx, me, fridge, edit); err != nil {
		t.Errorf("I can't make my own warranty private: %v", err)
	}
	if names(t, st, partner)["Fridge"] {
		t.Error("partner still sees the fridge after I made it private")
	}
	if _, err := st.DeleteWarranty(ctx, me, fridge); err != nil {
		t.Errorf("I can't delete my warranty: %v", err)
	}
}

func TestMoveAllToHousehold(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	me, partner := user(t, st, "me"), user(t, st, "partner")
	personal, household := spaces(t, st, me)
	add(t, st, me, "TV", &personal, "Electronics")
	add(t, st, me, "Phone", &personal, "Electronics")
	add(t, st, partner, "Laptop", nil, "Electronics")

	n, err := st.MoveAllToSpace(ctx, me, household)
	if err != nil || n != 2 {
		t.Fatalf("moved %d, %v", n, err)
	}
	if got := names(t, st, partner); !got["TV"] || !got["Phone"] {
		t.Errorf("partner sees %v after the move", got)
	}
	tags, _ := st.ListTags(ctx, me)
	if len(tags) != 1 || tags[0].Count != 3 || tags[0].SpaceID != household {
		t.Errorf("tags should merge into one household Electronics tag: %+v", tags)
	}
	if _, err := st.MoveAllToSpace(ctx, me, personal); err == nil {
		t.Error("moving into a personal space should be refused")
	}
}

func TestRemindersArePerPerson(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	me, partner := user(t, st, "me"), user(t, st, "partner")
	id := add(t, st, me, "Shared", nil)
	if err := st.MarkRemindersSent(ctx, me, id, []int{30}, "2027-01-01"); err != nil {
		t.Fatal(err)
	}
	if sent, _ := st.ReminderSent(ctx, partner, id, 30, "2027-01-01"); sent {
		t.Error("my reminder counted as the partner's")
	}
	if sent, _ := st.ReminderSent(ctx, me, id, 30, "2027-01-01"); !sent {
		t.Error("my reminder wasn't recorded")
	}
}
