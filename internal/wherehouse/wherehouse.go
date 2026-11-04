// Package wherehouse is a client for the Wherehouse inventory's API,
// authenticated with a person's API token from connecting.
package wherehouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// Source is the name links from Warranteed carry in Wherehouse.
const Source = "warranteed"

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimSuffix(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Error is a response Wherehouse refused.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string {
	body := e.Body
	var msg struct{ Error string }
	if json.Unmarshal([]byte(body), &msg) == nil && msg.Error != "" {
		body = msg.Error
	}
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("Wherehouse returned %d: %s", e.Status, strings.TrimSpace(body))
}

// Unauthorized reports whether Wherehouse no longer takes the token, e.g.
// because it was deleted there.
func (e *Error) Unauthorized() bool { return e.Status == http.StatusUnauthorized }

func (e *Error) NotFound() bool { return e.Status == http.StatusNotFound }

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) send(req *http.Request, out any) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach Wherehouse: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &Error{Status: res.StatusCode, Body: string(b)}
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out)
}

// Connected is what connecting gives: a token for one household.
type Connected struct {
	Token     string `json:"token"`
	Household struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"household"`
}

// ConnectURL is where to send someone to connect: they allow Warranteed
// there, and come back to redirect with a code and state.
func ConnectURL(publicURL, redirect, state string) string {
	q := url.Values{"app": {"Warranteed"}, "redirect_uri": {redirect}, "state": {state}}
	return strings.TrimSuffix(publicURL, "/") + "/connect?" + q.Encode()
}

// Exchange swaps the code from connecting for a token.
func (c *Client) Exchange(ctx context.Context, code, redirect string) (*Connected, error) {
	var out Connected
	err := c.do(ctx, http.MethodPost, "/connect/token", map[string]string{"code": code, "redirect_uri": redirect}, &out)
	return &out, err
}

// User is the token's owner.
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (c *Client) Me(ctx context.Context) (*User, error) {
	var out User
	return &out, c.do(ctx, http.MethodGet, "/api/me", nil, &out)
}

// Ref names a thing in another's path.
type Ref struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Thing is as much of a Wherehouse thing as Warranteed uses.
type Thing struct {
	ID         int64   `json:"id"`
	ParentID   *int64  `json:"parentId"`
	Name       string  `json:"name"`
	Label      string  `json:"label"`
	Path       []Ref   `json:"path"`
	Cover      *int64  `json:"cover"`
	ArchivedAt *string `json:"archivedAt"`
	SoldDate   *string `json:"soldDate"`
	Contains   int     `json:"contains"`
	Kind       struct {
		IsPlace bool `json:"isPlace"`
	} `json:"kind"`
	Photos []struct {
		ID int64 `json:"id"`
	} `json:"photos"`
}

// PathText is where the thing is, e.g. "House › Garage".
func (t *Thing) PathText() string {
	names := make([]string, len(t.Path))
	for i, r := range t.Path {
		names[i] = r.Name
	}
	return strings.Join(names, " › ")
}

// Places lists the things that are places, for putting a new thing in.
func (c *Client) Places(ctx context.Context) ([]Thing, error) {
	var out []Thing
	return out, c.do(ctx, http.MethodGet, "/api/things?places=1", nil, &out)
}

func (c *Client) Search(ctx context.Context, q string) ([]Thing, error) {
	var out []Thing
	return out, c.do(ctx, http.MethodGet, "/api/search?q="+url.QueryEscape(q), nil, &out)
}

func (c *Client) Get(ctx context.Context, id int64) (*Thing, error) {
	var out Thing
	return &out, c.do(ctx, http.MethodGet, fmt.Sprintf("/api/things/%d", id), nil, &out)
}

// NewThing is what a new thing starts with.
type NewThing struct {
	ParentID     *int64 `json:"parentId"`
	Name         string `json:"name"`
	Quantity     int    `json:"quantity"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
}

func (c *Client) Create(ctx context.Context, t NewThing) (*Thing, error) {
	var out Thing
	return &out, c.do(ctx, http.MethodPost, "/api/things", t, &out)
}

// Purchase is what Warranteed owns of a linked thing.
type Purchase struct {
	PurchaseDate     *string  `json:"purchaseDate"`
	PurchasePrice    *float64 `json:"purchasePrice"`
	PurchaseFrom     string   `json:"purchaseFrom"`
	Currency         string   `json:"currency"`
	WarrantyUntil    *string  `json:"warrantyUntil"`
	LifetimeWarranty bool     `json:"lifetimeWarranty"`
	WarrantyDetails  string   `json:"warrantyDetails"`
}

// Link is a thing's link to its warranty.
type Link struct {
	ExternalID string    `json:"externalId"`
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	Purchase   *Purchase `json:"purchase"`
}

// SetLink links a thing to its warranty here, and gives Warranteed its
// purchase and warranty details.
func (c *Client) SetLink(ctx context.Context, thing int64, l Link) (*Thing, error) {
	var out Thing
	return &out, c.do(ctx, http.MethodPut, fmt.Sprintf("/api/things/%d/apps/%s", thing, Source), l, &out)
}

func (c *Client) RemoveLink(ctx context.Context, thing int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/things/%d/apps/%s", thing, Source), nil, nil)
}

// Maintenance is an entry in a thing's maintenance log.
type Maintenance struct {
	ID      int64    `json:"id,omitempty"`
	Title   string   `json:"title"`
	DoneOn  *string  `json:"doneOn"`
	NextDue *string  `json:"nextDue"`
	Notes   string   `json:"notes"`
	Cost    *float64 `json:"cost"`
}

func (c *Client) AddMaintenance(ctx context.Context, thing int64, m Maintenance) (int64, error) {
	var out Maintenance
	err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/things/%d/maintenance", thing), m, &out)
	return out.ID, err
}

func (c *Client) UpdateMaintenance(ctx context.Context, id int64, m Maintenance) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/maintenance/%d", id), m, nil)
}

func (c *Client) DeleteMaintenance(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/maintenance/%d", id), nil, nil)
}

// UploadPhoto adds a photo to a thing.
func (c *Client) UploadPhoto(ctx context.Context, thing int64, name, mimeType string, photo io.Reader) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", multipart.FileContentDisposition("file", name))
	h.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, photo); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/things/%d/photos", c.BaseURL, thing), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.send(req, nil)
}

// Changes are what changed in Wherehouse since a time.
type Changes struct {
	Until   string  `json:"until"`
	Things  []Thing `json:"things"`
	Deleted []int64 `json:"deleted"`
}

func (c *Client) Changes(ctx context.Context, since string) (*Changes, error) {
	path := "/api/changes"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	var out Changes
	return &out, c.do(ctx, http.MethodGet, path, nil, &out)
}

// FromHomeBox returns the thing a HomeBox item was imported as.
func (c *Client) FromHomeBox(ctx context.Context, homeboxID string) (*Thing, error) {
	var out Thing
	return &out, c.do(ctx, http.MethodGet, "/api/homebox/"+url.PathEscape(homeboxID), nil, &out)
}
