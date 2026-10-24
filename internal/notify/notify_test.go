package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/store"
)

func TestRemindersFireOncePerThreshold(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		messages = append(messages, string(b))
		mu.Unlock()
	}))
	defer hook.Close()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	st := store.New(database)
	ctx := context.Background()

	u, err := st.UpsertUser(ctx, store.UpsertUser{Issuer: "test", Subject: "1", Name: "T"})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := st.Settings(ctx, u.ID)
	settings.NotifyURLs = "generic+" + hook.URL + "/hook"
	settings.NotifyDays = "30, 7"
	settings.DateFormat = "dd.MM.yyyy"
	if err := st.SaveSettings(ctx, u.ID, settings); err != nil {
		t.Fatal(err)
	}

	today := time.Now()
	in5 := today.AddDate(0, 0, 5).Format(store.DateLayout)
	in20 := today.AddDate(0, 0, 20).Format(store.DateLayout)
	in90 := today.AddDate(0, 0, 90).Format(store.DateLayout)
	past := today.AddDate(-1, 0, 0).Format(store.DateLayout)
	for _, w := range []struct {
		name string
		end  string
	}{{"Soon", in5}, {"Later", in20}, {"Far", in90}} {
		end := w.end
		if _, err := st.CreateWarranty(ctx, u.ID, &store.WarrantyInput{ProductName: w.name, PurchaseDate: past, WarrantyEndDate: &end}); err != nil {
			t.Fatal(err)
		}
	}

	n := New(st, time.Local, 0, "", Policy{})
	for i := 0; i < 3; i++ {
		if err := n.CheckAll(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(messages) != 1 {
		t.Fatalf("expected exactly 1 digest after repeated checks, got %d: %q", len(messages), messages)
	}
	msg := messages[0]
	if !strings.Contains(msg, "Soon") || !strings.Contains(msg, "Later") || strings.Contains(msg, "Far") {
		t.Fatalf("unexpected digest: %s", msg)
	}
	// Dates in reminders follow the user's format.
	if want := today.AddDate(0, 0, 5).Format("02.01.2006"); !strings.Contains(msg, want) {
		t.Fatalf("digest should contain %s in the user's format: %s", want, msg)
	}
}

func TestParseDays(t *testing.T) {
	got, err := ParseDays("30, 7,1 7")
	if err != nil || len(got) != 3 || got[0] != 1 || got[2] != 30 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ParseDays("soon"); err == nil {
		t.Fatal("expected error")
	}
}
