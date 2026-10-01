// Package sse is the live-update endpoint, GET /events (docs/02 "Live
// updates", docs/07, TKT-01M29CRJ3X).
//
// A stream carries invalidation hints, `{thread_id, log_length}`, and
// nothing else: the client fetches what changed through the ordinary read
// path. The hint carries no more than the thread page, which shows every
// reader the thread's log length anyway, so a hint about a removed, hidden
// or redacted post tells a reader no more than their next page fetch would.
//
// It is not, for all that, free of permission checks. docs/14 requires that
// a live event reach a viewer only if that viewer may read the thread when
// the event is sent, so the stream is held to the thread page throughout:
//
//   - a subscription is admitted only as the page would admit the viewer;
//   - before every write, and at every heartbeat, the stream resolves its
//     session or agent credential again and checks access again, and closes
//     when either is lost;
//   - the hub routes an event only once the thread's projection row has
//     reached it, so a move is routed to the forum the thread joined, and
//     every access check reads a row that already reflects the event.
//
// The hub (this file) is the event-bus subscriber that routes events to
// streams. It never blocks on a stream, and it never decides access: that is
// the stream's own goroutine, at the moment it writes (handler.go).
package sse

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// The hub's defaults. Each is overridable in HubOptions, for tests.
const (
	// DefaultProjectionWait is how long the hub waits for a thread's
	// projection row to reach an event before it gives up on the thread and
	// closes every stream that could have been told about it (docs/02).
	DefaultProjectionWait = 5 * time.Second

	// DefaultProjectionPoll is how often the hub reads the row while it
	// waits. The projection writer normally gets there in a millisecond or
	// two, so most events cost one read.
	DefaultProjectionPoll = 20 * time.Millisecond

	// DefaultStreamQueue is how many hints a stream may have waiting before
	// it is closed as too slow. A browser reconnects and resynchronises from
	// the snapshot, which costs it less than a hint it never reads.
	DefaultStreamQueue = 64
)

// Limits bounds the streams open at once (config limits.sse_*). Each is at
// least 1; a zero is treated as 1, so a forgotten field refuses rather than
// admits without bound.
type Limits struct {
	// Connections is every stream.
	Connections int
	// PerUser is one signed-in user's streams.
	PerUser int
	// Anonymous is every anonymous stream together.
	Anonymous int
}

// Projections reads a thread's projection row: threads.Projections in the
// server.
type Projections interface {
	Projection(ctx context.Context, id threadid.ID) (threads.Projection, error)
}

// HubOptions configures a Hub.
type HubOptions struct {
	Projections Projections
	Limits      Limits
	// Logger receives lag and overflow lines. Nil is slog.Default.
	Logger *slog.Logger

	// ProjectionWait, ProjectionPoll and Queue override the defaults above
	// when non-zero.
	ProjectionWait time.Duration
	ProjectionPoll time.Duration
	Queue          int
}

// Errors from opening a stream.
var (
	// errFull is a limit reached: rate-limited, with Retry-After.
	errFull = errors.New("sse: the stream limit is reached")
	// errClosed is a hub that is shutting down: unavailable.
	errClosed = errors.New("sse: the server is shutting down")
)

// Hub routes events from the bus to the streams that asked about them. It is
// an events.Subscriber named "sse".
type Hub struct {
	proj   Projections
	log    *slog.Logger
	limits Limits
	wait   time.Duration
	poll   time.Duration
	queue  int

	// done is closed by Close: every stream returns, and no new one opens.
	done chan struct{}

	mu       sync.Mutex
	closed   bool
	streams  map[*stream]struct{}
	byThread map[threadid.ID]map[*stream]struct{}
	byForum  map[int64]map[*stream]struct{}
	forumed  int // streams naming at least one forum
	perUser  map[int64]int
	anon     int

	refused    atomic.Uint64
	overflowed atomic.Uint64
	lagged     atomic.Uint64
}

// stream is one open subscription, as the hub sees it.
type stream struct {
	threads []threadid.ID
	forums  []int64
	user    int64 // 0 for an anonymous stream

	// hints is the stream's queue. The hub sends without blocking; a full
	// queue closes the stream.
	hints chan hint
	// resync asks the stream to check access and resend its snapshot,
	// because the hub lost events it may have needed.
	resync chan struct{}
	// kill is closed when the hub ends the stream, and ended is set first,
	// so the hub queues nothing more for it.
	kill     chan struct{}
	killOnce sync.Once
	ended    atomic.Bool
}

// hint is one thread having reached a log length.
type hint struct {
	thread    threadid.ID
	logLength int64
}

func (s *stream) end() {
	s.ended.Store(true)
	s.killOnce.Do(func() { close(s.kill) })
}

// NewHub returns a hub with no streams.
func NewHub(o HubOptions) *Hub {
	h := &Hub{proj: o.Projections, log: o.Logger, limits: o.Limits, wait: o.ProjectionWait, poll: o.ProjectionPoll,
		queue: o.Queue, done: make(chan struct{}), streams: map[*stream]struct{}{},
		byThread: map[threadid.ID]map[*stream]struct{}{}, byForum: map[int64]map[*stream]struct{}{},
		perUser: map[int64]int{}}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.wait <= 0 {
		h.wait = DefaultProjectionWait
	}
	if h.poll <= 0 {
		h.poll = DefaultProjectionPoll
	}
	if h.queue <= 0 {
		h.queue = DefaultStreamQueue
	}
	h.limits.Connections = max(h.limits.Connections, 1)
	h.limits.PerUser = max(h.limits.PerUser, 1)
	h.limits.Anonymous = max(h.limits.Anonymous, 1)
	return h
}

// Name identifies the hub on the bus.
func (h *Hub) Name() string { return "sse" }

// open reserves a slot for a stream and registers it, in one step, so a
// stream is counted against the limits from the moment it can receive.
// user is 0 for an anonymous stream.
func (h *Hub) open(ts []threadid.ID, forums []int64, user int64) (*stream, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closed:
		return nil, errClosed
	case len(h.streams) >= h.limits.Connections,
		user != 0 && h.perUser[user] >= h.limits.PerUser,
		user == 0 && h.anon >= h.limits.Anonymous:
		h.refused.Add(1)
		return nil, errFull
	}
	s := &stream{threads: ts, forums: forums, user: user, hints: make(chan hint, h.queue),
		resync: make(chan struct{}, 1), kill: make(chan struct{})}
	h.streams[s] = struct{}{}
	for _, id := range ts {
		add(h.byThread, id, s)
	}
	for _, f := range forums {
		add(h.byForum, f, s)
	}
	if len(forums) > 0 {
		h.forumed++
	}
	if user != 0 {
		h.perUser[user]++
	} else {
		h.anon++
	}
	return s, nil
}

// release unregisters a stream and frees its slot. It is safe to call once
// per stream, from the stream's own goroutine as it returns.
func (h *Hub) release(s *stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.streams[s]; !ok {
		return
	}
	delete(h.streams, s)
	for _, id := range s.threads {
		remove(h.byThread, id, s)
	}
	for _, f := range s.forums {
		remove(h.byForum, f, s)
	}
	if len(s.forums) > 0 {
		h.forumed--
	}
	if s.user != 0 {
		if h.perUser[s.user]--; h.perUser[s.user] <= 0 {
			delete(h.perUser, s.user)
		}
	} else {
		h.anon--
	}
	s.end()
}

func add[K comparable](m map[K]map[*stream]struct{}, k K, s *stream) {
	set := m[k]
	if set == nil {
		set = map[*stream]struct{}{}
		m[k] = set
	}
	set[s] = struct{}{}
}

func remove[K comparable](m map[K]map[*stream]struct{}, k K, s *stream) {
	if set := m[k]; set != nil {
		delete(set, s)
		if len(set) == 0 {
			delete(m, k)
		}
	}
}

// Deliver routes a batch of events to the streams that asked about them.
//
// A batch reporting dropped events has lost hints some stream needed. A
// stream that names only threads resends its snapshot; one that names a
// forum has no snapshot to resend, so it is closed, and its client refetches
// on the reconnect's ready (docs/02, after Terva review 1708).
//
// Each thread is routed by its newest event in the batch, and only once its
// projection row has reached that event: the forum it routes by and the
// access every stream then checks are read from that row. A thread whose
// row is still behind after the wait gets no hint, and every stream that
// names it or any forum is closed, so each reconnect is judged afresh
// (Terva review 1712).
//
// Deliver never blocks on a stream. It may block on the projection, for at
// most the wait, which costs only this subscriber's own queue.
func (h *Hub) Deliver(ctx context.Context, b events.Batch) error {
	h.mu.Lock()
	if len(h.streams) == 0 {
		h.mu.Unlock()
		return nil
	}
	if b.Dropped > 0 {
		for s := range h.streams {
			if len(s.forums) > 0 {
				s.end()
				continue
			}
			select {
			case s.resync <- struct{}{}:
			default: // one is already pending, and one is enough
			}
		}
	}
	// Only threads some stream could hear about are waited for: one a stream
	// names, or any thread at all while a stream names a forum.
	var order []threadid.ID
	targets := map[threadid.ID]int64{}
	for _, ev := range b.Events {
		if _, named := h.byThread[ev.Thread]; !named && h.forumed == 0 {
			continue
		}
		if _, seen := targets[ev.Thread]; !seen {
			order = append(order, ev.Thread)
		}
		targets[ev.Thread] = max(targets[ev.Thread], ev.LogLength)
	}
	h.mu.Unlock()
	if len(order) == 0 {
		return nil
	}

	rows, lagging, timedOut := h.await(ctx, order, targets)

	h.mu.Lock()
	defer h.mu.Unlock()
	// A thread still behind ends the streams that could hear of it, whether
	// the wait ran out or was interrupted, because either way its hint
	// cannot be routed. Only a wait that ran out is lag (Terva review 1747).
	for _, id := range lagging {
		if timedOut {
			h.lagged.Add(1)
		}
		for s := range h.byThread[id] {
			s.end()
		}
		for s := range h.streams {
			if len(s.forums) > 0 {
				s.end()
			}
		}
	}
	for _, id := range order {
		row, ok := rows[id]
		if !ok {
			continue
		}
		hn := hint{thread: id, logLength: targets[id]}
		for s := range h.byThread[id] {
			h.send(s, hn)
		}
		for s := range h.byForum[row.ForumID] {
			if _, named := h.byThread[id][s]; !named {
				h.send(s, hn)
			}
		}
	}
	return nil
}

// send queues hn on s, or closes s when its queue is full. A stream already
// ended gets nothing. The caller holds mu.
func (h *Hub) send(s *stream, hn hint) {
	if s.ended.Load() {
		return
	}
	select {
	case s.hints <- hn:
	default:
		h.overflowed.Add(1)
		h.log.Warn("a live-update stream fell behind and was closed", "queue", cap(s.hints))
		s.end()
	}
}

// await waits until each thread's projection row has reached its target,
// and returns the rows that did and the threads that did not within the
// wait. The hub closing, or ctx ending, stops the wait: every thread still
// behind is then returned too. timedOut reports whether the wait ran its
// full length, which only then makes the threads behind lag.
func (h *Hub) await(ctx context.Context, order []threadid.ID, targets map[threadid.ID]int64) (rows map[threadid.ID]threads.Projection, behind []threadid.ID, timedOut bool) {
	rows = make(map[threadid.ID]threads.Projection, len(order))
	pending := order
	deadline := time.Now().Add(h.wait)
	var logged bool
	for {
		next := pending[:0:0]
		for _, id := range pending {
			p, err := h.proj.Projection(ctx, id)
			switch {
			case err != nil:
				if !logged {
					h.log.Warn("reading a thread's projection for live updates failed", "thread", id, "err", err)
					logged = true
				}
				next = append(next, id)
			case p.Found && p.LogLength >= targets[id]:
				rows[id] = p
			default:
				next = append(next, id)
			}
		}
		pending = next
		if len(pending) == 0 {
			return rows, nil, false
		}
		left := time.Until(deadline)
		if left <= 0 {
			h.log.Warn("a thread's projection did not reach its event in time; closing the live-update streams that could hear of it",
				"threads", len(pending), "wait", h.wait)
			return rows, pending, true
		}
		t := time.NewTimer(min(h.poll, left))
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return rows, pending, false
		case <-h.done:
			t.Stop()
			return rows, pending, false
		}
	}
}

// Close ends every stream and refuses new ones. serve calls it as shutdown
// begins (http.Server.RegisterOnShutdown), because a stream never finishes
// by itself and the drain waits for every handler. It is safe to call more
// than once.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

// Stats is the hub's standing, for the metrics endpoint (TKT-01M29CEE7S).
type Stats struct {
	// Open is every stream open now, and Anonymous the anonymous ones.
	Open      int
	Anonymous int
	// Refused counts streams a limit turned away, Overflowed streams closed
	// because their queue filled, and Lagged threads whose projection did
	// not reach an event within the wait.
	Refused    uint64
	Overflowed uint64
	Lagged     uint64
}

// Stats reports the hub's standing.
func (h *Hub) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Stats{Open: len(h.streams), Anonymous: h.anon, Refused: h.refused.Load(),
		Overflowed: h.overflowed.Load(), Lagged: h.lagged.Load()}
}
