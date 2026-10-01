package blob

import (
	"bytes"
	"encoding/binary"
	"errors"
	"go/build"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// countDecodes replaces decodeHook for the test with one that counts its
// calls and then runs inner, or the real decoder when inner is nil.
func countDecodes(t *testing.T, inner func(mime string, r io.Reader) (image.Image, error)) *int {
	t.Helper()
	real := decodeHook
	n := new(int)
	decodeHook = func(mime string, r io.Reader) (image.Image, error) {
		*n++
		if inner != nil {
			return inner(mime, r)
		}
		return real(mime, r)
	}
	t.Cleanup(func() { decodeHook = real })
	return n
}

// pngHeader is a PNG of a signature and an IHDR chunk only: what
// DecodeConfig reads, and nothing the decoder could get further with.
func pngHeader(w, h uint32, depth, colorType, interlace byte) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9], ihdr[12] = depth, colorType, interlace
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	b.WriteString("IHDR")
	b.Write(ihdr)
	binary.Write(&b, binary.BigEndian, crc.Sum32())
	return b.Bytes()
}

// jpegHeader is a JPEG of SOI, a baseline SOF declaring w × h with comps
// components, and the start of a scan, which is where DecodeConfig stops.
func jpegHeader(w, h uint16, comps int) []byte {
	b := []byte{0xff, 0xd8, 0xff, 0xc0}
	b = binary.BigEndian.AppendUint16(b, uint16(8+3*comps))
	b = append(b, 8)
	b = binary.BigEndian.AppendUint16(b, h)
	b = binary.BigEndian.AppendUint16(b, w)
	b = append(b, byte(comps))
	for i := range comps {
		b = append(b, byte(i+1), 0x11, 0)
	}
	return append(b, 0xff, 0xda, 0x00, 0x0c)
}

// gifHeader is a GIF of a header and a logical screen of w × h.
func gifHeader(w, h uint16) []byte {
	b := []byte("GIF89a")
	b = binary.LittleEndian.AppendUint16(b, w)
	b = binary.LittleEndian.AppendUint16(b, h)
	return append(b, 0, 0, 0, 0x3b)
}

// TestThumbnailRefusesBombBeforeDecode: a PNG, a JPEG and a GIF, each under
// 1 KB, whose headers declare images of gigabytes, are refused from the
// header alone: no decoder is called and under 1 MiB is allocated.
func TestThumbnailRefusesBombBeforeDecode(t *testing.T) {
	calls := countDecodes(t, nil)
	for _, c := range []struct {
		name, mime string
		data       []byte
	}{
		{"PNG 100000 x 100000", "image/png", pngHeader(100000, 100000, 8, 6, 0)},
		{"JPEG 65000 x 65000", "image/jpeg", jpegHeader(65000, 65000, 3)},
		{"GIF 65535 x 65535", "image/gif", gifHeader(65535, 65535)},
		// Under MaxSide, over the budget.
		{"PNG 16384 x 16384", "image/png", pngHeader(16384, 16384, 8, 6, 0)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if len(c.data) >= 1024 {
				t.Fatalf("the bomb is %d bytes", len(c.data))
			}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, _, err := Thumbnail(bytes.NewReader(c.data), c.mime)
			runtime.ReadMemStats(&after)
			if !errors.Is(err, ErrTooLarge) {
				t.Fatalf("err = %v, want ErrTooLarge", err)
			}
			if *calls != 0 {
				t.Fatalf("%d decoder calls", *calls)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= 1<<20 {
				t.Fatalf("%d bytes allocated", grew)
			}
		})
	}
}

// refusedOrDecoded runs Thumbnail on a header-only image with a decoder
// that fails, and says whether the bounds refused it (no decode) or let it
// through to the decoder.
func refusedOrDecoded(t *testing.T, data []byte, mime string) string {
	t.Helper()
	calls := countDecodes(t, func(string, io.Reader) (image.Image, error) {
		return nil, errors.New("not decoding in a test")
	})
	_, _, err := Thumbnail(bytes.NewReader(data), mime)
	switch {
	case errors.Is(err, ErrTooLarge) && *calls == 0:
		return "refused"
	case errors.Is(err, ErrDecode) && *calls == 1:
		return "decoded"
	}
	t.Fatalf("err %v after %d calls", err, *calls)
	return ""
}

// TestThumbnailBudgetCountsDepth: the budget counts bytes per pixel, so a
// 16-bit PNG over it is refused where an 8-bit one of the same size goes
// to the decoder; an interlaced PNG and a four-component JPEG count more
// than their plain forms too.
func TestThumbnailBudgetCountsDepth(t *testing.T) {
	// 8000 × 6000 is 48 Mpx: 192 MiB at 4 bytes, 384 MiB at 8.
	for _, c := range []struct {
		name, mime string
		data       []byte
		want       string
	}{
		{"8-bit RGBA PNG", "image/png", pngHeader(8000, 6000, 8, 6, 0), "decoded"},
		{"16-bit RGBA PNG", "image/png", pngHeader(8000, 6000, 16, 6, 0), "refused"},
		{"16-bit grey PNG", "image/png", pngHeader(8000, 6000, 16, 0, 0), "refused"},
		{"8-bit interlaced PNG", "image/png", pngHeader(8000, 6000, 8, 6, 1), "refused"},
		// 4000 × 3500 is 14 Mpx: 214 MiB at 16 bytes, 320 MiB at 24.
		{"three-component JPEG", "image/jpeg", jpegHeader(4000, 3500, 3), "decoded"},
		{"four-component JPEG", "image/jpeg", jpegHeader(4000, 3500, 4), "refused"},
		// 5000 × 4000 is 20 Mpx: 320 MiB at 16 bytes, so a 20 MP photo is
		// a card with no thumbnail.
		{"20 MP JPEG", "image/jpeg", jpegHeader(5000, 4000, 3), "refused"},
		{"GIF at the side limit", "image/gif", gifHeader(8000, 6000), "decoded"},
		{"GIF over the side limit", "image/gif", gifHeader(16385, 10), "refused"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := refusedOrDecoded(t, c.data, c.mime); got != c.want {
				t.Fatalf("%s, want %s", got, c.want)
			}
		})
	}
}

func decodeThumb(t *testing.T, data []byte, ctype string) image.Image {
	t.Helper()
	var img image.Image
	var err error
	switch ctype {
	case "image/jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case "image/png":
		img, err = png.Decode(bytes.NewReader(data))
	default:
		t.Fatalf("thumbnail type %q", ctype)
	}
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestThumbnailFitsAndKeepsAspect: an image is fitted inside 160 px with
// its aspect kept, and one already inside is never enlarged.
func TestThumbnailFitsAndKeepsAspect(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{1200, 640, 160, 85},
		{640, 1200, 85, 160},
		{100, 50, 100, 50},
		{160, 160, 160, 160},
		{4000, 3, 160, 1},
		{2, 4000, 1, 160},
	} {
		data := encodePNG(t, image.NewGray(image.Rect(0, 0, c.w, c.h)))
		out, ctype, err := Thumbnail(bytes.NewReader(data), "image/png")
		if err != nil {
			t.Fatalf("%dx%d: %v", c.w, c.h, err)
		}
		if b := decodeThumb(t, out, ctype).Bounds(); b.Dx() != c.wantW || b.Dy() != c.wantH {
			t.Errorf("%dx%d became %dx%d, want %dx%d", c.w, c.h, b.Dx(), b.Dy(), c.wantW, c.wantH)
		}
	}
}

// TestThumbnailAveragesArea: each thumbnail pixel is the mean of the source
// pixels it covers, and a transparent pixel's colour does not bleed into
// an opaque neighbour's.
func TestThumbnailAveragesArea(t *testing.T) {
	// A 320 × 320 checkerboard of black and white halves to mid grey.
	board := image.NewGray(image.Rect(0, 0, 320, 320))
	for y := range 320 {
		for x := range 320 {
			if (x+y)%2 == 0 {
				board.SetGray(x, y, color.Gray{255})
			}
		}
	}
	out, ctype, err := Thumbnail(bytes.NewReader(encodePNG(t, board)), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := decodeThumb(t, out, ctype).At(80, 80).RGBA()
	if v := r >> 8; v < 126 || v > 128 {
		t.Errorf("the checkerboard averages to %d, want about 127", v)
	}

	// Columns of transparent red beside opaque blue are half-transparent
	// blue, not purple.
	stripes := image.NewNRGBA(image.Rect(0, 0, 320, 2))
	for y := range 2 {
		for x := range 320 {
			c := color.NRGBA{0, 0, 255, 255}
			if x%2 == 0 {
				c = color.NRGBA{255, 0, 0, 0}
			}
			stripes.SetNRGBA(x, y, c)
		}
	}
	out, ctype, err = Thumbnail(bytes.NewReader(encodePNG(t, stripes)), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	got := color.NRGBAModel.Convert(decodeThumb(t, out, ctype).At(10, 0)).(color.NRGBA)
	if got.R != 0 || got.B != 255 || got.A < 126 || got.A > 128 {
		t.Errorf("the stripes average to %+v, want half-transparent blue", got)
	}
}

// TestThumbnailAreaAverageAtAFractionalRatio: at a scale that does not
// divide evenly, every thumbnail pixel is the exact area average, with a
// source pixel that straddles two counted in each by the fraction that
// falls there (Terva review 1748). The reference is computed in floating
// point from the definition.
func TestThumbnailAreaAverageAtAFractionalRatio(t *testing.T) {
	for _, size := range []image.Point{{403, 251}, {161, 3}, {400, 1}} {
		src := image.NewGray(image.Rect(0, 0, size.X, size.Y))
		seed := uint32(size.X*7919 + size.Y)
		for i := range src.Pix {
			seed = seed*1664525 + 1013904223
			src.Pix[i] = uint8(seed >> 24)
		}
		out, ctype, err := Thumbnail(bytes.NewReader(encodePNG(t, src)), "image/png")
		if err != nil {
			t.Fatal(err)
		}
		got := decodeThumb(t, out, ctype)
		dw, dh := got.Bounds().Dx(), got.Bounds().Dy()
		sx, sy := float64(size.X)/float64(dw), float64(size.Y)/float64(dh)
		bad := 0
		for dy := range dh {
			for dx := range dw {
				var sum float64
				for y := range size.Y {
					oy := math.Min(float64(y+1), float64(dy+1)*sy) - math.Max(float64(y), float64(dy)*sy)
					if oy <= 0 {
						continue
					}
					for x := range size.X {
						ox := math.Min(float64(x+1), float64(dx+1)*sx) - math.Max(float64(x), float64(dx)*sx)
						if ox > 0 {
							sum += float64(src.GrayAt(x, y).Y) * ox * oy
						}
					}
				}
				want := sum / (sx * sy)
				r, _, _, _ := got.At(dx, dy).RGBA()
				if math.Abs(float64(r>>8)-want) > 1 {
					if bad++; bad <= 3 {
						t.Errorf("%v: pixel (%d,%d) is %d, the area average is %.2f", size, dx, dy, r>>8, want)
					}
				}
			}
		}
	}
}

// TestThumbnailFormats: a JPEG's thumbnail is a JPEG; a PNG's and a GIF's
// are PNGs; alpha survives; anything else is not an image.
func TestThumbnailFormats(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 400, 200))
	for y := range 200 {
		for x := range 400 {
			if x < 200 {
				src.SetNRGBA(x, y, color.NRGBA{0, 200, 0, 255})
			}
		}
	}
	var jb, gb bytes.Buffer
	if err := jpeg.Encode(&jb, src, nil); err != nil {
		t.Fatal(err)
	}
	pal := image.NewPaletted(src.Bounds(), color.Palette{color.Transparent, color.NRGBA{0, 200, 0, 255}})
	for y := range 200 {
		for x := range 200 {
			pal.SetColorIndex(x, y, 1)
		}
	}
	if err := gif.Encode(&gb, pal, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mime, want string
		data       []byte
		alpha      bool
	}{
		{"image/jpeg", "image/jpeg", jb.Bytes(), false},
		{"image/png", "image/png", encodePNG(t, src), true},
		{"image/gif", "image/png", gb.Bytes(), true},
	} {
		out, ctype, err := Thumbnail(bytes.NewReader(c.data), c.mime)
		if err != nil {
			t.Fatalf("%s: %v", c.mime, err)
		}
		if ctype != c.want || signature(out) != c.want {
			t.Errorf("%s: answered %s, bytes are %s, want %s", c.mime, ctype, signature(out), c.want)
		}
		img := decodeThumb(t, out, ctype)
		if b := img.Bounds(); b.Dx() != 160 || b.Dy() != 80 {
			t.Errorf("%s: %v", c.mime, b)
		}
		_, g, _, a := img.At(10, 40).RGBA()
		if g>>8 < 180 || a>>8 != 255 {
			t.Errorf("%s: the left half is %d green at alpha %d", c.mime, g>>8, a>>8)
		}
		if _, _, _, a := img.At(150, 40).RGBA(); c.alpha && a != 0 {
			t.Errorf("%s: the transparent half has alpha %d", c.mime, a>>8)
		}
	}
	for _, mime := range []string{"application/pdf", "text/plain; charset=utf-8", "image/webp", ""} {
		if _, _, err := Thumbnail(bytes.NewReader([]byte("x")), mime); !errors.Is(err, ErrNotImage) {
			t.Errorf("%q: %v", mime, err)
		}
	}
}

// signature names the format of b by its signature.
func signature(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG")):
		return "image/png"
	case bytes.HasPrefix(b, []byte{0xff, 0xd8}):
		return "image/jpeg"
	}
	return "unknown"
}

// TestDecodePanicIsContained: a decoder that panics on its bytes gives
// ErrDecode, and the caller carries on.
func TestDecodePanicIsContained(t *testing.T) {
	countDecodes(t, func(string, io.Reader) (image.Image, error) {
		var m map[string]int
		m["boom"]++ // a nil map write, as a decoder bug would be
		return nil, nil
	})
	data := encodePNG(t, image.NewGray(image.Rect(0, 0, 4, 4)))
	if _, _, err := Thumbnail(bytes.NewReader(data), "image/png"); !errors.Is(err, ErrDecode) {
		t.Fatalf("err = %v", err)
	}
	// A truncated file, past a valid header, is an ordinary ErrDecode.
	decodeHook = func(mime string, r io.Reader) (image.Image, error) { return formats[mime].decode(r) }
	if _, _, err := Thumbnail(bytes.NewReader(data[:len(data)-20]), "image/png"); !errors.Is(err, ErrDecode) {
		t.Fatalf("truncated: %v", err)
	}
}

// TestThumbPath: the thumbnail sits beside its blob, and a bad hash is
// refused rather than joined.
func TestThumbPath(t *testing.T) {
	h := strings.Repeat("ab", 32)
	p, err := ThumbPath("/data", h)
	if err != nil || p != filepath.Join("/data", "blobs", "sha256", "ab", h+".thumb") {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := ThumbPath("/data", "../"+h[3:]); !errors.Is(err, ErrBadHash) {
		t.Fatalf("a bad hash: %v", err)
	}
}

// allowedImage is every image package this package may import.
var allowedImage = map[string]bool{
	"image": true, "image/color": true, "image/png": true, "image/jpeg": true, "image/gif": true,
}

const module = "github.com/terva-sh/ketju/"

// TestThumbImportsOnlyStdlibImage: this package, and every package of this
// module it imports, imports nothing outside the standard library and this
// module, and no image package but image, image/color and the three
// decoders, so no other decoder can reach the bytes a thumbnail reads.
func TestThumbImportsOnlyStdlibImage(t *testing.T) {
	// go test runs in the package's directory.
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(here, "..", "..", "..")
	seen := map[string]bool{}
	var visit func(path, dir string)
	visit = func(path, dir string) {
		if seen[path] {
			return
		}
		seen[path] = true
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, imp := range pkg.Imports {
			switch first, _, _ := strings.Cut(imp, "/"); {
			case strings.HasPrefix(imp, module):
				visit(imp, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(imp, module))))
			case strings.Contains(first, "."):
				t.Errorf("%s imports %s, outside the standard library", path, imp)
			case first == "image" && !allowedImage[imp]:
				t.Errorf("%s imports %s", path, imp)
			case imp == "C":
				t.Errorf("%s uses cgo", path)
			}
		}
	}
	visit(module+"internal/store/blob", here)
	if !seen[module+"internal/fsutil"] {
		t.Fatalf("the walk did not follow the module's imports: %v", seen)
	}
}
