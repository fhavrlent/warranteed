// Package thumb makes small JPEG previews of photos, so lists don't load
// phone photos of several megabytes each.
package thumb

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
)

// Size is the longest side of a preview in pixels: sharp on a high-density
// screen at the sizes the app shows photos.
const Size = 600

// maxPixels guards against images that would take too much memory to decode.
const maxPixels = 60_000_000

// ErrUnsupported means the image can't be previewed; serve the original.
var ErrUnsupported = errors.New("thumb: unsupported image")

// Supported reports whether Make can preview images of this type.
func Supported(mimeType string) bool {
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif":
		return true
	}
	return false
}

// Make writes a JPEG preview of the image in r to w, turned upright as its
// EXIF orientation says. An image already within Size is only re-encoded.
func Make(w io.Writer, r io.Reader) error {
	// Phones put large metadata before a JPEG's size, so look well ahead.
	br := bufio.NewReaderSize(r, 512<<10)
	head, _ := br.Peek(512 << 10)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ErrUnsupported
	}
	if cfg.Width*cfg.Height > maxPixels {
		return ErrUnsupported
	}
	orientation := exifOrientation(head)
	src, _, err := image.Decode(br)
	if err != nil {
		return ErrUnsupported
	}
	dst := shrink(src, Size)
	return jpeg.Encode(w, orient(dst, orientation), &jpeg.Options{Quality: 80})
}

// shrink scales img down so its longest side is at most size, averaging the
// pixels each output pixel covers, onto white so transparency stays light.
func shrink(img image.Image, size int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if w > size || h > size {
		if w >= h {
			dw, dh = size, max(1, h*size/w)
		} else {
			dw, dh = max(1, w*size/h), size
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if dw == w && dh == h {
		draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
		return dst
	}
	// Sums per output pixel, filled one source row at a time.
	sums := make([]uint64, dw*dh*4)
	counts := make([]uint32, dw*dh)
	ycc, isYCC := img.(*image.YCbCr)
	for y := range h {
		oy := y * dh / h
		row := sums[oy*dw*4 : (oy+1)*dw*4]
		crow := counts[oy*dw : (oy+1)*dw]
		for x := range w {
			ox := x * dw / w
			var r, g, bl, a uint32
			if isYCC {
				c := ycc.YCbCrAt(b.Min.X+x, b.Min.Y+y)
				r8, g8, b8 := color.YCbCrToRGB(c.Y, c.Cb, c.Cr)
				r, g, bl, a = uint32(r8)*257, uint32(g8)*257, uint32(b8)*257, 0xffff
			} else {
				r, g, bl, a = img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			}
			// Premultiplied, so blending onto white adds what's missing.
			row[ox*4] += uint64(r + 0xffff - a)
			row[ox*4+1] += uint64(g + 0xffff - a)
			row[ox*4+2] += uint64(bl + 0xffff - a)
			crow[ox]++
		}
	}
	for i, n := range counts {
		if n == 0 {
			continue
		}
		for c := range 3 {
			dst.Pix[i*4+c] = uint8(sums[i*4+c] / uint64(n) >> 8)
		}
		dst.Pix[i*4+3] = 0xff
	}
	return dst
}

// orient turns img upright for EXIF orientation o (1 to 8).
func orient(img *image.RGBA, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var nx, ny int
			switch o {
			case 2:
				nx, ny = w-1-x, y
			case 3:
				nx, ny = w-1-x, h-1-y
			case 4:
				nx, ny = x, h-1-y
			case 5:
				nx, ny = y, x
			case 6:
				nx, ny = h-1-y, x
			case 7:
				nx, ny = h-1-y, w-1-x
			case 8:
				nx, ny = y, w-1-x
			}
			si, di := img.PixOffset(x, y), out.PixOffset(nx, ny)
			copy(out.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return out
}

// exifOrientation reads the orientation tag from a JPEG's EXIF data, or
// returns 1 (upright) when there is none.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return 1
		}
		marker := data[i+1]
		if marker == 0xda || marker == 0xd9 { // image data starts: no EXIF
			return 1
		}
		n := int(binary.BigEndian.Uint16(data[i+2:]))
		end := i + 2 + n
		if n < 2 {
			return 1
		}
		if marker == 0xe1 && n >= 8 && i+10 <= len(data) && string(data[i+4:i+10]) == "Exif\x00\x00" {
			// The orientation is near the start, even if the rest was cut off.
			return tiffOrientation(data[i+10 : min(end, len(data))])
		}
		i = end
	}
	return 1
}

// tiffOrientation finds tag 0x0112 in the first IFD of EXIF's TIFF data.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[ifd:]))
	for k := range count {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
