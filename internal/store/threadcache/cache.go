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
	"os"
	"sync"

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
// DefaultBound, group commit, and nothing known to be archived.
type Options struct {
	// Bound is the maximum number of threads held open. Zero means
	// DefaultBound. Negative is refused.
	Bound int

	// Sync is the durability policy handed to each thread's actor.
	Sync threadlog.SyncPolicy

	// Archived reports whether a thread is archived, consulted once when the
	// thread is opened. An archived thread is read-only and will not be
	// written again, so the cache drops it before any live thread.
	//
	// The log knows this — it ends in a ThreadArchived record — but the index
	// deliberately does not carry thread-level state, and finding out would
	// mean a replay on every open, which is the cost the index exists to
	// avoid. So the caller supplies it; once the database in docs/04 exists,
	// its projection is the authority. Nil means nothing is archived.
	Archived func(threadid.ID) bool
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

// Archived reports whether the thread was archived when it was opened, or has
// been marked since.
func (t *Thread) Archived() bool { return t.archived }

// Cache holds open threads, bounded, evicting least-recently-used first and
// archived threads before that.
type Cache struct {
	root   string
	bound  int
	policy threadlog.SyncPolicy
	isArch func(threadid.ID) bool

	mu     sync.Mutex
	open   map[threadid.ID]*entry
	clock  uint64
	closed bool

	// opened counts how many times a thread was actually opened from disk,
	// which is how a test tells a cache hit from a miss without reaching
	// inside.
	opened  int
	evicted int
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
	return &Cache{
		root: root, bound: bound, policy: opts.Sync, isArch: opts.Archived,
		open: make(map[threadid.ID]*entry),
	}, nil
}

// Bound returns the configured maximum.
func (c *Cache) Bound() int { return c.bound }

// Len returns how many threads are open right now.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.open)
}

// Stats returns how many opens and evictions have happened.
func (c *Cache) Stats() (opens, evictions int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opened, c.evicted
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
	c.opened++
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
		c.mu.Unlock()
		return nil, err
	}
	t.refs++
	t.used = c.tick()
	c.mu.Unlock()

	c.evict()
	return t, nil
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
// of live threads. Call it after appending the ThreadArchived record.
func (c *Cache) MarkArchived(id threadid.ID) {
	c.mu.Lock()
	if e, ok := c.open[id]; ok && e.t != nil {
		e.t.archived = true
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

	idxf, err := os.OpenFile(idxPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		logf.Close()
		return nil, fmt.Errorf("threadcache: opening %s: %w", idxPath, err)
	}

	t := &Thread{
		ID: id, Index: rec.Index,
		Actor:    threadlog.NewActor(rec.Index, rec.LogLength, logf, idxf, c.policy),
		Reader:   threadlog.NewReader(logf, rec.Index),
		Outcome:  rec.Outcome,
		Replayed: rec.Replayed,
		log:      logf, idx: idxf,
	}
	if c.isArch != nil {
		t.archived = c.isArch(id)
	}
	return t, nil
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
		c.evicted++
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
	keep(t.idx.Close())
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
	c.mu.Unlock()

	var first error
	for _, t := range all {
		if err := c.closeThread(t); err != nil && first == nil {
			first = err
		}
	}
	return first
}
