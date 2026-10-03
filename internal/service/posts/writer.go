// Package posts holds the one seam every thread append goes through.
package posts

import (
	"context"
	"errors"
	"sync"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Writer appends records to threads and announces them (docs/03 "Write
// path", steps 3 to 6). Every write to a thread log that the server makes on
// a request goes through Append, so nothing appends without publishing and
// nothing publishes what was not appended. Edits, deletes and moderation
// (TKT-01M29CA9BP and later) reuse it. TestEveryAppendGoesThroughTheWriter
// holds the rest of the module to that.
//
// Because every append passes here, Hold can stop them all for the moment a
// backup takes its snapshot (TKT-01M29CXK35). The zero value is ready to use.
type Writer struct {
	// Publish announces a durable append: events.Bus.Publish in the server.
	// Nil publishes nothing, for tests that do not watch.
	Publish func(events.Event)

	gate gate
}

// Append writes a to th through its actor, which returns once the record is
// durable under the thread's sync policy, then publishes the event every
// subscriber (the projection, and later search, SSE and notifications)
// catches up from.
//
// While a Hold is in force, Append waits for its release before it starts.
// A Hold is capped by its context, so the wait is too.
//
// A failed append publishes nothing: there is nothing new to catch up to.
func (w *Writer) Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error) {
	w.gate.enter()
	defer w.gate.leave()
	res, err := th.Actor.Append(a)
	if err != nil {
		return res, err
	}
	if w.Publish != nil {
		w.Publish(events.Event{Thread: th.ID, Ordinal: res.Ordinal, LogLength: res.LogLength})
	}
	return res, nil
}

// AppendRun writes run to th as one contiguous run through its actor
// (threadlog.Actor.AppendRun), all of it or none, and publishes one event
// at the run's end, as a thread-level change: a subscriber catches up from
// its log length and reads every record the run wrote. A move's target run
// is the caller (docs/03 "How a move is written"). It waits on a Hold as
// Append does.
func (w *Writer) AppendRun(th *threadcache.Thread, run []threadlog.Append) ([]threadlog.Result, error) {
	w.gate.enter()
	defer w.gate.leave()
	res, err := th.Actor.AppendRun(run)
	if err != nil || len(res) == 0 {
		return res, err
	}
	if w.Publish != nil {
		w.Publish(events.Event{Thread: th.ID, LogLength: res[len(res)-1].LogLength})
	}
	return res, nil
}

// ErrHeld is Hold while another Hold is in force.
var ErrHeld = errors.New("posts: appends are already held")

// Hold stops new appends and waits for those in flight to finish, then
// returns with no append running and none able to start until release is
// called. Reads are never held.
//
// If ctx is done before the appends in flight finish, Hold gives up: the
// appends that queued behind it go on, the one still running finishes
// whenever its actor does, and Hold returns ctx's error. This is a gate
// rather than a sync.RWMutex because a pending Lock cannot be abandoned and
// blocks every new reader while it waits, so one stalled append would hold
// every other past any cap (Terva review 1943).
//
// release may be called more than once. Only one Hold is in force at a time.
func (w *Writer) Hold(ctx context.Context) (release func(), err error) {
	return w.gate.hold(ctx)
}

// gate is Writer's hold. mu guards every field. open is closed whenever
// held is false; drained, when set, is closed once active reaches zero.
type gate struct {
	mu      sync.Mutex
	active  int
	held    bool
	open    chan struct{}
	drained chan struct{}
}

func (g *gate) enter() {
	g.mu.Lock()
	for g.held {
		wait := g.open
		g.mu.Unlock()
		<-wait
		g.mu.Lock()
	}
	g.active++
	g.mu.Unlock()
}

func (g *gate) leave() {
	g.mu.Lock()
	g.active--
	if g.active == 0 && g.drained != nil {
		close(g.drained)
		g.drained = nil
	}
	g.mu.Unlock()
}

func (g *gate) hold(ctx context.Context) (func(), error) {
	g.mu.Lock()
	if g.held {
		g.mu.Unlock()
		return nil, ErrHeld
	}
	g.held = true
	g.open = make(chan struct{})
	var drained chan struct{}
	if g.active > 0 {
		g.drained = make(chan struct{})
		drained = g.drained
	}
	g.mu.Unlock()

	release := sync.OnceFunc(func() {
		g.mu.Lock()
		g.held = false
		g.drained = nil
		close(g.open)
		g.mu.Unlock()
	})
	if drained != nil {
		select {
		case <-drained:
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}
