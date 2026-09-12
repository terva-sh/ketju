package threadlog

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		h    FrameHeader
		body []byte
	}{
		{"zero header, empty body", FrameHeader{}, nil},
		{"empty body", FrameHeader{Type: 2, Encoding: EncodingCBOR, Ordinal: 1, Revision: 1}, []byte{}},
		{"one byte body", FrameHeader{Type: 2, Encoding: EncodingCBOR, Ordinal: 7, Revision: 2}, []byte{0xA1}},
		{
			"every field distinct",
			FrameHeader{
				Type:        30,
				Encoding:    EncodingJSON,
				Flags:       0x1234,
				Ordinal:     123456,
				Revision:    789,
				TimestampMS: 1757500000000,
				Actor:       0x0011223344556677,
			},
			bytes.Repeat([]byte("body"), 64),
		},
		{
			"max values",
			FrameHeader{
				Type:        math.MaxUint8,
				Encoding:    math.MaxUint8,
				Flags:       math.MaxUint16,
				Ordinal:     math.MaxUint32,
				Revision:    math.MaxUint32,
				TimestampMS: math.MaxInt64,
				Actor:       math.MaxUint64,
			},
			[]byte("x"),
		},
		{"negative timestamp", FrameHeader{TimestampMS: math.MinInt64}, []byte("y")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := AppendFrame(nil, tc.h, tc.body)
			if err != nil {
				t.Fatalf("AppendFrame: %v", err)
			}
			wantSize := FrameFixedSize + len(tc.body) + 4
			if len(buf) != wantSize {
				t.Fatalf("encoded %d bytes, want %d", len(buf), wantSize)
			}

			gotH, gotBody, total, err := ParseFrame(buf)
			if err != nil {
				t.Fatalf("ParseFrame: %v", err)
			}
			if gotH != tc.h {
				t.Errorf("header changed\n got %+v\nwant %+v", gotH, tc.h)
			}
			if !bytes.Equal(gotBody, tc.body) {
				t.Errorf("body changed\n got %q\nwant %q", gotBody, tc.body)
			}
			if total != wantSize {
				t.Errorf("total size %d, want %d", total, wantSize)
			}

			// The header-only path must agree with the full parse.
			hdrOnly, hdrTotal, err := ParseFrameHeader(buf)
			if err != nil {
				t.Fatalf("ParseFrameHeader: %v", err)
			}
			if hdrOnly != gotH || hdrTotal != total {
				t.Errorf("header-only parse disagrees: %+v/%d vs %+v/%d", hdrOnly, hdrTotal, gotH, total)
			}
		})
	}
}

func TestFrameAppendPreservesPrefix(t *testing.T) {
	buf := []byte("prefix")
	buf, err := AppendFrame(buf, FrameHeader{Ordinal: 1}, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	buf, err = AppendFrame(buf, FrameHeader{Ordinal: 2}, []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:6]) != "prefix" {
		t.Fatalf("prefix overwritten: %q", buf[:6])
	}

	h, body, n, err := ParseFrame(buf[6:])
	if err != nil {
		t.Fatal(err)
	}
	if h.Ordinal != 1 || string(body) != "one" {
		t.Fatalf("first frame wrong: %+v %q", h, body)
	}
	h, body, _, err = ParseFrame(buf[6+n:])
	if err != nil {
		t.Fatal(err)
	}
	if h.Ordinal != 2 || string(body) != "two" {
		t.Fatalf("second frame wrong: %+v %q", h, body)
	}
}

// Flags is the frame's forward-compatibility surface. Bits this build does not
// know must survive a read and a rewrite untouched.
func TestFrameUnknownFlagBitsRoundTrip(t *testing.T) {
	buf, err := AppendFrame(nil, FrameHeader{Ordinal: 5, Flags: 0xFFFF}, []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	h, _, _, err := ParseFrame(buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.Flags != 0xFFFF {
		t.Fatalf("flags %#04x, want 0xffff", h.Flags)
	}
}

func TestFrameDetectsCorruptBody(t *testing.T) {
	const ordinal = 4242
	body := bytes.Repeat([]byte("payload"), 32)
	buf, err := AppendFrame(nil, FrameHeader{Type: 2, Ordinal: ordinal}, body)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt every byte of the body in turn.
	for i := FrameFixedSize; i < len(buf)-4; i++ {
		damaged := bytes.Clone(buf)
		damaged[i] ^= 0x01

		h, gotBody, total, err := ParseFrame(damaged)
		if err == nil {
			t.Fatalf("corrupting body byte %d was not detected", i)
		}
		if !errors.Is(err, ErrChecksum) {
			t.Fatalf("corrupting body byte %d: got %v, want ErrChecksum", i, err)
		}
		// Reported, not returned: nothing from a failed frame reaches a caller
		// who ignores the error.
		if gotBody != nil || h != (FrameHeader{}) || total != 0 {
			t.Fatalf("corrupt frame returned data: %+v %q %d", h, gotBody, total)
		}

		var ce *ChecksumError
		if !errors.As(err, &ce) {
			t.Fatalf("error is not a *ChecksumError: %v", err)
		}
		if ce.Ordinal != ordinal {
			t.Errorf("ChecksumError names ordinal %d, want %d", ce.Ordinal, ordinal)
		}
		if ce.Stored == ce.Computed {
			t.Errorf("ChecksumError reports matching sums %#08x", ce.Stored)
		}
	}
}

// The checksum covers the length field, so a corrupted length is caught rather
// than believed. This is the case that matters after a torn write, where a
// plausible-looking length would otherwise send a reader into the middle of
// another frame.
func TestFrameDetectsCorruptLength(t *testing.T) {
	buf, err := AppendFrame(nil, FrameHeader{Ordinal: 9}, bytes.Repeat([]byte("z"), 200))
	if err != nil {
		t.Fatal(err)
	}
	// Shrink the length to another in-range value.
	binary.LittleEndian.PutUint32(buf[frLength:], uint32(frameLengthBase+100))

	if _, _, _, err := ParseFrame(buf); !errors.Is(err, ErrChecksum) {
		t.Fatalf("got %v, want ErrChecksum", err)
	}
}

func TestFrameRejectsMalformedLength(t *testing.T) {
	cases := []struct {
		name   string
		length uint32
		want   error
	}{
		{"zero", 0, ErrMalformed},
		{"below the fixed part", frameLengthBase - 1, ErrMalformed},
		{"at the cap", MaxFrameSize, ErrMalformed},
		{"past the cap", MaxFrameSize + 1, ErrMalformed},
		{"max uint32", math.MaxUint32, ErrMalformed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, FrameFixedSize)
			binary.LittleEndian.PutUint32(buf[frLength:], tc.length)

			if _, _, err := ParseFrameHeader(buf); !errors.Is(err, tc.want) {
				t.Errorf("ParseFrameHeader: got %v, want %v", err, tc.want)
			}
			if _, _, _, err := ParseFrame(buf); !errors.Is(err, tc.want) {
				t.Errorf("ParseFrame: got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFrameRejectsShortBuffer(t *testing.T) {
	buf, err := AppendFrame(nil, FrameHeader{Ordinal: 1}, []byte("some body"))
	if err != nil {
		t.Fatal(err)
	}

	for n := 0; n < len(buf); n++ {
		_, _, _, err := ParseFrame(buf[:n])
		if !errors.Is(err, ErrShortBuffer) {
			t.Fatalf("ParseFrame with %d of %d bytes: got %v, want ErrShortBuffer", n, len(buf), err)
		}
	}
	// A truncated fixed part must not be parsed as a header either.
	for n := 0; n < FrameFixedSize; n++ {
		if _, _, err := ParseFrameHeader(buf[:n]); !errors.Is(err, ErrShortBuffer) {
			t.Fatalf("ParseFrameHeader with %d bytes: got %v, want ErrShortBuffer", n, err)
		}
	}
}

func TestAppendFrameRejectsOversizeBody(t *testing.T) {
	body := make([]byte, MaxFrameSize)
	if _, err := AppendFrame(nil, FrameHeader{Ordinal: 1}, body); !errors.Is(err, ErrMalformed) {
		t.Fatalf("got %v, want ErrMalformed", err)
	}
}

// trackingReaderAt records every byte range it hands out, so a test can prove
// what a walk did not read.
type trackingReaderAt struct {
	data  []byte
	reads [][2]int64 // offset, length
	total int64
}

func (t *trackingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(t.data)) {
		return 0, io.EOF
	}
	n := copy(p, t.data[off:])
	t.reads = append(t.reads, [2]int64{off, int64(n)})
	t.total += int64(n)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (t *trackingReaderAt) touched(lo, hi int64) bool {
	for _, r := range t.reads {
		if r[0] < hi && lo < r[0]+r[1] {
			return true
		}
	}
	return false
}

// This is the property the whole frame split exists for: the reindexer and the
// index builder walk an entire log without decoding, or even reading, a single
// body. The tracking reader turns that from a claim into a measurement.
func TestWalkLogWithoutReadingBodies(t *testing.T) {
	const frames = 200

	log := NewFileHeader(0x3f7a9c21e0000001, 1757500000000, 42).AppendTo(nil)
	type span struct{ lo, hi int64 }
	bodies := make([]span, 0, frames)
	wantOrdinals := make([]uint32, 0, frames)

	for i := 1; i <= frames; i++ {
		body := []byte(fmt.Sprintf("body of post %d: %s", i, bytes.Repeat([]byte("x"), 50+i)))
		frameStart := int64(len(log))
		var err error
		log, err = AppendFrame(log, FrameHeader{
			Type:        2,
			Encoding:    EncodingCBOR,
			Ordinal:     uint32(i),
			Revision:    1,
			TimestampMS: 1757500000000 + int64(i),
			Actor:       uint64(1000 + i%40),
		}, body)
		if err != nil {
			t.Fatal(err)
		}
		bodyLo := frameStart + FrameFixedSize
		bodies = append(bodies, span{bodyLo, bodyLo + int64(len(body))})
		wantOrdinals = append(wantOrdinals, uint32(i))
	}

	r := &trackingReaderAt{data: log}

	// Read the file header, then walk frame by frame reading only the fixed
	// part of each.
	hdrBuf := make([]byte, FileHeaderSize)
	if _, err := r.ReadAt(hdrBuf, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFileHeader(hdrBuf); err != nil {
		t.Fatal(err)
	}

	var gotOrdinals []uint32
	fixed := make([]byte, FrameFixedSize)
	for off := int64(FileHeaderSize); off < int64(len(log)); {
		if _, err := r.ReadAt(fixed, off); err != nil && err != io.EOF {
			t.Fatalf("read at %d: %v", off, err)
		}
		h, total, err := ParseFrameHeader(fixed)
		if err != nil {
			t.Fatalf("parse at %d: %v", off, err)
		}
		gotOrdinals = append(gotOrdinals, h.Ordinal)
		off += int64(total)
	}

	if len(gotOrdinals) != frames {
		t.Fatalf("walked %d frames, want %d", len(gotOrdinals), frames)
	}
	for i := range wantOrdinals {
		if gotOrdinals[i] != wantOrdinals[i] {
			t.Fatalf("frame %d has ordinal %d, want %d", i, gotOrdinals[i], wantOrdinals[i])
		}
	}

	for i, b := range bodies {
		if r.touched(b.lo, b.hi) {
			t.Errorf("the walk read body bytes of ordinal %d, range [%d,%d)", i+1, b.lo, b.hi)
		}
	}

	// And it should be cheap: the header plus 32 bytes per frame, against a
	// log many times that size.
	wantRead := int64(FileHeaderSize + frames*FrameFixedSize)
	if r.total != wantRead {
		t.Errorf("walk read %d bytes, want exactly %d", r.total, wantRead)
	}
	if r.total >= int64(len(log)) {
		t.Errorf("walk read %d bytes of a %d byte log, which is no saving", r.total, len(log))
	}
}

// The fuzz target in TKT-01M29C2WTV goes deeper. This keeps the obvious
// mutations covered here, where the format is defined.
func TestParseNeverPanics(t *testing.T) {
	base, err := AppendFrame(nil, FrameHeader{Type: 2, Ordinal: 1, Revision: 1}, []byte("a body worth mangling"))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < len(base); i++ {
		for _, mask := range []byte{0x01, 0x80, 0xFF} {
			damaged := bytes.Clone(base)
			damaged[i] ^= mask
			_, _, _, _ = ParseFrame(damaged)
			_, _, _ = ParseFrameHeader(damaged)
			for n := 0; n <= len(damaged); n++ {
				_, _, _, _ = ParseFrame(damaged[:n])
				_, _, _ = ParseFrameHeader(damaged[:n])
				_, _ = ParseFileHeader(damaged[:n])
			}
		}
	}
}
