package threadlog

import (
	"fmt"
)

// Apply folds one record into an index under construction.
//
// This is the single definition of what a record does to the index, and it
// exists in one place on purpose. The index is built two ways — by full replay
// when it is missing or stale, and incrementally by the thread actor as it
// appends — and those two have to agree byte for byte or the design is unsound.
// Sharing this function makes them agree by construction rather than by two
// people implementing the same table twice.
//
// offset and frameSize describe where the record sits in the log, which the
// caller knows and this function cannot work out for itself.
//
// A record with no per-post effect — a thread rename, a pin, a tag — returns
// nil having changed nothing. That is not an oversight: the index holds one
// entry per post ordinal, and thread-level state belongs to the projection in
// the database.
func (ix *Index) Apply(h FrameHeader, body []byte, offset uint64, frameSize int) error {
	v, err := DecodeBody(h.Type, h.Encoding, body)
	if err != nil {
		// An advisory unknown type changes nothing we can see, and by the
		// docs/03 rule it is safe to ignore. A critical unknown type, damage,
		// or a bad encoding is returned and stops the replay.
		if SkipUnknown(err) {
			return nil
		}
		return err
	}

	// The lock covers the mutation and nothing else. Decoding happens above it,
	// outside, so a reader never waits on CBOR.
	ix.mu.Lock()
	defer ix.mu.Unlock()

	switch b := v.(type) {
	case *PostBody:
		return ix.applyPost(h, b, offset, frameSize)
	case *EditBody:
		return ix.applyEdit(h, b, offset, frameSize)
	case *DeleteBody:
		return ix.setFlags(h.Ordinal, EntryDeleted, 0)
	case *RestoreBody:
		return ix.setFlags(h.Ordinal, 0, EntryDeleted)
	default:
		// ThreadCreated and the thread events. No per-post effect.
		return nil
	}
}

// applyPost appends the entry for a new post.
//
// Ordinals are dense and one-based and the actor assigns them from post_count,
// so the only ordinal a Post may carry is the next one. A gap or a repeat means
// the log disagrees with itself, and continuing would silently misalign every
// entry after it — the slice position IS the ordinal.
func (ix *Index) applyPost(h FrameHeader, b *PostBody, offset uint64, frameSize int) error {
	want := uint32(len(ix.entries)) + 1
	if h.Ordinal != want {
		return fmt.Errorf("%w: Post claims ordinal %d, the index holds %d posts so the next is %d",
			ErrMalformed, h.Ordinal, len(ix.entries), want)
	}

	flags := h.Flags.ToEntryFlags()
	if len(b.Attachments) > 0 {
		flags |= EntryHasAttachments
	}
	if h.Revision > 1 {
		flags |= EntryEdited
	}

	ix.entries = append(ix.entries, IndexEntry{
		Offset:      offset,
		CreatedMS:   h.TimestampMS,
		Author:      h.Actor,
		FrameLength: uint32(frameSize),
		ReplyTo:     b.ReplyTo,
		Revision:    revisionOf(h),
		Flags:       flags,
	})
	return nil
}

// applyEdit repoints an entry at the newer revision.
//
// CreatedMS is deliberately NOT touched. It is revision 1's timestamp, so that
// "since my last visit" does not drag a four-year-old post back into view
// because somebody fixed a typo in it. Author is not touched either: an edit is
// attributed to whoever made it, but the post still belongs to its author, and
// the index answers the author lens.
func (ix *Index) applyEdit(h FrameHeader, b *EditBody, offset uint64, frameSize int) error {
	e, err := ix.entryRef(h.Ordinal, "Edit")
	if err != nil {
		return err
	}

	e.Offset = offset
	e.FrameLength = uint32(frameSize)
	e.ReplyTo = b.ReplyTo
	e.Revision = revisionOf(h)
	e.Flags |= EntryEdited
	return nil
}

// setFlags turns bits on and off in one entry.
func (ix *Index) setFlags(ordinal uint32, set, clear EntryFlags) error {
	e, err := ix.entryRef(ordinal, "record")
	if err != nil {
		return err
	}
	e.Flags = (e.Flags | set) &^ clear
	return nil
}

// entryRef returns a pointer to an existing entry so a caller can mutate it.
//
// Unexported and returning a pointer into the slice, which is safe only because
// entries are appended and never removed: an ordinal that exists keeps its
// position forever. That is also why a Delete is a flag rather than a removal.
func (ix *Index) entryRef(ordinal uint32, what string) (*IndexEntry, error) {
	if ordinal == 0 || int(ordinal) > len(ix.entries) {
		return nil, fmt.Errorf("%w: %s names ordinal %d, the index holds %d posts",
			ErrMalformed, what, ordinal, len(ix.entries))
	}
	return &ix.entries[ordinal-1], nil
}

// revisionOf narrows a frame's 32-bit revision to the index entry's 16.
//
// The widths genuinely differ in docs/03: 4 bytes in the frame, 2 in the entry.
// 65535 edits to one post is not a real thread, so the entry saturates rather
// than wrapping — a wrapped revision would read as revision 0 and break the
// cache key, while a stuck maximum only stops the "edited N times" count
// climbing.
func revisionOf(h FrameHeader) uint16 {
	if h.Revision > 0xffff {
		return 0xffff
	}
	if h.Revision == 0 {
		return 1
	}
	return uint16(h.Revision)
}
