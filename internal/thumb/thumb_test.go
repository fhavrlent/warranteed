package thumb

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// photo is a w×h JPEG whose left half is red and right half blue, with an
// EXIF orientation tag unless orientation is 0.
func photo(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if orientation == 0 {
		return buf.Bytes()
	}
	// A big-endian TIFF with one IFD entry: orientation.
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint16(tiff[18:], uint16(orientation))
	payload := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(payload)+2))
	out := append([]byte{0xff, 0xd8}, app1...)
	out = append(out, payload...)
	return append(out, buf.Bytes()[2:]...)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0xc000 && g < 0x4000 && b < 0x4000
}

func TestMakeShrinks(t *testing.T) {
	var out bytes.Buffer
	if err := Make(&out, bytes.NewReader(photo(t, 2400, 1800, 0))); err != nil {
		t.Fatal(err)
	}
	img := decode(t, out.Bytes())
	if b := img.Bounds(); b.Dx() != Size || b.Dy() != 450 {
		t.Errorf("size = %v, want %dx450", b.Size(), Size)
	}
	if !isRed(img.At(10, 200)) || isRed(img.At(590, 200)) {
		t.Error("the picture changed")
	}
}

func TestMakeTurnsUpright(t *testing.T) {
	// Orientation 6: the camera was turned, so the picture is shown rotated
	// clockwise, and the red left half ends up on top.
	var out bytes.Buffer
	if err := Make(&out, bytes.NewReader(photo(t, 1200, 800, 6))); err != nil {
		t.Fatal(err)
	}
	img := decode(t, out.Bytes())
	if b := img.Bounds(); b.Dx() != 400 || b.Dy() != Size {
		t.Fatalf("size = %v, want 400x%d", b.Size(), Size)
	}
	if !isRed(img.At(200, 10)) || isRed(img.At(200, 590)) {
		t.Error("not turned upright")
	}
}

func TestMakeSmallAndTransparent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30)) // fully transparent
	var in, out bytes.Buffer
	png.Encode(&in, img)
	if err := Make(&out, &in); err != nil {
		t.Fatal(err)
	}
	got := decode(t, out.Bytes())
	if b := got.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Errorf("a small image was resized to %v", b.Size())
	}
	if r, g, b, _ := got.At(5, 5).RGBA(); r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Error("transparency isn't white")
	}
}

func TestMakeUnsupported(t *testing.T) {
	if err := Make(&bytes.Buffer{}, strings.NewReader("not an image")); err != ErrUnsupported {
		t.Errorf("err = %v", err)
	}
}

func TestExifOrientation(t *testing.T) {
	for _, o := range []int{1, 3, 6, 8} {
		if got := exifOrientation(photo(t, 8, 8, o)); got != o {
			t.Errorf("orientation %d read as %d", o, got)
		}
	}
	if got := exifOrientation(photo(t, 8, 8, 0)); got != 1 {
		t.Errorf("no EXIF read as %d", got)
	}
	if got := exifOrientation([]byte{0xff, 0xd8, 0xff, 0xe1, 0xff}); got != 1 {
		t.Errorf("cut-off data read as %d", got)
	}
}
