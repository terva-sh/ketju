// Package respcache is the response cache (docs/03 "Caching", docs/07
// "Thread response shape"): the serialized posts of a thread page, keyed on
// everything they are drawn from, bounded in bytes, least-recently-used
// first.
//
// It has no delete and no invalidate, on purpose (TKT-01M29CCBY0). An entry is
// addressed by the thread's log length, so any mutation of the thread makes a
// new key and the old entry ages out; a change outside the log that the posts
// depend on is in the key too (the authors' rows) or is checked on every hit
// (the render dependencies). A cache that needed to be told when to forget
// would be one call site away from serving a stale page.
//
// What is stored is shared by every viewer of one viewer class, so nothing
// per-viewer may go in: the read position, the watch level and what the
// viewer may do live in the page's viewer block, which is built for each
// request and never stored here.
package respcache

import (
	"container/list"
	"sync"
	"sync/atomic"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// entryOverhead is what one entry costs beyond its body and dependencies: the
// key, the list element, the map bucket share and the entry struct. As in
// internal/render, the bound is a promise about memory, so the bookkeeping
// charges it.
const entryOverhead = 192

// Key addresses one page's posts as one viewer class sees them. Every field
// is something the posts differ by; leaving one out serves one page as
// another.
type Key struct {
	Thread threadid.ID
	// LogLength is the thread's version: the length of the index snapshot
	// the page was drawn from. Every append, a delete, restore, edit, lock or
	// move included, changes it.
	LogLength int64
	// Projected is the projection's log length when the page was drawn
	// (settled in grooming). The posts are drawn from the index, not the
	// projection; it is here so that a later field drawn from the projection
	// cannot be pinned under a key that a lagging projection shares with a
	// caught-up one.
	Projected int64
	// Page and PageSize are the page as resolved, not as asked for: a
	// permalink or a landing page names a page number by here. Page size is
	// per user (docs/06), so page 2 at 10 is not page 2 at 25.
	Page     int
	PageSize int
	Lens     string
	// Renderer is the render.Version the HTML was drawn with.
	Renderer int
	// Class is the viewer class: anon, member, moderator, or author:<user
	// id> for a page holding a post a moderator removed that this viewer
	// wrote (docs/07, TKT-01M29CKRJ9). Moderators see tombstone contents, and
	// an author sees their own removed post, so those posts must never reach
	// anyone else, and the reverse.
	Class string
	// Authors is a digest of the page's authors' rows as they were read for
	// this request. A rename, a role change or an anonymization moves no log,
	// and may be made by another process (`ketju admin promote`), so it is
	// the rows themselves rather than a counter this process keeps.
	Authors [32]byte
}

type entry struct {
	key  Key
	body []byte
	deps render.Deps
	size int64
}

// Cache is a byte-bounded LRU of serialized posts.
type Cache struct {
	maxBytes int64

	mu    sync.Mutex
	ll    *list.List // front is most recently used
	items map[Key]*list.Element
	bytes int64

	hits      atomic.Uint64
	misses    atomic.Uint64
	stale     atomic.Uint64
	inserts   atomic.Uint64
	evictions atomic.Uint64
	rejected  atomic.Uint64
	dropped   atomic.Uint64
}

// New returns a cache bounded by maxBytes. Zero or less disables it: every
// lookup misses and every Put is dropped, which is what
// response_cache_mb = 0 means.
func New(maxBytes int64) *Cache {
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &Cache{maxBytes: maxBytes, ll: list.New(), items: map[Key]*list.Element{}}
}

// NewMB is New in config.toml's unit.
func NewMB(mb int) *Cache {
	if mb <= 0 {
		return New(0)
	}
	return New(int64(mb) << 20)
}

// Enabled reports whether the cache stores anything.
func (c *Cache) Enabled() bool { return c != nil && c.maxBytes > 0 }

// Get returns the body stored under k if current accepts the dependencies it
// was drawn against. current runs without the cache's lock held, since it may
// read the database. A rejected entry counts as stale and as a miss, and the
// caller's Put of the fresh page replaces it.
//
// The body is shared with other readers and must not be modified.
func (c *Cache) Get(k Key, current func(render.Deps) bool) ([]byte, bool) {
	if !c.Enabled() {
		if c != nil {
			c.misses.Add(1)
		}
		return nil, false
	}
	c.mu.Lock()
	el, ok := c.items[k]
	var e entry
	if ok {
		c.ll.MoveToFront(el)
		e = *el.Value.(*entry)
	}
	c.mu.Unlock()
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	if current != nil && !current(e.deps) {
		c.stale.Add(1)
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	return e.body, true
}

// Put stores body under k with the dependencies it was drawn against, both
// copied. An entry larger than the whole bound is refused.
func (c *Cache) Put(k Key, body []byte, deps render.Deps) {
	if !c.Enabled() {
		if c != nil {
			c.dropped.Add(1)
		}
		return
	}
	e := &entry{key: k, body: append([]byte(nil), body...), deps: render.Deps{
		Quotes:  append([]render.Quote(nil), deps.Quotes...),
		Handles: append([]render.HandleLookup(nil), deps.Handles...),
	}}
	e.size = int64(len(e.body)) + depsSize(e.deps) + entryOverhead
	if e.size > c.maxBytes {
		c.rejected.Add(1)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		old := el.Value.(*entry)
		c.bytes += e.size - old.size
		el.Value = e
		c.ll.MoveToFront(el)
	} else {
		c.items[k] = c.ll.PushFront(e)
		c.bytes += e.size
		c.inserts.Add(1)
	}
	for c.bytes > c.maxBytes {
		back := c.ll.Back()
		if back == nil {
			c.bytes = 0
			return
		}
		old := back.Value.(*entry)
		c.ll.Remove(back)
		delete(c.items, old.key)
		c.bytes -= old.size
		c.evictions.Add(1)
	}
}

func depsSize(d render.Deps) int64 {
	n := int64(len(d.Quotes)) * 8
	for _, h := range d.Handles {
		n += 32 + int64(len(h.Typed)+len(h.Canonical))
	}
	return n
}

// Stats is a snapshot of the cache's behaviour, for the metrics endpoint
// (TKT-01M29CEE7S). It has render.Stats' shape plus Stale.
type Stats struct {
	Entries int
	// Bytes includes the per-entry overhead, so it compares with Max.
	Bytes int64
	Max   int64

	Hits   uint64
	Misses uint64
	// Stale counts lookups that found an entry whose render dependencies had
	// moved (a mentioned handle registered or renamed); each is also a miss.
	Stale uint64

	Inserts   uint64
	Evictions uint64
	Rejected  uint64
	Dropped   uint64
}

// HitRatio is hits over lookups, and zero when nothing has been looked up.
func (s Stats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Stats returns a snapshot. A nil cache reports zeroes.
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.Lock()
	entries, bytes := c.ll.Len(), c.bytes
	c.mu.Unlock()
	return Stats{
		Entries: entries, Bytes: bytes, Max: c.maxBytes,
		Hits: c.hits.Load(), Misses: c.misses.Load(), Stale: c.stale.Load(),
		Inserts: c.inserts.Load(), Evictions: c.evictions.Load(),
		Rejected: c.rejected.Load(), Dropped: c.dropped.Load(),
	}
}
