package paperless

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A Paperless that hangs while looking up a field doesn't hold up lookups
// on another one.
func TestCustomFieldLookupsDontWaitOnOtherInstances(t *testing.T) {
	release := make(chan struct{})
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Write([]byte(`{"results":[]}`))
	}))
	defer stuck.Close()
	defer close(release)
	fine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"id":7,"name":"Warranty","data_type":"url"}]}`))
	}))
	defer fine.Close()

	go New(stuck.URL, "token").CustomFieldID(context.Background(), "Warranty", "url", true)
	time.Sleep(50 * time.Millisecond) // the stuck lookup now holds its lock

	done := make(chan int64)
	go func() {
		id, _ := New(fine.URL, "token").CustomFieldID(context.Background(), "Warranty", "url", true)
		done <- id
	}()
	select {
	case id := <-done:
		if id != 7 {
			t.Errorf("id = %d, want 7", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a lookup on one Paperless waited for another")
	}
}
