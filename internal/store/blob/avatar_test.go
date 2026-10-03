package blob

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

// thirds is a w × h image whose left, middle and right thirds are red,
// green and blue.
func thirds(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		c := color.NRGBA{255, 0, 0, 255}
		switch {
		case x >= 2*w/3:
			c = color.NRGBA{0, 0, 255, 255}
		case x >= w/3:
			c = color.NRGBA{0, 255, 0, 255}
		}
		for y := range h {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// withPNGText is a PNG with a tEXt chunk holding text after its IHDR.
func withPNGText(t *testing.T, img image.Image, text string) []byte {
	t.Helper()
	b := encodePNG(t, img)
	body := append([]byte("tEXt"), append([]byte("Comment\x00"), text...)...)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)-4))
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	// The signature (8) and the IHDR (4 length, 4 type, 13 data, 4 CRC).
	at := 8 + 25
	return append(append(append([]byte{}, b[:at]...), chunk...), b[at:]...)
}

// withEXIF is a JPEG with an APP1 Exif segment holding text after its SOI.
func withEXIF(t *testing.T, img image.Image, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), text...)
	seg := append([]byte{0xFF, 0xE1}, binary.BigEndian.AppendUint16(nil, uint16(len(payload)+2))...)
	seg = append(seg, payload...)
	return append(append(append([]byte{}, b[:2]...), seg...), b[2:]...)
}

// TestAvatarIsASquareOfItsOwnType: a PNG and a JPEG, larger and smaller
// than the avatar, come out AvatarEdge square in the type they went in as,
// and what the file carried beyond its pixels is gone (TKT-01M3YSQ4CR).
func TestAvatarIsASquareOfItsOwnType(t *testing.T) {
	const secret = "GPS 60.1699N 24.9384E"
	cases := []struct {
		name, mime string
		data       []byte
		decode     func(io.Reader) (image.Image, error)
	}{
		{"a large PNG", "image/png", withPNGText(t, thirds(900, 600), secret), png.Decode},
		{"a small PNG", "image/png", withPNGText(t, thirds(30, 30), secret), png.Decode},
		{"a large JPEG", "image/jpeg", withEXIF(t, thirds(900, 600), secret), jpeg.Decode},
		{"a small JPEG", "image/jpeg", withEXIF(t, thirds(40, 60), secret), jpeg.Decode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !bytes.Contains(c.data, []byte(secret)) {
				t.Fatal("the input does not carry the metadata")
			}
			out, err := Avatar(bytes.NewReader(c.data), c.mime)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out, []byte(secret)) || bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("tEXt")) {
				t.Fatal("the avatar keeps the file's metadata")
			}
			img, err := c.decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("the avatar is not a %s: %v", c.mime, err)
			}
			if b := img.Bounds(); b.Dx() != AvatarEdge || b.Dy() != AvatarEdge {
				t.Fatalf("the avatar is %v", b)
			}
		})
	}
}

// TestAvatarTakesTheCentredSquare: a wide image's avatar is its middle,
// and a tall one's likewise.
func TestAvatarTakesTheCentredSquare(t *testing.T) {
	for _, src := range []image.Image{thirds(300, 100), rotate(thirds(300, 100))} {
		out, err := Avatar(bytes.NewReader(encodePNG(t, src)), "image/png")
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		// The middle third is green, across or down.
		want := color.NRGBA{0, 255, 0, 255}
		for _, p := range []image.Point{{0, 0}, {AvatarEdge - 1, 0}, {0, AvatarEdge - 1}, {AvatarEdge - 1, AvatarEdge - 1}, {128, 128}} {
			if got := color.NRGBAModel.Convert(img.At(p.X, p.Y)).(color.NRGBA); got != want {
				t.Errorf("%v of a %v image is %v, want %v", p, src.Bounds(), got, want)
			}
		}
	}
}

// rotate is img turned a quarter, so its width is its height.
func rotate(img *image.NRGBA) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for x := range b.Dx() {
		for y := range b.Dy() {
			out.Set(y, x, img.At(x, y))
		}
	}
	return out
}

// TestAvatarRefusesOtherTypes: a GIF, a PDF and text are not avatars, and
// nothing is decoded for them.
func TestAvatarRefusesOtherTypes(t *testing.T) {
	n := countDecodes(t, nil)
	var g bytes.Buffer
	if err := gif.Encode(&g, thirds(9, 9), nil); err != nil {
		t.Fatal(err)
	}
	for mime, data := range map[string][]byte{
		"image/gif":       g.Bytes(),
		"application/pdf": []byte("%PDF-1.4\n"),
		"text/plain":      []byte("hello"),
	} {
		if _, err := Avatar(bytes.NewReader(data), mime); !errors.Is(err, ErrNotImage) {
			t.Errorf("%s: %v", mime, err)
		}
	}
	if *n != 0 {
		t.Fatalf("%d decodes", *n)
	}
}

// TestAvatarRefusesBombBeforeDecode: a header over the thumbnail's bounds
// is refused from the header alone, as Thumbnail refuses it.
func TestAvatarRefusesBombBeforeDecode(t *testing.T) {
	n := countDecodes(t, nil)
	for mime, data := range map[string][]byte{
		"image/png":  pngHeader(MaxSide+1, 1, 8, 6, 0),
		"image/jpeg": jpegHeader(MaxSide, MaxSide, 3),
	} {
		if _, err := Avatar(bytes.NewReader(data), mime); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: %v", mime, err)
		}
	}
	if *n != 0 {
		t.Fatalf("%d decodes", *n)
	}
}
