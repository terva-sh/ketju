//go:build ignore

// Command icons draws the placeholder app icon (TKT-01M29CGBYY): two linked
// rings, because "ketju" is Finnish for chain, white on the light theme's
// accent. It writes the SVG the shell links as its favicon and the PNGs the
// web app manifest and iOS name, all from the one geometry below, so the
// vector and the rasters cannot drift apart.
//
// The output is committed. The build copies it and never runs this, so CI
// needs no rasterizer and the icons change only when someone runs
//
//	go run web/client/scripts/icons/main.go
//
// from the repository root. It uses the standard library alone: the mark is
// circles and a rounded square, so a pixel's colour is a distance test, and
// supersampling each pixel is all the antialiasing it needs.
//
// The design names no icon, so this is the run's placeholder (docs/06
// "Installable", decided by the run on 2026-10-01). A designed icon replaces
// the output, and this program with it.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const out = "web/client/src/assets/icon"

// The geometry, as fractions of the icon's side.
const (
	ringR   = 0.19   // a ring's centre-line radius
	ringW   = 0.075  // a ring's stroke width
	gap     = 0.03   // the background shown either side of a ring where it crosses over the other
	offsetX = 0.11   // each ring's centre, either side of the middle
	cornerR = 0.1875 // the rounded square's corner radius
)

// The mark's farthest point from the middle is offsetX+ringR+ringW/2 =
// 0.3375 of the side, inside the 0.4 radius a maskable icon keeps clear of
// any launcher's mask (the W3C manifest's safe zone), so one geometry serves
// both the rounded and the full-bleed icons.

var (
	accent = color.RGBA{0x2F, 0x5F, 0xB3, 0xFF} // --accent in the light theme (styles/tokens.css)
	white  = color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
)

func main() {
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	// The apple-touch-icon and the maskable icon are full-bleed squares:
	// iOS and the launcher draw their own corners, and iOS fills any
	// transparency with black. The others carry their own rounded corners.
	for _, f := range []struct {
		name    string
		size    int
		rounded bool
	}{
		{"icon-180.png", 180, false},
		{"icon-192.png", 192, true},
		{"icon-512.png", 512, true},
		{"icon-maskable-512.png", 512, false},
	} {
		var buf bytes.Buffer
		if err := png.Encode(&buf, draw(f.size, f.rounded)); err != nil {
			log.Fatal(err)
		}
		write(f.name, buf.Bytes())
	}
	write("icon.svg", []byte(svg()))
}

func write(name string, b []byte) {
	if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
		log.Fatal(err)
	}
}

// ss is the supersampling factor: ss*ss samples per pixel.
const ss = 4

// draw rasterizes the icon at size px.
func draw(size int, rounded bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a uint32
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					c := sample(x, y, rounded)
					// Premultiplied, as image.RGBA stores it.
					r += uint32(c.R) * uint32(c.A) / 0xFF
					g += uint32(c.G) * uint32(c.A) / 0xFF
					b += uint32(c.B) * uint32(c.A) / 0xFF
					a += uint32(c.A)
				}
			}
			n := uint32(ss * ss)
			img.SetRGBA(px, py, color.RGBA{uint8((r + n/2) / n), uint8((g + n/2) / n), uint8((b + n/2) / n), uint8((a + n/2) / n)})
		}
	}
	return img
}

// sample is the colour at (x, y) in the unit square.
//
// The rings cross twice, above and below the middle. The left ring passes
// over the right one at the upper crossing and under it at the lower, which
// is what makes them read as linked rather than stacked: whichever ring is
// on top there shows a band of background either side of itself, cutting
// the one beneath.
func sample(x, y float64, rounded bool) color.RGBA {
	if rounded && !inRoundedSquare(x, y) {
		return color.RGBA{}
	}
	left := math.Abs(math.Hypot(x-(0.5-offsetX), y-0.5) - ringR)
	right := math.Abs(math.Hypot(x-(0.5+offsetX), y-0.5) - ringR)
	top, under := left, right
	if y >= 0.5 {
		top, under = right, left
	}
	switch {
	case top <= ringW/2:
		return white
	case top <= ringW/2+gap:
		return accent
	case under <= ringW/2:
		return white
	}
	return accent
}

func inRoundedSquare(x, y float64) bool {
	// The distance from the point to the square shrunk by the corner
	// radius, compared with the radius.
	dx := math.Max(math.Abs(x-0.5)-(0.5-cornerR), 0)
	dy := math.Max(math.Abs(y-0.5)-(0.5-cornerR), 0)
	return math.Hypot(dx, dy) <= cornerR
}

// svg is the same mark as a 512-unit vector: the right ring, then the left
// ring over it with its band, then the lower half of the right ring again
// over the left, clipped to below the middle.
func svg() string {
	const s = 512.0
	u := func(f float64) string { return fmt.Sprintf("%g", math.Round(f*s*100)/100) }
	acc := fmt.Sprintf("#%02X%02X%02X", accent.R, accent.G, accent.B)
	ring := func(cx float64, stroke string, w float64) string {
		return fmt.Sprintf(`<circle cx="%s" cy="%s" r="%s" stroke="%s" stroke-width="%s"/>`,
			u(cx), u(0.5), u(ringR), stroke, u(w))
	}
	l, r := 0.5-offsetX, 0.5+offsetX
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">`+
		`<rect width="512" height="512" rx="%s" fill="%s"/>`+
		`<clipPath id="lower"><rect y="256" width="512" height="256"/></clipPath>`+
		`<g fill="none">%s%s%s<g clip-path="url(#lower)">%s%s</g></g></svg>`+"\n",
		u(cornerR), acc,
		ring(r, "#FFF", ringW),
		ring(l, acc, ringW+2*gap), ring(l, "#FFF", ringW),
		ring(r, acc, ringW+2*gap), ring(r, "#FFF", ringW))
}
