package threadlog

import (
	"fmt"
	"io"
)

// Reader reads posts back out of a thread.
//
// It never touches the actor's channel. os.File.ReadAt is safe for concurrent
// use, so a read needs nothing from the writer but the index, and queueing
// readers behind a writer would make a busy thread slow to read — which is the
// opposite of what a message board wants, since reads outnumber writes by
// orders of magnitude.
//
// The only synchronisation a reader takes is the index's read lock, held for
// the microseconds it takes to copy a page of entries and never across I/O.
type Reader struct {
	log io.ReaderAt
	ix  *Index
}

// NewReader returns a reader over a thread's log and index.
func NewReader(log io.ReaderAt, ix *Index) *Reader {
	return &Reader{log: log, ix: ix}
}

// Post is one record read back from the log.
//
// A tombstoned or moved post arrives with Tombstone set and Body nil, having
// cost no log read at all. The entry said everything the renderer needs.
type Post struct {
	Ordinal   uint32
	Revision  uint16
	Author    uint64
	CreatedMS int64
	ReplyTo   uint32
	Flags     EntryFlags

	// Source is the markdown of the current revision. Empty when Tombstone.
	Source string

	// Attachments and Mentions come from the record, not the index, so they
	// are present only when the body was actually read.
	Attachments []string
	Mentions    []uint64

	Tombstone bool
}

// ReadError names the ordinal that failed, because "checksum mismatch" without
// one tells an operator nothing they can act on and a thread with a single bad
// record should not read as a thread that is entirely unreadable.
type ReadError struct {
	Ordinal uint32
	Offset  uint64
	Err     error
}

func (e *ReadError) Error() string {
	return fmt.Sprintf("threadlog: reading ordinal %d at offset %d: %v", e.Ordinal, e.Offset, e.Err)
}

func (e *ReadError) Unwrap() error { return e.Err }

// Range returns the posts for ordinals from..to inclusive, clamped to the
// thread.
//
// Consecutive entries that are contiguous on disk are fetched in ONE ReadAt and
// sliced apart, which is the common case for a page of unedited posts: 25
// syscalls become one. An edited post's current revision lives wherever that
// edit was appended, so it breaks the run and pays its own read — which is
// correct, not a shortcoming, since it genuinely is somewhere else.
func (r *Reader) Range(from, to uint32) ([]Post, error) {
	entries := r.ix.Entries(from, to)
	if len(entries) == 0 {
		return nil, nil
	}
	first := from
	if first == 0 {
		first = 1
	}

	out := make([]Post, 0, len(entries))
	for i := 0; i < len(entries); {
		e := entries[i]
		ordinal := first + uint32(i)

		// A post that renders as a tombstone or a pointer never reaches the
		// log. This is the property the 40-byte entry was widened for.
		if !e.Flags.Visible() {
			out = append(out, Post{
				Ordinal: ordinal, Revision: e.Revision, Author: e.Author,
				CreatedMS: e.CreatedMS, ReplyTo: e.ReplyTo, Flags: e.Flags,
				Tombstone: true,
			})
			i++
			continue
		}

		// How far does the contiguous run of visible entries reach?
		runEnd := i + 1
		span := int64(e.FrameLength)
		for runEnd < len(entries) {
			next := entries[runEnd]
			if !next.Flags.Visible() || next.Offset != e.Offset+uint64(span) {
				break
			}
			span += int64(next.FrameLength)
			runEnd++
		}

		buf := make([]byte, span)
		if _, err := r.log.ReadAt(buf, int64(e.Offset)); err != nil {
			return nil, &ReadError{Ordinal: ordinal, Offset: e.Offset, Err: err}
		}

		at := 0
		for j := i; j < runEnd; j++ {
			ent := entries[j]
			p, err := decodePost(buf[at:at+int(ent.FrameLength)], first+uint32(j), ent)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
			at += int(ent.FrameLength)
		}
		i = runEnd
	}

	return out, nil
}

// One reads a single post.
func (r *Reader) One(ordinal uint32) (Post, error) {
	posts, err := r.Range(ordinal, ordinal)
	if err != nil {
		return Post{}, err
	}
	if len(posts) == 0 {
		return Post{}, fmt.Errorf("%w: ordinal %d", ErrNoSuchOrdinal, ordinal)
	}
	return posts[0], nil
}

// decodePost turns one frame into a Post.
//
// The record at a post's offset is a Post for an unedited one and an Edit for
// an edited one, because the entry points at the CURRENT revision. Both carry
// the source, so both are accepted; anything else means the index and the log
// disagree about what lives at that offset, which is a bug rather than damage
// and is reported as such.
func decodePost(frame []byte, ordinal uint32, e IndexEntry) (Post, error) {
	h, body, _, err := ParseFrame(frame)
	if err != nil {
		return Post{}, &ReadError{Ordinal: ordinal, Offset: e.Offset, Err: err}
	}

	p := Post{
		Ordinal: ordinal, Revision: e.Revision, Author: e.Author,
		CreatedMS: e.CreatedMS, ReplyTo: e.ReplyTo, Flags: e.Flags,
	}

	v, err := DecodeBody(h.Type, h.Encoding, body)
	if err != nil {
		return Post{}, &ReadError{Ordinal: ordinal, Offset: e.Offset, Err: err}
	}

	switch b := v.(type) {
	case *PostBody:
		p.Source, p.Attachments, p.Mentions = b.Source, b.Attachments, b.Mentions
	case *EditBody:
		p.Source = b.Source
	default:
		return Post{}, &ReadError{
			Ordinal: ordinal, Offset: e.Offset,
			Err: fmt.Errorf("%w: index points at a %s record", ErrMalformed, h.Type),
		}
	}
	return p, nil
}
