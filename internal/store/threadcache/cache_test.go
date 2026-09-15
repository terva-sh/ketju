package threadcache

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

func newCache(t *testing.T, opts Options) (*Cache, string) {
	t.Helper()
	root := t.TempDir()
	c, err := New(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, root
}

// ids returns n distinct ids, ascending, without needing a real clock.
func ids(n int) []threadid.ID {
	out := make([]threadid.ID, n)
	for i := range out {
		out[i] = threadid.ID(uint64(1_700_000_000_000)<<16 | uint64(i))
	}
	return out
}

// post appends one post to a thread and returns its ordinal.
func post(t *testing.T, th *Thread, src string) uint32 {
	t.Helper()
	res, err := th.Actor.Append(threadlog.Append{
		Type: threadlog.TypePost, Author: 7, Body: &threadlog.PostBody{Source: src},
	})
	if err != nil {
		t.Fatalf("appending %q: %v", src, err)
	}
	return res.Ordinal
}

func create(t *testing.T, c *Cache, id threadid.ID, posts int) {
	t.Helper()
	th, err := c.Create(id, 1, 1_700_000_000_000)
	if err != nil {
		t.Fatalf("creating %s: %v", id, err)
	}
	for i := range posts {
		post(t, th, fmt.Sprintf("post %d in %s", i, id))
	}
	c.Release(th)
}

// Criterion: opening more threads than the bound evicts the least recently
// used, and the evicted thread's index header is flushed first.
//
// The flush is checked against the file rather than inferred, because "we
// called Close" is not the same claim as "the header on disk names the whole
// log".
func TestEvictionDropsTheLeastRecentlyUsedAndFlushesItsHeader(t *testing.T) {
	c, root := newCache(t, Options{Bound: 3, Sync: threadlog.SyncNever})
	id := ids(4)

	for _, i := range id[:3] {
		create(t, c, i, 2)
	}
	// Touch the first two so the third is least recently used.
	for _, i := range id[:2] {
		if err := c.With(i, func(*Thread) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}

	create(t, c, id[3], 2) // one over the bound

	if got := c.Len(); got != 3 {
		t.Errorf("cache holds %d threads, want 3", got)
	}
	if _, ev := c.Stats(); ev != 1 {
		t.Errorf("%d evictions, want 1", ev)
	}

	// The least recently used one is the one that went.
	victim := id[2]
	logSize := fileSize(t, victim.LogPath(root))
	idxBytes, err := os.ReadFile(victim.IndexPath(root))
	if err != nil {
		t.Fatalf("the evicted thread has no index file: %v", err)
	}
	h, err := threadlog.ParseIndexHeader(idxBytes)
	if err != nil {
		t.Fatalf("parsing the evicted thread's index header: %v", err)
	}
	if h.LogLength != uint64(logSize) {
		t.Errorf("index header claims %d bytes of log, the log is %d; the header was not flushed on eviction",
			h.LogLength, logSize)
	}
	if h.PostCount != 2 {
		t.Errorf("index header claims %d posts, want 2", h.PostCount)
	}
}

// Criterion: an archived thread is evicted ahead of a live one regardless of
// recency.
func TestArchivedThreadIsEvictedBeforeALiveOne(t *testing.T) {
	id := ids(3)
	archived := map[threadid.ID]bool{id[0]: true}
	c, _ := newCache(t, Options{
		Bound: 2, Sync: threadlog.SyncNever,
		Archived: func(i threadid.ID) bool { return archived[i] },
	})

	create(t, c, id[0], 1) // archived, and about to be the most recently used
	create(t, c, id[1], 1)
	if err := c.With(id[0], func(*Thread) error { return nil }); err != nil {
		t.Fatal(err)
	}

	create(t, c, id[2], 1) // forces one out

	c.mu.Lock()
	_, stillOpen := c.open[id[0]]
	_, liveOpen := c.open[id[1]]
	c.mu.Unlock()

	if stillOpen {
		t.Error("the archived thread is still open; it should go first even though it was used most recently")
	}
	if !liveOpen {
		t.Error("the live thread was evicted instead of the archived one")
	}
}

// MarkArchived must have the same effect for a thread archived while open.
func TestMarkArchivedMovesAThreadToTheFrontOfTheQueue(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 2, Sync: threadlog.SyncNever})
	id := ids(3)

	create(t, c, id[0], 1)
	create(t, c, id[1], 1)
	c.MarkArchived(id[0])
	if err := c.With(id[0], func(*Thread) error { return nil }); err != nil {
		t.Fatal(err)
	}

	create(t, c, id[2], 1)

	c.mu.Lock()
	_, stillOpen := c.open[id[0]]
	c.mu.Unlock()
	if stillOpen {
		t.Error("the thread marked archived was not evicted first")
	}
}

// Criterion: re-opening an evicted thread trusts its index with no replay.
//
// This is what eviction is for. If a reopen replays, the cache is a pure cost:
// it has thrown away the index and then rebuilt it from the log.
func TestReopeningAnEvictedThreadTrustsItsIndex(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 1, Sync: threadlog.SyncNever})
	id := ids(2)

	create(t, c, id[0], 25)
	create(t, c, id[1], 1) // evicts the first

	if c.Len() != 1 {
		t.Fatalf("cache holds %d threads, want 1", c.Len())
	}

	th, err := c.Acquire(id[0])
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release(th)

	if th.Outcome != threadlog.Trusted {
		t.Errorf("reopened with outcome %v, want Trusted", th.Outcome)
	}
	if th.Replayed != 0 {
		t.Errorf("reopening replayed %d bytes, want 0", th.Replayed)
	}
	if got := th.Index.Len(); got != 25 {
		t.Errorf("reopened index holds %d posts, want 25", got)
	}

	// And the posts actually read back.
	posts, err := th.Reader.Range(1, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 25 {
		t.Fatalf("read %d posts, want 25", len(posts))
	}
	if want := fmt.Sprintf("post 0 in %s", id[0]); posts[0].Source != want {
		t.Errorf("first post is %q, want %q", posts[0].Source, want)
	}
}

// Criterion: the cache bound is configurable and its default is 512.
func TestBoundIsConfigurableAndDefaultsTo512(t *testing.T) {
	c, err := New(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Bound() != DefaultBound {
		t.Errorf("default bound is %d, want %d", c.Bound(), DefaultBound)
	}
	if DefaultBound != 512 {
		t.Errorf("DefaultBound is %d, want 512", DefaultBound)
	}

	c2, err := New(t.TempDir(), Options{Bound: 7})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if c2.Bound() != 7 {
		t.Errorf("configured bound is %d, want 7", c2.Bound())
	}

	if _, err := New(t.TempDir(), Options{Bound: -1}); err == nil {
		t.Error("a negative bound was accepted")
	}
}

// Nothing is opened until it is asked for, which is what keeps startup off the
// number of threads on disk.
func TestNothingIsOpenedUntilRequested(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 4, Sync: threadlog.SyncNever})
	id := ids(4)
	for _, i := range id {
		create(t, c, i, 1)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	fresh, err := New(c.root, Options{Bound: 4, Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	if got := fresh.Len(); got != 0 {
		t.Errorf("a new cache has %d threads open, want 0", got)
	}
	if opens, _ := fresh.Stats(); opens != 0 {
		t.Errorf("a new cache opened %d threads, want 0", opens)
	}
}

// A thread somebody is holding must not be closed under them, even if that
// means sitting over the bound while it is held.
func TestAThreadInUseIsNotEvicted(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 1, Sync: threadlog.SyncNever})
	id := ids(2)

	create(t, c, id[0], 1)
	held, err := c.Acquire(id[0])
	if err != nil {
		t.Fatal(err)
	}

	// Create hands the thread back acquired, so now both are referenced and
	// neither can go. The cache must exceed its bound rather than close one
	// out from under a caller.
	second, err := c.Create(id[1], 1, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 2 {
		t.Errorf("cache holds %d threads; with both in use it should exceed its bound of 1", c.Len())
	}

	// And the held one is still usable, which is the property that matters.
	if got := post(t, held, "still alive"); got != 2 {
		t.Errorf("appended ordinal %d, want 2", got)
	}

	// Releasing one lets the cache come back under its bound at the next
	// opportunity, and the one still held is not the one that goes.
	c.Release(second)
	if c.Len() != 1 {
		t.Errorf("after releasing one, the cache holds %d threads, want 1", c.Len())
	}
	c.mu.Lock()
	_, heldStillOpen := c.open[id[0]]
	c.mu.Unlock()
	if !heldStillOpen {
		t.Error("the thread still held was the one evicted")
	}

	c.Release(held)
}

// Two goroutines asking for the same cold thread must open it once, not twice.
// Opening twice would give two actors over one log, which is the one thing the
// single-writer design must never allow.
func TestConcurrentAcquireOpensTheThreadOnce(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 8, Sync: threadlog.SyncNever})
	id := ids(1)[0]
	create(t, c, id, 3)

	// Drop it so the next acquire is a cold open.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := New(c.root, Options{Bound: 8, Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	const n = 32
	var wg sync.WaitGroup
	got := make([]*Thread, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], errs[i] = fresh.Acquire(id)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if got[i] != got[0] {
			t.Fatalf("acquire %d returned a different Thread; the log has two actors", i)
		}
	}
	if opens, _ := fresh.Stats(); opens != 1 {
		t.Errorf("the thread was opened %d times, want 1", opens)
	}
	for _, th := range got {
		fresh.Release(th)
	}
}

func TestAcquireAfterCloseIsRefused(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 2, Sync: threadlog.SyncNever})
	id := ids(1)[0]
	create(t, c, id, 1)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(id); err != ErrClosed {
		t.Errorf("acquire after close = %v, want ErrClosed", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
}

func TestAcquireAMissingThreadFails(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 2, Sync: threadlog.SyncNever})
	if _, err := c.Acquire(ids(1)[0]); err == nil {
		t.Error("acquiring a thread that was never created succeeded")
	}
	// And the failed entry must not be left in the map poisoning later tries.
	if c.Len() != 0 {
		t.Errorf("a failed open left %d entries in the cache", c.Len())
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
