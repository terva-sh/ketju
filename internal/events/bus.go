package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// DefaultQueue is how many events a subscriber may fall behind by before its
// events start being dropped.
//
// 1024 is roughly a burst of a thousand posts arriving while one subscriber is
// stalled — far more than a friend group produces in a minute, and small
// enough that four subscribers holding full queues cost tens of kilobytes
// rather than something an operator has to think about. A subscriber that
// needs more than this is not behind, it is broken, and the catch-up path
// serves it better than a larger buffer would.
const DefaultQueue = 1024

// Errors returned by the bus. All of them are programming errors except
// ErrDrainTimeout, which is an operational one.
var (
	// ErrStarted means Subscribe was called after Start. The subscriber set is
	// fixed at startup so that it is knowable from one place in the code.
	ErrStarted = errors.New("events: bus already started")

	// ErrClosed means Start or Subscribe was called on a closed bus.
	ErrClosed = errors.New("events: bus closed")

	// ErrDuplicateName means two subscribers share a name.
	ErrDuplicateName = errors.New("events: duplicate subscriber name")

	// ErrDrainTimeout means Close's context expired while subscribers were
	// still draining. The bus stopped accepting, but at least one subscriber
	// goroutine is still running.
	ErrDrainTimeout = errors.New("events: timed out draining subscribers")
)

// Options configures a bus. The zero value is usable.
type Options struct {
	// Logger receives the lag and failure lines. Nil means slog.Default.
	Logger *slog.Logger

	// Queue is the per-subscriber queue depth. Zero means DefaultQueue.
	// Negative is refused by Subscribe.
	Queue int
}

// Bus is the in-process event bus. The zero value is not usable; call New.
//
// The lifecycle is deliberately rigid: subscribe, start, publish, close. It is
// rigid because the alternative — subscribers appearing at runtime — makes the
// question "what reacts to an append?" unanswerable without running the
// program.
type Bus struct {
	log   *slog.Logger
	queue int

	// mu guards the fields below and, more importantly, orders Publish
	// against Close. Publish holds it for reading, so Close taking it for
	// writing means every in-flight Publish has returned before any queue is
	// closed — which is what makes closing the queues safe rather than a send
	// on a closed channel.
	mu      sync.RWMutex
	subs    []*sub
	started bool
	closed  bool

	wg sync.WaitGroup
}

// sub is one registered subscriber and its queue.
type sub struct {
	s  Subscriber
	ch chan Event

	delivered atomic.Uint64
	dropped   atomic.Uint64
	failed    atomic.Uint64

	// pending is the drop count not yet reported to the subscriber. Publish
	// raises it, a successful delivery lowers it by exactly what that delivery
	// reported, so drops that happen mid-delivery are not lost.
	pending atomic.Int64

	// behind is true between falling behind and catching up. It exists so the
	// bus logs the two transitions rather than one line per dropped event,
	// which under load is the difference between a signal and a log flood.
	behind atomic.Bool

	lastErr atomic.Pointer[string]

	// done is set when run returns, so a Close that times out can name the
	// subscribers that are still going rather than just the count.
	done atomic.Bool
}

// New returns a bus with no subscribers.
func New(opts Options) *Bus {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	q := opts.Queue
	if q == 0 {
		q = DefaultQueue
	}
	return &Bus{log: log, queue: q}
}

// Subscribe registers a subscriber. It must be called before Start.
//
// queue overrides the bus-wide queue depth for this subscriber; zero means the
// bus default. A batched subscriber such as the search indexer is the reason
// the override exists.
func (b *Bus) Subscribe(s Subscriber, queue int) error {
	if s == nil {
		return errors.New("events: nil subscriber")
	}
	if s.Name() == "" {
		return errors.New("events: subscriber has no name")
	}
	if queue < 0 {
		return fmt.Errorf("events: negative queue %d for %q", queue, s.Name())
	}
	if queue == 0 {
		queue = b.queue
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrClosed
	}
	if b.started {
		return fmt.Errorf("%w: cannot subscribe %q", ErrStarted, s.Name())
	}
	for _, existing := range b.subs {
		if existing.s.Name() == s.Name() {
			return fmt.Errorf("%w: %q", ErrDuplicateName, s.Name())
		}
	}
	b.subs = append(b.subs, &sub{s: s, ch: make(chan Event, queue)})
	return nil
}

// Subscribers reports the registered names, in registration order. This is the
// one place the set is visible, and it is what the startup log line prints.
func (b *Bus) Subscribers() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	names := make([]string, len(b.subs))
	for i, s := range b.subs {
		names[i] = s.s.Name()
	}
	return names
}

// Start launches one goroutine per subscriber. The context is passed to every
// Deliver call and cancelling it asks subscribers to stop mid-batch; it does
// not itself shut the bus down. Call Close for that.
func (b *Bus) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	if b.started {
		b.mu.Unlock()
		return ErrStarted
	}
	b.started = true
	subs := b.subs
	b.mu.Unlock()

	for _, s := range subs {
		b.wg.Add(1)
		go b.run(ctx, s)
	}
	b.log.Info("event bus started", "subscribers", b.Subscribers())
	return nil
}

// Publish hands an event to every subscriber. It never blocks and never
// returns an error: the append it describes is already durable, and there is
// nothing a caller could usefully do about a full queue that the bus is not
// already doing.
//
// Publishing before Start or after Close is a no-op. Before Start there is
// nothing draining the queues, so an event would sit there and then arrive out
// of order with respect to the catch-up a subscriber does at startup.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.started || b.closed {
		return
	}
	for _, s := range b.subs {
		select {
		case s.ch <- ev:
		default:
			b.drop(s, ev)
		}
	}
}

// drop accounts for an event that did not fit. The caller holds mu for
// reading, which is what keeps this off the path of a concurrent Close.
//
// This logs on the publishing goroutine, which is the append path, so the log
// line is emitted once per fall-behind transition rather than once per dropped
// event. One write to the log handler when a subscriber first goes bad is a
// price worth paying; one per event under load would be the stall that
// promise one exists to prevent.
func (b *Bus) drop(s *sub, ev Event) {
	s.dropped.Add(1)
	s.pending.Add(1)
	if s.behind.CompareAndSwap(false, true) {
		b.log.Warn("event subscriber fell behind; events are being dropped and will need a catch-up",
			"subscriber", s.s.Name(),
			"queue", cap(s.ch),
			"thread", ev.Thread.String(),
			"log_length", ev.LogLength,
		)
	}
}

// run drains one subscriber's queue until the queue is closed.
//
// It takes one event, then takes whatever else is already queued, and delivers
// the lot as a batch. That is the same shape as the thread actor's group
// commit and for the same reason: a subscriber that is briefly slow gets to
// amortize the next round over everything that arrived while it was busy,
// without anyone having waited for a batch to fill.
func (b *Bus) run(ctx context.Context, s *sub) {
	defer b.wg.Done()
	defer s.done.Store(true)

	batch := make([]Event, 0, 64)
	for {
		ev, ok := <-s.ch
		if !ok {
			return
		}
		batch = append(batch[:0], ev)
	drain:
		for {
			select {
			case ev, ok := <-s.ch:
				if !ok {
					b.deliver(ctx, s, batch)
					return
				}
				batch = append(batch, ev)
			default:
				break drain
			}
		}
		b.deliver(ctx, s, batch)
	}
}

// deliver hands one batch over and folds the result into the counters.
func (b *Bus) deliver(ctx context.Context, s *sub, events []Event) {
	if len(events) == 0 {
		return
	}
	reported := s.pending.Load()
	if reported < 0 {
		reported = 0
	}
	batch := Batch{Events: events, Dropped: uint64(reported)}

	err := s.s.Deliver(ctx, batch)
	if err != nil {
		s.failed.Add(1)
		// A batch that was not processed is a hole in the subscriber's view,
		// which is the same thing a dropped event is. Counting it as one means
		// the subscriber learns it must catch up through the one channel it
		// already has to handle.
		s.pending.Add(int64(len(events)))
		s.dropped.Add(uint64(len(events)))
		msg := err.Error()
		s.lastErr.Store(&msg)
		if s.behind.CompareAndSwap(false, true) {
			b.log.Error("event subscriber failed a batch; its events need a catch-up",
				"subscriber", s.s.Name(), "events", len(events), "err", err)
		} else {
			b.log.Error("event subscriber failed a batch while already behind",
				"subscriber", s.s.Name(), "events", len(events), "err", err)
		}
		return
	}

	s.delivered.Add(uint64(len(events)))
	if reported > 0 {
		s.pending.Add(-reported)
	}
	if s.pending.Load() <= 0 && s.behind.CompareAndSwap(true, false) {
		b.log.Info("event subscriber caught up",
			"subscriber", s.s.Name(), "dropped_total", s.dropped.Load())
	}
}

// Stat is one subscriber's standing. Queued and Dropped together are the
// bus-side lag: how far behind the subscriber is right now, and how much it
// has already missed. Projection lag proper — log length minus projected
// length — belongs to the projection writer, which knows both numbers; the bus
// knows only what it handed over.
type Stat struct {
	Name      string
	Queued    int
	Capacity  int
	Delivered uint64
	Dropped   uint64
	Failed    uint64

	// Behind is true when the subscriber has dropped events it has not yet
	// been told about, or is being told about right now.
	Behind bool

	// LastError is the most recent Deliver error, empty if there was none.
	LastError string
}

// Stats reports every subscriber's standing, in registration order.
func (b *Bus) Stats() []Stat {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Stat, len(b.subs))
	for i, s := range b.subs {
		st := Stat{
			Name:      s.s.Name(),
			Queued:    len(s.ch),
			Capacity:  cap(s.ch),
			Delivered: s.delivered.Load(),
			Dropped:   s.dropped.Load(),
			Failed:    s.failed.Load(),
			Behind:    s.behind.Load(),
		}
		if p := s.lastErr.Load(); p != nil {
			st.LastError = *p
		}
		out[i] = st
	}
	return out
}

// Close stops accepting events and waits for subscribers to drain what is
// already queued. It is safe to call more than once and safe to call on a bus
// that was never started.
//
// If ctx expires first, Close returns ErrDrainTimeout naming the subscribers
// still running. Those goroutines are left alone: a subscriber that ignores
// its context is not one the bus can safely interrupt, and reporting the leak
// is more useful than pretending the shutdown was clean.
func (b *Bus) Close(ctx context.Context) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	started := b.started
	subs := b.subs
	b.mu.Unlock()

	// Past the write lock no Publish is in flight and no new one will send, so
	// closing the queues cannot race a send.
	for _, s := range subs {
		close(s.ch)
	}
	if !started {
		return nil
	}

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		b.log.Info("event bus stopped")
		return nil
	case <-ctx.Done():
		var stuck []string
		for _, s := range subs {
			if !s.done.Load() {
				stuck = append(stuck, s.s.Name())
			}
		}
		b.log.Error("event bus shutdown timed out", "draining", stuck)
		return fmt.Errorf("%w: %v", ErrDrainTimeout, stuck)
	}
}
