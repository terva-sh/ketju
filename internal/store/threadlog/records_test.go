package threadlog

import (
	"errors"
	"reflect"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// everyType is the docs/03 table, transcribed by hand rather than derived from
// the catalogue. Deriving it would make the test agree with the code by
// construction and prove nothing about either.
var everyType = []struct {
	num  uint8
	name string
	typ  RecordType
	body any
}{
	{1, "ThreadCreated", TypeThreadCreated, &ThreadCreatedBody{
		Title: "Why we left Slack", Slug: "why-we-left-slack", ForumID: 7, Tags: []string{"meta", "tools"},
	}},
	{2, "Post", TypePost, &PostBody{
		Source: "The thing nobody mentions is the *export*.", ReplyTo: 3,
		Mentions: []uint64{11, 12}, Attachments: []string{"sha256:abcd"},
		Origin: &PostOrigin{ThreadID: 1 << 40, Ordinal: 4},
	}},
	{3, "Edit", TypeEdit, &EditBody{Source: "fixed a typo", Reason: "typo", ReplyTo: 3}},
	{4, "Delete", TypeDelete, &DeleteBody{Reason: "spam", Hard: false}},
	{5, "Restore", TypeRestore, &RestoreBody{Reason: "misflagged"}},
	{10, "ThreadRenamed", TypeThreadRenamed, &ThreadRenamedBody{Title: "New", Slug: "new"}},
	{11, "ThreadMoved", TypeThreadMoved, &ThreadMovedBody{ForumID: 9}},
	{12, "ThreadLocked", TypeThreadLocked, &ThreadLockedBody{Reason: "off the rails"}},
	{13, "ThreadUnlocked", TypeThreadUnlocked, &ThreadUnlockedBody{Reason: "cooled off"}},
	{14, "ThreadPinned", TypeThreadPinned, &ThreadPinnedBody{Scope: "forum", Until: 1789431429000}},
	{15, "ThreadUnpinned", TypeThreadUnpinned, &ThreadUnpinnedBody{Scope: "global"}},
	{16, "ThreadTagged", TypeThreadTagged, &ThreadTaggedBody{Tags: []string{"a", "b", "c"}}},
	{30, "ThreadArchived", TypeThreadArchived, &ThreadArchivedBody{Reason: "dormant"}},
	{0x80, "Redacted", TypeRedacted, &RedactedBody{
		Ordinal: 4, Revisions: []uint32{1, 2}, Reason: "court order 12", Operator: 3,
		Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE",
	}},
}

// Criterion: every record type in docs/03 round-trips through encode and
// decode with its documented body fields. Both encodings, because JSON is a
// supported encoding rather than a debugging afterthought.
func TestEveryRecordTypeRoundTrips(t *testing.T) {
	for _, tc := range everyType {
		for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
			t.Run(tc.name+"/"+encName(enc), func(t *testing.T) {
				raw, err := EncodeBody(enc, tc.body)
				if err != nil {
					t.Fatalf("EncodeBody: %v", err)
				}

				got, err := DecodeBody(tc.typ, enc, raw)
				if err != nil {
					t.Fatalf("DecodeBody: %v", err)
				}
				if !reflect.DeepEqual(got, tc.body) {
					t.Errorf("round trip changed the body:\n got %#v\nwant %#v", got, tc.body)
				}
			})
		}
	}
}

// Criterion: type numbers match the docs/03 table exactly.
func TestTypeNumbersMatchTheDocsTable(t *testing.T) {
	for _, tc := range everyType {
		if uint8(tc.typ) != tc.num {
			t.Errorf("%s is %d, docs/03 says %d", tc.name, uint8(tc.typ), tc.num)
		}
		if tc.typ.String() != tc.name {
			t.Errorf("RecordType(%d).String() = %q, want %q", tc.num, tc.typ.String(), tc.name)
		}
		if !tc.typ.Known() {
			t.Errorf("%s should be Known", tc.name)
		}
	}

	// The catalogue holds nothing the table above does not, so a type added to
	// the code without being added to docs/03 fails here.
	if len(catalogue) != len(everyType) {
		t.Errorf("catalogue has %d types, the docs/03 table has %d", len(catalogue), len(everyType))
	}
}

// Criterion: reserved numbers for split and merge stay unused.
func TestSplitAndMergeNumbersStayReserved(t *testing.T) {
	for _, tc := range []struct {
		num uint8
		typ RecordType
	}{{20, TypePostsSplitOut}, {21, TypePostsMergedIn}} {
		if uint8(tc.typ) != tc.num {
			t.Errorf("reserved type is %d, docs/03 says %d", uint8(tc.typ), tc.num)
		}
		if !tc.typ.Reserved() {
			t.Errorf("RecordType(%d) should report Reserved", tc.num)
		}
		if tc.typ.Known() {
			t.Errorf("RecordType(%d) is reserved and must have no body in this build", tc.num)
		}
		if _, ok := NewBody(tc.typ); ok {
			t.Errorf("NewBody(%d) returned a body for a reserved type", tc.num)
		}

		// A reserved type decodes as unknown, so a Phase 4 log replays on a
		// Phase 0 binary by skipping rather than failing.
		_, err := DecodeBody(tc.typ, EncodingCBOR, []byte{0xa0})
		if !errors.Is(err, ErrUnknownRecordType) {
			t.Errorf("DecodeBody(%d) error = %v, want ErrUnknownRecordType", tc.num, err)
		}
		if !SkipUnknown(err) {
			t.Errorf("a reserved type should be skippable, got %v", err)
		}
	}
}

// Criterion: an unknown field in a known body is ignored and the rest of the
// body still decodes.
//
// The newer body is built as a raw map rather than by adding a field to a Go
// struct, because that is what is actually on disk: a map with a key this
// build has no name for.
func TestUnknownBodyFieldIsIgnored(t *testing.T) {
	newer := map[string]any{
		"source":   "hello",
		"reply_to": uint32(4),
		// Fields a later phase might add. docs/03 says provenance detail
		// belongs in the body precisely so this can happen without a bump.
		"model":      "some-model-v9",
		"tool_calls": []any{"a", "b"},
		"nested":     map[string]any{"deep": map[string]any{"deeper": 1}},
	}

	raw, err := cbor.Marshal(newer)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := DecodeBody(TypePost, EncodingCBOR, raw)
	if err != nil {
		t.Fatalf("DecodeBody refused a body with unknown fields: %v", err)
	}

	want := &PostBody{Source: "hello", ReplyTo: 4}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("known fields did not survive an unknown one:\n got %#v\nwant %#v", got, want)
	}
}

// An unknown TYPE is a different thing from an unknown FIELD, and the errors
// must not be confusable: one is expected forward compatibility, the other
// would be damage if it wrapped ErrMalformed. This pins that they do not.
func TestUnknownRecordTypeIsNotMalformed(t *testing.T) {
	_, err := DecodeBody(RecordType(99), EncodingCBOR, []byte{0xa0})
	if !errors.Is(err, ErrUnknownRecordType) {
		t.Fatalf("error = %v, want ErrUnknownRecordType", err)
	}
	if errors.Is(err, ErrMalformed) {
		t.Error("an unknown record type must not satisfy ErrMalformed: a newer build's log is not a damaged one")
	}
	if !SkipUnknown(err) {
		t.Error("SkipUnknown should be true for an unknown type")
	}
	if SkipUnknown(nil) {
		t.Error("SkipUnknown(nil) must be false")
	}
	if SkipUnknown(ErrChecksum) {
		t.Error("a checksum failure is damage and must never be skipped")
	}
}

// Criterion: an unknown record type is skipped using the frame length and does
// not stop a replay.
//
// The log below is built with a newer build's record in the MIDDLE, because a
// skip that works at the end of a file proves nothing — the interesting claim
// is that the reader lands exactly on the next frame boundary.
func TestUnknownRecordTypeIsSkippedMidReplay(t *testing.T) {
	type rec struct {
		typ  RecordType
		enc  BodyEncoding
		body []byte
	}

	post := func(s string) []byte {
		raw, err := EncodeBody(EncodingCBOR, &PostBody{Source: s})
		if err != nil {
			t.Fatalf("EncodeBody: %v", err)
		}
		return raw
	}

	// A record from a build that knows something we do not: an unknown type,
	// and a body long enough that stepping over it by the wrong amount lands
	// in the middle of the next frame rather than plausibly near it.
	future, err := cbor.Marshal(map[string]any{
		"what": "a record type from a later phase",
		"pad":  make([]byte, 300),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	want := []rec{
		{TypeThreadCreated, EncodingCBOR, mustEncode(t, &ThreadCreatedBody{Title: "t", Slug: "t"})},
		{TypePost, EncodingCBOR, post("first")},
		{RecordType(99), EncodingCBOR, future},
		{TypePost, EncodingCBOR, post("second")},
		{TypePostsSplitOut, EncodingCBOR, future}, // reserved, also skipped
		{TypePost, EncodingJSON, mustEncodeJSON(t, &PostBody{Source: "third"})},
	}

	var log []byte
	for i, r := range want {
		log, err = AppendFrame(log, FrameHeader{
			Type: r.typ, Encoding: r.enc, Ordinal: uint32(i + 1), Revision: 1,
		}, r.body)
		if err != nil {
			t.Fatalf("AppendFrame %d: %v", i, err)
		}
	}

	// Replay, exactly as a projection rebuild would.
	var sources []string
	var skipped []RecordType
	for off := 0; off < len(log); {
		h, body, size, err := ParseFrame(log[off:])
		if err != nil {
			t.Fatalf("ParseFrame at %d: %v", off, err)
		}

		v, err := DecodeBody(h.Type, h.Encoding, body)
		switch {
		case SkipUnknown(err):
			// The whole point: advance by the frame length and carry on.
			skipped = append(skipped, h.Type)
		case err != nil:
			t.Fatalf("DecodeBody at offset %d: %v", off, err)
		default:
			if p, ok := v.(*PostBody); ok {
				sources = append(sources, p.Source)
			}
		}
		off += size
	}

	if got := []string{"first", "second", "third"}; !reflect.DeepEqual(sources, got) {
		t.Errorf("replay lost or reordered posts: got %q, want %q", sources, got)
	}
	if wantSkipped := []RecordType{RecordType(99), TypePostsSplitOut}; !reflect.DeepEqual(skipped, wantSkipped) {
		t.Errorf("skipped %v, want %v", skipped, wantSkipped)
	}
}

// A duplicate key in a body is refused rather than resolved. Accepting one
// means two builds can disagree about which value wins, which is a silent
// divergence between two replicas of the same log.
func TestDuplicateBodyKeyIsRefused(t *testing.T) {
	// {"source": "a", "source": "b"} hand-built: CBOR map of 2 pairs.
	raw := []byte{0xa2,
		0x66, 's', 'o', 'u', 'r', 'c', 'e', 0x61, 'a',
		0x66, 's', 'o', 'u', 'r', 'c', 'e', 0x61, 'b',
	}
	if _, err := DecodeBody(TypePost, EncodingCBOR, raw); err == nil {
		t.Error("a body naming the same key twice should be refused")
	}
}

func TestEncodeBodyRejectsUnknownEncoding(t *testing.T) {
	if _, err := EncodeBody(BodyEncoding(9), &PostBody{}); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Errorf("error = %v, want ErrUnsupportedEncoding", err)
	}
	if _, err := DecodeBody(TypePost, BodyEncoding(9), []byte{0xa0}); !errors.Is(err, ErrUnsupportedEncoding) {
		t.Errorf("error = %v, want ErrUnsupportedEncoding", err)
	}
}

func encName(e BodyEncoding) string {
	if e == EncodingCBOR {
		return "cbor"
	}
	return "json"
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := EncodeBody(EncodingCBOR, v)
	if err != nil {
		t.Fatalf("EncodeBody: %v", err)
	}
	return raw
}

func mustEncodeJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := EncodeBody(EncodingJSON, v)
	if err != nil {
		t.Fatalf("EncodeBody: %v", err)
	}
	return raw
}
