package threadlog

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// TestScanFramesFromResumes: a walk started at a frame boundary sees exactly
// the frames from there, and a walk bounded short of a frame's end reports
// that frame as running past the end rather than handing over a partial one.
func TestScanFramesFromResumes(t *testing.T) {
	log := NewFileHeader(1, 1, 1).AppendTo(nil)
	var offsets []int64
	for i := range 3 {
		body, err := EncodeBody(EncodingCBOR, &PostBody{Source: string(rune('a' + i))})
		if err != nil {
			t.Fatal(err)
		}
		offsets = append(offsets, int64(len(log)))
		if log, err = AppendFrame(log, FrameHeader{Type: TypePost, Encoding: EncodingCBOR, Ordinal: uint32(i + 1), Revision: 1}, body); err != nil {
			t.Fatal(err)
		}
	}
	r := bytes.NewReader(log)

	var got []int64
	if err := ScanFramesFrom(r, offsets[1], int64(len(log)), func(rec ScanRecord) bool {
		if rec.Err != nil {
			t.Fatalf("frame at %d: %v", rec.Offset, rec.Err)
		}
		got = append(got, rec.Offset)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != offsets[1] || got[1] != offsets[2] {
		t.Fatalf("walked %v from %d, want %v", got, offsets[1], offsets[1:])
	}

	var last ScanRecord
	if err := ScanFramesFrom(r, offsets[0], int64(len(log))-1, func(rec ScanRecord) bool {
		last = rec
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if last.Offset != offsets[2] || !last.Fatal || !errors.Is(last.Err, ErrShortBuffer) {
		t.Fatalf("a bound one byte short of the last frame gave %+v, want a fatal short-buffer report at %d", last, offsets[2])
	}

	for _, from := range []int64{0, FileHeaderSize - 1, int64(len(log)) + 1} {
		if err := ScanFramesFrom(r, from, int64(len(log)), func(ScanRecord) bool { return true }); !errors.Is(err, ErrMalformed) {
			t.Errorf("starting at %d: got %v, want ErrMalformed", from, err)
		}
	}
}

// TestEditMentionsKeyIsOptional: an Edit without the key decodes to nil (keep
// the post's mentions), one with an empty list decodes to an empty non-nil
// slice (remove them all), and an Edit written before the key existed still
// decodes.
func TestEditMentionsKeyIsOptional(t *testing.T) {
	empty := []uint64{}
	two := []uint64{4, 9}
	for _, c := range []struct {
		name string
		in   EditBody
		want *[]uint64
	}{
		{"absent", EditBody{Source: "x"}, nil},
		{"empty", EditBody{Source: "x", Mentions: &empty}, &empty},
		{"two", EditBody{Source: "x", Mentions: &two}, &two},
	} {
		for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
			raw, err := EncodeBody(enc, &c.in)
			if err != nil {
				t.Fatal(err)
			}
			v, err := DecodeBody(TypeEdit, enc, raw)
			if err != nil {
				t.Fatal(err)
			}
			got := v.(*EditBody).Mentions
			switch {
			case c.want == nil && got != nil:
				t.Errorf("%s/%d: got %v, want the key absent", c.name, enc, *got)
			case c.want != nil && (got == nil || len(*got) != len(*c.want)):
				t.Errorf("%s/%d: got %v, want %v", c.name, enc, got, *c.want)
			}
		}
	}

	old, err := cbor.Marshal(map[string]any{"source": "before the key", "reply_to": 3})
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeBody(TypeEdit, EncodingCBOR, old)
	if err != nil {
		t.Fatal(err)
	}
	if b := v.(*EditBody); b.Mentions != nil || b.ReplyTo != 3 {
		t.Fatalf("an Edit from before the key decoded as %+v", b)
	}
}

// TestEditGraceKeyIsOptional: grace is written only when true, so an edit
// that is not a grace edit is byte for byte what a build before the key
// wrote, and an Edit from before the key decodes as not grace
// (TKT-01M29CKRHS).
func TestEditGraceKeyIsOptional(t *testing.T) {
	for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
		plain, err := EncodeBody(enc, &EditBody{Source: "x", ReplyTo: 3})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(plain, []byte("grace")) {
			t.Errorf("%d: an edit that is not grace carries the key: %q", enc, plain)
		}
		graced, err := EncodeBody(enc, &EditBody{Source: "x", Grace: true})
		if err != nil {
			t.Fatal(err)
		}
		v, err := DecodeBody(TypeEdit, enc, graced)
		if err != nil {
			t.Fatal(err)
		}
		if !v.(*EditBody).Grace {
			t.Errorf("%d: grace did not survive a round trip", enc)
		}
	}
	old, err := cbor.Marshal(map[string]any{"source": "before the key"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeBody(TypeEdit, EncodingCBOR, old)
	if err != nil {
		t.Fatal(err)
	}
	if v.(*EditBody).Grace {
		t.Fatal("an Edit from before the key decoded as grace")
	}
}
