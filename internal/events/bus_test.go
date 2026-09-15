package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/threadid"
)

// safeBuf is a bytes.Buffer that survives the bus logging from several
// goroutines at once.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testBus returns a bus that logs into a buffer instead of the process logger,
// so a test can assert on what an operator would have seen.
func testBus(t *testing.T, queue int) (*Bus, *safeBuf) {
	t.Helper()
	var buf safeBuf
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return New(Options{Logger: log, Queue: queue}), &buf
}

// collector records every batch it is handed.
type collector struct {
	name string

	mu      sync.Mutex
	batches []Batch
	events  []Event

	// gate, when non-nil, is received from once per Deliver, which lets a test
	// hold a subscriber still.
	gate chan struct{}

	// fail, when non-nil, decides whether a batch is rejected.
	fail func(n int) error
	// calls counts Deliver invocations, passed to fail.
	calls int
}

func (c *collector) Name() string { return c.name }

func (c *collector) Deliver(ctx context.Context, b Batch) error {
	if c.gate != nil {
		<-c.gate
	}
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()

	if c.fail != nil {
		if err := c.fail(n); err != nil {
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, b)
	c.events = append(c.events, b.Events...)
	return nil
}

func (c *collector) snapshot() ([]Batch, []Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Batch(nil), c.batches...), append([]Event(nil), c.events...)
}

// waitFor polls until cond holds or the deadline passes. The bus is
// asynchronous by design, so a test either polls or sleeps; polling is the one
// that does not trade flakiness for wall-clock time.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func mustSubscribe(t *testing.T, b *Bus, s Subscriber, queue int) {
	t.Helper()
	if err := b.Subscribe(s, queue); err != nil {
		t.Fatalf("Subscribe(%s): %v", s.Name(), err)
	}
}

func mustStart(t *testing.T, b *Bus) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return ctx
}

func closeBus(t *testing.T, b *Bus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestDeliversOneThreadInLogOrder covers the ordering promise. Events for
// several threads are interleaved on the way in, and each thread's events must
// come out in the order they went in.
func TestDeliversOneThreadInLogOrder(t *testing.T) {
	t.Parallel()

	const (
		threads   = 4
		perThread = 250
	)
	c := &collector{name: "orderly"}
	b, _ := testBus(t, threads*perThread+1)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	ids := make([]threadid.ID, threads)
	for i := range ids {
		ids[i] = threadid.ID(0x0100_0000_0000_0000 + uint64(i))
	}
	// Interleave: thread 0 post 1, thread 1 post 1, ... so that a bus that
	// merely preserved global arrival order and one that shuffled per thread
	// would both be caught.
	for n := 1; n <= perThread; n++ {
		for i, id := range ids {
			b.Publish(Event{Thread: id, Ordinal: uint32(n), LogLength: int64(n*100 + i)})
		}
	}

	waitFor(t, "every event delivered", func() bool {
		_, evs := c.snapshot()
		return len(evs) == threads*perThread
	})
	closeBus(t, b)

	_, evs := c.snapshot()
	last := make(map[threadid.ID]uint32, threads)
	for _, ev := range evs {
		if prev, ok := last[ev.Thread]; ok && ev.Ordinal != prev+1 {
			t.Fatalf("thread %s: ordinal %d followed %d; per-thread order broken",
				ev.Thread, ev.Ordinal, prev)
		}
		last[ev.Thread] = ev.Ordinal
	}
	for _, id := range ids {
		if last[id] != perThread {
			t.Errorf("thread %s: last ordinal %d, want %d", id, last[id], perThread)
		}
	}
}

// TestStalledSubscriberNeverBlocksPublish is the ticket's central claim: a
// subscriber that has stopped consuming costs itself events and costs the
// writer nothing.
func TestStalledSubscriberNeverBlocksPublish(t *testing.T) {
	t.Parallel()

	const n = 5000
	stalled := &collector{name: "stalled", gate: make(chan struct{})}
	healthy := &collector{name: "healthy"}

	b, buf := testBus(t, 0)
	// A queue of 4 for the stalled one so it fills almost immediately, and a
	// queue larger than the run for the healthy one so any loss it suffers is
	// a real defect rather than a scheduling artifact.
	mustSubscribe(t, b, stalled, 4)
	mustSubscribe(t, b, healthy, n+1)
	mustStart(t, b)

	id := threadid.ID(0x0200_0000_0000_0001)
	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		for i := 1; i <= n; i++ {
			b.Publish(Event{Thread: id, Ordinal: uint32(i), LogLength: int64(i)})
		}
		done <- time.Since(start)
	}()

	var elapsed time.Duration
	select {
	case elapsed = <-done:
	case <-time.After(20 * time.Second):
		close(stalled.gate)
		t.Fatal("Publish blocked on a stalled subscriber")
	}
	// Not a performance assertion: 5000 non-blocking sends that took seconds
	// would mean something waited, and that is the only thing being ruled out.
	if elapsed > 5*time.Second {
		t.Errorf("publishing %d events took %v with one subscriber stalled", n, elapsed)
	}

	waitFor(t, "the healthy subscriber to catch up", func() bool {
		_, evs := healthy.snapshot()
		return len(evs) == n
	})

	stats := statByName(t, b)
	if got := stats["healthy"]; got.Dropped != 0 {
		t.Errorf("healthy subscriber dropped %d events; its queue was large enough for all of them", got.Dropped)
	}
	if got := stats["stalled"]; got.Dropped == 0 {
		t.Error("stalled subscriber dropped nothing; the queue cannot have been bounded")
	} else if !got.Behind {
		t.Error("stalled subscriber dropped events but is not reported as behind")
	}
	if !strings.Contains(buf.String(), "fell behind") {
		t.Error("dropping events produced no log line; a silent drop is the failure this guards")
	}

	close(stalled.gate)
	closeBus(t, b)
}

// TestDropsAreReportedToTheSubscriber covers the other half of the drop
// contract: the count reaches the subscriber, which is how it knows to catch
// up from durable state.
func TestDropsAreReportedToTheSubscriber(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	c := &collector{name: "reader", gate: gate}
	b, _ := testBus(t, 2)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	id := threadid.ID(0x0300_0000_0000_0001)
	const n = 200
	for i := 1; i <= n; i++ {
		b.Publish(Event{Thread: id, Ordinal: uint32(i), LogLength: int64(i)})
	}
	waitFor(t, "the queue to overflow", func() bool {
		return statByName(t, b)["reader"].Dropped > 0
	})
	dropped := statByName(t, b)["reader"].Dropped

	// Let it run. Every Deliver takes one token; the first batches carry the
	// drop count, and once it is reported the subscriber is caught up again.
	go func() {
		for range 2 * n {
			select {
			case gate <- struct{}{}:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	waitFor(t, "the drop count to reach the subscriber", func() bool {
		batches, _ := c.snapshot()
		var total uint64
		for _, bt := range batches {
			total += bt.Dropped
		}
		return total >= dropped
	})
	waitFor(t, "the subscriber to be reported as caught up", func() bool {
		return !statByName(t, b)["reader"].Behind
	})

	closeBus(t, b)
}

// TestFailedBatchIsCountedAsDropped: a batch the subscriber rejected is a hole
// in its view, and the subscriber learns about it the same way it learns about
// an overflow.
func TestFailedBatchIsCountedAsDropped(t *testing.T) {
	t.Parallel()

	boom := errors.New("indexer is on fire")
	c := &collector{name: "flaky", fail: func(n int) error {
		if n == 1 {
			return boom
		}
		return nil
	}}
	b, buf := testBus(t, 64)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	id := threadid.ID(0x0400_0000_0000_0001)
	b.Publish(Event{Thread: id, Ordinal: 1, LogLength: 100})
	waitFor(t, "the first batch to fail", func() bool {
		return statByName(t, b)["flaky"].Failed == 1
	})

	b.Publish(Event{Thread: id, Ordinal: 2, LogLength: 200})
	waitFor(t, "the second batch to arrive", func() bool {
		batches, _ := c.snapshot()
		return len(batches) == 1
	})

	batches, _ := c.snapshot()
	if got := batches[0].Dropped; got != 1 {
		t.Errorf("batch after a failure reported Dropped=%d, want 1 (the rejected event)", got)
	}
	st := statByName(t, b)["flaky"]
	if st.Dropped != 1 || st.Failed != 1 {
		t.Errorf("stats after one failure: dropped=%d failed=%d, want 1 and 1", st.Dropped, st.Failed)
	}
	if st.LastError != boom.Error() {
		t.Errorf("LastError = %q, want %q", st.LastError, boom.Error())
	}
	if !strings.Contains(buf.String(), "failed a batch") {
		t.Error("a failed batch produced no log line")
	}

	closeBus(t, b)
}

// TestSubscriberSetIsFixedAtStartup: the set is decided once, is visible in
// one place, and refuses the two ways it could become ambiguous.
func TestSubscriberSetIsFixedAtStartup(t *testing.T) {
	t.Parallel()

	b, _ := testBus(t, 8)
	mustSubscribe(t, b, &collector{name: "projections"}, 0)
	mustSubscribe(t, b, &collector{name: "search"}, 0)

	if err := b.Subscribe(&collector{name: "search"}, 0); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate name: err = %v, want ErrDuplicateName", err)
	}
	if err := b.Subscribe(&collector{name: ""}, 0); err == nil {
		t.Error("an unnamed subscriber was accepted; stats and logs would have nothing to key on")
	}
	if err := b.Subscribe(&collector{name: "negative"}, -1); err == nil {
		t.Error("a negative queue was accepted")
	}

	want := []string{"projections", "search"}
	if got := b.Subscribers(); !equalStrings(got, want) {
		t.Errorf("Subscribers() = %v, want %v", got, want)
	}

	mustStart(t, b)
	if err := b.Subscribe(&collector{name: "late"}, 0); !errors.Is(err, ErrStarted) {
		t.Errorf("Subscribe after Start: err = %v, want ErrStarted", err)
	}
	if err := b.Start(context.Background()); !errors.Is(err, ErrStarted) {
		t.Errorf("second Start: err = %v, want ErrStarted", err)
	}
	if got := b.Subscribers(); !equalStrings(got, want) {
		t.Errorf("Subscribers() after a refused Subscribe = %v, want %v", got, want)
	}

	closeBus(t, b)
}

// TestCloseDrainsWhatIsQueued: shutdown hands over the events already
// accepted, so a clean stop does not manufacture a catch-up.
func TestCloseDrainsWhatIsQueued(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	c := &collector{name: "slow", gate: gate}
	b, _ := testBus(t, 64)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	id := threadid.ID(0x0500_0000_0000_0001)
	const n = 20
	for i := 1; i <= n; i++ {
		b.Publish(Event{Thread: id, Ordinal: uint32(i), LogLength: int64(i)})
	}

	closed := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		closed <- b.Close(ctx)
	}()
	go func() {
		for range n {
			select {
			case gate <- struct{}{}:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Close never returned")
	}

	_, evs := c.snapshot()
	if len(evs) != n {
		t.Errorf("Close delivered %d of %d queued events", len(evs), n)
	}
	if got := statByName(t, b)["slow"].Dropped; got != 0 {
		t.Errorf("a clean shutdown dropped %d events", got)
	}
}

// TestCloseReportsSubscribersStillDraining: a subscriber that ignores its
// context is named rather than waited on forever.
func TestCloseReportsSubscribersStillDraining(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	c := &collector{name: "stuck", gate: gate}
	b, _ := testBus(t, 8)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	id := threadid.ID(0x0600_0000_0000_0001)
	b.Publish(Event{Thread: id, Ordinal: 1, LogLength: 10})
	waitFor(t, "the subscriber to be inside Deliver", func() bool {
		return statByName(t, b)["stuck"].Queued == 0
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := b.Close(ctx)
	if !errors.Is(err, ErrDrainTimeout) {
		t.Fatalf("Close with a stuck subscriber: err = %v, want ErrDrainTimeout", err)
	}
	if !strings.Contains(err.Error(), "stuck") {
		t.Errorf("Close error %q does not name the subscriber that is still running", err)
	}

	// Release it so the goroutine ends with the test rather than outliving it.
	close(gate)
	waitFor(t, "the released subscriber to finish", func() bool {
		_, evs := c.snapshot()
		return len(evs) == 1
	})
}

// TestPublishOutsideTheRunningWindowIsANoop: before Start there is nothing
// draining, and after Close the queues are gone. Both are silent no-ops rather
// than panics, because the append they describe already succeeded.
func TestPublishOutsideTheRunningWindowIsANoop(t *testing.T) {
	t.Parallel()

	c := &collector{name: "quiet"}
	b, _ := testBus(t, 8)
	mustSubscribe(t, b, c, 0)

	id := threadid.ID(0x0700_0000_0000_0001)
	b.Publish(Event{Thread: id, Ordinal: 1, LogLength: 10})
	if got := statByName(t, b)["quiet"].Queued; got != 0 {
		t.Errorf("publishing before Start queued %d events", got)
	}

	mustStart(t, b)
	closeBus(t, b)

	b.Publish(Event{Thread: id, Ordinal: 2, LogLength: 20})
	if _, evs := c.snapshot(); len(evs) != 0 {
		t.Errorf("publishing after Close delivered %d events", len(evs))
	}
}

// TestCloseIsIdempotentAndSurvivesNeverStarting covers the shutdown paths a
// failed startup takes.
func TestCloseIsIdempotentAndSurvivesNeverStarting(t *testing.T) {
	t.Parallel()

	b, _ := testBus(t, 8)
	mustSubscribe(t, b, &collector{name: "unused"}, 0)
	closeBus(t, b)
	closeBus(t, b)

	if err := b.Start(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("Start after Close: err = %v, want ErrClosed", err)
	}
	if err := b.Subscribe(&collector{name: "later"}, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("Subscribe after Close: err = %v, want ErrClosed", err)
	}
}

// TestPublishRacesCloseWithoutPanicking is the test that holds the lock
// discipline in place. Closing a queue while a publisher is mid-send is a send
// on a closed channel, and the RWMutex is the only thing preventing it.
func TestPublishRacesCloseWithoutPanicking(t *testing.T) {
	t.Parallel()

	for round := range 20 {
		c := &collector{name: "racer"}
		b, _ := testBus(t, 4)
		mustSubscribe(t, b, c, 0)
		mustStart(t, b)

		id := threadid.ID(0x0800_0000_0000_0000 + uint64(round))
		var wg sync.WaitGroup
		for w := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 1; i <= 200; i++ {
					b.Publish(Event{Thread: id, Ordinal: uint32(i), LogLength: int64(w*1000 + i)})
				}
			}()
		}
		// Close partway through, which is the interleaving that matters.
		time.Sleep(time.Duration(round%5) * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := b.Close(ctx); err != nil {
			cancel()
			t.Fatalf("round %d: Close: %v", round, err)
		}
		cancel()
		wg.Wait()
	}
}

// TestBatchGroupsWhatWasWaiting: the drain-what-is-queued shape is what makes
// a batched subscriber cheap, so it is worth asserting rather than assuming.
func TestBatchGroupsWhatWasWaiting(t *testing.T) {
	t.Parallel()

	gate := make(chan struct{})
	c := &collector{name: "batcher", gate: gate}
	b, _ := testBus(t, 256)
	mustSubscribe(t, b, c, 0)
	mustStart(t, b)

	id := threadid.ID(0x0900_0000_0000_0001)
	const n = 100
	for i := 1; i <= n; i++ {
		b.Publish(Event{Thread: id, Ordinal: uint32(i), LogLength: int64(i)})
	}
	waitFor(t, "the queue to hold the whole burst", func() bool {
		// One event may already have been taken by the runner before it
		// blocked on the gate.
		return statByName(t, b)["batcher"].Queued >= n-1
	})

	go func() {
		for range n {
			select {
			case gate <- struct{}{}:
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()
	waitFor(t, "everything to be delivered", func() bool {
		_, evs := c.snapshot()
		return len(evs) == n
	})
	closeBus(t, b)

	batches, _ := c.snapshot()
	if len(batches) >= n {
		t.Errorf("%d events arrived in %d batches; nothing was grouped", n, len(batches))
	}
	var biggest int
	for _, bt := range batches {
		if len(bt.Events) > biggest {
			biggest = len(bt.Events)
		}
	}
	if biggest < 2 {
		t.Error("no batch held more than one event")
	}
}

// TestSubscriberFuncAdaptsAFunction keeps the convenience wrapper honest.
func TestSubscriberFuncAdaptsAFunction(t *testing.T) {
	t.Parallel()

	got := make(chan Event, 1)
	b, _ := testBus(t, 8)
	mustSubscribe(t, b, SubscriberFunc{N: "fn", F: func(_ context.Context, batch Batch) error {
		got <- batch.Events[0]
		return nil
	}}, 0)
	mustStart(t, b)

	want := Event{Thread: threadid.ID(0x0A00_0000_0000_0001), Ordinal: 7, LogLength: 4242}
	b.Publish(want)
	select {
	case ev := <-got:
		if ev != want {
			t.Errorf("delivered %+v, want %+v", ev, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("nothing delivered")
	}
	closeBus(t, b)
}

func statByName(t *testing.T, b *Bus) map[string]Stat {
	t.Helper()
	out := make(map[string]Stat)
	for _, s := range b.Stats() {
		out[s.Name] = s
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
