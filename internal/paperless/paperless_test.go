package paperless

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer mimics a Paperless instance advertising apiVersion and
// returning tasksBody for /api/tasks/.
func fakeServer(t *testing.T, apiVersion int, tasksBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Api-Version", fmt.Sprint(apiVersion))
		w.Header().Set("X-Version", "test")
		switch {
		case r.URL.Path == "/api/documents/":
			io.WriteString(w, `{"count":3,"results":[{"id":1,"title":"Receipt","created":"2024-05-01","original_file_name":"r.pdf","mime_type":"application/pdf"}]}`)
		case r.URL.Path == "/api/tasks/":
			io.WriteString(w, tasksBody)
		case r.URL.Path == "/api/documents/post_document/":
			// Like Django, reject chunked bodies without a Content-Length.
			if r.ContentLength <= 0 {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"document":["No file was submitted."]}`)
				return
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil || r.MultipartForm.File["document"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			io.WriteString(w, `"abc-123"`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestTaskShapes(t *testing.T) {
	cases := []struct {
		name    string
		version int
		body    string
		state   TaskState
		docID   int64
	}{
		{"v9 success", 9, `[{"task_id":"abc-123","status":"SUCCESS","result":"Success. New document id 42 created","related_document":"42"}]`, TaskSucceeded, 42},
		{"v9 success id only in message", 9, `[{"task_id":"abc-123","status":"SUCCESS","result":"Success. New document id 43 created","related_document":null}]`, TaskSucceeded, 43},
		{"v9 pending", 9, `[{"task_id":"abc-123","status":"STARTED"}]`, TaskPending, 0},
		{"v9 not created yet", 9, `[]`, TaskPending, 0},
		{"v9 failure", 9, `[{"task_id":"abc-123","status":"FAILURE","result":"Not consuming r.pdf: It is a duplicate."}]`, TaskFailed, 0},
		{"v10 paginated success", 10, `{"count":1,"results":[{"task_id":"abc-123","status":"success","result_data":{"document_id":7},"related_document_ids":[7]}]}`, TaskSucceeded, 7},
		{"v10 result_data only", 10, `{"results":[{"task_id":"abc-123","status":"success","result_data":{"document_id":8}}]}`, TaskSucceeded, 8},
		{"v10 duplicate rejected links existing", 10, `{"results":[{"task_id":"abc-123","status":"failure","result_data":{"duplicate_of":9},"related_document_ids":[9]}]}`, TaskSucceeded, 9},
		{"v10 failure", 10, `{"results":[{"task_id":"abc-123","status":"failure","result_data":{"error":"OCR failed"}}]}`, TaskFailed, 0},
		{"v10 pending", 10, `{"results":[{"task_id":"abc-123","status":"pending"}]}`, TaskPending, 0},
		{"future version capped", 14, `{"results":[{"task_id":"abc-123","status":"success","related_document_ids":[5]}]}`, TaskSucceeded, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeServer(t, tc.version, tc.body)
			defer srv.Close()
			c := New(srv.URL, "secret")
			res, err := c.Task(context.Background(), "abc-123")
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tc.state || res.DocID != tc.docID {
				t.Fatalf("got state=%v doc=%d err=%q, want state=%v doc=%d", res.State, res.DocID, res.Error, tc.state, tc.docID)
			}
			if tc.state == TaskFailed && res.Error == "" {
				t.Fatal("failed task without an error message")
			}
		})
	}
}

func TestUploadAndCheck(t *testing.T) {
	srv := fakeServer(t, 10, `[]`)
	defer srv.Close()
	c := New(srv.URL, "secret")
	id, err := c.Upload(context.Background(), Upload{Filename: "r.pdf", Title: "Receipt", Created: "2024-05-01", Body: strings.NewReader("%PDF-1.4")})
	if err != nil || id != "abc-123" {
		t.Fatalf("upload: id=%q err=%v", id, err)
	}
	info, err := c.Check(context.Background())
	if err != nil || info.APIVersion != 10 || info.Documents != 3 {
		t.Fatalf("check: %+v %v", info, err)
	}
	docs, err := c.Search(context.Background(), "")
	if err != nil || len(docs) != 1 || docs[0].Created != "2024-05-01" {
		t.Fatalf("search: %+v %v", docs, err)
	}
}

func TestBadToken(t *testing.T) {
	srv := fakeServer(t, 10, `[]`)
	defer srv.Close()
	_, err := New(srv.URL, "wrong").Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("want token error, got %v", err)
	}
}
