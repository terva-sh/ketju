// Package blobstest makes the files the attachment tests upload: small
// images that their decoders accept, a PDF, and text, each different for a
// different seed so that tests can tell blobs apart. It is imported only
// from _test files.
package blobstest

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
)

func picture(w, h int, seed byte) *image.Paletted {
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White, color.RGBA{seed, 0, 0, 255}})
	img.SetColorIndex(0, 0, 2)
	return img
}

// PNG is a w by h PNG whose pixels depend on seed.
func PNG(w, h int, seed byte) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, picture(w, h, seed)); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// JPEG is a w by h JPEG whose pixels depend on seed.
func JPEG(w, h int, seed byte) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, picture(w, h, seed), nil); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// GIF is a w by h GIF whose pixels depend on seed.
func GIF(w, h int, seed byte) []byte {
	var b bytes.Buffer
	if err := gif.Encode(&b, picture(w, h, seed), nil); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// PDF is a minimal PDF naming seed.
func PDF(seed byte) []byte {
	return fmt.Appendf(nil, "%%PDF-1.4\n%% seed %d\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%%%EOF\n", seed)
}

// Text is plain UTF-8 text naming seed.
func Text(seed byte) []byte {
	return fmt.Appendf(nil, "notes, seed %d: nothing here runs\n", seed)
}

// Refused are files of types the allowlist refuses, by what they are.
var Refused = map[string][]byte{
	"svg":  []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
	"html": []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>"),
	"webp": append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 32)...),
	"zip":  append([]byte("PK\x03\x04\x14\x00\x00\x00"), make([]byte, 32)...),
	"elf":  append([]byte("\x7fELF\x02\x01\x01\x00"), make([]byte, 56)...),
}
