package threadlog

import (
	"errors"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// The rule from docs/03, "Unrecognised record types": the high bit of the type
// byte says what a reader that does not know the type must do with it.
//
// These tests are the ones that matter for the format freeze. Skipping and
// refusing are not interchangeable once data exists, and the whole point of
// deciding this before the freeze is that the numbering stops being editable
// afterwards.

func TestTypeSpaceIsPartitionedByTheHighBit(t *testing.T) {
	if CriticalTypeBit != 0x80 {
		t.Fatalf("CriticalTypeBit = %#02x, docs/03 partitions on the high bit", CriticalTypeBit)
	}

	for _, tc := range []struct {
		n        uint8
		critical bool
		advisory bool
	}{
		{0, false, false}, // never assigned: neither
		{1, false, true},
		{30, false, true},
		{0x7f, false, true},
		{0x80, true, false},
		{0xff, true, false},
	} {
		got := RecordType(tc.n)
		if got.Critical() != tc.critical {
			t.Errorf("RecordType(%#02x).Critical() = %v, want %v", tc.n, got.Critical(), tc.critical)
		}
		if got.Advisory() != tc.advisory {
			t.Errorf("RecordType(%#02x).Advisory() = %v, want %v", tc.n, got.Advisory(), tc.advisory)
		}
	}
}

// Every type in the v1 catalogue is advisory. Not because those records are
// harmless to ignore — skipping a Delete or an Edit would be a disclosure —
// but because the partition only governs a reader that does NOT know the type,
// and every build knows these.
//
// The one exception is Redacted, the first critical type, claimed by docs/03's
// procedure on 2026-09-28 (TKT-01M29BSPE2) and landed with the redaction tool
// (TKT-01M29CXJZ9): builds before it do not know it, and must refuse it.
func TestEveryCatalogueTypeIsAdvisory(t *testing.T) {
	if !TypeRedacted.Critical() {
		t.Errorf("Redacted is %#02x; docs/03 puts it in the critical range", uint8(TypeRedacted))
	}
	for typ := range catalogue {
		if typ.Critical() && typ != TypeRedacted {
			t.Errorf("%s is %#02x, in the critical range; the v1 catalogue is known to every build",
				typ, uint8(typ))
		}
	}
	for _, typ := range []RecordType{TypePostsSplitOut, TypePostsMergedIn} {
		if !typ.Advisory() {
			t.Errorf("%s is %#02x; skipping a split or merge leaves a stale render rather "+
				"than a disclosure, so it stays advisory", typ, uint8(typ))
		}
	}
}

// Criterion: a test that feeds a reader an unknown type.
//
// An advisory unknown is stepped over by frame length and the replay finishes.
// A critical unknown stops it dead. Same log shape, same reader, one byte of
// difference in the type.
func TestReaderSkipsAdvisoryAndRefusesCritical(t *testing.T) {
	future, err := cbor.Marshal(map[string]any{"from": "a later phase"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	build := func(unknown RecordType) []byte {
		var log []byte
		post := func(s string) []byte {
			raw, err := EncodeBody(EncodingCBOR, &PostBody{Source: s})
			if err != nil {
				t.Fatalf("EncodeBody: %v", err)
			}
			return raw
		}
		for i, r := range []struct {
			typ  RecordType
			body []byte
		}{
			{TypePost, post("first")},
			{unknown, future},
			{TypePost, post("second")},
		} {
			log, err = AppendFrame(log, FrameHeader{
				Type: r.typ, Encoding: EncodingCBOR, Ordinal: uint32(i + 1), Revision: 1,
			}, r.body)
			if err != nil {
				t.Fatalf("AppendFrame: %v", err)
			}
		}
		return log
	}

	// replay returns the posts it read and the error that stopped it, if any.
	replay := func(log []byte) ([]string, error) {
		var got []string
		for off := 0; off < len(log); {
			h, body, size, err := ParseFrame(log[off:])
			if err != nil {
				return got, err
			}
			v, err := DecodeBody(h.Type, h.Encoding, body)
			switch {
			case SkipUnknown(err):
			case err != nil:
				return got, err
			default:
				if p, ok := v.(*PostBody); ok {
					got = append(got, p.Source)
				}
			}
			off += size
		}
		return got, nil
	}

	t.Run("advisory", func(t *testing.T) {
		got, err := replay(build(0x63)) // 99: advisory
		if err != nil {
			t.Fatalf("an advisory unknown type stopped the replay: %v", err)
		}
		if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
			t.Errorf("replay read %q, want %q", got, want)
		}
	})

	t.Run("critical", func(t *testing.T) {
		got, err := replay(build(0xe3)) // 227: same low bits, high bit set
		if err == nil {
			t.Fatal("a critical unknown type let the replay finish; it must refuse")
		}
		if !errors.Is(err, ErrCriticalRecordType) {
			t.Errorf("error = %v, want ErrCriticalRecordType", err)
		}
		if SkipUnknown(err) {
			t.Error("SkipUnknown said yes to a critical type; that is the disclosure bug")
		}
		// It wraps the general case, so a caller that only asks "did this build
		// know the type" still gets a useful yes.
		if !errors.Is(err, ErrUnknownRecordType) {
			t.Error("ErrCriticalRecordType should also satisfy ErrUnknownRecordType")
		}

		// The refusal must be the WHOLE thread. A caller that rendered what
		// came before would leak exactly what the unknown record withheld, so
		// the posts read before the stop are not a partial result to display.
		if len(got) != 1 || got[0] != "first" {
			t.Fatalf("expected the reader to have got as far as the first post, got %q", got)
		}
		t.Log("posts read before the refusal are diagnostic only; docs/03 requires " +
			"the caller to render nothing")
	})
}

// Type 0 is never assigned, so meeting one is damage rather than a newer
// writer. It must not be reported as a record from the future.
func TestTypeZeroIsDamageNotTheFuture(t *testing.T) {
	_, err := DecodeBody(0, EncodingCBOR, []byte{0xa0})
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("error = %v, want ErrMalformed", err)
	}
	if errors.Is(err, ErrUnknownRecordType) {
		t.Error("type 0 is not an unknown type, it is a corrupt one")
	}
	if SkipUnknown(err) {
		t.Error("type 0 must never be skipped")
	}
}

// The planned Redacted type from docs/10 item 4 is the first real customer for
// the critical range. This pins the shape of that future assignment: whatever
// number it gets, skipping it must be impossible.
func TestARedactionTypeWouldBeRefused(t *testing.T) {
	const redacted = RecordType(0x80 | 4) // a plausible number, not yet assigned

	if !redacted.Critical() {
		t.Fatal("a redaction record must land in the critical range")
	}
	_, err := DecodeBody(redacted, EncodingCBOR, []byte{0xa0})
	if SkipUnknown(err) {
		t.Fatal("an old build would have skipped a redaction and rendered the content " +
			"it was legally required to remove")
	}
}
