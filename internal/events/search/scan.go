package search

import (
	"errors"
	"fmt"
	"io"

	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// errNotABoundary is a scan whose first frame could not be read: the offset
// it started at is not where a frame begins.
var errNotABoundary = errors.New("search: the offset is not a frame boundary")

// creator is the thread's ThreadCreated: who started it and when, which the
// title row carries.
type creator struct {
	actor uint64
	ms    int64
}

// scan is what a walk over a range of frames collected.
type scan struct {
	// touched are the ordinals of records that can change a post's text or
	// visibility.
	touched map[uint32]bool

	// title is the last title the range set, when titleSeen. A title
	// Redacted record sets it empty; the ThreadRenamed after it sets the
	// replacement.
	titleSeen bool
	title     string

	creatorSeen bool
	creator     creator
}

// scanFrames walks the frames in [from, upTo) of a log and collects what the
// index needs from them.
//
// The first frame failing to parse, when from is not the file header's end,
// is errNotABoundary: a recorded length inside a frame reads a length field
// out of the middle of a record, which fails its checksum or runs off the
// end. The caller then indexes the thread from its first frame (Terva review
// 1728). Damage anywhere else is an error, never skipped: indexing past it
// would index whatever follows as if the damaged record had not happened.
func scanFrames(r interface {
	ReadAt(p []byte, off int64) (int, error)
}, from, upTo int64) (scan, error) {
	s := scan{touched: map[uint32]bool{}}
	var scanErr error
	first := true
	err := threadlog.ScanFramesFrom(r, from, upTo, func(rec threadlog.ScanRecord) bool {
		atStart := first
		first = false
		if rec.Err != nil {
			if atStart && from != threadlog.FileHeaderSize {
				scanErr = fmt.Errorf("%w: %d: %w", errNotABoundary, from, rec.Err)
			} else {
				scanErr = fmt.Errorf("frame at offset %d: %w", rec.Offset, rec.Err)
			}
			return false
		}
		body, err := threadlog.DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body)
		if err != nil {
			if threadlog.SkipUnknown(err) {
				return true // unknown and advisory: stepped over, as the index does
			}
			scanErr = fmt.Errorf("frame at offset %d: %w", rec.Offset, err)
			return false
		}
		d, ok := decisions[rec.Header.Type]
		if !ok {
			// TestEveryRecordTypeHasAnIndexDecision should have caught it;
			// refusing here keeps it from being skipped quietly.
			scanErr = fmt.Errorf("frame at offset %d: %s has no index decision", rec.Offset, rec.Header.Type)
			return false
		}
		switch d.effect {
		case touchesOrdinal:
			if rb, ok := body.(*threadlog.RedactedBody); ok && rb.Title {
				s.titleSeen, s.title = true, ""
				return true
			}
			if rec.Header.Ordinal == 0 {
				scanErr = fmt.Errorf("frame at offset %d: a %s carries ordinal 0", rec.Offset, rec.Header.Type)
				return false
			}
			s.touched[rec.Header.Ordinal] = true
		case touchesOrdinals:
			b, ok := body.(*threadlog.PostsSplitOutBody)
			if !ok {
				scanErr = fmt.Errorf("frame at offset %d: a %s names no ordinals the indexer can read",
					rec.Offset, rec.Header.Type)
				return false
			}
			for _, o := range b.Ordinals {
				if o == 0 {
					scanErr = fmt.Errorf("frame at offset %d: a %s names ordinal 0", rec.Offset, rec.Header.Type)
					return false
				}
				s.touched[o] = true
			}
		case setsTitle:
			switch b := body.(type) {
			case *threadlog.ThreadCreatedBody:
				s.title = b.Title
				s.creatorSeen = true
				s.creator = creator{actor: rec.Header.Actor, ms: rec.Header.TimestampMS}
			case *threadlog.ThreadRenamedBody:
				s.title = b.Title
			}
			s.titleSeen = true
		}
		return true
	})
	if err := errors.Join(err, scanErr); err != nil {
		if errors.Is(scanErr, errNotABoundary) {
			return s, scanErr
		}
		return s, err
	}
	return s, nil
}

// firstFrame reads the log's first frame, which is its ThreadCreated, for
// the title row of a range that renamed the thread without creating it.
func firstFrame(r interface {
	ReadAt(p []byte, off int64) (int, error)
}, upTo int64) (creator, error) {
	var c creator
	var found error = errors.New("the log has no ThreadCreated frame")
	err := threadlog.ScanFramesFrom(r, threadlog.FileHeaderSize, upTo, func(rec threadlog.ScanRecord) bool {
		switch {
		case rec.Err != nil:
			found = fmt.Errorf("reading the first frame: %w", rec.Err)
		case rec.Header.Type != threadlog.TypeThreadCreated:
			found = fmt.Errorf("the first frame is a %s, not a ThreadCreated", rec.Header.Type)
		default:
			c, found = creator{actor: rec.Header.Actor, ms: rec.Header.TimestampMS}, nil
		}
		return false
	})
	return c, errors.Join(err, found)
}

// source is one thread as the indexer reads it: its log, opened read-only
// for scanning, and an index to take entries from.
type source struct {
	f      *readRecorder
	length int64 // the log's whole frames when the source was opened

	// One of these: the thread as the cache has it open, held by a Peek
	// reference until close, or the thread opened here from its files.
	cache *threadcache.Cache
	th    *threadcache.Thread
	rec   *threadlog.Recovery
}

// readRecorder is a log whose reads remember the first error that is not
// the end of the file. Such an error is the disk's or the filesystem's,
// never the log's bytes: threadlog reports what a failed read left it as if
// the log were damaged, so a fault found after one is not the log's (Terva
// review 1766). An end of file is the log's: it is how a log cut short
// reads.
type readRecorder struct {
	logFile
	err error
}

func (r *readRecorder) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.logFile.ReadAt(p, off)
	if err != nil && r.err == nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		r.err = err
	}
	return n, err
}

// fault classifies err, a failure found reading the log through r: a
// logFault, unless a read failed for a reason other than the log's bytes,
// in which case it is that read failure and not the log's.
func (r *readRecorder) fault(err error) error {
	if r.err != nil {
		return fmt.Errorf("%w (reading the log failed: %w)", err, r.err)
	}
	return logFault{err}
}

// fault classifies a failure found reading the source's log (readRecorder).
// A thread open in the cache is read through the cache's own file, whose
// reads are not recorded; Rebuild, the one caller that tells the two kinds
// apart, never reads through the cache.
func (s *source) fault(err error) error { return s.f.fault(err) }

// source opens thread id for indexing. A thread the cache has open is read
// from its index in memory, and stays open under a Peek reference until
// close; one that is not is opened from its log and sidecar index, both
// read-only, and nothing is written back, the .idx included. The cache is
// never asked to open a thread, so indexing does not evict one a reader has
// open.
//
// fromLog opens the thread from its log alone, as Rebuild does: the cache is
// not peeked, the sidecar index is not read, and the index is built in
// memory by replaying every frame, so a stale or damaged .idx cannot reach
// the rebuilt index.
func (x *Indexer) source(id threadid.ID, fromLog bool) (*source, error) {
	opened, err := x.open(id.LogPath(x.root))
	if err != nil {
		return nil, err
	}
	f := &readRecorder{logFile: opened}
	src := &source{f: f, cache: x.threads}
	if err := checkHeader(f, id); err != nil {
		f.Close()
		return nil, f.fault(err)
	}
	if x.threads != nil && !fromLog {
		if th, ok := x.threads.Peek(id); ok {
			src.th = th
			src.length = th.Actor.LogLength()
			return src, nil
		}
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	var idx []byte
	if !fromLog {
		if idx, err = x.readIndex(id.IndexPath(x.root)); err != nil {
			f.Close()
			return nil, err
		}
	}
	rec, err := threadlog.Open(f, fi.Size(), idx)
	if err != nil {
		f.Close()
		return nil, f.fault(err)
	}
	src.rec, src.length = rec, rec.LogLength
	return src, nil
}

// entries reads the index entries for ordinals, in order, and the log length
// they describe. A thread open in the cache is read inside one Actor.Stable
// pass, so every entry is the log at that one length.
func (s *source) entries(ordinals []uint32) ([]threadlog.IndexEntry, int64, error) {
	out := make([]threadlog.IndexEntry, len(ordinals))
	var length int64
	var err error
	read := func(ix *threadlog.Index) {
		for i, o := range ordinals {
			if out[i], err = ix.Entry(o); err != nil {
				return
			}
		}
	}
	if s.th != nil {
		s.th.Actor.Stable(func(l int64) {
			length = l
			read(s.th.Index)
		})
	} else {
		length = s.rec.LogLength
		read(s.rec.Index)
	}
	return out, length, err
}

// text reads the current revision's source of a visible post.
func (s *source) text(ordinal uint32, e threadlog.IndexEntry) (string, error) {
	var r *threadlog.Reader
	if s.th != nil {
		r = s.th.Reader
	} else {
		r = threadlog.NewReader(s.f, s.rec.Index)
	}
	posts, err := r.ReadEntries(ordinal, []threadlog.IndexEntry{e})
	if err != nil {
		return "", err
	}
	if len(posts) != 1 || posts[0].Tombstone {
		return "", fmt.Errorf("post %d read as a tombstone", ordinal)
	}
	return posts[0].Source, nil
}

func (s *source) close() {
	s.f.Close()
	if s.th != nil {
		s.cache.Release(s.th)
	}
}

// checkHeader reads the log's file header and refuses a damaged one, a
// version this build cannot read, or a log that names another thread, which
// would index its posts under this thread's id.
func checkHeader(r logFile, id threadid.ID) error {
	b := make([]byte, threadlog.FileHeaderSize)
	if _, err := r.ReadAt(b, 0); err != nil {
		return fmt.Errorf("reading the file header: %w", err)
	}
	h, err := threadlog.ParseFileHeader(b)
	if err != nil {
		return err
	}
	if err := h.CheckVersion(); err != nil {
		return err
	}
	if h.ThreadID != uint64(id) {
		return fmt.Errorf("the log at thread %s's path names thread %s", id, threadid.ID(h.ThreadID))
	}
	return nil
}
