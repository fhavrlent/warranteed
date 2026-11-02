// Package homebox is a small client for the HomeBox inventory API (v0.27+,
// where items are "entities"), authenticated with a user's API key.
package homebox

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

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Error is a response HomeBox refused.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string {
	body := e.Body
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("HomeBox returned %d: %s", e.Status, strings.TrimSpace(body))
}

// Unauthorized reports whether HomeBox rejected the API key, e.g. because it expired.
func (e *Error) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

func (e *Error) NotFound() bool { return e.Status == http.StatusNotFound }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.BaseURL + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

// send makes the request with the API key and decodes the JSON answer into out.
func (c *Client) send(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach HomeBox: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &Error{Status: res.StatusCode, Body: string(b)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out)
}

// User is the owner of the API key.
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Self checks the API key and returns whose it is.
func (c *Client) Self(ctx context.Context) (*User, error) {
	var out struct {
		Item User `json:"item"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/self", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out.Item, nil
}

// Summary is an item or location as listed by HomeBox.
type Summary struct {
	ID      string `json:"id"`
	AssetID string `json:"assetId"`
	Name    string `json:"name"`
	Parent  *struct {
		Name string `json:"name"`
	} `json:"parent,omitempty"`
}

func (c *Client) list(ctx context.Context, q url.Values) ([]Summary, error) {
	var out struct {
		Items []Summary `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/entities", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []Summary{}
	}
	return out.Items, nil
}

// Search finds items by name, serial or model number, manufacturer and more;
// "#000-123" looks one up by asset ID.
func (c *Client) Search(ctx context.Context, query string) ([]Summary, error) {
	return c.list(ctx, url.Values{"q": {query}, "page": {"1"}, "pageSize": {"20"}})
}

// Locations lists where items can be put.
func (c *Client) Locations(ctx context.Context) ([]Summary, error) {
	return c.list(ctx, url.Values{"isLocation": {"true"}, "page": {"1"}, "pageSize": {"500"}, "orderBy": {"name"}})
}

// Item is the subset of a HomeBox item that Warranteed reads or fills in.
type Item struct {
	ID               string       `json:"id"`
	AssetID          string       `json:"assetId"`
	Name             string       `json:"name"`
	SerialNumber     string       `json:"serialNumber"`
	ModelNumber      string       `json:"modelNumber"`
	Manufacturer     string       `json:"manufacturer"`
	PurchaseDate     string       `json:"purchaseDate"`
	PurchasePrice    float64      `json:"purchasePrice"`
	PurchaseFrom     string       `json:"purchaseFrom"`
	WarrantyExpires  string       `json:"warrantyExpires"`
	WarrantyDetails  string       `json:"warrantyDetails"`
	LifetimeWarranty bool         `json:"lifetimeWarranty"`
	Attachments      []Attachment `json:"attachments"`
}

// Attachment is a file or photo on an item.
type Attachment struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	Primary   bool   `json:"primary"`
}

// HasPrimaryPhoto reports whether the item has a main photo.
func (it *Item) HasPrimaryPhoto() bool {
	for _, a := range it.Attachments {
		if a.Type == "photo" && a.Primary {
			return true
		}
	}
	return false
}

// Create adds an item, optionally in a location.
func (c *Client) Create(ctx context.Context, name, locationID string) (*Item, error) {
	body := map[string]any{"name": name, "quantity": 1}
	if locationID != "" {
		body["parentId"] = locationID
	}
	var out Item
	if err := c.do(ctx, http.MethodPost, "/entities", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Get(ctx context.Context, id string) (*Item, error) {
	var out Item
	if err := c.do(ctx, http.MethodGet, "/entities/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// updateFields are the fields of an item that HomeBox's update replaces; any
// left out would be cleared, so Update copies them from the current item.
var updateFields = []string{
	"id", "name", "description", "quantity", "insured", "archived", "assetId", "locationId",
	"serialNumber", "modelNumber", "manufacturer", "notes", "fields", "syncChildEntityLocations",
	"lifetimeWarranty", "warrantyExpires", "warrantyDetails",
	"purchaseDate", "purchaseFrom", "purchasePrice", "soldDate", "soldTo", "soldPrice", "soldNotes",
}

// Update changes an item's fields, keeping everything else as it is.
func (c *Client) Update(ctx context.Context, id string, change func(*Item)) (*Item, error) {
	var raw map[string]json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/entities/"+url.PathEscape(id), nil, nil, &raw); err != nil {
		return nil, err
	}
	body := map[string]any{}
	for _, k := range updateFields {
		if v, ok := raw[k]; ok {
			body[k] = v
		}
	}
	// HomeBox lists related records as objects but takes their IDs on update.
	var refs struct {
		Parent     *struct{ ID string }  `json:"parent"`
		EntityType *struct{ ID string }  `json:"entityType"`
		Tags       []struct{ ID string } `json:"tags"`
	}
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &refs); err != nil {
		return nil, err
	}
	if refs.Parent != nil {
		body["parentId"] = refs.Parent.ID
	}
	if refs.EntityType != nil {
		body["entityTypeId"] = refs.EntityType.ID
	}
	tagIDs := []string{}
	for _, t := range refs.Tags {
		tagIDs = append(tagIDs, t.ID)
	}
	body["tagIds"] = tagIDs

	var item Item
	if err := json.Unmarshal(b, &item); err != nil {
		return nil, err
	}
	change(&item)
	body["serialNumber"] = item.SerialNumber
	body["modelNumber"] = item.ModelNumber
	body["manufacturer"] = item.Manufacturer
	body["purchaseDate"] = item.PurchaseDate
	body["purchasePrice"] = item.PurchasePrice
	body["purchaseFrom"] = item.PurchaseFrom
	body["warrantyExpires"] = item.WarrantyExpires
	body["warrantyDetails"] = item.WarrantyDetails
	body["lifetimeWarranty"] = item.LifetimeWarranty

	var out Item
	if err := c.do(ctx, http.MethodPut, "/entities/"+url.PathEscape(id), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddLink attaches a link to an item. kind is a HomeBox attachment type:
// "receipt", "warranty", "manual" or "attachment".
func (c *Client) AddLink(ctx context.Context, id, link, title, kind string) error {
	body := map[string]string{"source_type": "link", "external_id": link, "title": title, "attachment_type": kind}
	return c.do(ctx, http.MethodPost, "/entities/"+url.PathEscape(id)+"/attachments/external", nil, body, nil)
}

// UploadPhoto adds a photo to an item and returns its attachment ID; primary
// makes it the item's main photo.
func (c *Client) UploadPhoto(ctx context.Context, id, name, mimeType string, photo io.Reader, primary bool) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", multipart.FileContentDisposition("file", name))
	h.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, photo); err != nil {
		return "", err
	}
	for k, v := range map[string]string{"name": name, "type": "photo", "primary": fmt.Sprint(primary)} {
		if err := mw.WriteField(k, v); err != nil {
			return "", err
		}
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/api/v1/entities/"+url.PathEscape(id)+"/attachments", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	// HomeBox answers with the item; the photo is its newest attachment.
	var item Item
	if err := c.send(req, &item); err != nil {
		return "", err
	}
	newest, at := "", time.Time{}
	for _, a := range item.Attachments {
		if t, err := time.Parse(time.RFC3339Nano, a.CreatedAt); newest == "" || (err == nil && t.After(at)) {
			newest, at = a.ID, t
		}
	}
	if newest == "" {
		return "", fmt.Errorf("HomeBox did not list the uploaded photo")
	}
	return newest, nil
}

// OpenAttachment fetches an item's file or photo; the caller closes the body.
func (c *Client) OpenAttachment(ctx context.Context, itemID, attachmentID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/entities/"+url.PathEscape(itemID)+"/attachments/"+url.PathEscape(attachmentID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach HomeBox: %w", err)
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, &Error{Status: res.StatusCode, Body: string(b)}
	}
	return res, nil
}
