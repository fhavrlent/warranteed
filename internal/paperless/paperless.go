// Package paperless is a small Paperless-ngx REST client.
//
// It works across API versions: it reads the server's X-Api-Version header,
// asks for the newest version both sides understand, and parses task results
// in both the legacy (<= v9, unpaginated, related_document) and the v10+
// (paginated, related_document_ids / result_data) shapes.
package paperless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxKnownVersion is the newest API version this client has been written against.
const maxKnownVersion = 10

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

var (
	versionMu    sync.Mutex
	versionCache = map[string]int{}
)

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Paperless rejected the API token"
	case http.StatusNotFound:
		return "not found in Paperless"
	}
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("Paperless returned HTTP %d: %s", e.Status, body)
}

func IsNotFound(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}

func (c *Client) apiVersion() int {
	versionMu.Lock()
	defer versionMu.Unlock()
	return versionCache[c.BaseURL]
}

func (c *Client) rememberVersion(h http.Header) {
	v, err := strconv.Atoi(h.Get("X-Api-Version"))
	if err != nil || v <= 0 {
		return
	}
	versionMu.Lock()
	versionCache[c.BaseURL] = min(v, maxKnownVersion)
	versionMu.Unlock()
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	return c.doSized(ctx, method, path, body, -1, contentType)
}

// doSized sends a request; size >= 0 sets an explicit Content-Length.
func (c *Client) doSized(ctx context.Context, method, path string, body io.Reader, size int64, contentType string) (*http.Response, error) {
	if c.BaseURL == "" || c.Token == "" {
		return nil, errors.New("Paperless is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	if v := c.apiVersion(); v > 0 {
		req.Header.Set("Accept", fmt.Sprintf("application/json; version=%d", v))
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach Paperless: %w", err)
	}
	c.rememberVersion(resp.Header)
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// ensureVersion makes sure we know the server's API version before sending
// version-sensitive requests.
func (c *Client) ensureVersion(ctx context.Context) {
	if c.apiVersion() > 0 {
		return
	}
	if resp, err := c.do(ctx, http.MethodGet, "/api/documents/?page_size=1&fields=id", nil, ""); err == nil {
		resp.Body.Close()
	}
}

type ServerInfo struct {
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
	Documents  int    `json:"documents"`
}

// Check verifies URL and token and reports the server version.
func (c *Client) Check(ctx context.Context) (*ServerInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/documents/?page_size=1&fields=id", nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var page struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, errors.New("the URL does not look like a Paperless-ngx server")
	}
	apiV, _ := strconv.Atoi(resp.Header.Get("X-Api-Version"))
	return &ServerInfo{Version: resp.Header.Get("X-Version"), APIVersion: apiV, Documents: page.Count}, nil
}

type Upload struct {
	Filename string
	Title    string
	Created  string // YYYY-MM-DD, optional
	Body     io.Reader
}

// Upload sends a document to the consumer and returns the consumption task ID.
//
// The multipart body is spooled to a temporary file first: Paperless runs on
// Django, which does not accept chunked request bodies, so the request must
// carry a Content-Length (a streamed upload arrives as "No file was submitted").
func (c *Client) Upload(ctx context.Context, u Upload) (string, error) {
	c.ensureVersion(ctx)
	tmp, err := os.CreateTemp("", "warranteed-upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	mw := multipart.NewWriter(tmp)
	if err := func() error {
		if u.Title != "" {
			if err := mw.WriteField("title", u.Title); err != nil {
				return err
			}
		}
		if u.Created != "" {
			if err := mw.WriteField("created", u.Created); err != nil {
				return err
			}
		}
		fw, err := mw.CreateFormFile("document", u.Filename)
		if err != nil {
			return err
		}
		if _, err := io.Copy(fw, u.Body); err != nil {
			return err
		}
		return mw.Close()
	}(); err != nil {
		return "", err
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	resp, err := c.doSized(ctx, http.MethodPost, "/api/documents/post_document/", tmp, size, mw.FormDataContentType())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	// The body is a bare JSON string; some proxies or versions return it
	// unquoted, and newer ones may wrap it in an object.
	var id string
	if json.Unmarshal(raw, &id) != nil {
		var obj struct {
			TaskID string `json:"task_id"`
		}
		if json.Unmarshal(raw, &obj) == nil && obj.TaskID != "" {
			id = obj.TaskID
		} else {
			id = strings.Trim(strings.TrimSpace(string(raw)), `"`)
		}
	}
	if id == "" {
		return "", errors.New("Paperless accepted the upload but returned no task ID")
	}
	return id, nil
}

type TaskState int

const (
	TaskPending TaskState = iota
	TaskSucceeded
	TaskFailed
)

type TaskResult struct {
	State TaskState
	DocID int64
	Error string
}

// flexID accepts a document ID encoded as a number or a string.
type flexID int64

func (f *flexID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	*f = flexID(n)
	return nil
}

type task struct {
	TaskID             string          `json:"task_id"`
	Status             string          `json:"status"`
	Result             json.RawMessage `json:"result"`      // <= v9: message string
	ResultData         json.RawMessage `json:"result_data"` // v10+: object
	RelatedDocument    flexID          `json:"related_document"`
	RelatedDocumentIDs []int64         `json:"related_document_ids"`
}

// Task looks up a consumption task by its UUID.
func (c *Client) Task(ctx context.Context, taskID string) (*TaskResult, error) {
	c.ensureVersion(ctx)
	resp, err := c.do(ctx, http.MethodGet, "/api/tasks/?task_id="+url.QueryEscape(taskID), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var tasks []task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		var page struct {
			Results []task `json:"results"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("unexpected tasks response from Paperless")
		}
		tasks = page.Results
	}

	var t *task
	for i := range tasks {
		if tasks[i].TaskID == taskID {
			t = &tasks[i]
			break
		}
	}
	if t == nil {
		// Task records are created asynchronously; not existing yet is normal.
		return &TaskResult{State: TaskPending}, nil
	}

	docID := int64(t.RelatedDocument)
	if docID == 0 && len(t.RelatedDocumentIDs) > 0 {
		docID = t.RelatedDocumentIDs[0]
	}
	var data struct {
		DocumentID  flexID `json:"document_id"`
		DuplicateOf flexID `json:"duplicate_of"`
		Error       string `json:"error"`
		Message     string `json:"message"`
	}
	if len(t.ResultData) > 0 {
		_ = json.Unmarshal(t.ResultData, &data)
		if docID == 0 {
			docID = int64(data.DocumentID)
		}
		if docID == 0 {
			docID = int64(data.DuplicateOf)
		}
	}
	message := firstNonEmpty(data.Error, data.Message, rawString(t.Result))

	switch strings.ToLower(t.Status) {
	case "success":
		if docID == 0 {
			// Older versions only put the ID in the message: "Success. New document id 12 created".
			docID = trailingID(message)
		}
		if docID == 0 {
			return &TaskResult{State: TaskFailed, Error: "Paperless finished but did not report a document ID"}, nil
		}
		return &TaskResult{State: TaskSucceeded, DocID: docID}, nil
	case "failure", "revoked":
		// A rejected duplicate still points at the existing document; use it.
		if docID != 0 {
			return &TaskResult{State: TaskSucceeded, DocID: docID}, nil
		}
		if message == "" {
			message = "Paperless could not consume the document"
		}
		return &TaskResult{State: TaskFailed, Error: message}, nil
	default:
		return &TaskResult{State: TaskPending}, nil
	}
}

func rawString(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}

func trailingID(msg string) int64 {
	fields := strings.FieldsFunc(msg, func(r rune) bool { return r < '0' || r > '9' })
	for _, f := range fields {
		if n, err := strconv.ParseInt(f, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

type Document struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	Created          string `json:"created"`
	OriginalFileName string `json:"originalFileName"`
	MimeType         string `json:"mimeType"`
}

type apiDocument struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	Created          string `json:"created"`
	CreatedDate      string `json:"created_date"`
	OriginalFileName string `json:"original_file_name"`
	MimeType         string `json:"mime_type"`
}

func (d apiDocument) toDocument() Document {
	created := d.Created
	if len(created) >= 10 {
		created = created[:10]
	}
	if created == "" && len(d.CreatedDate) >= 10 {
		created = d.CreatedDate[:10]
	}
	return Document{ID: d.ID, Title: d.Title, Created: created, OriginalFileName: d.OriginalFileName, MimeType: d.MimeType}
}

// Search runs a full-text search, or lists the newest documents when query is empty.
func (c *Client) Search(ctx context.Context, query string) ([]Document, error) {
	c.ensureVersion(ctx)
	q := url.Values{}
	q.Set("page_size", "24")
	q.Set("fields", "id,title,created,created_date,original_file_name,mime_type")
	if query = strings.TrimSpace(query); query != "" {
		if n, err := strconv.ParseInt(query, 10, 64); err == nil {
			// A bare number is probably a document ID.
			d, err := c.Document(ctx, n)
			if err == nil {
				return []Document{*d}, nil
			}
		}
		q.Set("query", query)
	} else {
		q.Set("ordering", "-added")
	}
	var page struct {
		Results []apiDocument `json:"results"`
	}
	if err := c.getJSON(ctx, "/api/documents/?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(page.Results))
	for _, d := range page.Results {
		out = append(out, d.toDocument())
	}
	return out, nil
}

func (c *Client) Document(ctx context.Context, id int64) (*Document, error) {
	var d apiDocument
	if err := c.getJSON(ctx, fmt.Sprintf("/api/documents/%d/", id), &d); err != nil {
		return nil, err
	}
	doc := d.toDocument()
	return &doc, nil
}

// Stream returns the raw response for a document's file. variant is
// "download", "preview" or "thumb". The caller must close the body.
func (c *Client) Stream(ctx context.Context, id int64, variant string) (*http.Response, error) {
	switch variant {
	case "download", "preview", "thumb":
	default:
		return nil, fmt.Errorf("unknown variant %q", variant)
	}
	return c.do(ctx, http.MethodGet, fmt.Sprintf("/api/documents/%d/%s/", id, variant), nil, "")
}
