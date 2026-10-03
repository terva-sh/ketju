package blob

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// Avatars (docs/05 "Display name and bio", decided by the maintainer
// 2026-10-02, TKT-01M3YSQ4CR): a PNG or JPEG of at most AvatarMaxBytes,
// re-encoded to an AvatarEdge square. Only the re-encoded image is stored
// and served, so nothing the member's file carried beyond its pixels, EXIF
// location included, survives.
const (
	// AvatarEdge is an avatar's side, in pixels.
	AvatarEdge = 256

	// AvatarMaxBytes is the largest file an avatar is made from.
	AvatarMaxBytes = 1 << 20
)

// AvatarType reports whether an avatar is made from a file of type mime,
// and the type it is stored as: the same one.
func AvatarType(mime string) bool { return mime == "image/png" || mime == "image/jpeg" }

// Avatar makes the avatar from the image f holds, whose type was sniffed
// as mime, and returns its bytes. It is Thumbnail's path with a square:
// the header is read and its bounds checked first (ErrTooLarge), the image
// is decoded (ErrDecode for a decoder that fails or panics), its centred
// square is cut, and that is scaled to AvatarEdge × AvatarEdge by area
// averaging, up or down. A PNG stays a PNG, so transparency is kept, and a
// JPEG stays a JPEG. Any other type is ErrNotImage.
func Avatar(f io.ReadSeeker, mime string) ([]byte, error) {
	if !AvatarType(mime) {
		return nil, ErrNotImage
	}
	ft := formats[mime]
	cfg, err := ft.config(f)
	if err != nil {
		return nil, fmt.Errorf("%w: reading the header: %v", ErrDecode, err)
	}
	halves, err := decodeFactor(f, mime, cfg)
	if err != nil {
		return nil, err
	}
	if err := checkBounds(cfg, bytesPerPixel(mime, cfg), halves); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, err := decode(f, mime)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() > cfg.Width || b.Dy() > cfg.Height {
		return nil, fmt.Errorf("%w: the image is %v, its header said %dx%d", ErrDecode, b, cfg.Width, cfg.Height)
	}
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	square := scale(img, image.Rect(x0, y0, x0+side, y0+side), AvatarEdge, AvatarEdge)
	var buf bytes.Buffer
	if mime == "image/jpeg" {
		err = jpeg.Encode(&buf, square, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&buf, square)
	}
	if err != nil {
		return nil, fmt.Errorf("blob: encoding an avatar: %w", err)
	}
	return buf.Bytes(), nil
}
