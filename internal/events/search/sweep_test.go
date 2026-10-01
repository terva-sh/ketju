package search

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestHeaderOnlyLogDropsItsRows: a thread indexed with posts whose log is
// then cut back to its header loses every row, its search_state is recorded
// at the header, and a later catch-up passes it by (Terva reviews 1750 and
// 1755).
func TestHeaderOnlyLogDropsItsRows(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Cut back")
	f.deliver(created, f.post(id, 5, "vanishing words"))
	keep, kc := f.thread("Untouched")
	f.deliver(kc, f.post(keep, 5, "staying words"))
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(id.LogPath(f.root), threadlog.FileHeaderSize); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(id.IndexPath(f.root)); err != nil {
		t.Fatal(err)
	}
	f.openCache(threadcache.Options{Sync: threadlog.SyncNever})
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("vanishing") != 0 || f.hits("cut") != 0 {
		t.Errorf("a log cut to its header keeps rows: vanishing %d, title %d", f.hits("vanishing"), f.hits("cut"))
	}
	if f.state(id) != threadlog.FileHeaderSize {
		t.Errorf("a log cut to its header has search_state %d, want the header's %d", f.state(id), threadlog.FileHeaderSize)
	}
	if f.hits("staying") != 1 {
		t.Error("another thread lost its row")
	}
	if l := f.x.Lag(); l.Threads != 0 {
		t.Errorf("Lag = %+v after the catch-up", l)
	}
	f.begins.Store(0)
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := f.begins.Load(); n != 0 {
		t.Errorf("a second catch-up ran %d transactions, want none", n)
	}
	f.check()
}

// TestRunRetriesAFailedCatchUp: a catch-up that fails before it finds any
// thread, here in the directory walk, is run again by Run until one
// finishes, and the threads are then indexed (Terva review 1750).
func TestRunRetriesAFailedCatchUp(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Retried sweep")
	f.post(id, 5, "found later")
	floor, ceiling := retryFloor, retryCeiling
	retryFloor, retryCeiling = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { retryFloor, retryCeiling = floor, ceiling })

	var mu sync.Mutex
	calls := 0
	walk := f.x.walk
	f.x.walk = func(root string) (map[threadid.ID]int64, []string, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n <= 2 {
			return nil, nil, errors.New("the threads directory could not be read")
		}
		return walk(root)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.x.Run(ctx)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for f.hits("found") != 1 {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("Run never caught up after the failed sweeps:\n%s", f.log)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if calls < 3 {
		t.Errorf("the walk ran %d times, want the two failures and a success", calls)
	}
	if f.x.Indexing() {
		t.Error("Indexing is still true after a catch-up finished")
	}
	if !strings.Contains(f.log.String(), "the search catch-up failed; it will be run again") {
		t.Errorf("the failed sweep was not logged:\n%s", f.log)
	}
}

// TestRunRetriesALaterFailedSweep: after a catch-up has finished, a sweep a
// batch of dropped events asks for fails; Run runs the sweep again, and the
// post whose event was dropped is indexed.
func TestRunRetriesALaterFailedSweep(t *testing.T) {
	f := newFixture(t)
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	id, _ := f.thread("Dropped then swept")
	f.post(id, 5, "unseen words") // no event reaches the indexer
	floor, ceiling := retryFloor, retryCeiling
	retryFloor, retryCeiling = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { retryFloor, retryCeiling = floor, ceiling })

	walk := f.x.walk
	var mu sync.Mutex
	failing := true
	f.x.walk = func(root string) (map[threadid.ID]int64, []string, error) {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return nil, nil, errors.New("the threads directory could not be read")
		}
		return walk(root)
	}
	if err := f.x.Deliver(t.Context(), events.Batch{Dropped: 1}); err == nil {
		t.Fatal("a failed sweep after dropped events was not reported to the bus")
	}
	mu.Lock()
	failing = false
	mu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.x.Run(ctx)
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for f.hits("unseen") != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("Run never ran the failed sweep again:\n%s", f.log)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestCatchUpKeepsANewerFailedDelivery: a post appended after a catch-up's
// snapshot, whose delivery fails before the catch-up reaches its thread,
// stays behind for Retry; the catch-up clears only the lag its snapshot
// covers (Terva review 1753).
func TestCatchUpKeepsANewerFailedDelivery(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Raced")
	f.deliver(created, f.post(id, 5, "earlier words"))
	if l := f.x.Lag(); l.Threads != 0 {
		t.Fatalf("Lag = %+v before the race", l)
	}

	open := f.x.open
	fail := false
	f.x.open = func(path string) (logFile, error) {
		if fail {
			return nil, errors.New("disk on fire")
		}
		return open(path)
	}
	walk := f.x.walk
	raced := false
	f.x.walk = func(root string) (map[threadid.ID]int64, []string, error) {
		logs, bad, err := walk(root)
		if !raced {
			raced = true
			ev := f.post(id, 5, "racing words")
			fail = true
			if err := f.x.Deliver(t.Context(), events.Batch{Events: []events.Event{ev}}); err != nil {
				t.Errorf("Deliver: %v", err)
			}
			fail = false
		}
		return logs, bad, err
	}
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := f.x.Lag(); l.Threads != 1 {
		t.Fatalf("the catch-up forgot a failed delivery after its snapshot: Lag = %+v", l)
	}
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("racing") != 1 || f.x.Lag().Threads != 0 {
		t.Errorf("after Retry: hits %d, lag %+v", f.hits("racing"), f.x.Lag())
	}
	f.check()
}

// TestIndexingStaysTrueWhileThreadsFail: a catch-up that leaves a thread it
// could not index keeps Indexing true until a retry indexes it, and a thread
// that falls behind after that does not set it again (Terva review 1753).
func TestIndexingStaysTrueWhileThreadsFail(t *testing.T) {
	f := newFixture(t)
	id, _ := f.thread("Failing at first")
	f.post(id, 5, "late words")
	open := f.x.open
	fail := true
	f.x.open = func(path string) (logFile, error) {
		if fail {
			return nil, errors.New("disk on fire")
		}
		return open(path)
	}
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !f.x.Indexing() || f.x.Lag().Threads != 1 {
		t.Fatalf("after a catch-up with a failed thread: Indexing %v, lag %+v", f.x.Indexing(), f.x.Lag())
	}
	fail = false
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.x.Indexing() || f.hits("late") != 1 {
		t.Fatalf("after the retry: Indexing %v, hits %d", f.x.Indexing(), f.hits("late"))
	}

	fail = true
	f.deliver(f.post(id, 5, "later still"))
	if f.x.Lag().Threads != 1 {
		t.Fatalf("Lag = %+v after a failed delivery", f.x.Lag())
	}
	if f.x.Indexing() {
		t.Error("a thread falling behind after the pass set Indexing again")
	}
}

// TestHeaderOnlyLogWithoutStateDropsOrphanRows: rows left for a thread with
// no search_state, whose log holds only its header, are dropped by the next
// catch-up rather than passed by (Terva review 1755).
func TestHeaderOnlyLogWithoutStateDropsOrphanRows(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Orphaned")
	f.deliver(created, f.post(id, 5, "orphan words"))
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(id.LogPath(f.root), threadlog.FileHeaderSize); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(id.IndexPath(f.root)); err != nil {
		t.Fatal(err)
	}
	f.write(func(q *dbq.Queries, _ *sql.Tx) error { return q.DeleteSearchState(t.Context(), int64(id)) })
	if f.hits("orphan") != 1 || f.state(id) != 0 {
		t.Fatalf("setup: hits %d, state %d", f.hits("orphan"), f.state(id))
	}
	f.openCache(threadcache.Options{Sync: threadlog.SyncNever})
	if err := f.x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("orphan") != 0 || f.hits("orphaned") != 0 {
		t.Errorf("orphan rows survived a catch-up: post %d, title %d", f.hits("orphan"), f.hits("orphaned"))
	}
	if f.state(id) != threadlog.FileHeaderSize || f.x.Lag().Threads != 0 {
		t.Errorf("state %d, lag %+v after the catch-up", f.state(id), f.x.Lag())
	}
	f.check()
}

// TestAnOlderResultKeepsANewerFailedTarget: a result that reached the end
// of the log its run saw, settling after a delivery that failed with a later
// length, leaves the thread behind for Retry (Terva review 1755). The
// interleaving, a settle after a later hold, is driven directly because
// index releases its write lock before settling.
func TestAnOlderResultKeepsANewerFailedTarget(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Older result")
	first := f.post(id, 5, "first words")
	f.deliver(created, first)
	later := f.post(id, 5, "later words")

	end := first.LogLength
	f.x.hold(id, threadlog.FileHeaderSize, end)             // the older run was asked for end
	f.x.hold(id, threadlog.FileHeaderSize, later.LogLength) // a newer delivery failed
	f.x.settle(result{
		target: target{id: id, lag: end},
		out:    outcome{from: threadlog.FileHeaderSize, indexed: end, end: end},
	})
	if l := f.x.Lag(); l.Threads != 1 {
		t.Fatalf("an older result cleared a newer failed target: Lag = %+v", l)
	}
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("later") != 1 || f.x.Lag().Threads != 0 {
		t.Errorf("after Retry: hits %d, lag %+v", f.hits("later"), f.x.Lag())
	}

	// A target past the log's end, as a log swapped for a shorter one
	// leaves, still clears once a run is asked for it.
	f.x.hold(id, later.LogLength, later.LogLength+4096)
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := f.x.Lag(); l.Threads != 0 {
		t.Errorf("a target past the log's end was never cleared: Lag = %+v", l)
	}
}

// TestRetrySkipsAQuarantinedThread: a thread that fell behind and was then
// quarantined is forgotten by Retry rather than indexed (Terva review
// 1755).
func TestRetrySkipsAQuarantinedThread(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Quarantined later")
	ev := f.post(id, 5, "withheld words")
	open := f.x.open
	f.x.open = func(string) (logFile, error) { return nil, errors.New("disk on fire") }
	f.deliver(created, ev)
	if l := f.x.Lag(); l.Threads != 1 {
		t.Fatalf("Lag = %+v after a failed delivery", l)
	}
	f.x.open = open
	f.x.quarantined = func(q threadid.ID) bool { return q == id }
	if err := f.x.Retry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hits("withheld") != 0 || f.hits("quarantined") != 0 || f.x.Lag().Threads != 0 {
		t.Errorf("Retry indexed or kept a quarantined thread: hits %d, title %d, lag %+v",
			f.hits("withheld"), f.hits("quarantined"), f.x.Lag())
	}
}
