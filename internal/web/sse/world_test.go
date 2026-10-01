package sse

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
)

// world is the server as serve builds it, end to end: a database, real
// thread logs, the projection writer and the live-update hub on one event
// bus, the thread service writing through that bus, and the real middleware
// chain with sessions and agent credentials in front of both /events and the
// API, so a test can compare a stream with the thread page it must not
// outrun.
//
// Both ways a thread is projected, the bus subscriber and the service's
// synchronous Project, wait behind gate, so a test can hold the projection
// and see that the hub waits for it.
type world struct {
	t        *testing.T
	db       *db.DB
	root     string
	cache    *threadcache.Cache
	proj     *projection.Writer
	bus      *events.Bus
	hub      *Hub
	handler  *Handler
	perm     *perm.Evaluator
	svc      *threads.Service
	sessions *session.Store
	creds    *agentcred.Store
	srv      *httptest.Server
	clock    *clock
	gate     *gate
	users    map[string]int64
	forums   map[string]int64
	groups   map[string]int64

	// streamOf is the stream a case reads, which settleHintless drains.
	streamOf *client

	// published counts the events the service has published, so quiesce
	// can wait for the hub to have handled every one.
	published atomic.Int64
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// gate holds the projection writer's deliveries while it is held.
type gate struct {
	mu   sync.Mutex
	open chan struct{} // nil while not held
}

func (g *gate) hold() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open == nil {
		g.open = make(chan struct{})
	}
}

func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.open != nil {
		close(g.open)
		g.open = nil
	}
}

func (g *gate) wait(ctx context.Context) {
	g.mu.Lock()
	ch := g.open
	g.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}
}

type worldOpts struct {
	limits    Limits
	heartbeat time.Duration
	wait      time.Duration
	// wrap wraps the root handler, as a proxy in front of it would.
	wrap func(http.Handler) http.Handler
}

func defaultOpts() worldOpts {
	return worldOpts{limits: Limits{Connections: 100, PerUser: 8, Anonymous: 100}, heartbeat: time.Hour}
}

// newWorld seeds users (root an admin, bob a moderator, alice, carol, dave
// and erin members, scribe an agent), three forums (general public, club
// members-only, staff restricted to the stewards group, of which dave is a
// member with read, reply and moderate there), and starts the server.
func newWorld(t *testing.T, o worldOpts) *world {
	t.Helper()
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	root := filepath.Join(t.TempDir(), "threads")
	c, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, db: d, root: root, cache: c, clock: &clock{t: time.UnixMilli(1_757_500_000_000)}, gate: &gate{},
		users: map[string]int64{}, forums: map[string]int64{}, groups: map[string]int64{}}
	quiet := slog.New(slog.DiscardHandler)
	w.proj, err = projection.New(projection.Options{DB: d, Root: root, Logger: quiet,
		Sweep: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	w.bus = events.New(events.Options{Logger: quiet})
	projSub := events.SubscriberFunc{N: "projection", F: func(ctx context.Context, b events.Batch) error {
		w.gate.wait(ctx)
		return w.proj.Deliver(ctx, b)
	}}
	if err := w.bus.Subscribe(projSub, 0); err != nil {
		t.Fatal(err)
	}
	w.hub = NewHub(HubOptions{Projections: threads.Projections{DB: d}, Limits: o.limits, ProjectionWait: o.wait,
		Logger: quiet})
	if err := w.bus.Subscribe(w.hub, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.bus.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	w.perm = perm.New(d)
	w.svc = &threads.Service{DB: d, Perm: w.perm, Threads: c, Render: render.NewCache(1 << 20),
		Responses: respcache.New(1 << 20), Writer: &posts.Writer{Publish: func(ev events.Event) { w.published.Add(1); w.bus.Publish(ev) }}, IDs: threadid.NewGenerator(),
		Now: w.clock.now,
		Project: func(ctx context.Context, id threadid.ID, n int64) error {
			w.gate.wait(ctx)
			_, err := w.proj.ProjectThread(ctx, id, n)
			return err
		}}
	w.sessions = &session.Store{DB: d, Now: w.clock.now}
	w.creds = &agentcred.Store{DB: d, Now: w.clock.now}
	w.handler = &Handler{Hub: w.hub, Threads: w.svc, Sessions: w.sessions, Credentials: w.creds, Heartbeat: o.heartbeat,
		Logger: quiet}
	var h http.Handler = web.New(web.Groups{
		API: api.New(api.Deps{Threads: w.svc, Users: &users.Service{DB: d, Perm: w.perm},
			Forums: &forums.Service{DB: d, Perm: w.perm}, Forms: &formsvc.Service{DB: d},
			Applications: &applications.Service{DB: d}, Perm: w.perm}),
		Events: w.handler,
	}, web.Options{Logger: quiet, Session: agentcred.Middleware(w.creds, session.Middleware(w.sessions, quiet), quiet)})
	if o.wrap != nil {
		h = o.wrap(h)
	}
	w.srv = httptest.NewUnstartedServer(h)
	w.srv.Config.RegisterOnShutdown(w.hub.Close)
	w.srv.Start()
	t.Cleanup(func() {
		w.hub.Close()
		w.gate.release()
		w.srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = w.bus.Close(ctx)
		_ = c.Close()
	})

	w.write(func(q *dbq.Queries) error {
		for _, u := range []struct{ handle, role, kind string }{
			{"root", domain.RoleAdmin, domain.KindHuman},
			{"bob", domain.RoleModerator, domain.KindHuman},
			{"alice", domain.RoleMember, domain.KindHuman},
			{"carol", domain.RoleMember, domain.KindHuman},
			{"dave", domain.RoleMember, domain.KindHuman},
			{"erin", domain.RoleMember, domain.KindHuman},
			{"frank", domain.RoleMember, domain.KindHuman},
			{"scribe", domain.RoleMember, domain.KindAgent},
		} {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.handle,
				Status: domain.StatusActive, Role: u.role, Kind: u.kind, CreatedAt: 1, UpdatedAt: 1})
			if err != nil {
				return err
			}
			w.users[u.handle] = id
		}
		for _, f := range []struct{ slug, vis string }{
			{"general", domain.VisibilityPublic},
			{"club", domain.VisibilityMembers},
			{"staff", domain.VisibilityRestricted},
		} {
			id, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: f.slug, Name: f.slug, Visibility: f.vis,
				CreatedAt: 1, UpdatedAt: 1})
			if err != nil {
				return err
			}
			w.forums[f.slug] = id
		}
		id, err := q.CreateGroup(t.Context(), dbq.CreateGroupParams{Name: "Stewards", Slug: "stewards", CreatedAt: 1, UpdatedAt: 1})
		w.groups["stewards"] = id
		return err
	})
	by := audit.By{Actor: w.users["root"], IP: "192.0.2.1"}
	for _, p := range []domain.Permission{domain.PermRead, domain.PermReply, domain.PermCreateThread, domain.PermModerate} {
		if err := w.perm.Grant(t.Context(), by, w.forums["staff"], w.groups["stewards"], p); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.perm.AddMember(t.Context(), by, w.groups["stewards"], w.users["dave"]); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) write(fn func(q *dbq.Queries) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error { return fn(q) }); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) by() audit.By { return audit.By{Actor: w.users["root"], IP: "192.0.2.1"} }

// viewer is handle as the session middleware would resolve them.
func (w *world) viewer(handle string) domain.Viewer {
	role := domain.RoleMember
	switch handle {
	case "root":
		role = domain.RoleAdmin
	case "bob":
		role = domain.RoleModerator
	}
	return domain.Viewer{UserID: w.users[handle], Role: role, Active: true, SessionKey: "s-" + handle}
}

// signIn returns the Cookie header of a fresh session for handle.
func (w *world) signIn(handle string) []string {
	w.t.Helper()
	return []string{"Cookie", session.CookieName + "=" + sessiontest.SignIn(w.t, w.db, w.users[handle], w.clock.now())}
}

// thread starts a thread in forum as author and waits for its projection.
func (w *world) thread(forum, author string) threadid.ID {
	w.t.Helper()
	res, err := w.svc.CreateThread(w.t.Context(), w.viewer(author), forum,
		threads.CreateThreadRequest{Title: "A thread in " + forum, Source: "the first post"})
	if err != nil {
		w.t.Fatalf("creating a thread in %s: %v", forum, err)
	}
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	w.settle(id)
	w.quiesce()
	return id
}

// quiesce waits until the hub has handled every event published so far, so
// a stream opened next hears nothing from before it. Without it a forum
// stream could be handed a hint about a thread's creation, harmless on the
// wire, that a test reading for silence would count.
func (w *world) quiesce() {
	w.t.Helper()
	want := uint64(w.published.Load())
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, st := range w.bus.Stats() {
			if st.Name == "sse" && st.Delivered+st.Dropped >= want {
				return
			}
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("the hub did not handle %d events in time: %+v", want, w.bus.Stats())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// reply appends a post as author and returns the log length it left.
func (w *world) reply(id threadid.ID, author string) int64 {
	w.t.Helper()
	res, err := w.svc.Reply(w.t.Context(), w.viewer(author), id, threads.ReplyRequest{Source: "a reply from " + author})
	if err != nil {
		w.t.Fatalf("%s replying: %v", author, err)
	}
	return res.LogLength
}

// length is thread id's acknowledged log length.
func (w *world) length(id threadid.ID) int64 {
	w.t.Helper()
	var n int64
	if err := w.cache.With(id, func(th *threadcache.Thread) error { n = th.Actor.LogLength(); return nil }); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// settle waits until the projection has reached thread id's log.
func (w *world) settle(id threadid.ID) {
	w.t.Helper()
	want := w.length(id)
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := (threads.Projections{DB: w.db}).Projection(w.t.Context(), id)
		if err != nil {
			w.t.Fatal(err)
		}
		if p.Found && p.LogLength >= want {
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("the projection of %s did not reach %d", id, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// get requests path with header pairs and returns the response, body read.
func (w *world) get(path string, header ...string) (*http.Response, string) {
	w.t.Helper()
	// Bounded, so a refusal that wrongly opened a stream fails the test
	// rather than hanging it.
	ctx, cancel := context.WithTimeout(w.t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.srv.URL+path, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Add(header[i], header[i+1])
	}
	resp, err := w.srv.Client().Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// pageLength is the log length thread id's page serves the viewer whose
// headers are given.
func (w *world) pageLength(id threadid.ID, header ...string) int64 {
	w.t.Helper()
	resp, body := w.get("/api/v1/threads/"+id.String(), header...)
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("GET the thread page = %d: %s", resp.StatusCode, body)
	}
	var p dto.ThreadPage
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		w.t.Fatal(err)
	}
	return p.Thread.LogLength
}

// event is one server-sent event, or a comment.
type event struct {
	name    string
	data    string
	comment string
}

// client is an open stream.
type client struct {
	t      *testing.T
	header []string
	resp   *http.Response
	events chan event
	// lines is every line the stream sent, in order.
	mu    sync.Mutex
	lines []string
}

// open opens a stream at path and fails unless it is admitted.
func (w *world) open(path string, header ...string) *client {
	w.t.Helper()
	return openAt(w.t, w.srv.Client(), w.srv.URL+path, header...)
}

func openAt(t *testing.T, hc *http.Client, url string, header ...string) *client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Add(header[i], header[i+1])
	}
	resp, err := hc.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, b)
	}
	c := &client{t: t, header: header, resp: resp, events: make(chan event, 256)}
	go c.read()
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return c
}

func (c *client) read() {
	defer close(c.events)
	sc := bufio.NewScanner(c.resp.Body)
	var ev event
	for sc.Scan() {
		line := sc.Text()
		c.mu.Lock()
		c.lines = append(c.lines, line)
		c.mu.Unlock()
		switch {
		case line == "":
			if ev != (event{}) {
				c.events <- ev
			}
			ev = event{}
		case strings.HasPrefix(line, ":"):
			ev.comment = strings.TrimSpace(strings.TrimPrefix(line, ":"))
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.data = strings.TrimPrefix(line, "data: ")
		}
	}
}

// close ends the stream from the client's side.
func (c *client) close() { c.resp.Body.Close() }

// next is the next event, or false when the stream ended or nothing came
// within d.
func (c *client) next(d time.Duration) (event, bool) {
	c.t.Helper()
	select {
	case ev, ok := <-c.events:
		return ev, ok
	case <-time.After(d):
		return event{}, false
	}
}

// ready reads the opening ready event and the snapshot that follows it,
// one thread event for each of n threads.
func (c *client) ready(n int) []dto.ThreadHint {
	c.t.Helper()
	ev, ok := c.next(5 * time.Second)
	if !ok || ev.name != "ready" {
		c.t.Fatalf("the stream did not open with ready: %+v, %v", ev, ok)
	}
	var out []dto.ThreadHint
	for range n {
		out = append(out, c.hint())
	}
	return out
}

// hint reads the next event, which must be a thread event.
func (c *client) hint() dto.ThreadHint {
	c.t.Helper()
	for {
		ev, ok := c.next(5 * time.Second)
		if !ok {
			c.t.Fatal("the stream ended, or sent nothing, where a hint was due")
		}
		if ev.comment != "" && ev.name == "" {
			continue // a heartbeat
		}
		if ev.name != "thread" {
			c.t.Fatalf("got %+v, want a thread event", ev)
		}
		return decodeHint(c.t, ev.data)
	}
}

// redactOffline redacts post ordinal of thread id as ketju redact does, with
// the thread closed, and restarts the stores the service reads on the
// rewritten log, as a restart would. It returns the new log length.
func (w *world) redactOffline(id threadid.ID, ordinal uint32) int64 {
	w.t.Helper()
	if err := w.cache.Close(); err != nil {
		w.t.Fatal(err)
	}
	path := id.LogPath(w.root)
	old, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := threadlog.RedactPosts(bytes.NewReader(old), int64(len(old)), &out, []uint32{ordinal},
		threadlog.RedactOptions{TimeMS: w.clock.now().UnixMilli(), Operator: uint64(w.users["root"]), Reason: "takedown",
			Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o640); err != nil {
		w.t.Fatal(err)
	}
	c, err := threadcache.New(w.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { c.Close() })
	w.cache, w.svc.Threads = c, c
	w.svc.Render, w.svc.Responses = render.NewCache(1<<20), respcache.New(1<<20)
	if _, err := w.proj.ProjectThread(w.t.Context(), id, int64(out.Len())); err != nil {
		w.t.Fatal(err)
	}
	return int64(out.Len())
}

// hintAt reads hints until one for id reaches n. A forum stream may first
// hear of an event from just before it opened, which is harmless, so a
// shorter hint for id is skipped; a hint for any other thread fails.
func (c *client) hintAt(id threadid.ID, n int64) dto.ThreadHint {
	c.t.Helper()
	for {
		h := c.hint()
		if h.ThreadID != id.String() {
			c.t.Fatalf("a hint for %s where one for %s was due", h.ThreadID, id)
		}
		if h.LogLength >= n {
			return h
		}
	}
}

// ended reads to the end of the stream and fails if a thread event comes
// first, or if the stream is still open after d.
func (c *client) ended(d time.Duration) {
	c.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return
			}
			if ev.name == "thread" {
				c.t.Fatalf("a hint was written to a stream that should have ended: %s", ev.data)
			}
		case <-deadline:
			c.t.Fatalf("the stream was still open after %s", d)
		}
	}
}

// quiet fails if a thread event arrives within d.
func (c *client) quiet(d time.Duration) {
	c.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				c.t.Fatal("the stream ended where it should have stayed open")
			}
			if ev.name == "thread" {
				c.t.Fatalf("an unexpected hint: %s", ev.data)
			}
		case <-deadline:
			return
		}
	}
}

func decodeHint(t *testing.T, data string) dto.ThreadHint {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	var h dto.ThreadHint
	if err := dec.Decode(&h); err != nil {
		t.Fatalf("decoding %q strictly as {thread_id, log_length}: %v", data, err)
	}
	if h.ThreadID == "" || h.LogLength <= 0 {
		t.Fatalf("hint %q lacks a field", data)
	}
	return h
}

func eventsPath(threadIDs []threadid.ID, forums ...string) string {
	var q []string
	if len(threadIDs) > 0 {
		var s []string
		for _, id := range threadIDs {
			s = append(s, id.String())
		}
		q = append(q, "threads="+strings.Join(s, ","))
	}
	if len(forums) > 0 {
		q = append(q, "forums="+strings.Join(forums, ","))
	}
	return "/events?" + strings.Join(q, "&")
}

func ids(xs ...threadid.ID) []threadid.ID { return xs }
