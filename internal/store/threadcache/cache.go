// Package threadcache keeps a bounded set of threads open.
//
// A thread costs two file descriptors and a decoded index — about 40 bytes per
// post — so holding every thread open does not scale, and opening one per
// request throws away the index on every read. The cache is the middle: the
// hot threads stay open, the cold ones are closed and reopened cheaply because
// closing flushes the index header and reopening therefore trusts it.
//
// Nothing is opened until it is asked for, so startup does not scale with the
// number of threads on disk.
package threadcache

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// DefaultBound is how many threads stay open when nothing says otherwise.
//
// 512 threads is roughly 1024 descriptors plus the indexes; a board with more
// than 512 threads in flight at once is not a board a friend group is holding a
// conversation on.
const DefaultBound = 512

// Options configures a cache. The zero value is usable and means a bound of
// DefaultBound, group commit, and no projection, so each log is walked for a
// ThreadArchived when its thread is opened.
type Options struct {
	// Bound is the maximum number of threads held open. Zero means
	// DefaultBound. Negative is refused.
	Bound int

	// Sync is the durability policy handed to each thread's actor.
	Sync threadlog.SyncPolicy

	// Archived reports what the projection knows of a thread's archive,
	// consulted once when the thread is opened. An archived thread is
	// read-only: its actor is sealed, so nothing more is appended, and the
	// cache drops it before any live thread.
	//
	// The log is the authority: it holds a ThreadArchived record. But the
	// index deliberately carries no thread-level state, and walking the
	// whole log on every open is the cost the index exists to avoid. So the
	// projection answers for the part of the log it has applied, and the
	// cache walks only the rest, which is usually nothing. Nil means no
	// projection, and the cache walks the whole log (TKT-01M29CRJ64, Terva
	// review 1989): the seal never depends on a caller remembering this.
	Archived func(threadid.ID) ArchiveState

	// Logger receives what the cache cannot return to a caller, such as an
	// index it could not write back. Nil means slog.Default().
	Logger *slog.Logger

	// SyncObserver, when set, is told how long each fsync of a thread's log
	// or index took, as the actor issues them: file is "log" or "index"
	// (TKT-01M29CEE7S). It runs on the actor's goroutine, so it must be
	// quick. Under SyncNever nothing is fsynced and it is never called.
	SyncObserver func(file string, d time.Duration)
}

// ArchiveState is what the projection knows of a thread's archive.
type ArchiveState struct {
	// Archived is the projection's flag: it has applied a ThreadArchived.
	Archived bool
	// Projected is the log length the projection has applied, the point
	// past which the cache looks for a ThreadArchived itself. Zero, or a
	// length this log does not have, means the cache walks the whole log.
	Projected int64
}

// Thread is one open thread: its index, its actor, and a reader over its log.
//
// A Thread is valid only between Acquire and Release. Holding one past Release
// is a use-after-close, because eviction closes the files.
type Thread struct {
	ID     threadid.ID
	Index  *threadlog.Index
	Actor  *threadlog.Actor
	Reader *threadlog.Reader

	// Outcome and Replayed record how this thread's index was obtained, which
	// is what proves a reopened thread did not have to replay.
	Outcome  threadlog.Outcome
	Replayed int64

	log *os.File
	idx *os.File

	// Guarded by the owning cache's mutex.
	archived bool
	refs     int
	used     uint64
}

// Archived reports whether the thread was archived when it was opened, has
// been marked since, or its actor has written a ThreadArchived: the actor
// knows first, before the projection or MarkArchived has caught up.
func (t *Thread) Archived() bool { return t.archived || t.Actor.Sealed() }

// Cache holds open threads, bounded, evicting least-recently-used first and
// archived threads before that.
type Cache struct {
	root   string
	bound  int
	policy threadlog.SyncPolicy
	isArch func(threadid.ID) ArchiveState
	log    *slog.Logger
	onSync func(file string, d time.Duration)

	mu     sync.Mutex
	open   map[threadid.ID]*entry
	clock  uint64
	closed bool

	// opened counts how many times a thread was actually opened from disk,
	// which is how a test tells a cache hit from a miss without reaching
	// inside. They and size, which follows len(open), are atomics so the
	// metrics scrape reads them without the mutex every Acquire takes
	// (TKT-01M29CEE7S); each is written only with mu held.
	opened  atomic.Int64
	evicted atomic.Int64
	size    atomic.Int64
}

// entry wraps a thread with the machinery that lets two goroutines ask for the
// same thread at once without both opening it.
type entry struct {
	ready chan struct{} // closed once t and err are set
	t     *Thread
	err   error

	// loading is true between the entry going into the map and the open
	// finishing. A second caller for the same id waits on ready rather than
	// opening the thread again, and takes its reference before the wait, so
	// the thread cannot be evicted between being opened and being handed out.
	loading bool
}

// ErrClosed means the cache has been closed.
var ErrClosed = errors.New("threadcache: cache is closed")

// New returns a cache over a data root.
func New(root string, opts Options) (*Cache, error) {
	bound := opts.Bound
	switch {
	case bound == 0:
		bound = DefaultBound
	case bound < 0:
		return nil, fmt.Errorf("threadcache: bound %d is negative", bound)
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Cache{
		root: root, bound: bound, policy: opts.Sync, isArch: opts.Archived, log: log, onSync: opts.SyncObserver,
		open: make(map[threadid.ID]*entry),
	}, nil
}

// Bound returns the configured maximum.
func (c *Cache) Bound() int { return c.bound }

// Len returns how many threads are open right now, those still opening
// included. It takes no lock.
func (c *Cache) Len() int { return int(c.size.Load()) }

// Stats returns how many opens and evictions have happened. It takes no
// lock.
func (c *Cache) Stats() (opens, evictions int) {
	return int(c.opened.Load()), int(c.evicted.Load())
}

// LogSize is the byte length of id's log on disk, without opening the
// thread. A caller compares it with how far a projection has got
// (threads.log_length_projected) to learn whether the projection has seen
// the whole log, for the price of a stat rather than an index load. A write
// in progress can make it longer than any length an append has reported,
// which reads as behind: the caller then asks the index, which is never
// wrong, only slower.
func (c *Cache) LogSize(id threadid.ID) (int64, error) {
	fi, err := os.Stat(id.LogPath(c.root))
	if err != nil {
		return 0, fmt.Errorf("threadcache: sizing %s: %w", id, err)
	}
	return fi.Size(), nil
}

// Create makes a new thread's files and returns it acquired.
//
// The file header is written and fsynced before the thread is handed back, so
// a thread that exists is a thread that can be reopened.
func (c *Cache) Create(id threadid.ID, forumID uint64, createdMS int64) (*Thread, error) {
	dir := id.Dir(c.root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("threadcache: creating %s: %w", dir, err)
	}
	path := id.LogPath(c.root)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("threadcache: creating %s: %w", path, err)
	}
	hdr := threadlog.NewFileHeader(uint64(id), createdMS, forumID).AppendTo(nil)
	if _, err := f.Write(hdr); err != nil {
		f.Close()
		return nil, fmt.Errorf("threadcache: writing the file header: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, fmt.Errorf("threadcache: syncing the file header: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("threadcache: closing %s: %w", path, err)
	}
	return c.Acquire(id)
}

// Acquire opens a thread if it is not already open and returns it with a
// reference held. Every Acquire must be matched by a Release.
func (c *Cache) Acquire(id threadid.ID) (*Thread, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}

	if e, ok := c.open[id]; ok {
		// Register the reference before waiting. A thread with a reference
		// cannot be evicted, so this closes the window between another
		// goroutine finishing the open and this one being handed the result.
		if e.t != nil {
			e.t.refs++
			e.t.used = c.tick()
		}
		loading := e.loading
		c.mu.Unlock()

		if loading {
			<-e.ready
			c.mu.Lock()
			if e.err != nil {
				c.mu.Unlock()
				return nil, e.err
			}
			e.t.refs++
			e.t.used = c.tick()
			c.mu.Unlock()
		}
		return e.t, nil
	}

	e := &entry{ready: make(chan struct{}), loading: true}
	c.open[id] = e
	c.size.Store(int64(len(c.open)))
	c.opened.Add(1)
	c.mu.Unlock()

	// The open does I/O — two file opens, an index read, possibly a replay —
	// and holding the cache mutex across it would serialise every other
	// thread's lookup behind this one disk seek.
	t, err := c.openThread(id)

	c.mu.Lock()
	e.t, e.err, e.loading = t, err, false
	close(e.ready)
	if err != nil {
		delete(c.open, id)
		c.size.Store(int64(len(c.open)))
		c.mu.Unlock()
		return nil, err
	}
	t.refs++
	t.used = c.tick()
	c.mu.Unlock()

	c.evict()
	return t, nil
}

// Peek returns a thread with a reference held if it is already open, and
// false without opening it otherwise. A caller that would answer differently
// for an open thread, such as one that has a cheaper path when the index is
// already in memory, asks here first; a true result must be Released.
func (c *Cache) Peek(id threadid.ID) (*Thread, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// An entry still loading has no thread yet, and Close empties the map, so
	// these two checks cover a thread mid-open and a closed cache.
	e, ok := c.open[id]
	if !ok || e.t == nil {
		return nil, false
	}
	e.t.refs++
	e.t.used = c.tick()
	return e.t, true
}

// Release drops a reference taken by Acquire.
func (c *Cache) Release(t *Thread) {
	if t == nil {
		return
	}
	c.mu.Lock()
	if t.refs > 0 {
		t.refs--
	}
	c.mu.Unlock()
	c.evict()
}

// With acquires a thread, runs fn, and releases it whatever fn does.
//
// This is the form to reach for. Acquire and Release exist for callers that
// genuinely need to hold a thread across a call boundary, and every one of
// those is a leak waiting to be written.
func (c *Cache) With(id threadid.ID, fn func(*Thread) error) error {
	t, err := c.Acquire(id)
	if err != nil {
		return err
	}
	defer c.Release(t)
	return fn(t)
}

// MarkArchived records that a thread is archived, so the cache drops it ahead
// of live threads, and seals its actor, so nothing more is appended. Call it
// after appending the ThreadArchived record, whose actor has sealed itself
// already, so the seal here only matters to a caller that wrote the record
// some other way.
func (c *Cache) MarkArchived(id threadid.ID) {
	c.mu.Lock()
	if e, ok := c.open[id]; ok && e.t != nil {
		e.t.archived = true
		e.t.Actor.Seal()
	}
	c.mu.Unlock()
	c.evict()
}

func (c *Cache) tick() uint64 { c.clock++; return c.clock }

// openThread does the I/O. No cache lock is held here.
func (c *Cache) openThread(id threadid.ID) (*Thread, error) {
	logPath := id.LogPath(c.root)
	// O_APPEND so every write lands at the end whatever else happened to the
	// descriptor. ReadAt still takes an explicit offset, which is what the
	// reader uses, so appends and reads do not fight over a file position.
	logf, err := os.OpenFile(logPath, os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("threadcache: opening %s: %w", logPath, err)
	}
	fi, err := logf.Stat()
	if err != nil {
		logf.Close()
		return nil, fmt.Errorf("threadcache: sizing %s: %w", logPath, err)
	}

	idxPath := id.IndexPath(c.root)
	idxBytes, err := os.ReadFile(idxPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		logf.Close()
		return nil, fmt.Errorf("threadcache: reading %s: %w", idxPath, err)
	}

	rec, err := threadlog.Open(logf, fi.Size(), idxBytes)
	if err != nil {
		logf.Close()
		return nil, fmt.Errorf("threadcache: opening thread %s: %w", id, err)
	}
	if rec.Torn > 0 {
		if err := rec.Truncate(logf); err != nil {
			logf.Close()
			return nil, err
		}
	}
	archived := c.archivedOnOpen(id, logf, rec.LogLength)

	// An index that was replayed onto or rebuilt describes the log only in
	// memory. The file still holds the entries it had, or none, and the
	// actor's first header would make the next open trust every one of them:
	// a Delete the replay applied would be visible again (TKT-01M3VHSYFR). So
	// the whole index is written back before the actor can write a header.
	var idxw threadlog.IndexWriter
	var idxf *os.File
	if rec.Outcome != threadlog.Trusted {
		if err := writeBack(idxPath, rec.Index, c.policy); err != nil {
			// The file keeps its older header, which does not cover the
			// log, so the next open replays and tries again. Until then the
			// actor writes nothing to it, and the thread is served from the
			// index in memory, which is correct.
			c.log.Warn("could not write a thread's index back; serving it from memory, and the next open replays it again",
				"thread", id.String(), "outcome", rec.Outcome.String(), "err", err)
			idxw = discardIndex{}
		}
	}
	if idxw == nil {
		idxf, err = os.OpenFile(idxPath, os.O_RDWR|os.O_CREATE, 0o644)
		if err != nil {
			logf.Close()
			return nil, fmt.Errorf("threadcache: opening %s: %w", idxPath, err)
		}
		idxw = idxf
	}

	// The actor writes through timed wrappers when fsync latency is
	// observed; the reader keeps the bare file, since it never syncs.
	var logw threadlog.LogWriter = logf
	if c.onSync != nil {
		logw = timedLog{LogWriter: logf, observe: c.onSync}
		idxw = timedIndex{IndexWriter: idxw, observe: c.onSync}
	}
	t := &Thread{
		ID: id, Index: rec.Index,
		Actor:    threadlog.NewActor(rec.Index, rec.LogLength, logw, idxw, c.policy),
		Reader:   threadlog.NewReader(logf, rec.Index),
		Outcome:  rec.Outcome,
		Replayed: rec.Replayed,
		log:      logf, idx: idxf,
	}
	// A thread opened archived is sealed before anyone can append to it:
	// the index does not say the log holds a ThreadArchived, so the actor
	// could not know otherwise.
	if archived {
		t.archived = true
		t.Actor.Seal()
	}
	return t, nil
}

// archivedOnOpen says whether id's log, size bytes long, holds a
// ThreadArchived. The projection answers for the part of the log it has
// applied, and the rest is walked: all of it when there is no projection,
// none of it when the projection is current. So the seal comes from the
// log, whatever the projection's lag, as after a crash between an archive
// and its projection (TKT-01M29CRJ64, Terva review 1989).
//
// A walk that cannot go on before it finds one seals the thread: it is
// logged rather than failing the open, so the thread stays readable, and
// fsck is the tool for the damaged log.
func (c *Cache) archivedOnOpen(id threadid.ID, log io.ReaderAt, size int64) bool {
	from := int64(threadlog.FileHeaderSize)
	if c.isArch != nil {
		st := c.isArch(id)
		if st.Archived {
			return true
		}
		if st.Projected > from && st.Projected <= size {
			from = st.Projected
		}
	}
	found, err := threadlog.ArchivedIn(log, from, size)
	if err != nil && !found && from != threadlog.FileHeaderSize {
		// A projected length that is no frame boundary of this log, as
		// after a restore: walk all of it.
		found, err = threadlog.ArchivedIn(log, threadlog.FileHeaderSize, size)
	}
	if err != nil && !found {
		// What the walk could not read may hold the archive, so an unknown
		// state is taken as archived, never as live: the thread stays
		// readable and takes no append until its log is repaired (Terva
		// review 1992).
		c.log.Warn("could not walk a thread's log for an archive; sealing it, so it takes no append until the log is repaired",
			"thread", id.String(), "err", err)
		return true
	}
	return found
}

// evict closes threads until the cache is within its bound.
//
// A thread with a live reference is never evicted, so the cache can sit over
// its bound while that many threads are genuinely in use. That is the right
// way round: the bound is a target for memory, and closing a thread somebody
// is reading from would be a use-after-close.
func (c *Cache) evict() {
	for {
		c.mu.Lock()
		if len(c.open) <= c.bound {
			c.mu.Unlock()
			return
		}
		victim := c.pick()
		if victim == nil {
			c.mu.Unlock()
			return
		}
		delete(c.open, victim.ID)
		c.size.Store(int64(len(c.open)))
		c.evicted.Add(1)
		c.mu.Unlock()

		// Closing is I/O and happens outside the lock. The thread is already
		// out of the map, so nobody can acquire it and see a half-closed one.
		c.closeThread(victim)
	}
}

// pick chooses the next thread to drop. Caller holds the mutex.
//
// Archived first, regardless of recency: an archived thread is read-only and
// will never be written again, so the index it is holding open is pure cost,
// where a live thread's index is about to be used. Among equals, least
// recently used.
func (c *Cache) pick() *Thread {
	var best *Thread
	for _, e := range c.open {
		t := e.t
		if t == nil || t.refs > 0 {
			continue
		}
		switch {
		case best == nil:
			best = t
		case t.archived != best.archived:
			if t.archived {
				best = t
			}
		case t.used < best.used:
			best = t
		}
	}
	return best
}

// closeThread stops the actor and closes the files.
//
// Order matters. The actor's Close flushes the index header and fsyncs the
// index, and it writes to idx, so it has to finish before idx is closed. That
// header is the whole point of evicting cleanly: without it the next open
// cannot trust the index and pays a full replay.
func (c *Cache) closeThread(t *Thread) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	keep(t.Actor.Close())
	if t.idx != nil {
		keep(t.idx.Close())
	}
	keep(t.log.Close())
	return first
}

// Close evicts everything and closes the cache.
//
// Threads still held by a caller are closed anyway: Close means the process is
// going away, and an index header that never reached disk costs a replay on
// the next start.
func (c *Cache) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	all := make([]*Thread, 0, len(c.open))
	for _, e := range c.open {
		if e.t != nil {
			all = append(all, e.t)
		}
	}
	c.open = make(map[threadid.ID]*entry)
	c.size.Store(0)
	c.mu.Unlock()

	var first error
	for _, t := range all {
		if err := c.closeThread(t); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// timedLog and timedIndex time each Sync for Options.SyncObserver. The actor
// calls Sync only where its policy fsyncs, so what they report is exactly
// the fsyncs a write waited on, and nothing under SyncNever.
type timedLog struct {
	threadlog.LogWriter
	observe func(string, time.Duration)
}

func (t timedLog) Sync() error {
	start := time.Now()
	err := t.LogWriter.Sync()
	t.observe("log", time.Since(start))
	return err
}

type timedIndex struct {
	threadlog.IndexWriter
	observe func(string, time.Duration)
}

func (t timedIndex) Sync() error {
	start := time.Now()
	err := t.IndexWriter.Sync()
	t.observe("index", time.Since(start))
	return err
}
