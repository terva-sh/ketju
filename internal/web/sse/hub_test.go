package sse

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// fakeProjections answers every thread as projected to forum, up to len, or
// as never projected when behind is set.
type fakeProjections struct {
	forum  int64
	behind bool
}

func (f fakeProjections) Projection(_ context.Context, _ threadid.ID) (threads.Projection, error) {
	if f.behind {
		return threads.Projection{}, nil
	}
	return threads.Projection{Found: true, ForumID: f.forum, LogLength: 1 << 40}, nil
}

// fakeThreads admits everything but the threads it has been told to
// refuse, which it refuses as forbidden. A thread is in forum 1 unless
// forumOf says otherwise; every forum is readable until forumGone.
type fakeThreads struct {
	mu        sync.Mutex
	refuse    map[threadid.ID]bool
	forumOf   map[threadid.ID]int64
	forumGone bool
	// onLength and onRead, when set, run as LogLength or ReadAccess is
	// asked about a thread.
	onLength func(threadid.ID)
	onRead   func(threadid.ID)
}

func (f *fakeThreads) refuseRead(id threadid.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse == nil {
		f.refuse = map[threadid.ID]bool{}
	}
	f.refuse[id] = true
}

func (f *fakeThreads) ReadAccess(_ context.Context, _ domain.Viewer, id threadid.ID) (int64, error) {
	f.mu.Lock()
	hook := f.onRead
	f.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse[id] {
		return 0, problem.Coded(problem.CodeForbidden, errors.New("refused"))
	}
	if forum, ok := f.forumOf[id]; ok {
		return forum, nil
	}
	return 1, nil
}

func (f *fakeThreads) ForumAccess(context.Context, domain.Viewer, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forumGone {
		return 0, problem.Coded(problem.CodeForbidden, errors.New("refused"))
	}
	return 1, nil
}

func (f *fakeThreads) LogLength(_ context.Context, id threadid.ID) (int64, error) {
	f.mu.Lock()
	hook := f.onLength
	f.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	return 100, nil
}

var quiet = slog.New(slog.DiscardHandler)

// TestSlowClientIsClosedAndPublishNeverBlocks: a stream that never reads is
// closed once its queue fills, and every Deliver returns at once while it
// does; a hub stuck waiting on the projection never makes Publish wait.
func TestSlowClientIsClosedAndPublishNeverBlocks(t *testing.T) {
	h := NewHub(HubOptions{Projections: fakeProjections{forum: 1}, Queue: 4, Logger: quiet,
		Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}})
	id := threadid.ID(1 << 20)
	s, err := h.open([]threadid.ID{id}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		start := time.Now()
		if err := h.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: int64(100 + i)}}}); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("Deliver %d took %s with a stream not reading", i, d)
		}
	}
	select {
	case <-s.kill:
	default:
		t.Fatal("a stream whose queue filled is still open")
	}
	if got := h.Stats().Overflowed; got != 1 {
		t.Errorf("overflowed = %d, want 1", got)
	}

	// The bus side: the hub blocks on a projection that never arrives, and
	// publishing goes on regardless.
	stuck := NewHub(HubOptions{Projections: fakeProjections{behind: true}, ProjectionWait: time.Hour, Logger: quiet,
		Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}})
	if _, err := stuck.open([]threadid.ID{id}, nil, 0); err != nil {
		t.Fatal(err)
	}
	bus := events.New(events.Options{Logger: quiet})
	if err := bus.Subscribe(stuck, 0); err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := range 10 * events.DefaultQueue {
		bus.Publish(events.Event{Thread: id, LogLength: int64(i + 1)})
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("publishing past a stuck hub took %s", d)
	}
	if st := bus.Stats()[0]; st.Dropped == 0 {
		t.Errorf("the stuck hub's queue never filled: %+v", st)
	}
	stuck.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bus.Close(ctx); err != nil {
		t.Errorf("the bus did not drain once the hub closed: %v", err)
	}
}

// pipeWriter is a ResponseWriter whose writes arrive on a channel, so a
// test can read a stream as the handler writes it, inside a synctest bubble
// as well as out.
type pipeWriter struct {
	hdr    http.Header
	mu     sync.Mutex
	status int
	writes chan string
}

func newPipeWriter() *pipeWriter {
	return &pipeWriter{hdr: http.Header{}, writes: make(chan string, 1024)}
}

func (p *pipeWriter) Header() http.Header { return p.hdr }
func (p *pipeWriter) WriteHeader(code int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == 0 {
		p.status = code
	}
}

func (p *pipeWriter) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusOK)
	p.writes <- string(b)
	return len(b), nil
}

// TestHeartbeat: an idle stream sends ": hb" every 25 seconds, and nothing
// else.
func TestHeartbeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := &Handler{Hub: NewHub(HubOptions{Projections: fakeProjections{forum: 1}, Logger: quiet,
			Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}}), Threads: &fakeThreads{}, Logger: quiet}
		id := threadid.ID(1 << 20)
		ctx, cancel := context.WithCancel(t.Context())
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events?threads="+id.String(), nil)
		pw := newPipeWriter()
		done := make(chan struct{})
		go func() { defer close(done); h.ServeHTTP(pw, req) }()

		synctest.Wait()
		start := time.Now()
		if first := <-pw.writes; !strings.Contains(first, "event: ready") {
			t.Fatalf("the stream opened with %q", first)
		}
		if snap := <-pw.writes; !strings.Contains(snap, `"log_length":100`) {
			t.Fatalf("the snapshot is %q", snap)
		}
		for i := 1; i <= 3; i++ {
			time.Sleep(DefaultHeartbeat)
			synctest.Wait()
			select {
			case got := <-pw.writes:
				if got != ": hb\n\n" {
					t.Fatalf("at %s the stream wrote %q", time.Since(start), got)
				}
			default:
				t.Fatalf("no heartbeat at %s", time.Since(start))
			}
			if d := time.Since(start); d != time.Duration(i)*DefaultHeartbeat {
				t.Errorf("heartbeat %d at %s", i, d)
			}
		}
		if DefaultHeartbeat != 25*time.Second {
			t.Errorf("the heartbeat is %s; docs/02 says 25 s", DefaultHeartbeat)
		}
		cancel()
		<-done
		if n := h.Hub.Stats().Open; n != 0 {
			t.Errorf("%d streams still registered after the client went", n)
		}
	})
}

// TestForumHintForUnreadableThreadIsSkipped: a hint that reached a stream
// through a forum, for a thread the viewer cannot read when it is written,
// is skipped and the stream stays open, because the forum may still be
// readable. The same refusal for a thread the stream names ends it.
func TestForumHintForUnreadableThreadIsSkipped(t *testing.T) {
	readable, hidden := threadid.ID(1<<20), threadid.ID(2<<20)
	ft := &fakeThreads{}
	ft.refuseRead(hidden)
	h := &Handler{Hub: NewHub(HubOptions{Projections: fakeProjections{forum: 1}, Logger: quiet,
		Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}}),
		Threads: ft, Logger: quiet, Heartbeat: time.Hour}
	run := func(query string) (*pipeWriter, context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(t.Context())
		pw := newPipeWriter()
		done := make(chan struct{})
		go func() {
			defer close(done)
			h.ServeHTTP(pw, httptest.NewRequestWithContext(ctx, http.MethodGet, "/events?"+query, nil))
		}()
		return pw, cancel, done
	}
	next := func(pw *pipeWriter) string {
		select {
		case s := <-pw.writes:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("nothing written")
			return ""
		}
	}

	pw, cancel, done := run("forums=general")
	defer cancel()
	next(pw) // ready
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	deliver := func(id threadid.ID, n int64) {
		if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: n}}}); err != nil {
			t.Fatal(err)
		}
	}
	deliver(hidden, 500)
	deliver(readable, 600)
	if got := next(pw); !strings.Contains(got, readable.String()) || strings.Contains(got, hidden.String()) {
		t.Errorf("after a hidden thread's hint and a readable one's, the stream wrote %q", got)
	}
	select {
	case <-done:
		t.Fatal("the forum stream ended over a thread it does not name")
	default:
	}

	pw, cancel2, done := run("threads=" + readable.String() + "&forums=general")
	defer cancel2()
	next(pw) // ready
	next(pw) // the snapshot
	waitFor(t, func() bool { return h.Hub.Stats().Open == 2 })
	ft.refuseRead(readable)
	deliver(readable, 700)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a stream kept going after a thread it names became unreadable")
	}
	select {
	case got := <-pw.writes:
		t.Errorf("the stream wrote %q before it ended", got)
	default:
	}
}

// TestHubLimitsAreAtLeastOne: a zero limit refuses rather than admitting
// without bound, and a closed hub refuses every new stream.
func TestHubLimitsAreAtLeastOne(t *testing.T) {
	h := NewHub(HubOptions{Projections: fakeProjections{}, Logger: quiet})
	if _, err := h.open([]threadid.ID{1}, nil, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := h.open([]threadid.ID{1}, nil, 8); !errors.Is(err, errFull) {
		t.Errorf("a second stream under zero limits = %v, want errFull", err)
	}
	h.Close()
	h.Close()
	if _, err := h.open([]threadid.ID{1}, nil, 0); !errors.Is(err, errClosed) {
		t.Errorf("a stream on a closed hub = %v, want errClosed", err)
	}
}

// streamOn runs h for query in the background and returns its writes, a
// cancel, and a channel closed when the handler returns.
func streamOn(t *testing.T, h *Handler, query string) (*pipeWriter, chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	pw := newPipeWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(pw, httptest.NewRequestWithContext(ctx, http.MethodGet, "/events?"+query, nil))
	}()
	return pw, done
}

// read is the next write, failing after five seconds.
func read(t *testing.T, pw *pipeWriter) string {
	t.Helper()
	select {
	case s := <-pw.writes:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("nothing written")
		return ""
	}
}

// endsSilently fails unless the handler returns without writing again.
func endsSilently(t *testing.T, pw *pipeWriter, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case got := <-pw.writes:
		t.Fatalf("the stream wrote %q where it should have ended", got)
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
	select {
	case got := <-pw.writes:
		t.Errorf("the stream wrote %q as it ended", got)
	default:
	}
}

func newFakeHandler(ft *fakeThreads) *Handler {
	return &Handler{Hub: NewHub(HubOptions{Projections: fakeProjections{forum: 1}, Logger: quiet,
		Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}}), Threads: ft, Logger: quiet, Heartbeat: time.Hour}
}

// TestForumHintChecksTheThreadsForumNow: the hub routes a hint by the forum
// the thread was in when it was projected. If, when the hint is written,
// the thread is in a forum the stream does not name, it is skipped, though
// the viewer can read it there (Terva review 1733).
func TestForumHintChecksTheThreadsForumNow(t *testing.T) {
	left, stayed := threadid.ID(1<<20), threadid.ID(2<<20)
	ft := &fakeThreads{forumOf: map[threadid.ID]int64{left: 2}}
	h := newFakeHandler(ft)
	pw, _ := streamOn(t, h, "forums=general")
	read(t, pw) // ready
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	for _, id := range []threadid.ID{left, stayed} {
		if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: 500}}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := read(t, pw); !strings.Contains(got, stayed.String()) || strings.Contains(got, left.String()) {
		t.Errorf("the stream wrote %q; a thread that left the forum was hinted, or the one that stayed was not", got)
	}
}

// TestHintChecksEveryNamedForum: losing read on a forum the stream names
// ends it before the next hint, even a hint for a thread it names that is
// still readable, without waiting for the heartbeat (Terva review 1733).
func TestHintChecksEveryNamedForum(t *testing.T) {
	id := threadid.ID(1 << 20)
	ft := &fakeThreads{}
	h := newFakeHandler(ft)
	pw, done := streamOn(t, h, "threads="+id.String()+"&forums=general")
	read(t, pw) // ready
	read(t, pw) // the snapshot
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	ft.mu.Lock()
	ft.forumGone = true
	ft.mu.Unlock()
	if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: 500}}}); err != nil {
		t.Fatal(err)
	}
	endsSilently(t, pw, done)
}

// TestSnapshotRechecksBeforeEachThread: access lost while a snapshot is
// being written stops it before the next thread's hint (Terva review 1733).
func TestSnapshotRechecksBeforeEachThread(t *testing.T) {
	a, b := threadid.ID(1<<20), threadid.ID(2<<20)
	ft := &fakeThreads{}
	ft.onLength = func(id threadid.ID) {
		if id == a {
			ft.refuseRead(b)
		}
	}
	h := newFakeHandler(ft)
	pw, done := streamOn(t, h, "threads="+a.String()+","+b.String())
	read(t, pw) // ready
	if got := read(t, pw); !strings.Contains(got, a.String()) {
		t.Fatalf("the first snapshot hint is %q", got)
	}
	endsSilently(t, pw, done)
}

// TestEndedStreamWritesNoQueuedHint: once the hub has ended a stream, a hint
// already in its queue is not written, however the select falls, and the hub
// queues nothing more for it (Terva review 1733).
func TestEndedStreamWritesNoQueuedHint(t *testing.T) {
	h := NewHub(HubOptions{Projections: fakeProjections{forum: 1}, Logger: quiet,
		Limits: Limits{Connections: 100, PerUser: 100, Anonymous: 100}})
	hd := &Handler{Hub: h, Threads: &fakeThreads{}, Logger: quiet}
	for range 200 {
		s, err := h.open([]threadid.ID{1}, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		h.send(s, hint{thread: 1, logLength: 10})
		s.end()
		h.send(s, hint{thread: 1, logLength: 20})
		h.mu.Unlock()
		if n := len(s.hints); n != 1 {
			t.Fatalf("%d hints queued; the hub queued for an ended stream", n)
		}
		st := &live{h: hd, s: s}
		if act, _ := st.next(t.Context(), nil); act != actStop {
			t.Fatalf("an ended stream with a hint queued chose %v, not to stop", act)
		}
		h.release(s)
	}
}

// TestHubEndDuringDeliverWritesNothing: a hint already past its access
// checks when the hub ends the stream, for overflow or lag, is not written
// (Terva review 1742).
func TestHubEndDuringDeliverWritesNothing(t *testing.T) {
	id := threadid.ID(1 << 20)
	ft := &fakeThreads{}
	h := newFakeHandler(ft)
	pw, done := streamOn(t, h, "threads="+id.String())
	read(t, pw) // ready
	read(t, pw) // the snapshot
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	ft.mu.Lock()
	ft.onRead = func(threadid.ID) {
		h.Hub.mu.Lock()
		defer h.Hub.mu.Unlock()
		for s := range h.Hub.streams {
			s.end()
		}
	}
	ft.mu.Unlock()
	if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: 500}}}); err != nil {
		t.Fatal(err)
	}
	endsSilently(t, pw, done)
}

// stuckWriter is a ResponseWriter whose writes, once stuck, block as a
// client that has stopped reading makes them block: until the write
// deadline passes.
type stuckWriter struct {
	pipeWriter
	mu       sync.Mutex
	stuck    bool
	deadline time.Time
	changed  chan struct{}
}

func (w *stuckWriter) SetWriteDeadline(d time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadline = d
	close(w.changed)
	w.changed = make(chan struct{})
	return nil
}

func (w *stuckWriter) Write(b []byte) (int, error) {
	for {
		w.mu.Lock()
		stuck, d, ch := w.stuck, w.deadline, w.changed
		w.mu.Unlock()
		if !stuck {
			return w.pipeWriter.Write(b)
		}
		if !d.IsZero() && !time.Now().Before(d) {
			return 0, os.ErrDeadlineExceeded
		}
		var timer <-chan time.Time
		if !d.IsZero() {
			timer = time.After(time.Until(d))
		}
		select {
		case <-ch:
		case <-timer:
		}
	}
}

// TestShutdownInterruptsABlockedWrite: a stream blocked writing to a client
// that has stopped reading returns as soon as the hub closes, not when its
// ten-second write deadline passes, so it cannot hold serve's drain (Terva
// review 1742).
func TestShutdownInterruptsABlockedWrite(t *testing.T) {
	id := threadid.ID(1 << 20)
	h := newFakeHandler(&fakeThreads{})
	sw := &stuckWriter{pipeWriter: *newPipeWriter(), changed: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(sw, httptest.NewRequestWithContext(ctx, http.MethodGet, "/events?threads="+id.String(), nil))
	}()
	read(t, &sw.pipeWriter) // ready
	read(t, &sw.pipeWriter) // the snapshot
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	sw.mu.Lock()
	sw.stuck = true
	sw.mu.Unlock()
	if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: 500}}}); err != nil {
		t.Fatal(err)
	}
	// Give the stream time to reach the write and block in it.
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	h.Hub.Close()
	select {
	case <-done:
	case <-time.After(DefaultWriteTimeout):
		t.Fatal("the stream blocked in its write until the deadline, past the hub's close")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the stream took %s to return after the hub closed", d)
	}
	if n := h.Hub.Stats().Open; n != 0 {
		t.Errorf("%d streams still registered", n)
	}
}

// TestReadyFollowsAFreshCheck: access lost between admission and the first
// write is refused as admission refuses it, and ready is never written
// (Terva review 1744).
func TestReadyFollowsAFreshCheck(t *testing.T) {
	id := threadid.ID(1 << 20)
	ft := &fakeThreads{}
	var calls int
	ft.onRead = func(got threadid.ID) {
		ft.mu.Lock()
		defer ft.mu.Unlock()
		if calls++; calls == 2 { // the first is admission
			if ft.refuse == nil {
				ft.refuse = map[threadid.ID]bool{}
			}
			ft.refuse[got] = true
		}
	}
	h := newFakeHandler(ft)
	pw := newPipeWriter()
	h.ServeHTTP(pw, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events?threads="+id.String(), nil))
	if pw.status != http.StatusForbidden || strings.HasPrefix(pw.hdr.Get("Content-Type"), "text/event-stream") {
		t.Errorf("a grant lost before ready answered %d %q, want the 403 admission gives", pw.status, pw.hdr.Get("Content-Type"))
	}
	for len(pw.writes) > 0 {
		if got := <-pw.writes; strings.Contains(got, "event:") {
			t.Errorf("the stream wrote %q", got)
		}
	}
	if n := h.Hub.Stats().Open; n != 0 {
		t.Errorf("%d streams still registered", n)
	}
}

// deadlineLog is a ResponseWriter that records every write deadline, and
// closes the hub from inside the first write after arm, as a shutdown
// landing mid-write would.
type deadlineLog struct {
	pipeWriter
	mu    sync.Mutex
	log   []time.Time
	armed func()
}

func (d *deadlineLog) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = append(d.log, t)
	return nil
}

func (d *deadlineLog) Write(b []byte) (int, error) {
	d.mu.Lock()
	f := d.armed
	d.armed = nil
	d.mu.Unlock()
	if f != nil {
		f()
	}
	return d.pipeWriter.Write(b)
}

// TestStoppedStreamKeepsAPastDeadline: once the hub closes in the middle of
// a write, no later deadline is in the future or cleared, so nothing can
// hold the drain (Terva review 1744).
func TestStoppedStreamKeepsAPastDeadline(t *testing.T) {
	id := threadid.ID(1 << 20)
	h := newFakeHandler(&fakeThreads{})
	dl := &deadlineLog{pipeWriter: *newPipeWriter()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(dl, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events?threads="+id.String(), nil))
	}()
	read(t, &dl.pipeWriter) // ready
	read(t, &dl.pipeWriter) // the snapshot
	waitFor(t, func() bool { return h.Hub.Stats().Open == 1 })
	var closedAt time.Time
	var from int
	dl.mu.Lock()
	dl.armed = func() {
		dl.mu.Lock()
		from = len(dl.log)
		dl.mu.Unlock()
		closedAt = time.Now()
		h.Hub.Close()
	}
	dl.mu.Unlock()
	if err := h.Hub.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: 500}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not return after the hub closed")
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	after := dl.log[from:]
	if len(after) == 0 {
		t.Fatal("no deadline was set after the hub closed")
	}
	for _, d := range after {
		if d.IsZero() || d.After(closedAt.Add(time.Second)) {
			t.Errorf("after the hub closed a deadline of %v was set; every one must be in the past", d)
		}
	}
}

// fakeCredentials answers every refresh with err, or with the credential
// unchanged when err is nil.
type fakeCredentials struct{ err error }

func (f fakeCredentials) Refresh(_ context.Context, c agentcred.Credential) (agentcred.Credential, error) {
	return c, f.err
}

// TestRefreshErrorIsNotAnEndedSession: a credential that is gone is
// answered unauthenticated before ready, and a refresh that failed for
// another reason is a server error, not a sign-out. Either way nothing is
// streamed (Terva review 1745).
func TestRefreshErrorIsNotAnEndedSession(t *testing.T) {
	id := threadid.ID(1 << 20)
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"gone", agentcred.ErrNoCredential, http.StatusUnauthorized},
		{"the database failed", errors.New("database is locked"), http.StatusInternalServerError},
		{"live", nil, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newFakeHandler(&fakeThreads{})
			h.Credentials = fakeCredentials{err: c.err}
			cred := agentcred.Credential{ID: 1, UserID: 9, Role: domain.RoleMember}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx = domain.WithViewer(agentcred.With(ctx, cred), cred.Viewer())
			pw := newPipeWriter()
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.ServeHTTP(pw, httptest.NewRequestWithContext(ctx, http.MethodGet, "/events?threads="+id.String(), nil))
			}()
			if c.want == http.StatusOK {
				if got := read(t, pw); !strings.Contains(got, "event: ready") {
					t.Fatalf("a live credential's stream opened with %q", got)
				}
				cancel()
				<-done
				return
			}
			<-done
			pw.mu.Lock()
			defer pw.mu.Unlock()
			if pw.status != c.want || strings.HasPrefix(pw.hdr.Get("Content-Type"), "text/event-stream") {
				t.Errorf("answered %d %q, want %d", pw.status, pw.hdr.Get("Content-Type"), c.want)
			}
		})
	}
}

// TestParseBoundsTheListAsSent: the thread and forum bounds count the
// elements sent, so a repeated id or slug past the bound is bad-request
// though it names one thread (Terva review 1745).
func TestParseBoundsTheListAsSent(t *testing.T) {
	id := threadid.ID(1 << 20).String()
	repeat := func(s string, n int) string { return strings.TrimSuffix(strings.Repeat(s+",", n), ",") }
	for _, c := range []struct {
		query string
		ok    bool
	}{
		{"threads=" + repeat(id, MaxThreads), true},
		{"threads=" + repeat(id, MaxThreads+1), false},
		{"forums=" + repeat("general", MaxForums), true},
		{"forums=" + repeat("general", MaxForums+1), false},
	} {
		sub, err := parse(httptest.NewRequest(http.MethodGet, "/events?"+c.query, nil))
		if (err == nil) != c.ok {
			t.Errorf("parse(%.40s… %d parts) = %v, want ok %v", c.query, strings.Count(c.query, ",")+1, err, c.ok)
		}
		if err == nil && len(sub.threads)+len(sub.forums) != 1 {
			t.Errorf("a repeated element was not deduplicated: %+v", sub)
		}
	}
}

// TestParseRefusesAMalformedQuery: a pair that cannot be decoded is
// bad-request, not dropped, so a malformed parameter is never read as one
// that was not sent (Terva review 1746).
func TestParseRefusesAMalformedQuery(t *testing.T) {
	id := threadid.ID(1 << 20).String()
	for _, raw := range []string{
		"threads=" + id + "&forums=%ZZ",
		"threads=" + id + "&%ZZ=general",
		"threads=" + id + ";forums=general",
	} {
		r := httptest.NewRequest(http.MethodGet, "/events", nil)
		r.URL.RawQuery = raw
		if sub, err := parse(r); err == nil {
			t.Errorf("parse(%q) = %+v, want an error", raw, sub)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/events", nil)
	r.URL.RawQuery = "threads=" + id + "&forums=gen%65ral"
	if sub, err := parse(r); err != nil || len(sub.forums) != 1 || sub.forums[0] != "general" {
		t.Errorf("a well-formed escape parsed as %+v, %v", sub, err)
	}
}

// TestInterruptedWaitIsNotLag: a projection wait cut short by the bus's
// context or by shutdown still ends the streams that could hear of the
// thread, but only a wait that ran its length counts as lag (Terva review
// 1747).
func TestInterruptedWaitIsNotLag(t *testing.T) {
	id := threadid.ID(1 << 20)
	batch := events.Batch{Events: []events.Event{{Thread: id, LogLength: 9}}}
	for _, c := range []struct {
		name string
		wait time.Duration
		cut  func(h *Hub, cancel context.CancelFunc)
		lag  uint64
	}{
		{"the context ended", time.Hour, func(_ *Hub, cancel context.CancelFunc) { cancel() }, 0},
		{"the hub closed", time.Hour, func(h *Hub, _ context.CancelFunc) { h.Close() }, 0},
		{"the wait ran out", 30 * time.Millisecond, func(*Hub, context.CancelFunc) {}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := NewHub(HubOptions{Projections: fakeProjections{behind: true}, ProjectionWait: c.wait,
				ProjectionPoll: time.Millisecond, Logger: quiet, Limits: Limits{Connections: 10, PerUser: 10, Anonymous: 10}})
			s, err := h.open([]threadid.ID{id}, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- h.Deliver(ctx, batch) }()
			time.Sleep(10 * time.Millisecond)
			c.cut(h, cancel)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Deliver did not return")
			}
			if !s.ended.Load() {
				t.Error("the stream naming the thread was left open")
			}
			if got := h.Stats().Lagged; got != c.lag {
				t.Errorf("lagged = %d, want %d", got, c.lag)
			}
		})
	}
}
