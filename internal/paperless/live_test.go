package paperless

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLive runs against a real Paperless-ngx instance:
//
//	PAPERLESS_TEST_URL=http://localhost:8000 PAPERLESS_TEST_TOKEN=... go test ./internal/paperless -run Live -v
//
// dev/compose.yml starts one.
func TestLive(t *testing.T) {
	base, token := os.Getenv("PAPERLESS_TEST_URL"), os.Getenv("PAPERLESS_TEST_TOKEN")
	if base == "" || token == "" {
		t.Skip("PAPERLESS_TEST_URL / PAPERLESS_TEST_TOKEN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := New(base, token)

	info, err := c.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Paperless %s, API v%d, %d documents", info.Version, info.APIVersion, info.Documents)

	// A unique body so Paperless never treats it as a duplicate.
	marker := fmt.Sprintf("warranteed-live-%d", time.Now().UnixNano())
	pdf := fmt.Sprintf("%%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n"+
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 400 200]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n"+
		"4 0 obj<</Length %d>>stream\nBT /F1 14 Tf 20 100 Td (%s) Tj ET\nendstream endobj\n"+
		"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%%%EOF\n",
		len(marker)+30, marker)

	taskID, err := c.Upload(ctx, Upload{Filename: marker + ".pdf", Title: marker, Created: "2024-05-01", Body: strings.NewReader(pdf)})
	if err != nil {
		t.Fatal(err)
	}
	var res *TaskResult
	for res == nil || res.State == TaskPending {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for consumption")
		case <-time.After(2 * time.Second):
		}
		if res, err = c.Task(ctx, taskID); err != nil {
			t.Fatal(err)
		}
	}
	if res.State != TaskSucceeded {
		t.Fatalf("consumption failed: %s", res.Error)
	}
	doc, err := c.Document(ctx, res.DocID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != marker || doc.Created != "2024-05-01" {
		t.Fatalf("unexpected document: %+v", doc)
	}
	for _, v := range []string{"thumb", "preview", "download"} {
		resp, err := c.Stream(ctx, doc.ID, v)
		if err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		resp.Body.Close()
	}
	t.Logf("uploaded and resolved document %d", doc.ID)
}
