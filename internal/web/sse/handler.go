package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// The handler's defaults and bounds.
const (
	// DefaultHeartbeat is how often an idle stream sends a comment and checks
	// its session and access again. 25 s stays under every proxy timer the
	// dogfood deployment has (TKT-01M29CRJ3X "Settled in grooming").
	DefaultHeartbeat = 25 * time.Second

	// DefaultWriteTimeout bounds one write. A client that stops reading is
	// cut off here rather than holding a goroutine and a slot forever.
	DefaultWriteTimeout = 10 * time.Second

	// RetryAfter is what a refused stream is told to wait.
	RetryAfter = 30 * time.Second

	// reconnect is the EventSource retry delay the stream asks for.
	reconnect = 5 * time.Second

	// MaxThreads and MaxForums bound one subscription.
	MaxThreads = 20
	MaxForums  = 10

	// maxSlug bounds a forum slug before it is looked up. Real slugs are far
	// shorter; this only keeps a junk query from costing a lookup.
	maxSlug = 100
)

// Threads is what a stream asks of the thread service (threads.Service).
type Threads interface {
	// ReadAccess admits v to a thread as the thread page does, and returns
	// the forum it is in.
	ReadAccess(ctx context.Context, v domain.Viewer, id threadid.ID) (int64, error)
	// ForumAccess admits v to read a forum, as the thread list does, and
	// returns its id.
	ForumAccess(ctx context.Context, v domain.Viewer, slug string) (int64, error)
	// LogLength is the thread's length as a reader's next page would see it.
	LogLength(ctx context.Context, id threadid.ID) (int64, error)
}

// Sessions reads a session again (session.Store).
type Sessions interface {
	Refresh(ctx context.Context, s session.Session) (session.Session, error)
}

// Credentials reads an agent credential again (agentcred.Store).
type Credentials interface {
	Refresh(ctx context.Context, c agentcred.Credential) (agentcred.Credential, error)
}

// Handler serves GET /events.
type Handler struct {
	Hub         *Hub
	Threads     Threads
	Sessions    Sessions
	Credentials Credentials
	// Logger receives refusals the server caused and streams ended by an
	// error. Nil is slog.Default.
	Logger *slog.Logger

	// Heartbeat and WriteTimeout override the defaults when non-zero.
	Heartbeat    time.Duration
	WriteTimeout time.Duration
}

// errEnded is a stream whose viewer can no longer hold it: the session or
// credential ended, or the user is no longer an active member.
var errEnded = errors.New("sse: the stream's session has ended")

// subscription is a parsed query.
type subscription struct {
	threads []threadid.ID
	forums  []string
}

// parse reads ?threads=a,b&forums=x,y. At least one id or slug is needed,
// each parameter at most once, no element empty, and nothing else in the
// query: a malformed subscription is a client bug, and saying so is cheaper
// than holding a stream open for it.
func parse(r *http.Request) (subscription, error) {
	var sub subscription
	// url.Values from URL.Query drops a pair it cannot decode, so a
	// malformed escape or a semicolon separator would be ignored rather
	// than refused. ParseQuery reports it (Terva review 1746).
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return sub, fmt.Errorf("sse: malformed query: %w", err)
	}
	for k, vs := range q {
		if k != "threads" && k != "forums" {
			return sub, fmt.Errorf("sse: unknown parameter %q", k)
		}
		if len(vs) != 1 {
			return sub, fmt.Errorf("sse: %q given %d times", k, len(vs))
		}
	}
	// The bounds count what was sent, before duplicates are dropped, so a
	// list of one id repeated is refused rather than parsed in full (Terva
	// review 1745).
	if v, ok := q["threads"]; ok {
		parts := strings.Split(v[0], ",")
		if len(parts) > MaxThreads {
			return sub, fmt.Errorf("sse: %d threads, at most %d", len(parts), MaxThreads)
		}
		for _, s := range parts {
			id, err := threadid.Parse(s)
			if err != nil {
				return sub, err
			}
			if !slices.Contains(sub.threads, id) {
				sub.threads = append(sub.threads, id)
			}
		}
	}
	if v, ok := q["forums"]; ok {
		parts := strings.Split(v[0], ",")
		if len(parts) > MaxForums {
			return sub, fmt.Errorf("sse: %d forums, at most %d", len(parts), MaxForums)
		}
		for _, s := range parts {
			if s == "" || len(s) > maxSlug {
				return sub, fmt.Errorf("sse: forum slug %q", s)
			}
			if !slices.Contains(sub.forums, s) {
				sub.forums = append(sub.forums, s)
			}
		}
	}
	if len(sub.threads) == 0 && len(sub.forums) == 0 {
		return sub, errors.New("sse: no thread or forum named")
	}
	return sub, nil
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// ServeHTTP admits a subscription as the thread page would admit the viewer,
// then streams hints until the client goes, the server shuts down, the hub
// ends the stream, or the viewer loses the session or access it opened with.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		problem.Write(w, r, problem.CodeMethodNotAllowed)
		return
	}
	sub, err := parse(r)
	if err != nil {
		problem.Write(w, r, problem.CodeBadRequest)
		return
	}
	// A browser says when another site started the request. The stream is
	// GET, so CSRF does not see it, and a page elsewhere holding a reader's
	// stream open would spend their slots and the server's.
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		problem.Write(w, r, problem.CodeForbidden)
		return
	}
	ctx := r.Context()
	p := principalFrom(ctx)
	// The pre-user gate refuses /events before it gets here. This says the
	// same in case the gate's list ever names it.
	if p.opened.PreUser {
		problem.Write(w, r, problem.CodeForbidden)
		return
	}
	v := p.opened
	for _, id := range sub.threads {
		if _, err := h.Threads.ReadAccess(ctx, v, id); err != nil {
			problem.WriteError(ctx, w, r, h.logger(), err)
			return
		}
	}
	forums := make([]int64, 0, len(sub.forums))
	for _, slug := range sub.forums {
		id, err := h.Threads.ForumAccess(ctx, v, slug)
		if err != nil {
			problem.WriteError(ctx, w, r, h.logger(), err)
			return
		}
		forums = append(forums, id)
	}

	var user int64
	if v.Member() {
		user = v.UserID
	}
	s, err := h.Hub.open(sub.threads, forums, user)
	switch {
	case errors.Is(err, errFull):
		problem.WriteRetryAfter(w, r, problem.CodeRateLimited, RetryAfter)
		return
	case err != nil:
		problem.Write(w, r, problem.CodeUnavailable)
		return
	}
	defer h.Hub.release(s)

	st := &live{h: h, w: w, rc: http.NewResponseController(w), sub: sub, forums: forums, p: p, s: s,
		sent: map[threadid.ID]int64{}}
	// Checked again now that the stream is registered and before anything
	// is written, ready included, as before every later write: a session
	// or a grant lost since admission is refused as admission would refuse
	// it (Terva review 1744). An ended session is unauthenticated, as the
	// request would be if it came now.
	if _, err := st.checkAll(ctx); err != nil {
		if errors.Is(err, errEnded) {
			problem.Write(w, r, problem.CodeUnauthenticated)
			return
		}
		problem.WriteError(ctx, w, r, h.logger(), err)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-store")
	// nginx buffers a proxied response unless told not to; Traefik and Go's
	// ReverseProxy flush text/event-stream at once without it.
	hdr.Set("X-Accel-Buffering", "no")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := st.run(ctx); err != nil && !errors.Is(err, errEnded) && !errors.Is(err, errStopped) && ctx.Err() == nil {
		h.logger().DebugContext(ctx, "a live-update stream ended", "err", err)
	}
}

// principal is who opened the stream, kept as the resolved session or
// credential, never as the cookie or the token.
type principal struct {
	opened domain.Viewer
	sess   *session.Session
	cred   *agentcred.Credential
}

func principalFrom(ctx context.Context) principal {
	p := principal{opened: domain.ViewerFrom(ctx)}
	if c, ok := agentcred.From(ctx); ok {
		p.cred = &c
		return p
	}
	if s, ok := session.From(ctx); ok {
		p.sess = &s
	}
	return p
}

// current is the viewer as the session or credential stands now. A stream
// opened by a member ends when that member's session or credential has
// ended, or when it no longer resolves to the same active member; one opened
// anonymously stays anonymous, so nothing that happens to a cookie it carried
// can widen what it sees.
//
// Only a session or credential that is gone, or a different or inactive
// member, is errEnded. A refresh that could not be answered, such as a
// database error, is returned as it is: the stream still ends, failing
// closed, but it is not reported as a sign-out (Terva review 1745).
func (h *Handler) current(ctx context.Context, p principal) (domain.Viewer, error) {
	if !p.opened.Member() {
		return domain.Anonymous(), nil
	}
	var v domain.Viewer
	switch {
	case p.cred != nil:
		c, err := h.Credentials.Refresh(ctx, *p.cred)
		if errors.Is(err, agentcred.ErrNoCredential) {
			return v, fmt.Errorf("%w: %w", errEnded, err)
		}
		if err != nil {
			return v, err
		}
		v = c.Viewer()
	case p.sess != nil:
		s, err := h.Sessions.Refresh(ctx, *p.sess)
		if errors.Is(err, session.ErrNoSession) {
			return v, fmt.Errorf("%w: %w", errEnded, err)
		}
		if err != nil {
			return v, err
		}
		v = s.Viewer()
	default:
		// A member viewer with neither is not something the middleware
		// produces. Refuse rather than guess.
		return v, errEnded
	}
	if !v.Member() || v.UserID != p.opened.UserID {
		return v, errEnded
	}
	return v, nil
}

// errStopped is a stream ended by the client, the server's shutdown, or the
// hub.
var errStopped = errors.New("sse: the stream was ended")

// live is one stream's writing side.
type live struct {
	h   *Handler
	w   http.ResponseWriter
	rc  *http.ResponseController
	sub subscription
	// forums are the ids of the forums sub names, as admitted.
	forums []int64
	p      principal
	s      *stream
	// sent is the longest log length written for each thread, so a hint
	// that arrives after a snapshot already covering it is not repeated.
	sent map[threadid.ID]int64

	// deadline orders every change to the write deadline, and halted is set
	// under it by run's watcher once the stream has stopped. From then on
	// the deadline only ever moves to the past (Terva review 1744).
	deadline sync.Mutex
	halted   bool
}

// setDeadline sets the write deadline, unless the stream has stopped, in
// which case it leaves the past deadline the watcher set and reports
// errStopped. A ResponseWriter with no deadline support is not an error.
func (st *live) setDeadline(ctx context.Context, d time.Time) error {
	st.deadline.Lock()
	defer st.deadline.Unlock()
	if st.halted || st.stopped(ctx) {
		return errStopped
	}
	if err := st.rc.SetWriteDeadline(d); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// halt marks the stream stopped and moves the write deadline to now, so a
// write blocked on a client that has stopped reading fails at once.
func (st *live) halt() {
	st.deadline.Lock()
	defer st.deadline.Unlock()
	st.halted = true
	_ = st.rc.SetWriteDeadline(time.Now())
}

// The work a stream's loop can be handed.
type action int

const (
	actStop action = iota
	actBeat
	actResync
	actHint
)

// Every write below is preceded by checkAll: the session or credential
// resolved again, and every thread and forum the stream names checked
// again. A hint is written only if that whole subscription still stands
// (Terva review 1733).
//
// A stream the client, the server or the hub ends stops at once, even in
// the middle of a write a client has stopped reading: the watcher sets the
// write deadline to now, so a blocked write fails rather than holding the
// handler past shutdown's drain (Terva review 1742). It is waited for before
// run returns, so it never touches the connection after the handler is
// done, and a stopped stream leaves the deadline in the past, so the
// response's own ending cannot block either.
func (st *live) run(ctx context.Context) error {
	quit := make(chan struct{})
	var watcher sync.WaitGroup
	watcher.Go(func() {
		select {
		case <-ctx.Done():
		case <-st.h.Hub.done:
		case <-st.s.kill:
		case <-quit:
			return
		}
		st.halt()
	})
	defer func() {
		close(quit)
		watcher.Wait()
		if st.stopped(ctx) {
			st.halt()
		}
	}()

	if err := st.write(ctx, []byte(fmt.Sprintf("retry: %d\n\nevent: ready\ndata: {}\n\n", reconnect.Milliseconds()))); err != nil {
		return err
	}
	if err := st.snapshot(ctx); err != nil {
		return err
	}
	hb := st.h.Heartbeat
	if hb <= 0 {
		hb = DefaultHeartbeat
	}
	tick := time.NewTicker(hb)
	defer tick.Stop()
	for {
		act, hn := st.next(ctx, tick.C)
		var err error
		switch act {
		case actStop:
			return errStopped
		case actBeat:
			if _, err = st.checkAll(ctx); err == nil {
				err = st.write(ctx, []byte(": hb\n\n"))
			}
		case actResync:
			err = st.snapshot(ctx)
		case actHint:
			err = st.deliver(ctx, hn)
		}
		if err != nil {
			return err
		}
	}
}

// stopped reports whether the client, the server or the hub has ended the
// stream.
func (st *live) stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-st.h.Hub.done:
		return true
	case <-st.s.kill:
		return true
	default:
		return false
	}
}

// next waits for the stream's next piece of work. An end wins over
// everything else that is ready: a select picks among ready cases at random,
// so whatever it picked, the end is checked once more after it, and a hint
// the hub queued before it ended the stream is never written (Terva review
// 1733).
func (st *live) next(ctx context.Context, beat <-chan time.Time) (action, hint) {
	act, hn := actStop, hint{}
	select {
	case <-ctx.Done():
	case <-st.h.Hub.done:
	case <-st.s.kill:
	case <-beat:
		act = actBeat
	case <-st.s.resync:
		act = actResync
	case hn = <-st.s.hints:
		act = actHint
	}
	if st.stopped(ctx) {
		return actStop, hint{}
	}
	return act, hn
}

// checkAll resolves the viewer again and checks every thread and forum the
// stream names. Any loss ends the stream: the reconnect is judged afresh.
func (st *live) checkAll(ctx context.Context) (domain.Viewer, error) {
	v, err := st.h.current(ctx, st.p)
	if err != nil {
		return v, err
	}
	for _, id := range st.sub.threads {
		if _, err := st.h.Threads.ReadAccess(ctx, v, id); err != nil {
			return v, err
		}
	}
	for _, slug := range st.sub.forums {
		if _, err := st.h.Threads.ForumAccess(ctx, v, slug); err != nil {
			return v, err
		}
	}
	return v, nil
}

// snapshot sends the current length of every thread named that the client
// has not already been told, checking the whole subscription again before
// each one, so a loss partway through writes nothing after it.
func (st *live) snapshot(ctx context.Context) error {
	if len(st.sub.threads) == 0 {
		_, err := st.checkAll(ctx)
		return err
	}
	for _, id := range st.sub.threads {
		if _, err := st.checkAll(ctx); err != nil {
			return err
		}
		n, err := st.h.Threads.LogLength(ctx, id)
		if err != nil {
			return err
		}
		if err := st.hint(ctx, id, n); err != nil {
			return err
		}
	}
	return nil
}

// deliver writes one hint, if the whole subscription still stands and the
// viewer may read the hint's thread now. A hint that reached the stream
// through a forum is written only if its thread is still in a forum the
// stream names, since the hub routed it by the forum the thread was in then
// (Terva review 1733); an unreadable one is skipped rather than ending the
// stream, because the forum may still be readable.
func (st *live) deliver(ctx context.Context, hn hint) error {
	v, err := st.checkAll(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(st.sub.threads, hn.thread) {
		forum, err := st.h.Threads.ReadAccess(ctx, v, hn.thread)
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			if c := problem.FromError(err); c != problem.CodeNotFound && c != problem.CodeForbidden &&
				c != problem.CodeUnauthenticated && c != problem.CodeUnavailable {
				return err // not a refusal: fail closed
			}
			return nil
		}
		if !slices.Contains(st.forums, forum) {
			return nil
		}
	}
	return st.hint(ctx, hn.thread, hn.logLength)
}

// hint writes `event: thread` for id at n, unless the client already has n.
func (st *live) hint(ctx context.Context, id threadid.ID, n int64) error {
	if n <= 0 || n <= st.sent[id] {
		return nil
	}
	data, err := json.Marshal(dto.ThreadHint{ThreadID: id.String(), LogLength: n})
	if err != nil {
		return err
	}
	if err := st.write(ctx, append(append([]byte("event: thread\ndata: "), data...), '\n', '\n')); err != nil {
		return err
	}
	st.sent[id] = n
	return nil
}

// write sends b and flushes it under a write deadline, so a client that has
// stopped reading ends its stream rather than holding it.
//
// It writes nothing to a stream that has been ended, even one whose hint
// was already past its access checks when the hub ended it (Terva review
// 1742): setDeadline refuses once the stream has stopped. If the stream
// stops after that, the watcher's past deadline replaces this one and the
// write or flush fails at once, and nothing here can set it back (Terva
// review 1744).
func (st *live) write(ctx context.Context, b []byte) error {
	wt := st.h.WriteTimeout
	if wt <= 0 {
		wt = DefaultWriteTimeout
	}
	if err := st.setDeadline(ctx, time.Now().Add(wt)); err != nil {
		return err
	}
	if _, err := st.w.Write(b); err != nil {
		return err
	}
	if err := st.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	// Cleared between writes while the stream runs, so a deadline does not
	// fire on an idle stream; a stopped stream keeps its past one.
	if err := st.setDeadline(ctx, time.Time{}); err != nil && !errors.Is(err, errStopped) {
		return err
	}
	return nil
}
