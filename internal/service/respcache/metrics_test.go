package respcache

import (
	"testing"
	"time"
)

// TestCountersTakeNoLock: the metrics scrape reads Counters while a lookup
// holds the cache's lock, and does not wait for it (TKT-01M29CEE7S).
func TestCountersTakeNoLock(t *testing.T) {
	c := New(1 << 20)
	c.Get(Key{}, nil)
	c.mu.Lock()
	defer c.mu.Unlock()
	done := make(chan Stats, 1)
	go func() { done <- c.Counters() }()
	select {
	case s := <-done:
		if s.Misses != 1 {
			t.Errorf("Counters = %+v, want one miss", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Counters waited on the lock a lookup takes")
	}
}
