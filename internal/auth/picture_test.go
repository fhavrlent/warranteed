package auth

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPicture(t *testing.T) {
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	bodies := map[string][]byte{
		"/png":   pngData.Bytes(),
		"/svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"/large": append(append([]byte{}, pngData.Bytes()...), make([]byte, maxPictureSize)...),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(body)
	}))
	defer srv.Close()

	data, contentType, err := fetchPicture(context.Background(), srv.URL+"/png")
	if err != nil || contentType != "image/png" || !bytes.Equal(data, pngData.Bytes()) {
		t.Fatalf("png: got %q, %v", contentType, err)
	}
	for _, url := range []string{srv.URL + "/svg", srv.URL + "/large", srv.URL + "/missing", "file:///etc/passwd", "javascript:alert(1)"} {
		if _, _, err := fetchPicture(context.Background(), url); err == nil {
			t.Errorf("%s: expected an error", url)
		}
	}
}
