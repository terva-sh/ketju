package threadlog

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

// The parser reads a length field off disk and sizes work from it, which is
// the classic shape of a memory blow-up. The threat here is corruption rather
// than an attacker — logs are not user-supplied files — but the failure mode is
// the same either way: a panic in the parser takes down the process for every
// thread, not just the damaged one.
//
// TestParseNeverPanics in frame_test.go covers the obvious mutations
// deterministically, where the format is defined. These targets go deeper, and
// they check more than the absence of a panic: a parser that returns success
// while mis-slicing a body is just as broken as one that crashes, and only an
// invariant catches it.

// seedFrames returns valid frames worth mutating, plus the shapes that are
// interesting because they sit on a boundary.
//
// It panics rather than taking a *testing.T: every input here is built from
// constants, so a failure is a mistake in this file and not a test result. The
// frame at the size cap is deliberately absent — it is a megabyte, it would
// dominate the corpus, and TestAppendFrameRejectsOversizeBody already pins
// that boundary deterministically.
func seedFrames() [][]byte {
	var out [][]byte
	add := func(h FrameHeader, body []byte) {
		b, err := AppendFrame(nil, h, body)
		if err != nil {
			panic("threadlog fuzz seed: " + err.Error())
		}
		out = append(out, b)
	}

	postBody, err := EncodeBody(EncodingCBOR, &PostBody{Source: "hello", ReplyTo: 3})
	if err != nil {
		panic("threadlog fuzz seed: " + err.Error())
	}

	add(FrameHeader{Type: TypePost, Encoding: EncodingCBOR, Ordinal: 1, Revision: 1}, postBody)
	add(FrameHeader{Type: TypeThreadCreated, Encoding: EncodingCBOR, Ordinal: 0, Revision: 1}, []byte{0xa0})
	// Empty body: the minimum legal frame, where L == frameLengthBase exactly.
	add(FrameHeader{Type: TypeRestore, Encoding: EncodingCBOR, Ordinal: 9, Revision: 2}, nil)
	// Every header field at its maximum, so a sign or width mistake shows.
	add(FrameHeader{
		Type: 255, Encoding: 255, Flags: 0xffff, Ordinal: ^uint32(0),
		Revision: ^uint32(0), TimestampMS: -1, Actor: ^uint64(0),
	}, []byte("edge"))
	// Two frames back to back, so a parser that reports the wrong total size
	// is caught by landing off the second frame's boundary.
	add(FrameHeader{Type: TypeEdit, Encoding: EncodingCBOR, Ordinal: 1, Revision: 2}, []byte("second"))

	return out
}

// FuzzParseFrame drives the frame parser with arbitrary bytes.
//
// The interesting assertion is not "did not panic" — the fuzzing harness gets
// that for free. It is that a frame the parser ACCEPTS re-encodes to exactly
// the bytes it came from. That catches a parser which reports success while
// slicing the body a byte short, which no amount of crash-hunting would find.
func FuzzParseFrame(f *testing.F) {
	for _, b := range seedFrames() {
		f.Add(b)
	}
	f.Add([]byte(nil))
	f.Add(make([]byte, FrameFixedSize))
	// A length field claiming far more than the buffer holds: the shape the
	// ticket is named for.
	huge := make([]byte, FrameFixedSize+4)
	for i := range huge[:4] {
		huge[i] = 0xff
	}
	f.Add(huge)

	f.Fuzz(func(t *testing.T, data []byte) {
		// A header parse must never disagree with a full parse about size.
		hdr, hdrSize, hdrErr := ParseFrameHeader(data)
		if hdrErr == nil {
			if hdrSize < FrameFixedSize+4 {
				t.Fatalf("header reports a %d byte frame, below the %d byte minimum", hdrSize, FrameFixedSize+4)
			}
			if hdrSize > MaxFrameSize {
				t.Fatalf("header reports a %d byte frame, above the %d byte cap", hdrSize, MaxFrameSize)
			}
		}

		h, body, total, err := ParseFrame(data)
		if err != nil {
			return
		}

		if total > len(data) {
			t.Fatalf("parse accepted a %d byte frame from %d bytes of input", total, len(data))
		}
		if total < FrameFixedSize+4 {
			t.Fatalf("parse accepted a %d byte frame, below the minimum", total)
		}
		if want := total - FrameFixedSize - 4; len(body) != want {
			t.Fatalf("body is %d bytes, the frame size implies %d", len(body), want)
		}
		if hdrErr != nil {
			t.Fatalf("ParseFrame accepted what ParseFrameHeader rejected: %v", hdrErr)
		}
		if hdrSize != total {
			t.Fatalf("header says %d bytes, full parse says %d", hdrSize, total)
		}
		if h != hdr {
			t.Fatalf("header differs between the two parsers:\n %+v\n %+v", h, hdr)
		}

		// The strong one: what was accepted must re-encode byte for byte.
		again, err := AppendFrame(nil, h, body)
		if err != nil {
			t.Fatalf("a frame this package parsed cannot be re-encoded: %v", err)
		}
		if !bytes.Equal(again, data[:total]) {
			t.Fatalf("re-encoding changed the bytes:\n got %x\nwant %x", again, data[:total])
		}
	})
}

// FuzzDecodeBody drives the body decoder, which is the layer under the frame
// parser and the one that allocates from what it reads.
func FuzzDecodeBody(f *testing.F) {
	for _, tc := range []struct {
		typ RecordType
		enc BodyEncoding
		v   any
	}{
		{TypePost, EncodingCBOR, &PostBody{Source: "hello", Mentions: []uint64{1, 2}}},
		{TypePost, EncodingJSON, &PostBody{Source: "hello"}},
		{TypeThreadCreated, EncodingCBOR, &ThreadCreatedBody{Title: "t", Tags: []string{"a"}}},
		{TypeDelete, EncodingCBOR, &DeleteBody{Reason: "spam"}},
		{TypeThreadTagged, EncodingCBOR, &ThreadTaggedBody{Tags: []string{"x", "y"}}},
	} {
		raw, err := EncodeBody(tc.enc, tc.v)
		if err != nil {
			f.Fatalf("EncodeBody: %v", err)
		}
		f.Add(uint8(tc.typ), uint8(tc.enc), raw)
	}
	// Nested and indefinite-length CBOR, which is where a decoder's depth and
	// element limits earn their keep.
	f.Add(uint8(TypePost), uint8(EncodingCBOR), []byte{0x9f, 0x9f, 0x9f, 0xff, 0xff, 0xff})
	f.Add(uint8(TypePost), uint8(EncodingCBOR), []byte{0xbf, 0xff})
	f.Add(uint8(200), uint8(1), []byte{0xa0})

	f.Fuzz(func(t *testing.T, typ uint8, enc uint8, raw []byte) {
		v, err := DecodeBody(RecordType(typ), BodyEncoding(enc), raw)
		if err != nil {
			if v != nil {
				t.Fatalf("DecodeBody returned both a body and an error %v", err)
			}
			return
		}
		if v == nil {
			t.Fatal("DecodeBody returned no body and no error")
		}

		// Anything decoded must be encodable again. A body that decodes into a
		// state this package cannot write back is a state nothing downstream
		// can round-trip either.
		if _, err := EncodeBody(BodyEncoding(enc), v); err != nil {
			t.Fatalf("a body this package decoded cannot be re-encoded: %v", err)
		}
	})
}

// Criterion: a length field larger than the remaining file is refused before
// any allocation of that size.
//
// Counting allocations is not enough on its own — building the error value
// allocates, so the count is never zero. What matters is the VOLUME: if the
// parser ever believed the claimed length, a single call would allocate about
// a megabyte. Measuring total bytes across many calls and comparing against
// one claimed allocation is what actually tests the claim.
func TestOversizeLengthRefusedBeforeAllocating(t *testing.T) {
	const iterations = 2000

	// A well-formed fixed part whose length field claims the largest frame the
	// parser will entertain, in a buffer holding only the fixed part.
	claim := func(l uint32) []byte {
		b, err := AppendFrame(nil, FrameHeader{Type: TypePost, Ordinal: 1, Revision: 1}, nil)
		if err != nil {
			t.Fatalf("AppendFrame: %v", err)
		}
		b = b[:FrameFixedSize]
		b[0] = byte(l)
		b[1] = byte(l >> 8)
		b[2] = byte(l >> 16)
		b[3] = byte(l >> 24)
		return b
	}

	cases := []struct {
		name string
		in   []byte
	}{
		{"just under the cap", claim(MaxFrameSize - 8)},
		{"at the cap", claim(MaxFrameSize)},
		{"maximum uint32", claim(^uint32(0))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)

			for range iterations {
				if _, _, _, err := ParseFrame(tc.in); err == nil {
					t.Fatal("a frame claiming more than the buffer holds was accepted")
				}
			}

			runtime.ReadMemStats(&after)
			grew := after.TotalAlloc - before.TotalAlloc

			// One believed allocation of the claimed size would exceed this on
			// its own, so staying under it across 2000 calls proves the length
			// was never trusted.
			const oneClaimedAllocation = MaxFrameSize
			if grew >= oneClaimedAllocation {
				t.Errorf("%d refusals allocated %d bytes, more than a single claimed frame (%d) — "+
					"the length was believed before it was checked", iterations, grew, oneClaimedAllocation)
			}
			t.Logf("%d refusals allocated %d bytes total (%.1f per call)",
				iterations, grew, float64(grew)/iterations)
		})
	}
}

// A frame whose length is legal but which the buffer cannot satisfy must be
// ErrShortBuffer, not ErrMalformed: the distinction is the difference between
// "read more" at the end of a coalesced read and "this log is damaged".
func TestOversizeLengthErrorsAreDistinguishable(t *testing.T) {
	b, err := AppendFrame(nil, FrameHeader{Type: TypePost, Ordinal: 1, Revision: 1}, []byte("body"))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := ParseFrame(b[:len(b)-1]); !errors.Is(err, ErrShortBuffer) {
		t.Errorf("a truncated but legal frame: error = %v, want ErrShortBuffer", err)
	}

	bad := bytes.Clone(b)
	bad[0], bad[1], bad[2], bad[3] = 0xff, 0xff, 0xff, 0xff
	if _, _, _, err := ParseFrame(bad); !errors.Is(err, ErrMalformed) {
		t.Errorf("an impossible length: error = %v, want ErrMalformed", err)
	}
}
