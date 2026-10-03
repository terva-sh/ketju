package threadlog

import (
	"encoding/hex"
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
		Continues: &PostRef{ThreadID: 1 << 40, Ordinal: 4},
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
	{20, "PostsSplitOut", TypePostsSplitOut, &PostsSplitOutBody{Ordinals: []uint32{3, 70000}, TargetThreadID: 1 << 41}},
	{21, "PostsMergedIn", TypePostsMergedIn, &PostsMergedInBody{SourceThreadID: 1 << 40, Ordinals: []uint32{3, 70000}}},
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

// Criterion (TKT-01M29CXJZZ): the split and merge records landed at the
// numbers held for them since the freeze, 20 and 21, and stay advisory, so
// a build from before Phase 4 skips them rather than refusing the thread.
func TestSplitAndMergeKeepTheirNumbers(t *testing.T) {
	for _, tc := range []struct {
		num uint8
		typ RecordType
	}{{20, TypePostsSplitOut}, {21, TypePostsMergedIn}} {
		if uint8(tc.typ) != tc.num {
			t.Errorf("%s is %d, docs/03 says %d", tc.typ, uint8(tc.typ), tc.num)
		}
		if !tc.typ.Known() {
			t.Errorf("%s has no body in this build", tc.typ)
		}
		if !tc.typ.Advisory() {
			t.Errorf("%s is %#02x, out of the advisory range", tc.typ, uint8(tc.typ))
		}
	}
}

// Criterion (TKT-01M29CXJZZ): the move bodies' key names are pinned, in CBOR
// and in JSON, by a golden: docs/03's fields, snake_case, thread ids as
// uint64 and ordinals as uint32. A renamed key is a format change, and fails
// here before it reaches a log.
func TestMoveBodiesRoundTrip(t *testing.T) {
	split := &PostsSplitOutBody{Ordinals: []uint32{2, 5, 70000}, TargetThreadID: 1<<40 | 7}
	merged := &PostsMergedInBody{SourceThreadID: 1<<40 | 3, Ordinals: []uint32{2, 5, 70000}}
	for _, c := range []struct {
		typ  RecordType
		body any
		json string
		cbor string
	}{
		{TypePostsSplitOut, split, `{"ordinals":[2,5,70000],"target_thread_id":1099511627783}`,
			"a2686f7264696e616c738302051a00011170707461726765745f7468726561645f69641b0000010000000007"},
		{TypePostsMergedIn, merged, `{"source_thread_id":1099511627779,"ordinals":[2,5,70000]}`,
			"a270736f757263655f7468726561645f69641b0000010000000003686f7264696e616c738302051a00011170"},
	} {
		for _, enc := range []BodyEncoding{EncodingCBOR, EncodingJSON} {
			raw, err := EncodeBody(enc, c.body)
			if err != nil {
				t.Fatal(err)
			}
			want := c.json
			got := string(raw)
			if enc == EncodingCBOR {
				want, got = c.cbor, hex.EncodeToString(raw)
			}
			if got != want {
				t.Errorf("%s %s body is %s, the golden is %s", c.typ, encName(enc), got, want)
			}
			back, err := DecodeBody(c.typ, enc, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(back, c.body) {
				t.Errorf("%s %s round trip gave %#v", c.typ, encName(enc), back)
			}
		}
	}
}

// TestMoveBodyMalformed: CheckMove refuses an empty move, ordinal 0, an
// ordinal named twice, a move over the cap, thread 0 and the record's own
// thread, for both records, and takes a body as a pointer or a value.
func TestMoveBodyMalformed(t *testing.T) {
	const self = 9
	many := make([]uint32, MaxMoveOrdinals+1)
	for i := range many {
		many[i] = uint32(i + 1)
	}
	for _, c := range []struct {
		name string
		ords []uint32
		id   uint64
		ok   bool
	}{
		{"a move", []uint32{3, 1, 2}, 4, true},
		{"the cap", many[:MaxMoveOrdinals], 4, true},
		{"no post", nil, 4, false},
		{"ordinal 0", []uint32{1, 0}, 4, false},
		{"a repeat", []uint32{2, 3, 2}, 4, false},
		{"over the cap", many, 4, false},
		{"thread 0", []uint32{1}, 0, false},
		{"its own thread", []uint32{1}, self, false},
	} {
		for _, body := range []any{
			&PostsSplitOutBody{Ordinals: c.ords, TargetThreadID: c.id},
			PostsSplitOutBody{Ordinals: c.ords, TargetThreadID: c.id},
			&PostsMergedInBody{SourceThreadID: c.id, Ordinals: c.ords},
			PostsMergedInBody{SourceThreadID: c.id, Ordinals: c.ords},
		} {
			err := CheckMove(body, self)
			if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrMalformed)) {
				t.Errorf("%s, %T: %v, want ok=%v", c.name, body, err, c.ok)
			}
		}
	}
	if err := CheckMove(&PostBody{}, self); !errors.Is(err, ErrMalformed) {
		t.Errorf("a Post body: %v, want ErrMalformed", err)
	}
}

// TestMoveRecordFitsAtCap: a move of MaxMoveOrdinals posts, every ordinal
// above 65536, encodes far inside MaxFrameSize; and a split of 209000 such
// ordinals, docs/03's ceiling for one record, still fits.
func TestMoveRecordFitsAtCap(t *testing.T) {
	for _, n := range []int{MaxMoveOrdinals, 209000} {
		ords := make([]uint32, n)
		for i := range ords {
			ords[i] = uint32(70000 + i)
		}
		body, err := EncodeBody(EncodingCBOR, &PostsSplitOutBody{Ordinals: ords, TargetThreadID: 1 << 60})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AppendFrame(nil, FrameHeader{Type: TypePostsSplitOut, Encoding: EncodingCBOR, Revision: 1}, body); err != nil {
			t.Errorf("a split of %d ordinals: %v", n, err)
		}
		if n == MaxMoveOrdinals && len(body)*4 > MaxFrameSize {
			t.Errorf("a split at the cap is %d bytes, more than a quarter of MaxFrameSize", len(body))
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
		{RecordType(22), EncodingCBOR, future}, // unassigned and advisory, also skipped
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
	if wantSkipped := []RecordType{RecordType(99), RecordType(22)}; !reflect.DeepEqual(skipped, wantSkipped) {
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
