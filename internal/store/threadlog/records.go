package threadlog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// Record types, matching the docs/03 table exactly. The numbers are on disk in
// every log ever written, so they are assigned once and never reused — a type
// that is retired leaves its number burned rather than handing it to something
// else and silently changing the meaning of old records.
//
// The gaps are deliberate. 1-5 are post lifecycle, 10-16 are thread events,
// 20-21 are split and merge, and 30 is archival. Leaving room inside each band
// means a new thread event does not have to be filed after the archival record
// just because that is where the next free number happens to be.
const (
	TypeThreadCreated  RecordType = 1
	TypePost           RecordType = 2
	TypeEdit           RecordType = 3
	TypeDelete         RecordType = 4
	TypeRestore        RecordType = 5
	TypeThreadRenamed  RecordType = 10
	TypeThreadMoved    RecordType = 11
	TypeThreadLocked   RecordType = 12
	TypeThreadUnlocked RecordType = 13
	TypeThreadPinned   RecordType = 14
	TypeThreadUnpinned RecordType = 15
	TypeThreadTagged   RecordType = 16

	// TypePostsSplitOut and TypePostsMergedIn are RESERVED and carry no body
	// in this build. Split and merge are Phase 4 (docs/03, "Split and merge
	// are the hardest events").
	//
	// They are declared now, and deliberately left out of the catalogue, so
	// that the numbers cannot be handed to some other record in the meantime.
	// A constant is cheaper to hold than a format version bump.
	TypePostsSplitOut RecordType = 20
	TypePostsMergedIn RecordType = 21

	TypeThreadArchived RecordType = 30
)

// Bodies.
//
// Every body is a self-describing map on disk: string keys, not an array of
// positional fields. That is what lets a field be added without a format bump,
// and it is why an unknown field is ignored rather than fatal — an older build
// reading a newer log simply does not see the fields it has no name for.
//
// The cbor and json tags are kept identical on purpose. Encoding 2 exists for
// `ketju dump` and debugging, and a dump whose field names differ from the
// bytes it came from is a worse debugging tool than no dump at all.
//
// omitempty throughout: a zero field costs nothing on disk and reads back as
// the zero value either way.
type (
	// ThreadCreatedBody opens a log. Exactly one per thread, always ordinal 0.
	ThreadCreatedBody struct {
		Title   string   `cbor:"title,omitempty" json:"title,omitempty"`
		Slug    string   `cbor:"slug,omitempty" json:"slug,omitempty"`
		ForumID uint64   `cbor:"forum_id,omitempty" json:"forum_id,omitempty"`
		Tags    []string `cbor:"tags,omitempty" json:"tags,omitempty"`
	}

	// PostBody is a new post. Source is markdown; the rendered HTML is a cache
	// entry, never a log record, because the renderer version changes and the
	// source does not.
	//
	// Mentions and Attachments are extracted at write time rather than
	// re-derived on every read. Their element shapes are the least settled
	// thing in this file, and they are also the cheapest to change: a
	// self-describing body means a new field can arrive beside an old one.
	PostBody struct {
		Source      string   `cbor:"source,omitempty" json:"source,omitempty"`
		ReplyTo     uint32   `cbor:"reply_to,omitempty" json:"reply_to,omitempty"`
		Mentions    []uint64 `cbor:"mentions,omitempty" json:"mentions,omitempty"`
		Attachments []string `cbor:"attachments,omitempty" json:"attachments,omitempty"`
	}

	// EditBody replaces the source of an existing post. The original Post
	// record's bytes stay on disk untouched; the index points at the newest
	// revision and the older ones remain readable for history.
	EditBody struct {
		Source  string `cbor:"source,omitempty" json:"source,omitempty"`
		Reason  string `cbor:"reason,omitempty" json:"reason,omitempty"`
		ReplyTo uint32 `cbor:"reply_to,omitempty" json:"reply_to,omitempty"`
	}

	// DeleteBody soft-deletes a post. The body stays on disk and the reader
	// returns a tombstone from the index flags without reading it.
	//
	// Hard is always false in v1. Hard deletion is an offline command that
	// rewrites the log, which is the one operation in this design that is not
	// append-only; the field exists so that a log carrying one is
	// distinguishable rather than ambiguous.
	DeleteBody struct {
		Reason string `cbor:"reason,omitempty" json:"reason,omitempty"`
		Hard   bool   `cbor:"hard,omitempty" json:"hard,omitempty"`
	}

	// RestoreBody clears a soft deletion.
	RestoreBody struct {
		Reason string `cbor:"reason,omitempty" json:"reason,omitempty"`
	}

	// ThreadRenamedBody changes title and slug. Old slugs stay valid as
	// redirects; the permalink is the id.
	ThreadRenamedBody struct {
		Title string `cbor:"title,omitempty" json:"title,omitempty"`
		Slug  string `cbor:"slug,omitempty" json:"slug,omitempty"`
	}

	// ThreadMovedBody moves a thread between forums. The file header's forum
	// id is informational and is not rewritten; the move is this record.
	ThreadMovedBody struct {
		ForumID uint64 `cbor:"forum_id,omitempty" json:"forum_id,omitempty"`
	}

	// ThreadLockedBody and ThreadUnlockedBody are separate types carrying the
	// same field. Sharing one Go type would save four lines and lose the
	// property that a decoded body names what happened.
	ThreadLockedBody struct {
		Reason string `cbor:"reason,omitempty" json:"reason,omitempty"`
	}

	// ThreadUnlockedBody reopens a locked thread.
	ThreadUnlockedBody struct {
		Reason string `cbor:"reason,omitempty" json:"reason,omitempty"`
	}

	// ThreadPinnedBody pins a thread. Scope is "forum" or "global". Until is
	// unix ms, zero meaning indefinitely.
	ThreadPinnedBody struct {
		Scope string `cbor:"scope,omitempty" json:"scope,omitempty"`
		Until int64  `cbor:"until,omitempty" json:"until,omitempty"`
	}

	// ThreadUnpinnedBody unpins a thread. Scope says which pin was removed, so
	// that clearing a forum pin does not silently clear a global one.
	ThreadUnpinnedBody struct {
		Scope string `cbor:"scope,omitempty" json:"scope,omitempty"`
		Until int64  `cbor:"until,omitempty" json:"until,omitempty"`
	}

	// ThreadTaggedBody replaces the tag set wholesale rather than describing a
	// delta. Replaying a delta requires every earlier record to have been
	// applied correctly; replaying a replacement does not.
	ThreadTaggedBody struct {
		Tags []string `cbor:"tags,omitempty" json:"tags,omitempty"`
	}

	// ThreadArchivedBody makes a thread read-only. Its index may be dropped
	// from memory permanently.
	ThreadArchivedBody struct {
		Reason string `cbor:"reason,omitempty" json:"reason,omitempty"`
	}
)

// catalogue maps a record type to a constructor for its body.
//
// Membership here is the definition of "known". A type absent from this map is
// one a walker skips by frame length, which is exactly what an older binary
// does when it meets a record a newer one wrote — so the reserved split and
// merge numbers are absent on purpose, not by oversight.
var catalogue = map[RecordType]func() any{
	TypeThreadCreated:  func() any { return new(ThreadCreatedBody) },
	TypePost:           func() any { return new(PostBody) },
	TypeEdit:           func() any { return new(EditBody) },
	TypeDelete:         func() any { return new(DeleteBody) },
	TypeRestore:        func() any { return new(RestoreBody) },
	TypeThreadRenamed:  func() any { return new(ThreadRenamedBody) },
	TypeThreadMoved:    func() any { return new(ThreadMovedBody) },
	TypeThreadLocked:   func() any { return new(ThreadLockedBody) },
	TypeThreadUnlocked: func() any { return new(ThreadUnlockedBody) },
	TypeThreadPinned:   func() any { return new(ThreadPinnedBody) },
	TypeThreadUnpinned: func() any { return new(ThreadUnpinnedBody) },
	TypeThreadTagged:   func() any { return new(ThreadTaggedBody) },
	TypeThreadArchived: func() any { return new(ThreadArchivedBody) },
}

// typeNames is for errors and `ketju dump`. Reserved types are named here
// although they have no body, so a log containing one reports what it met
// rather than a bare number.
var typeNames = map[RecordType]string{
	TypeThreadCreated:  "ThreadCreated",
	TypePost:           "Post",
	TypeEdit:           "Edit",
	TypeDelete:         "Delete",
	TypeRestore:        "Restore",
	TypeThreadRenamed:  "ThreadRenamed",
	TypeThreadMoved:    "ThreadMoved",
	TypeThreadLocked:   "ThreadLocked",
	TypeThreadUnlocked: "ThreadUnlocked",
	TypeThreadPinned:   "ThreadPinned",
	TypeThreadUnpinned: "ThreadUnpinned",
	TypeThreadTagged:   "ThreadTagged",
	TypePostsSplitOut:  "PostsSplitOut",
	TypePostsMergedIn:  "PostsMergedIn",
	TypeThreadArchived: "ThreadArchived",
}

// String names a record type, falling back to the number for one this build
// does not know.
func (t RecordType) String() string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("RecordType(%d)", uint8(t))
}

// Known reports whether this build has a body for t.
//
// A walker uses this to decide between decoding a record and stepping over it
// by frame length. Reserved types report false: the number is spoken for, but
// there is nothing here that can read one.
func (t RecordType) Known() bool {
	_, ok := catalogue[t]
	return ok
}

// Reserved reports whether t is a number held for a record this build does not
// implement yet.
func (t RecordType) Reserved() bool {
	return t == TypePostsSplitOut || t == TypePostsMergedIn
}

// decMode bounds what a body decoder will do with a length it read off disk.
//
// The frame parser already refuses an impossible frame length, so these limits
// guard the layer below it: a body whose bytes are intact enough to parse but
// which describes a map with a billion pairs. The numbers are far above any
// legitimate body — docs/07 caps a post at 64 KB — and far below anything that
// would exhaust memory.
//
// DupMapKeyEnforcedAPF rejects a body that names the same key twice. Accepting
// one means two builds can disagree about which value wins, which is a
// divergence between replicas of the same log.
var decMode = func() cbor.DecMode {
	m, err := cbor.DecOptions{
		MaxArrayElements: 1 << 16,
		MaxMapPairs:      1 << 16,
		MaxNestedLevels:  32,
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthAllowed,
	}.DecMode()
	if err != nil {
		panic("threadlog: bad CBOR decoder options: " + err.Error())
	}
	return m
}()

// ErrUnknownRecordType means a record's type has no body in this build. It is
// not damage: it is what an older binary reports when it meets a record a
// newer one wrote, and the caller's correct response is to skip the frame by
// its length and carry on.
//
// Deliberately NOT wrapping [ErrMalformed]. A caller that treats malformed as
// damage — stop the replay, report corruption — would otherwise be told a log
// is broken because it was written by a newer build, which is the exact
// failure this format's forward compatibility exists to prevent.
var ErrUnknownRecordType = errors.New("threadlog: unknown record type")

// ErrUnsupportedEncoding means the body encoding byte names something this
// build cannot decode.
//
// Also not [ErrMalformed], and for the same reason: the encoding byte lives in
// the CRC-covered fixed header, so a value this build does not recognise is far
// more likely to be a third encoding added later than a flipped bit.
var ErrUnsupportedEncoding = errors.New("threadlog: unsupported body encoding")

// EncodeBody encodes a body for the frame's encoding byte.
//
// CBOR is what writers use; JSON is for dumps and debugging. Encoding is
// chosen per record rather than per file so a log can carry both, which is
// what makes `ketju dump --json` able to write a readable log rather than a
// separate sidecar.
func EncodeBody(enc BodyEncoding, v any) ([]byte, error) {
	switch enc {
	case EncodingCBOR:
		return cbor.Marshal(v)
	case EncodingJSON:
		return json.Marshal(v)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedEncoding, uint8(enc))
	}
}

// NewBody returns an empty body for t, or false if this build has no body for
// that type.
func NewBody(t RecordType) (any, bool) {
	f, ok := catalogue[t]
	if !ok {
		return nil, false
	}
	return f(), true
}

// DecodeBody decodes raw into the body for t and returns a pointer to it.
//
// A field in raw that this build has no name for is ignored and the rest of
// the body still decodes, which is the forward-compatibility property the
// self-describing map exists to provide.
//
// An unknown or reserved type returns [ErrUnknownRecordType] without touching
// raw. That error is a signal to skip the frame, not a reason to abandon a
// replay — see [SkipUnknown].
func DecodeBody(t RecordType, enc BodyEncoding, raw []byte) (any, error) {
	v, ok := NewBody(t)
	if !ok {
		if t.Reserved() {
			return nil, fmt.Errorf("%w: %s is reserved for a later phase", ErrUnknownRecordType, t)
		}
		return nil, fmt.Errorf("%w: %d", ErrUnknownRecordType, uint8(t))
	}

	switch enc {
	case EncodingCBOR:
		if err := decMode.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("threadlog: decoding %s body: %w", t, err)
		}
	case EncodingJSON:
		if err := json.Unmarshal(raw, v); err != nil {
			return nil, fmt.Errorf("threadlog: decoding %s body: %w", t, err)
		}
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedEncoding, uint8(enc))
	}

	return v, nil
}

// SkipUnknown reports whether err is the kind a replay steps over rather than
// stops on.
//
// The distinction matters because the two look alike at the call site and
// behave nothing alike: a record this build cannot name is expected and
// survivable, while a checksum failure is damage and must stop the replay
// before it projects corrupt state.
func SkipUnknown(err error) bool {
	return errors.Is(err, ErrUnknownRecordType)
}
