package blob

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
)

// Thumbnails (docs/03 "Attachments", decided by the run 2026-10-01,
// TKT-01M29CYS9W). Decoding an image a member uploaded is the riskiest thing
// the server does with untrusted bytes: a file of a few hundred bytes can
// declare a picture that decodes to gigabytes. So the bounds below are
// checked from the format's own DecodeConfig, which reads only the header,
// before any decoder is called; one decode runs at a time (the service's
// semaphore), so the bounds hold for the whole process.
//
// Only the standard library's image/png, image/jpeg and image/gif decode
// these bytes, each called directly by the type the upload sniffed and never
// through image.Decode's format registry, so a decoder some other import
// registers is never reached (TestThumbImportsOnlyStdlibImage).
const (
	// ThumbSuffix names a blob's thumbnail beside it: <hash>.thumb.
	ThumbSuffix = ".thumb"

	// ThumbEdge is the box a thumbnail is fitted inside, in pixels.
	ThumbEdge = 160

	// MaxSide is the longest side, in pixels, an image may declare and be
	// decoded.
	MaxSide = 16384

	// MaxDecodeBytes bounds what one decode may allocate, estimated from
	// the header: width × height × the decoder's bytes per pixel.
	MaxDecodeBytes = 256 << 20
)

var (
	// ErrTooLarge means the image's header declares more than the bounds
	// allow; nothing was decoded.
	ErrTooLarge = errors.New("blob: the image is too large to thumbnail")

	// ErrDecode means the decoder refused the bytes, or panicked on them.
	ErrDecode = errors.New("blob: the image could not be decoded")

	// ErrNotImage means the type is not one a thumbnail is made for.
	ErrNotImage = errors.New("blob: not an image this server thumbnails")
)

// ThumbPath is where the thumbnail of the blob for hash is kept under root:
// beside the blob, as <hash>.thumb. A hash that is not valid is refused, as
// Path refuses it.
func ThumbPath(root, hash string) (string, error) {
	p, err := Path(root, hash)
	if err != nil {
		return "", err
	}
	return p + ThumbSuffix, nil
}

// ThumbType is the type of the thumbnail made for a blob of type mime, and
// false for a type no thumbnail is made for: JPEG for a JPEG, PNG for a PNG
// or a GIF.
func ThumbType(mime string) (string, bool) {
	switch mime {
	case "image/jpeg":
		return "image/jpeg", true
	case "image/png", "image/gif":
		return "image/png", true
	}
	return "", false
}

// format is one image type's two entry points.
type format struct {
	config func(io.Reader) (image.Config, error)
	decode func(io.Reader) (image.Image, error)
}

var formats = map[string]format{
	"image/png":  {png.DecodeConfig, png.Decode},
	"image/jpeg": {jpeg.DecodeConfig, jpeg.Decode},
	"image/gif":  {gif.DecodeConfig, gif.Decode},
}

// decodeHook is the call into a decoder. Tests replace it to count calls
// and to make one panic.
var decodeHook = func(mime string, r io.Reader) (image.Image, error) {
	return formats[mime].decode(r)
}

// Thumbnail makes the thumbnail of the image f holds, whose type the upload
// sniffed as mime, and returns its bytes and type. The header is read first
// and its bounds checked (ErrTooLarge), then the image is decoded, fitted
// inside ThumbEdge × ThumbEdge by area averaging, and encoded. A decoder
// that fails or panics is ErrDecode.
func Thumbnail(f io.ReadSeeker, mime string) (data []byte, contentType string, err error) {
	ctype, ok := ThumbType(mime)
	if !ok {
		return nil, "", ErrNotImage
	}
	ft := formats[mime]
	cfg, err := ft.config(f)
	if err != nil {
		return nil, "", fmt.Errorf("%w: reading the header: %v", ErrDecode, err)
	}
	halves, err := decodeFactor(f, mime, cfg)
	if err != nil {
		return nil, "", err
	}
	if err := checkBounds(cfg, bytesPerPixel(mime, cfg), halves); err != nil {
		return nil, "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, "", err
	}
	img, err := decode(f, mime)
	if err != nil {
		return nil, "", err
	}
	// The decoder agreed with its header, or the bounds checked nothing.
	if b := img.Bounds(); b.Dx() > cfg.Width || b.Dy() > cfg.Height {
		return nil, "", fmt.Errorf("%w: the image is %v, its header said %dx%d", ErrDecode, b, cfg.Width, cfg.Height)
	}
	small := fit(img)
	var buf bytes.Buffer
	if ctype == "image/jpeg" {
		err = jpeg.Encode(&buf, small, &jpeg.Options{Quality: 82})
	} else {
		err = png.Encode(&buf, small)
	}
	if err != nil {
		return nil, "", fmt.Errorf("blob: encoding a thumbnail: %w", err)
	}
	return buf.Bytes(), ctype, nil
}

// decode calls the decoder for mime, and turns an error or a panic into
// ErrDecode, so a decoder bug on hostile bytes costs one thumbnail and
// never the process.
func decode(r io.Reader, mime string) (img image.Image, err error) {
	defer func() {
		if p := recover(); p != nil {
			img, err = nil, fmt.Errorf("%w: the decoder panicked: %v", ErrDecode, p)
		}
	}()
	img, err = decodeHook(mime, r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	if img == nil {
		return nil, fmt.Errorf("%w: no image", ErrDecode)
	}
	return img, nil
}

// bytesPerPixel is a deliberate overestimate of what a decoder allocates
// per pixel at its peak, by type and by what the header says (docs/03,
// TKT-01M29CYS9W "Settled in grooming"):
//   - a PNG decodes into at most 8 bytes a pixel for 16 bits a channel, and
//     4 otherwise;
//   - a GIF's first frame is paletted, one byte a pixel, and bounded by the
//     logical screen; 4 leaves room;
//   - a progressive JPEG keeps an int32 coefficient for every sample of
//     every component as well as the image, so 16.
func bytesPerPixel(mime string, cfg image.Config) int64 {
	switch mime {
	case "image/png":
		switch cfg.ColorModel {
		case color.RGBA64Model, color.NRGBA64Model, color.Gray16Model:
			return 8
		}
		return 4
	case "image/jpeg":
		return 16
	}
	return 4
}

// decodeFactor is how many times bytesPerPixel the decoder may hold at
// once, from header facts DecodeConfig does not report. Both cases were
// found at implementation and make the estimate stricter, never looser,
// so that it stays the overestimate the grooming decision calls it:
//   - an interlaced PNG is decoded pass by pass into images of its own,
//     merged into a full-size one, so it may hold twice the image;
//   - a four-component (CMYK) JPEG keeps four coefficient planes and four
//     image planes, past the 16 bytes that cover three, so it counts 1.5×.
//
// The factor is returned in halves: 2 is 1×.
func decodeFactor(f io.ReadSeeker, mime string, cfg image.Config) (int64, error) {
	switch mime {
	case "image/png":
		// The IHDR is the first chunk (the decoder refuses anything else):
		// 8 bytes of signature, 8 of chunk header, then width, height,
		// depth, colour type, compression, filter, and interlace at 28.
		var b [1]byte
		if _, err := f.Seek(28, io.SeekStart); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(f, b[:]); err != nil {
			return 0, fmt.Errorf("%w: reading the header: %v", ErrDecode, err)
		}
		if b[0] != 0 {
			return 4, nil
		}
	case "image/jpeg":
		if cfg.ColorModel == color.CMYKModel {
			return 3, nil
		}
	}
	return 2, nil
}

// checkBounds refuses a header whose image is too large to decode: a side
// over MaxSide, or an estimated decode, bpp bytes a pixel times halves/2,
// over MaxDecodeBytes.
func checkBounds(cfg image.Config, bpp, halves int64) error {
	w, h := int64(cfg.Width), int64(cfg.Height)
	if w <= 0 || h <= 0 {
		return fmt.Errorf("%w: the header says %dx%d", ErrDecode, w, h)
	}
	if w > MaxSide || h > MaxSide {
		return fmt.Errorf("%w: %dx%d has a side over %d px", ErrTooLarge, w, h, MaxSide)
	}
	// Each side is at most MaxSide, so this cannot overflow.
	if need := w * h * bpp * halves / 2; need > MaxDecodeBytes {
		return fmt.Errorf("%w: %dx%d needs about %d MiB to decode, over %d MiB", ErrTooLarge,
			w, h, need>>20, MaxDecodeBytes>>20)
	}
	return nil
}

// fitSize is w × h fitted inside ThumbEdge × ThumbEdge with its aspect
// kept, rounded, and never enlarged; neither side is below one.
func fitSize(w, h int) (int, int) {
	if w <= ThumbEdge && h <= ThumbEdge {
		return w, h
	}
	if w >= h {
		return ThumbEdge, max(1, (h*ThumbEdge+w/2)/w)
	}
	return max(1, (w*ThumbEdge+h/2)/h), ThumbEdge
}

// fit is img scaled by area averaging into its fitted size: each pixel of
// the thumbnail is the mean of the source area it covers, a source pixel
// that straddles two thumbnail pixels counting in each by the fraction of
// it that falls there (Terva review 1748). Colour is averaged
// premultiplied, so a transparent pixel's colour does not bleed, and
// stored unpremultiplied.
//
// The weights are exact integers: on each axis, a source pixel is dst
// units long and a thumbnail pixel src units, so a thumbnail pixel's
// weights sum to sw × sh. That is at most 2^28 (MaxSide squared), and a
// 16-bit channel times it, times 0xffff to unpremultiply, fits in uint64.
func fit(img image.Image) *image.NRGBA {
	b := img.Bounds()
	dw, dh := fitSize(b.Dx(), b.Dy())
	return scale(img, b, dw, dh)
}

// scale is the part b of img scaled to dw × dh by fit's area averaging,
// up or down: an avatar's square is cut from the image and may be smaller
// than the size it is drawn at (TKT-01M3YSQ4CR).
func scale(img image.Image, b image.Rectangle, dw, dh int) *image.NRGBA {
	sw, sh := b.Dx(), b.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	px := pixelReader(img)
	cols, rows := spans(sw, dw), spans(sh, dh)
	total := uint64(sw) * uint64(sh)
	for dy, row := range rows {
		for dx, col := range cols {
			var r, g, bl, a uint64
			for j, wy := range row.weights {
				y := b.Min.Y + row.start + j
				for i, wx := range col.weights {
					c := px(b.Min.X+col.start+i, y)
					w := wx * wy
					r += uint64(c.R) * w
					g += uint64(c.G) * w
					bl += uint64(c.B) * w
					a += uint64(c.A) * w
				}
			}
			i := out.PixOffset(dx, dy)
			out.Pix[i+3] = uint8(a / total >> 8)
			if a == 0 {
				continue
			}
			// Unpremultiply: colour × 0xffff / alpha, from the sums,
			// which share the weights.
			out.Pix[i+0] = uint8(min(r*0xffff/a, 0xffff) >> 8)
			out.Pix[i+1] = uint8(min(g*0xffff/a, 0xffff) >> 8)
			out.Pix[i+2] = uint8(min(bl*0xffff/a, 0xffff) >> 8)
		}
	}
	return out
}

// span is the source pixels one thumbnail pixel covers on one axis, from
// start, with the length of each that it covers.
type span struct {
	start   int
	weights []uint64
}

// spans maps src pixels onto dst. Thumbnail pixel d covers [d×src,
// (d+1)×src) and source pixel x covers [x×dst, (x+1)×dst), in units of
// 1/dst of a source pixel, so every overlap is a whole number of units
// and each span's weights sum to src.
func spans(src, dst int) []span {
	out := make([]span, dst)
	for d := range dst {
		lo, hi := d*src, (d+1)*src
		x0, x1 := lo/dst, (hi+dst-1)/dst
		ws := make([]uint64, x1-x0)
		for x := x0; x < x1; x++ {
			ws[x-x0] = uint64(min(hi, (x+1)*dst) - max(lo, x*dst))
		}
		out[d] = span{start: x0, weights: ws}
	}
	return out
}

// pixelReader reads img's premultiplied 16-bit colour without boxing a
// color.Color for every pixel, which every decoder's image type allows.
func pixelReader(img image.Image) func(x, y int) color.RGBA64 {
	if r, ok := img.(image.RGBA64Image); ok {
		return r.RGBA64At
	}
	return func(x, y int) color.RGBA64 {
		r, g, b, a := img.At(x, y).RGBA()
		return color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}
	}
}
