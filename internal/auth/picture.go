package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const maxPictureSize = 2 << 20

// Raster formats only: an SVG could carry scripts.
var pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

var pictureClient = &http.Client{Timeout: 5 * time.Second}

// savePicture downloads the profile picture named by the provider's
// "picture" claim and stores it, so the app can serve it from its own origin.
// A failed download keeps the picture from the previous sign-in.
func (a *Auth) savePicture(ctx context.Context, userID int64, pictureURL string) {
	if pictureURL == "" {
		if err := a.store.SetUserPicture(ctx, userID, nil, ""); err != nil {
			slog.Warn("clear profile picture", "err", err)
		}
		return
	}
	data, contentType, err := fetchPicture(ctx, pictureURL)
	if err != nil {
		slog.Warn("download profile picture", "user", userID, "err", err)
		return
	}
	if err := a.store.SetUserPicture(ctx, userID, data, contentType); err != nil {
		slog.Warn("store profile picture", "err", err)
	}
}

func fetchPicture(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, "", errors.New("not an http(s) URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	res, err := pictureClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxPictureSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxPictureSize {
		return nil, "", errors.New("picture too large")
	}
	// The content is checked, not the header: providers often send a generic type.
	contentType := http.DetectContentType(data)
	if !pictureTypes[contentType] {
		return nil, "", fmt.Errorf("unsupported picture type %q", contentType)
	}
	return data, contentType, nil
}
