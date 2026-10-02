package projection

import (
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadid"
)

// TestQuarantinedCountTakesNoLock: the metrics scrape and the health check
// read QuarantinedCount while the writer's mutex is held, which Quarantined
// takes on the request path, and it follows the set (TKT-01M29CEE7S).
func TestQuarantinedCountTakesNoLock(t *testing.T) {
	f := newFixture(t)
	f.w.quarantine(threadid.ID(1<<16|1), 0, 10)
	f.w.quarantine(threadid.ID(2<<16|1), 0, 10)
	f.w.mu.Lock()
	done := make(chan int, 1)
	go func() { done <- f.w.QuarantinedCount() }()
	select {
	case n := <-done:
		if n != 2 {
			t.Errorf("QuarantinedCount = %d, want 2", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("QuarantinedCount waited on the writer's mutex")
	}
	f.w.mu.Unlock()
}
