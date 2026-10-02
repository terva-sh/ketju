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

	// EditorID is the actor of the current revision: the record the entry
	// points at. It equals Author for an unedited post, and for an edited one
	// it is whoever made the last edit, a moderator's included
	// (TKT-01M29CRJ5J). Zero when Tombstone, since no record was read.
	EditorID uint64

	// RevisionMS is when the current revision was written: the timestamp of
	// the record the entry points at. It equals CreatedMS for an unedited
	// post and is the edit time for an edited one. Zero when Tombstone, since
	// no record was read.
	RevisionMS int64

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
	return r.ReadEntries(from, r.ix.Entries(from, to))
}

// ReadEntries is Range over entries the caller already took from the index,
// the first of them for ordinal from. A reader holding a snapshot from
// [Actor.Stable] reads through it rather than the live index, so the posts are
// the revisions that snapshot names even if an edit lands in between; the log
// is append-only, so an offset once in an entry stays readable.
func (r *Reader) ReadEntries(from uint32, entries []IndexEntry) ([]Post, error) {
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

// Hidden reads a post's current revision whether or not it renders as a
// tombstone. Range never reads a tombstoned post's bytes, which is the point
// of the flags; a moderator is the one reader entitled to them (docs/07's
// viewer classes), and pays one read per such post.
func (r *Reader) Hidden(ordinal uint32) (Post, error) {
	e, err := r.ix.Entry(ordinal)
	if err != nil {
		return Post{}, err
	}
	return r.HiddenEntry(ordinal, e)
}

// HiddenEntry is Hidden for an entry the caller already holds.
//
// A redacted post is the exception: it is a tombstone to every reader,
// moderators included, and is returned as one without reading the log,
// since its bytes on disk are padding (docs/03 "Hard redaction").
func (r *Reader) HiddenEntry(ordinal uint32, e IndexEntry) (Post, error) {
	if e.Flags.Has(EntryRedacted) {
		return Post{
			Ordinal: ordinal, Revision: e.Revision, Author: e.Author,
			CreatedMS: e.CreatedMS, ReplyTo: e.ReplyTo, Flags: e.Flags,
			Tombstone: true,
		}, nil
	}
	buf := make([]byte, e.FrameLength)
	if _, err := r.log.ReadAt(buf, int64(e.Offset)); err != nil {
		return Post{}, &ReadError{Ordinal: ordinal, Offset: e.Offset, Err: err}
	}
	return decodePost(buf, ordinal, e)
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
	// An entry can be plausible and wrong: a filesystem that shows stale
	// blocks where a lost entry was can leave another post's offset there.
	// A Post or an Edit carries its post's ordinal, so the frame says whose
	// it is, and a frame for another post is never served as this one
	// (TKT-01M3VRFRFW).
	if h.Ordinal != ordinal {
		return Post{}, &ReadError{
			Ordinal: ordinal, Offset: e.Offset,
			Err: fmt.Errorf("%w: index entry %d points at the frame of ordinal %d", ErrMalformed, ordinal, h.Ordinal),
		}
	}

	p := Post{
		Ordinal: ordinal, Revision: e.Revision, Author: e.Author,
		CreatedMS: e.CreatedMS, ReplyTo: e.ReplyTo, Flags: e.Flags,
		RevisionMS: h.TimestampMS, EditorID: h.Actor,
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

// Revision is one version of a post as the log recorded it: the Post that
// created it or an Edit that replaced it.
type Revision struct {
	Revision uint32
	Actor    uint64
	TimeMS   int64
	Source   string
	// Reason is the edit's stated reason; empty for revision 1 and for an
	// edit that gave none.
	Reason string
	// Grace is an author's edit made within the grace window
	// (EditBody.Grace): a revision like any other, which did not mark the
	// post edited. A redaction keeps it (blankEdit).
	Grace bool
}

// Removals walks the log from from to upTo bytes and calls fn for each
// Delete, with its body, and each Restore, with a nil body, in log order.
// from is a frame boundary: FileHeaderSize, or a length an earlier walk or
// an append reported, so a caller can carry on from where it stopped.
//
// It is a walk of the log, as History is, for a moderator's action rather
// than a page: the index keeps a post's flags but not which Delete set
// them, and a suspension's restore needs the Delete's body
// (TKT-01M29CP2QK). A damaged frame stops the walk with its error.
func (r *Reader) Removals(from, upTo int64, fn func(ordinal uint32, del *DeleteBody)) error {
	var scanErr error
	err := ScanFramesFrom(r.log, from, upTo, func(rec ScanRecord) bool {
		h := rec.Header
		if rec.Err != nil {
			scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		switch h.Type {
		case TypeRestore:
			fn(h.Ordinal, nil)
		case TypeDelete:
			v, err := DecodeBody(h.Type, h.Encoding, rec.Body)
			if err != nil {
				scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: err}
				return false
			}
			fn(h.Ordinal, v.(*DeleteBody))
		}
		return true
	})
	if err == nil {
		err = scanErr
	}
	return err
}

// Restored reports whether the log, from from to upTo bytes, holds a Restore
// of post ordinal by actor stamped at ms: one particular restore, named by
// who wrote it and the millisecond it carries. The moderator queue asks it
// whether its own Restore landed, when the flags it dismissed for that
// Restore are still owed their audit row (TKT-01M29CRJ4E): the post being
// shown is not enough, since somebody else's restore shows it too. from is
// a frame boundary, as for Removals. A damaged frame stops the walk with its
// error.
func (r *Reader) Restored(from, upTo int64, ordinal uint32, actor uint64, ms int64) (bool, error) {
	var (
		found   bool
		scanErr error
	)
	err := ScanFramesFrom(r.log, from, upTo, func(rec ScanRecord) bool {
		h := rec.Header
		if rec.Err != nil {
			scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		found = h.Type == TypeRestore && h.Ordinal == ordinal && h.Actor == actor && h.TimestampMS == ms
		return !found
	})
	if err == nil {
		err = scanErr
	}
	return found && err == nil, err
}

// Moved reports whether the log, from from to upTo bytes, holds a
// ThreadMoved: whether the thread's forum changed in that stretch. A reader
// deciding access by a projection that has reached only from asks it of the
// rest of the log, since the index keeps each post's flags but not the
// thread's forum (Terva review 1781 on PR #323). It reads frame headers
// only. from is a frame boundary, as for Removals, or zero for the whole
// log. A damaged frame stops the walk with its error.
func (r *Reader) Moved(from, upTo int64) (bool, error) {
	from = max(from, FileHeaderSize)
	var (
		found   bool
		scanErr error
	)
	err := ScanFramesFrom(r.log, from, upTo, func(rec ScanRecord) bool {
		h := rec.Header
		if rec.Err != nil {
			scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		found = h.Type == TypeThreadMoved
		return !found
	})
	if err == nil {
		err = scanErr
	}
	return found && err == nil, err
}

// History returns every revision of a post in log order, reading the log up
// to upTo bytes.
//
// Revisions are not chained in the index or the format, which keeps only the
// current one, so this is a walk of the whole log: O(log size) per call. That
// is acceptable for a history view a person opens deliberately, and is the
// reason nothing on the page path calls it. upTo bounds the walk at a length
// the caller holds, so a revision being appended is never half-read.
func (r *Reader) History(ordinal uint32, upTo int64) ([]Revision, error) {
	if _, err := r.ix.Entry(ordinal); err != nil {
		return nil, err
	}
	var (
		out     []Revision
		scanErr error
	)
	err := ScanFramesFrom(r.log, FileHeaderSize, upTo, func(rec ScanRecord) bool {
		if rec.Err != nil {
			scanErr = &ReadError{Ordinal: ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		h := rec.Header
		if h.Ordinal != ordinal || (h.Type != TypePost && h.Type != TypeEdit) {
			return true
		}
		v, err := DecodeBody(h.Type, h.Encoding, rec.Body)
		if err != nil {
			scanErr = &ReadError{Ordinal: ordinal, Offset: uint64(rec.Offset), Err: err}
			return false
		}
		rev := Revision{Revision: h.Revision, Actor: h.Actor, TimeMS: h.TimestampMS}
		switch b := v.(type) {
		case *PostBody:
			rev.Source = b.Source
		case *EditBody:
			rev.Source, rev.Reason, rev.Grace = b.Source, b.Reason, b.Grace
		}
		out = append(out, rev)
		return true
	})
	if err == nil {
		err = scanErr
	}
	return out, err
}
