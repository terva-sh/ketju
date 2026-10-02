package threadcache

import (
	"testing"
	"time"
)

// TestStatsTakeNoLock: the metrics scrape reads Stats and Len while a
// request holds the cache mutex, and neither waits for it (TKT-01M29CEE7S).
func TestStatsTakeNoLock(t *testing.T) {
	c, _ := newCache(t, Options{})
	c.mu.Lock()
	defer c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.Stats()
		c.Len()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stats or Len waited on the mutex every Acquire takes")
	}
}

// TestLenFollowsTheMap: Len, read without the mutex, still counts what is
// open as threads are opened, evicted and closed.
func TestLenFollowsTheMap(t *testing.T) {
	c, _ := newCache(t, Options{Bound: 2})
	for _, id := range ids(3) {
		th, err := c.Create(id, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		c.Release(th)
	}
	if got := c.Len(); got != 2 {
		t.Errorf("Len = %d after three opens with a bound of 2, want 2", got)
	}
	if opens, evictions := c.Stats(); opens != 3 || evictions != 1 {
		t.Errorf("Stats = %d opens, %d evictions; want 3, 1", opens, evictions)
	}
	c.Close()
	if got := c.Len(); got != 0 {
		t.Errorf("Len = %d after Close, want 0", got)
	}
}
