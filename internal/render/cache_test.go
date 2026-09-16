package render

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
)

func key(thread uint64, ordinal, revision uint32) Key {
	return Key{Thread: threadid.ID(thread), Ordinal: ordinal, Revision: revision}
}

// --- Acceptance criterion 1: bounded by bytes --------------------------------

// TestTheBoundHoldsUnderAMixedWorkload is the first criterion, and the mixture
// is the whole point of it.
//
// Post sizes vary by orders of magnitude: a "+1" is a few bytes of HTML and a
// long technical answer with code blocks is tens of kilobytes. A count-based
// bound sized for the small ones blows through memory on the large ones, and
// sized for the large ones wastes almost all of it on the small. So the bound
// is bytes, and this test is the one that would notice if it stopped being.
func TestTheBoundHoldsUnderAMixedWorkload(t *testing.T) {
	const max = 256 << 10
	c := NewCache(max)

	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 5000 {
		// Roughly the real distribution: mostly short, occasionally very long.
		n := 16 + rng.IntN(64)
		if i%25 == 0 {
			n = 4096 + rng.IntN(16<<10)
		}
		c.Put(key(1, uint32(i), 1), strings.Repeat("x", n))

		if got := c.Stats().Bytes; got > max {
			t.Fatalf("after %d inserts the cache holds %d bytes, over the %d bound", i+1, got, max)
		}
	}

	s := c.Stats()
	if s.Bytes > max {
		t.Errorf("Bytes = %d, over the bound of %d", s.Bytes, max)
	}
	if s.Evictions == 0 {
		t.Error("nothing was ever evicted, so the bound was never actually exercised")
	}
	// The accounting must also match reality, not merely stay under the bound:
	// a leak that under-counts would pass the check above forever.
	if want := c.measure(); s.Bytes != want {
		t.Errorf("Bytes = %d but the entries add up to %d", s.Bytes, want)
	}
}

// measure recomputes the size from the entries, for tests that check the
// running total has not drifted.
func (c *Cache) measure() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for el := c.ll.Front(); el != nil; el = el.Next() {
		total += el.Value.(*entry).size
	}
	return total
}

// TestASmallPostFloodDoesNotEscapeTheBound is why entryOverhead is charged.
//
// Ten thousand one-word posts is not a contrived shape; it is what a long
// agreement thread looks like. Accounting only for the length of the HTML would
// report a few hundred kilobytes while the heap held the keys, the list
// elements and the map buckets for all of them.
func TestASmallPostFloodDoesNotEscapeTheBound(t *testing.T) {
	const max = 64 << 10
	const inserted = 10000
	c := NewCache(max)
	for i := range inserted {
		c.Put(key(1, uint32(i), 1), "ok")
	}
	s := c.Stats()
	if s.Bytes > max {
		t.Errorf("Bytes = %d, over the bound of %d", s.Bytes, max)
	}
	// The assertion that actually bites. If only the length of the HTML were
	// counted, all 10000 two-byte entries would fit inside 64 KiB on paper and
	// the cache would retain every one of them, holding roughly a megabyte of
	// keys, list elements and map buckets while reporting 20 KB.
	if s.Entries >= inserted {
		t.Errorf("all %d tiny posts were retained; the bound is counting HTML length "+
			"and not what an entry costs", inserted)
	}
}

func TestAPostLargerThanTheBoundIsRefused(t *testing.T) {
	c := NewCache(1024)
	c.Put(key(1, 1, 1), strings.Repeat("a", 512))
	c.Put(key(1, 2, 1), strings.Repeat("b", 4096))

	// Storing it would mean evicting everything to hold one entry that the next
	// insert evicts anyway.
	if _, ok := c.Get(key(1, 2, 1)); ok {
		t.Error("an entry larger than the whole cache was stored")
	}
	if _, ok := c.Get(key(1, 1, 1)); !ok {
		t.Error("the oversized entry evicted an entry that fits")
	}
	if n := c.Stats().Rejected; n != 1 {
		t.Errorf("Rejected = %d, want 1", n)
	}
}

func TestLeastRecentlyUsedGoesFirst(t *testing.T) {
	// Three entries fit, the fourth does not.
	c := NewCache(3*(10+entryOverhead) + 5)
	for i := uint32(1); i <= 3; i++ {
		c.Put(key(1, i, 1), strings.Repeat("x", 10))
	}
	// Touch 1, leaving 2 as the least recently used.
	if _, ok := c.Get(key(1, 1, 1)); !ok {
		t.Fatal("entry 1 was not stored")
	}
	c.Put(key(1, 4, 1), strings.Repeat("x", 10))

	if _, ok := c.Get(key(1, 2, 1)); ok {
		t.Error("the least recently used entry survived")
	}
	for _, ord := range []uint32{1, 3, 4} {
		if _, ok := c.Get(key(1, ord, 1)); !ok {
			t.Errorf("entry %d was evicted before the least recently used one", ord)
		}
	}
}

func TestOverwritingAKeyDoesNotDoubleCount(t *testing.T) {
	c := NewCache(64 << 10)
	k := key(1, 1, 1)
	c.Put(k, strings.Repeat("a", 100))
	first := c.Stats().Bytes
	c.Put(k, strings.Repeat("b", 100))

	if got := c.Stats().Bytes; got != first {
		t.Errorf("Bytes = %d after replacing an entry with one the same size, want %d", got, first)
	}
	if n := c.Stats().Entries; n != 1 {
		t.Errorf("Entries = %d, want 1", n)
	}
	html, _ := c.Get(k)
	if !strings.HasPrefix(html, "b") {
		t.Error("the replacement was not stored")
	}
}

// --- Acceptance criterion 2: an edit invalidates one post --------------------

// TestEditingAPostInvalidatesOnlyThatPost is the second criterion.
//
// Two things have to hold and they are easy to confuse. The new revision must
// not be served the old HTML, which the revision in the key gives for free; and
// the neighbouring posts must survive, which is what makes the cache worth
// having on a thread where one post is being edited repeatedly.
func TestEditingAPostInvalidatesOnlyThatPost(t *testing.T) {
	c := NewCache(64 << 10)
	for i := uint32(1); i <= 20; i++ {
		c.Put(key(1, i, 1), fmt.Sprintf("<p>post %d</p>", i))
	}

	dropped := c.InvalidatePost(threadid.ID(1), 7)
	if dropped != 1 {
		t.Errorf("invalidating post 7 dropped %d entries, want 1", dropped)
	}
	if _, ok := c.Get(key(1, 7, 1)); ok {
		t.Error("the edited post is still cached")
	}
	for i := uint32(1); i <= 20; i++ {
		if i == 7 {
			continue
		}
		if _, ok := c.Get(key(1, i, 1)); !ok {
			t.Fatalf("editing post 7 also evicted post %d", i)
		}
	}
}

func TestANewRevisionIsNeverServedTheOldHTML(t *testing.T) {
	c := NewCache(64 << 10)
	c.Put(key(1, 1, 1), "<p>before</p>")

	if _, ok := c.Get(key(1, 1, 2)); ok {
		t.Fatal("revision 2 was served an entry stored for revision 1")
	}
	c.Put(key(1, 1, 2), "<p>after</p>")
	if html, _ := c.Get(key(1, 1, 2)); html != "<p>after</p>" {
		t.Errorf("revision 2 = %q", html)
	}
	if html, _ := c.Get(key(1, 1, 1)); html != "<p>before</p>" {
		t.Errorf("revision 1 = %q; a separate key should be a separate entry", html)
	}
}

// TestSupersededRevisionsAreReclaimed is the memory half of the same criterion.
//
// Correctness never needed it: an old revision is already unreachable. But a
// post being actively edited would otherwise hold every intermediate rendering
// in memory behind the one rendering anybody wants, until the LRU happened to
// reach them.
func TestSupersededRevisionsAreReclaimed(t *testing.T) {
	c := NewCache(64 << 10)
	for rev := uint32(1); rev <= 8; rev++ {
		c.Put(key(1, 1, rev), strings.Repeat("x", 500))
	}
	if n := c.Stats().Entries; n != 8 {
		t.Fatalf("Entries = %d, want 8 revisions held", n)
	}

	dropped := c.InvalidatePost(threadid.ID(1), 1)
	if dropped != 8 {
		t.Errorf("dropped %d revisions, want all 8", dropped)
	}
	if n := c.Stats().Entries; n != 0 {
		t.Errorf("Entries = %d after invalidating the post, want 0", n)
	}
	if b := c.Stats().Bytes; b != 0 {
		t.Errorf("Bytes = %d after invalidating everything, want 0", b)
	}
}

func TestInvalidateThreadDropsOnlyThatThread(t *testing.T) {
	c := NewCache(64 << 10)
	for thread := uint64(1); thread <= 3; thread++ {
		for ord := uint32(1); ord <= 5; ord++ {
			c.Put(key(thread, ord, 1), "<p>x</p>")
		}
	}
	if n := c.InvalidateThread(threadid.ID(2)); n != 5 {
		t.Errorf("dropped %d entries, want 5", n)
	}
	if _, ok := c.Get(key(2, 3, 1)); ok {
		t.Error("thread 2 is still cached")
	}
	if _, ok := c.Get(key(1, 3, 1)); !ok {
		t.Error("thread 1 was dropped as well")
	}
	if b, want := c.Stats().Bytes, c.measure(); b != want {
		t.Errorf("Bytes = %d but the entries add up to %d", b, want)
	}
}

// --- Acceptance criterion 3: a version bump retires everything ---------------

// TestBumpingTheRendererVersionMakesEveryEntryUnreachable is the third
// criterion, and it is worth saying what it is really for.
//
// The cache is in memory, so a version bump implies a new binary, which implies
// an empty cache; the property is free today. It is held down anyway because
// the reason this cache is not persisted is precisely the renderer-version
// coupling (docs/04), and "not persisted" is a decision somebody will revisit.
// When they do, the key is already right. Getting it wrong later means serving
// HTML from the old pipeline under an ETag that claims the new one.
func TestBumpingTheRendererVersionMakesEveryEntryUnreachable(t *testing.T) {
	c := NewCache(64 << 10)
	for i := uint32(1); i <= 50; i++ {
		c.Put(Key{Thread: 1, Ordinal: i, Revision: 1, Version: Version}, "<p>old</p>")
	}

	next := Version + 1
	for i := uint32(1); i <= 50; i++ {
		if _, ok := c.Get(Key{Thread: 1, Ordinal: i, Revision: 1, Version: next}); ok {
			t.Fatalf("post %d rendered by version %d was served to version %d", i, Version, next)
		}
	}
	// And the old entries are still addressable by the version that made them,
	// which is what would let a persistent cache hold both across an upgrade.
	if _, ok := c.Get(Key{Thread: 1, Ordinal: 1, Revision: 1, Version: Version}); !ok {
		t.Error("the entry became unreachable by its own version too")
	}
}

func TestTheZeroVersionMeansThisBinary(t *testing.T) {
	c := NewCache(64 << 10)
	// A caller inside the process should not have to name the version, and
	// naming it explicitly must reach the same entry as leaving it out.
	c.Put(Key{Thread: 1, Ordinal: 1, Revision: 1}, "<p>x</p>")
	if _, ok := c.Get(Key{Thread: 1, Ordinal: 1, Revision: 1, Version: Version}); !ok {
		t.Error("an entry stored with the implicit version was not found by the explicit one")
	}
}

// --- Acceptance criterion 4: the hit ratio is exposed ------------------------

func TestTheHitRatioIsExposed(t *testing.T) {
	c := NewCache(64 << 10)
	c.Put(key(1, 1, 1), "<p>x</p>")

	for range 3 {
		if _, ok := c.Get(key(1, 1, 1)); !ok {
			t.Fatal("a stored entry missed")
		}
	}
	for i := uint32(100); i < 101; i++ {
		c.Get(key(1, i, 1))
	}

	s := c.Stats()
	if s.Hits != 3 || s.Misses != 1 {
		t.Fatalf("Hits = %d Misses = %d, want 3 and 1", s.Hits, s.Misses)
	}
	if got, want := s.HitRatio(), 0.75; got != want {
		t.Errorf("HitRatio = %v, want %v", got, want)
	}
	if s.Max != 64<<10 {
		t.Errorf("Max = %d; the bound has to be reportable next to the usage or the "+
			"usage means nothing", s.Max)
	}
}

func TestAnUntouchedCacheHasNoHitRatio(t *testing.T) {
	// Zero over zero is the shape that produces NaN and then a dashboard panel
	// that says nothing.
	if got := NewCache(1024).Stats().HitRatio(); got != 0 {
		t.Errorf("HitRatio on an untouched cache = %v, want 0", got)
	}
}

// TestTheMissReasonsAreDistinguishable is why Stats carries four counters
// rather than one.
//
// A miss because the bound is too small for the working set and a miss because
// the post is larger than the whole cache call for opposite responses: raise
// the bound, or stop trying to cache that post. One "misses" counter cannot
// tell an operator which they have.
func TestTheMissReasonsAreDistinguishable(t *testing.T) {
	c := NewCache(2 * (10 + entryOverhead))
	c.Put(key(1, 1, 1), strings.Repeat("x", 10))
	c.Put(key(1, 2, 1), strings.Repeat("x", 10))
	c.Put(key(1, 3, 1), strings.Repeat("x", 10))   // evicts
	c.Put(key(1, 4, 1), strings.Repeat("x", 5000)) // refused

	s := c.Stats()
	if s.Evictions == 0 {
		t.Error("Evictions = 0; the too-small bound is invisible")
	}
	if s.Rejected != 1 {
		t.Errorf("Rejected = %d, want 1; the oversized post is invisible", s.Rejected)
	}
	if s.Inserts != 3 {
		t.Errorf("Inserts = %d, want 3 (the refused one never entered)", s.Inserts)
	}
}

// --- The disabled configuration ---------------------------------------------

// TestABoundOfZeroDisablesTheCache. rendered_cache_mb = 0 is an operator
// turning the cache off, and internal/config goes to some trouble to tell that
// apart from an absent key so that it can mean this.
func TestABoundOfZeroDisablesTheCache(t *testing.T) {
	for _, max := range []int64{0, -1} {
		c := NewCache(max)
		if c.Enabled() {
			t.Errorf("a bound of %d reports as enabled", max)
		}
		c.Put(key(1, 1, 1), "<p>x</p>")
		if _, ok := c.Get(key(1, 1, 1)); ok {
			t.Errorf("a cache bounded at %d stored something", max)
		}
		if s := c.Stats(); s.Entries != 0 || s.Bytes != 0 {
			t.Errorf("a disabled cache holds %d entries and %d bytes", s.Entries, s.Bytes)
		}
		if s := c.Stats(); s.Dropped == 0 {
			t.Errorf("a cache bounded at %d does not count what it dropped, so a "+
				"misconfiguration looks like a cold cache", max)
		}
	}
}

func TestNewCacheMBUsesTheConfiguredUnit(t *testing.T) {
	if got, want := NewCacheMB(256).Stats().Max, int64(256<<20); got != want {
		t.Errorf("Max = %d, want %d", got, want)
	}
	if NewCacheMB(0).Enabled() || NewCacheMB(-1).Enabled() {
		t.Error("a zero or negative megabyte bound should disable the cache")
	}
}

// --- Concurrency -------------------------------------------------------------

// TestCacheIsSafeForConcurrentUse. Run under -race; without it this asserts
// only that nothing panics.
func TestCacheIsSafeForConcurrentUse(t *testing.T) {
	const max = 128 << 10
	c := NewCache(max)

	var wg sync.WaitGroup
	for w := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(w), 99))
			for i := range 500 {
				k := key(uint64(rng.IntN(4)), uint32(rng.IntN(200)), 1)
				switch i % 8 {
				case 0, 1, 2, 3, 4:
					c.Get(k)
				case 5, 6:
					c.Put(k, strings.Repeat("x", 1+rng.IntN(2000)))
				case 7:
					c.InvalidatePost(k.Thread, k.Ordinal)
				}
			}
		}()
	}
	wg.Wait()

	if got := c.Stats().Bytes; got > max {
		t.Errorf("Bytes = %d, over the bound of %d after concurrent use", got, max)
	}
	// The running total and the entries must still agree: a race that lost an
	// update would show up here and nowhere else.
	if got, want := c.Stats().Bytes, c.measure(); got != want {
		t.Errorf("Bytes = %d but the entries add up to %d; the accounting drifted", got, want)
	}
}

// TestInvalidateOnAnAbsentPostIsHarmless. The projection writer will invalidate
// on every edit it sees, including ones for posts nobody has read.
func TestInvalidateOnAnAbsentPostIsHarmless(t *testing.T) {
	c := NewCache(1024)
	if n := c.InvalidatePost(threadid.ID(9), 9); n != 0 {
		t.Errorf("dropped %d entries for a post that was never cached", n)
	}
	if n := c.InvalidateThread(threadid.ID(9)); n != 0 {
		t.Errorf("dropped %d entries for a thread that was never cached", n)
	}
}
