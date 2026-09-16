package render

import (
	"container/list"
	"sync"
	"sync/atomic"

	"github.com/terva-sh/ketju/internal/store/threadid"
)

// entryOverhead is what one cached post costs beyond the HTML itself: the key,
// the list element, the map bucket share, and the entry struct.
//
// It is charged against the bound because the bound is a promise about the
// process's memory, not about the length of the strings in it. A thread of ten
// thousand one-word posts is a real shape -- a long "+1" thread is exactly
// that -- and without this the bookkeeping would say a few hundred kilobytes
// while the heap held several megabytes. The number is an estimate and does not
// need to be exact; being roughly right is the difference between a bound that
// holds and one that is decorative.
const entryOverhead = 96

// Cache holds rendered post HTML, bounded by bytes, least-recently-used first.
//
// Rendered HTML lives here and nowhere else: not in the thread log, which holds
// sources, and not in the database, which holds coordination. That is what lets
// the pipeline change without a rewrite of anything on disk.
//
// The key carries the renderer version, which is what makes the change safe:
// an entry rendered by an older pipeline is not addressable by a newer one, so
// a version bump retires the whole cache at once rather than leaving a mixture
// nobody can tell apart. Today the cache is in memory and a version bump
// implies a restart, so this costs nothing; it is here because the day someone
// makes this cache survive a restart, the key is already correct, and finding
// out later means serving stale HTML with a fresh ETag.
type Cache struct {
	maxBytes int64

	mu    sync.Mutex
	ll    *list.List // front is most recently used
	items map[Key]*list.Element
	// byPost indexes the revisions held for one post, so that an edit can drop
	// them without walking the whole cache.
	byPost map[post]map[Key]struct{}
	bytes  int64

	hits      atomic.Uint64
	misses    atomic.Uint64
	inserts   atomic.Uint64
	evictions atomic.Uint64
	rejected  atomic.Uint64
	dropped   atomic.Uint64
}

// Key addresses one rendering of one post revision. The shape is the one
// docs/03 specifies for the read path: thread, ordinal, revision, and the
// renderer version.
type Key struct {
	Thread   threadid.ID
	Ordinal  uint32
	Revision uint32

	// Version is the renderer version that produced the HTML. Zero means the
	// version this binary renders with, which is what every caller inside the
	// process wants; it is a field rather than a constant so that the property
	// can be tested and so that a persistent cache could hold more than one.
	Version int
}

func (k Key) normalise() Key {
	if k.Version == 0 {
		k.Version = Version
	}
	return k
}

type post struct {
	thread  threadid.ID
	ordinal uint32
}

type entry struct {
	key  Key
	html string
	size int64
}

// NewCache returns a cache bounded by maxBytes.
//
// A bound of zero disables the cache: every Get misses and every Put is
// dropped. That is a supported configuration rather than a degenerate one --
// rendered_cache_mb = 0 in config.toml means an operator turned it off, and
// internal/config distinguishes that from an absent key precisely so it can
// mean this. A negative bound is treated the same way.
func NewCache(maxBytes int64) *Cache {
	if maxBytes < 0 {
		maxBytes = 0
	}
	return &Cache{
		maxBytes: maxBytes,
		ll:       list.New(),
		items:    map[Key]*list.Element{},
		byPost:   map[post]map[Key]struct{}{},
	}
}

// NewCacheMB is NewCache in the unit config.toml uses, so that the conversion
// happens once here rather than at each call site.
func NewCacheMB(mb int) *Cache {
	if mb <= 0 {
		return NewCache(0)
	}
	return NewCache(int64(mb) << 20)
}

// Enabled reports whether the cache stores anything.
func (c *Cache) Enabled() bool { return c.maxBytes > 0 }

// Get returns the cached HTML for a key.
func (c *Cache) Get(k Key) (string, bool) {
	if !c.Enabled() {
		c.misses.Add(1)
		return "", false
	}
	k = k.normalise()

	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[k]
	if !ok {
		c.misses.Add(1)
		return "", false
	}
	c.ll.MoveToFront(el)
	c.hits.Add(1)
	return el.Value.(*entry).html, true
}

// Put stores the HTML for a key, evicting least-recently-used entries until the
// total fits.
//
// An entry larger than the whole bound is refused rather than stored, because
// storing it would mean evicting everything else to hold one post that the next
// insert evicts anyway. The rendering is still returned to the reader who asked
// for it; only the caching is declined.
func (c *Cache) Put(k Key, html string) {
	if !c.Enabled() {
		c.dropped.Add(1)
		return
	}
	k = k.normalise()
	size := int64(len(html)) + entryOverhead
	if size > c.maxBytes {
		c.rejected.Add(1)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[k]; ok {
		e := el.Value.(*entry)
		c.bytes += size - e.size
		e.html, e.size = html, size
		c.ll.MoveToFront(el)
		c.evictLocked()
		return
	}

	el := c.ll.PushFront(&entry{key: k, html: html, size: size})
	c.items[k] = el
	p := post{k.Thread, k.Ordinal}
	if c.byPost[p] == nil {
		c.byPost[p] = map[Key]struct{}{}
	}
	c.byPost[p][k] = struct{}{}
	c.bytes += size
	c.inserts.Add(1)
	c.evictLocked()
}

// InvalidatePost drops every cached revision of one post.
//
// A new revision is already unreachable through the old key, so correctness
// does not need this. Memory does: without it, every superseded revision sits
// in the cache occupying bytes that nothing can ever read, until the LRU
// happens to reach it. On a post being actively edited, that is the whole
// history of the edit held in memory behind the one rendering anybody wants.
func (c *Cache) InvalidatePost(thread threadid.ID, ordinal uint32) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := c.byPost[post{thread, ordinal}]
	n := 0
	for k := range keys {
		if el, ok := c.items[k]; ok {
			c.removeLocked(el)
			n++
		}
	}
	return n
}

// InvalidateThread drops every cached post of one thread, which is what a
// thread-level change such as a rename or a retag needs.
func (c *Cache) InvalidateThread(thread threadid.ID) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0
	for el := c.ll.Front(); el != nil; {
		next := el.Next()
		if el.Value.(*entry).key.Thread == thread {
			c.removeLocked(el)
			n++
		}
		el = next
	}
	return n
}

func (c *Cache) evictLocked() {
	for c.bytes > c.maxBytes {
		back := c.ll.Back()
		if back == nil {
			// Unreachable while the accounting is right, and a silent infinite
			// loop if it ever is not.
			c.bytes = 0
			return
		}
		c.removeLocked(back)
		c.evictions.Add(1)
	}
}

func (c *Cache) removeLocked(el *list.Element) {
	e := el.Value.(*entry)
	c.ll.Remove(el)
	delete(c.items, e.key)
	c.bytes -= e.size

	p := post{e.key.Thread, e.key.Ordinal}
	if set := c.byPost[p]; set != nil {
		delete(set, e.key)
		if len(set) == 0 {
			delete(c.byPost, p)
		}
	}
}

// Stats is a snapshot of the cache's behaviour.
type Stats struct {
	// Entries and Bytes are the current contents. Bytes includes the per-entry
	// overhead, so it is comparable against the configured bound rather than
	// against the length of the HTML.
	Entries int
	Bytes   int64
	Max     int64

	Hits   uint64
	Misses uint64

	// Inserts, Evictions, Rejected and Dropped separate the reasons a lookup
	// can miss. A high Rejected means posts larger than the whole bound; a high
	// Evictions with a low hit ratio means the bound is too small for the
	// working set. Reporting one number would make those indistinguishable.
	Inserts   uint64
	Evictions uint64
	Rejected  uint64
	Dropped   uint64
}

// HitRatio is hits over lookups, and zero when nothing has been looked up.
//
// It is the number that says whether this cache is earning its memory: a ratio
// near zero on a warm process means the bound is below the working set and the
// renders are being paid for twice.
func (s Stats) HitRatio() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// Stats returns a snapshot.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	entries, bytes := c.ll.Len(), c.bytes
	c.mu.Unlock()

	return Stats{
		Entries:   entries,
		Bytes:     bytes,
		Max:       c.maxBytes,
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Inserts:   c.inserts.Load(),
		Evictions: c.evictions.Load(),
		Rejected:  c.rejected.Load(),
		Dropped:   c.dropped.Load(),
	}
}
