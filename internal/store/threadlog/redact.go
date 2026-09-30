package threadlog

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// Hard redaction's rewrite of one log (docs/03 "Hard redaction",
// TKT-01M29CXJZ9). The offline `ketju redact` owns the journal, the lock,
// the swap and the database; this file owns the bytes.
//
// The rule every function here keeps is that the log never gets shorter.
// The log length is the thread's version number: the ETag, the response
// cache key, the live-update offset, and what the client compares before
// it replaces a page it holds (docs/03 "Caching"). So every blanked frame
// keeps its exact length, its body padded with a "pad" key, and the new
// log is the old one plus the appended markers. Every offset an index, a
// post_meta row or projection_state holds stays valid, and each of them
// catches up by replaying only the appended frames.

// RedactOptions is what the markers a rewrite appends say.
type RedactOptions struct {
	// TimeMS is the markers' timestamp.
	TimeMS int64
	// Operator is the admin who ran the command: the markers' actor, and
	// their body's operator.
	Operator uint64
	// Reason is the operator's, required by the command.
	Reason string
	// Journal is the ULID of the redaction journal the run works from.
	Journal string
}

// Rewrite is what one rewrite did.
type Rewrite struct {
	// Revisions is, for each post redacted, the revisions whose bodies were
	// blanked, in log order. Empty for a title.
	Revisions map[uint32][]uint32
	// Blanked is how many frames were blanked.
	Blanked int
	// Size is the new log's length: the old length plus the appended
	// frames.
	Size int64
}

// ErrRedact means a log cannot be redacted as asked: it is damaged, holds a
// record this build cannot read, lacks the post named, or already carries
// the marker. Nothing the caller wrote is usable.
var ErrRedact = errors.New("threadlog: cannot redact")

// RedactPosts copies the log of size bytes in src to dst with every Post and
// Edit frame of each ordinal in ordinals blanked, then appends one Redacted
// record per ordinal, in ordinal order.
//
// Each blanked frame keeps its type, ordinal, revision, timestamp, actor and
// flags, and its body keeps what is structure rather than text: a Post's
// reply_to and origin, an Edit's reply_to and grace. Source, mentions,
// attachments and an edit's reason are gone. The body is padded to its
// original length and the frame re-checksummed, so a CRC failure still
// always means damage. Every other frame is copied byte for byte.
//
// A log that is damaged anywhere, carries a torn tail, holds a critical
// record this build does not know, lacks a named post, or already carries a
// marker for one is refused: a redaction runs on a sound log or not at all,
// so the operator runs ketju fsck first.
func RedactPosts(src io.ReaderAt, size int64, dst io.Writer, ordinals []uint32, o RedactOptions) (Rewrite, error) {
	want := map[uint32]bool{}
	for _, n := range ordinals {
		if n == 0 {
			return Rewrite{}, fmt.Errorf("%w: ordinal 0 is the thread, not a post", ErrRedact)
		}
		want[n] = true
	}
	if len(want) == 0 {
		return Rewrite{}, fmt.Errorf("%w: no post named", ErrRedact)
	}
	rw := Rewrite{Revisions: map[uint32][]uint32{}}
	seen := map[uint32]bool{}
	err := rewrite(src, size, dst, &rw, func(h FrameHeader, v any) (bool, error) {
		switch b := v.(type) {
		case *PostBody:
			if want[h.Ordinal] {
				seen[h.Ordinal] = true
			}
		case *RedactedBody:
			if !b.Title && want[b.Ordinal] {
				return false, fmt.Errorf("%w: post %d is already redacted", ErrRedact, b.Ordinal)
			}
		}
		blank := (h.Type == TypePost || h.Type == TypeEdit) && want[h.Ordinal]
		if blank {
			rw.Revisions[h.Ordinal] = append(rw.Revisions[h.Ordinal], h.Revision)
		}
		return blank, nil
	})
	if err != nil {
		return Rewrite{}, err
	}

	keys := make([]uint32, 0, len(want))
	for n := range want {
		if !seen[n] {
			return Rewrite{}, fmt.Errorf("%w: the log has no post %d", ErrRedact, n)
		}
		keys = append(keys, n)
	}
	slices.Sort(keys)
	for _, n := range keys {
		m := RedactedBody{Ordinal: n, Revisions: rw.Revisions[n], Reason: o.Reason, Operator: o.Operator, Journal: o.Journal}
		if err := appendRecord(dst, &rw, TypeRedacted, n, o, &m); err != nil {
			return Rewrite{}, err
		}
	}
	return rw, nil
}

// RedactTitle is RedactPosts for the thread's title (decided 2026-09-29,
// TKT-01M3NS147C). It blanks title and slug in the ThreadCreated frame,
// keeping forum_id and tags, and in every ThreadRenamed, each padded to its
// length. It then appends a Redacted record with ordinal 0 and title set,
// and a ThreadRenamed to replacement, both with the operator as actor, so
// the thread reads the replacement as it would after any rename.
func RedactTitle(src io.ReaderAt, size int64, dst io.Writer, replacement ThreadRenamedBody, o RedactOptions) (Rewrite, error) {
	if replacement.Title == "" {
		return Rewrite{}, fmt.Errorf("%w: the replacement title may not be empty", ErrRedact)
	}
	rw := Rewrite{}
	created := false
	err := rewrite(src, size, dst, &rw, func(h FrameHeader, v any) (bool, error) {
		switch v.(type) {
		case *ThreadCreatedBody:
			created = true
			return true, nil
		case *ThreadRenamedBody:
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return Rewrite{}, err
	}
	if !created {
		return Rewrite{}, fmt.Errorf("%w: the log has no ThreadCreated", ErrRedact)
	}
	m := RedactedBody{Title: true, Reason: o.Reason, Operator: o.Operator, Journal: o.Journal}
	if err := appendRecord(dst, &rw, TypeRedacted, 0, o, &m); err != nil {
		return Rewrite{}, err
	}
	if err := appendRecord(dst, &rw, TypeThreadRenamed, 0, o, &replacement); err != nil {
		return Rewrite{}, err
	}
	return rw, nil
}

// rewrite copies the log to dst, blanking each frame blank says to. blank
// sees every frame's header and decoded body, in log order.
func rewrite(src io.ReaderAt, size int64, dst io.Writer, rw *Rewrite, blank func(FrameHeader, any) (bool, error)) error {
	if size < FileHeaderSize {
		return fmt.Errorf("%w: %w", ErrRedact, ErrLogTooShort)
	}
	hb := make([]byte, FileHeaderSize)
	if _, err := src.ReadAt(hb, 0); err != nil {
		return fmt.Errorf("%w: reading the file header: %w", ErrRedact, err)
	}
	fh, err := ParseFileHeader(hb)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRedact, err)
	}
	if err := fh.CheckVersion(); err != nil {
		return fmt.Errorf("%w: %w", ErrRedact, err)
	}
	if _, err := dst.Write(hb); err != nil {
		return err
	}
	rw.Size = FileHeaderSize

	var walkErr error
	end := int64(FileHeaderSize)
	err = ScanFrames(src, size, func(rec ScanRecord) bool {
		if rec.Err != nil {
			walkErr = fmt.Errorf("%w: the frame at %d is damaged, so run ketju fsck first: %w", ErrRedact, rec.Offset, rec.Err)
			return false
		}
		v, err := DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body)
		if err != nil && !SkipUnknown(err) {
			walkErr = fmt.Errorf("%w: the frame at %d: %w", ErrRedact, rec.Offset, err)
			return false
		}
		out := make([]byte, rec.Size)
		if _, err := src.ReadAt(out, rec.Offset); err != nil {
			walkErr = fmt.Errorf("%w: reading the frame at %d: %w", ErrRedact, rec.Offset, err)
			return false
		}
		if v != nil {
			b, err := blank(rec.Header, v)
			if err != nil {
				walkErr = err
				return false
			}
			if b {
				body, err := BlankBody(rec.Header, rec.Body)
				if err != nil {
					walkErr = fmt.Errorf("%w: the %s at %d: %w", ErrRedact, rec.Header.Type, rec.Offset, err)
					return false
				}
				if out, err = AppendFrame(out[:0], rec.Header, body); err != nil || len(out) != rec.Size {
					walkErr = fmt.Errorf("%w: the %s at %d did not keep its length: %v", ErrRedact, rec.Header.Type, rec.Offset, err)
					return false
				}
				rw.Blanked++
			}
		}
		if _, err := dst.Write(out); err != nil {
			walkErr = err
			return false
		}
		end = rec.Offset + int64(rec.Size)
		return true
	})
	if err = errors.Join(err, walkErr); err != nil {
		return err
	}
	if end != size {
		return fmt.Errorf("%w: the log ends in %d bytes that are not a whole frame; start ketju serve once to cut the torn write, or run ketju fsck",
			ErrRedact, size-end)
	}
	rw.Size = size
	return nil
}

// appendRecord writes one CBOR frame to dst and counts it in rw.
func appendRecord(dst io.Writer, rw *Rewrite, t RecordType, ordinal uint32, o RedactOptions, body any) error {
	b, err := EncodeBody(EncodingCBOR, body)
	if err != nil {
		return err
	}
	f, err := AppendFrame(nil, FrameHeader{
		Type: t, Encoding: EncodingCBOR, Ordinal: ordinal, Revision: 1,
		TimestampMS: o.TimeMS, Actor: o.Operator,
	}, b)
	if err != nil {
		return err
	}
	if _, err := dst.Write(f); err != nil {
		return err
	}
	rw.Size += int64(len(f))
	return nil
}

// What a blanked body keeps: structure, never text.
type (
	blankPost struct {
		ReplyTo uint32      `cbor:"reply_to,omitempty" json:"reply_to,omitempty"`
		Origin  *PostOrigin `cbor:"origin,omitempty" json:"origin,omitempty"`
	}
	blankEdit struct {
		ReplyTo uint32 `cbor:"reply_to,omitempty" json:"reply_to,omitempty"`
		Grace   bool   `cbor:"grace,omitempty" json:"grace,omitempty"`
	}
	blankCreated struct {
		ForumID uint64   `cbor:"forum_id,omitempty" json:"forum_id,omitempty"`
		Tags    []string `cbor:"tags,omitempty" json:"tags,omitempty"`
	}
	blankRenamed struct{}
)

// padKey is the key a blanked body's padding sits under. No body type has
// a field by that name, so every decoder ignores it.
const padKey = "pad"

// BlankBody returns body with its text removed, exactly as long as body.
//
// A Post keeps reply_to and origin, an Edit reply_to and grace, a
// ThreadCreated forum_id and tags, and a ThreadRenamed nothing. The rest of
// the length is a "pad" key: a CBOR byte string of zeros, whose header is
// always written in its five-byte form (0x5a and a uint32) so every length
// fits, or a JSON string of '0' characters. The smallest body any writer
// produces, {source:"x"}, is 10 bytes, exactly what the padding of an empty
// CBOR map needs.
func BlankBody(h FrameHeader, body []byte) ([]byte, error) {
	v, err := DecodeBody(h.Type, h.Encoding, body)
	if err != nil {
		return nil, err
	}
	var kept any
	switch b := v.(type) {
	case *PostBody:
		kept = blankPost{ReplyTo: b.ReplyTo, Origin: b.Origin}
	case *EditBody:
		kept = blankEdit{ReplyTo: b.ReplyTo, Grace: b.Grace}
	case *ThreadCreatedBody:
		kept = blankCreated{ForumID: b.ForumID, Tags: b.Tags}
	case *ThreadRenamedBody:
		kept = blankRenamed{}
	default:
		return nil, fmt.Errorf("a %s has no text to blank", h.Type)
	}
	return padBody(h.Encoding, kept, len(body))
}

// padBody encodes kept and pads it to exactly size bytes.
func padBody(enc BodyEncoding, kept any, size int) ([]byte, error) {
	switch enc {
	case EncodingCBOR:
		k, err := cbor.Marshal(kept)
		if err != nil {
			return nil, err
		}
		// A struct with omitempty fields encodes as a definite map with its
		// pair count in the first byte; one more pair is the pad.
		if len(k) == 0 || k[0] < 0xa0 || k[0] > 0xb6 {
			return nil, fmt.Errorf("a blanked body encoded to an unexpected map header %#x", k[0])
		}
		out := make([]byte, 0, size)
		out = append(out, k[0]+1)
		out = append(out, k[1:]...)
		out = append(out, 0x60+byte(len(padKey)))
		out = append(out, padKey...)
		pad := size - len(out) - 5
		if pad < 0 {
			return nil, fmt.Errorf("a %d-byte body is too short to blank and pad", size)
		}
		out = append(out, 0x5a)
		out = binary.BigEndian.AppendUint32(out, uint32(pad))
		return append(out, make([]byte, pad)...), nil
	case EncodingJSON:
		k, err := json.Marshal(kept)
		if err != nil {
			return nil, err
		}
		prefix := string(k[:len(k)-1])
		if len(k) > 2 {
			prefix += ","
		}
		prefix += `"` + padKey + `":"`
		pad := size - len(prefix) - 2
		if pad < 0 {
			return nil, fmt.Errorf("a %d-byte body is too short to blank and pad", size)
		}
		return []byte(prefix + strings.Repeat("0", pad) + `"}`), nil
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedEncoding, uint8(enc))
	}
}

// Markers returns every Redacted record in the log's first size bytes, in
// log order. A damaged frame is an error: a resumed redaction decides from
// the markers whether a log was swapped, and must not guess.
func Markers(src io.ReaderAt, size int64) ([]RedactedBody, error) {
	var (
		out     []RedactedBody
		walkErr error
	)
	err := ScanFrames(src, size, func(rec ScanRecord) bool {
		if rec.Err != nil {
			walkErr = fmt.Errorf("the frame at %d: %w", rec.Offset, rec.Err)
			return false
		}
		if rec.Header.Type != TypeRedacted {
			return true
		}
		v, err := DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body)
		if err != nil {
			walkErr = fmt.Errorf("the frame at %d: %w", rec.Offset, err)
			return false
		}
		out = append(out, *v.(*RedactedBody))
		return true
	})
	if err = errors.Join(err, walkErr); err != nil {
		return nil, fmt.Errorf("threadlog: reading the redaction markers: %w", err)
	}
	return out, nil
}

// RedactedOrdinals is the posts the log's markers name.
func RedactedOrdinals(src io.ReaderAt, size int64) (map[uint32]bool, error) {
	ms, err := Markers(src, size)
	if err != nil {
		return nil, err
	}
	out := map[uint32]bool{}
	for _, m := range ms {
		if !m.Title {
			out[m.Ordinal] = true
		}
	}
	return out, nil
}

// AttachmentsOf reads the attachments key of a Post or Edit body. Edit has
// no such field in this build, but a body map is open to one without a
// format bump, so a redaction reads it from every revision (Terva review
// 1351). Any other type has none.
func AttachmentsOf(h FrameHeader, body []byte) ([]string, error) {
	if h.Type != TypePost && h.Type != TypeEdit {
		return nil, nil
	}
	var v struct {
		Attachments []string `cbor:"attachments,omitempty" json:"attachments,omitempty"`
	}
	switch h.Encoding {
	case EncodingCBOR:
		if err := decMode.Unmarshal(body, &v); err != nil {
			return nil, fmt.Errorf("threadlog: reading a %s's attachments: %w", h.Type, err)
		}
	case EncodingJSON:
		if err := json.Unmarshal(body, &v); err != nil {
			return nil, fmt.Errorf("threadlog: reading a %s's attachments: %w", h.Type, err)
		}
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedEncoding, uint8(h.Encoding))
	}
	return v.Attachments, nil
}
