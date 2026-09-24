package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

var update = flag.Bool("update", false, "rewrite the thread page goldens")

// world is a seeded forum: a database, real thread logs, the projection, and
// the handler over them.
type world struct {
	t       *testing.T
	db      *db.DB
	cache   *threadcache.Cache
	proj    *projection.Writer
	svc     *threads.Service
	handler http.Handler
	users   map[string]int64
	forum   int64
	clock   int64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "threads")
	c, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	p, err := projection.New(projection.Options{DB: d, Root: root, Sweep: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	svc := &threads.Service{DB: d, Perm: perm.New(d), Threads: c, Render: render.NewCache(1 << 20)}
	w := &world{t: t, db: d, cache: c, proj: p, svc: svc, handler: New(Deps{Threads: svc}),
		users: map[string]int64{}, clock: 1_757_500_000_000}

	for _, u := range []struct{ handle, name, role, kind string }{
		{"alice", "Alice Nakamura", domain.RoleMember, "human"},
		{"scribe", "Scribe", domain.RoleMember, "agent"},
		{"bob", "Bob Iyer", domain.RoleModerator, "human"},
		{"carol", "Carol Ruiz", domain.RoleMember, "human"},
	} {
		w.write(func(q *dbq.Queries) error {
			id, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: u.handle, DisplayName: u.name,
				Status: "active", Role: u.role, Kind: u.kind, CreatedAt: 1, UpdatedAt: 1})
			w.users[u.handle] = id
			return err
		})
	}
	w.write(func(q *dbq.Queries) error {
		var err error
		w.forum, err = q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: domain.VisibilityPublic, CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	return w
}

func (w *world) write(fn func(q *dbq.Queries) error) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error { return fn(q) }); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) viewer(handle string) domain.Viewer {
	if handle == "" {
		return domain.Anonymous()
	}
	role := domain.RoleMember
	if handle == "bob" {
		role = domain.RoleModerator
	}
	return domain.Viewer{UserID: w.users[handle], Role: role, Active: true, SessionKey: "s-" + handle}
}

// thread creates a thread and returns its id and log length.
func (w *world) thread(seq uint64, title string, tags ...string) (threadid.ID, int64) {
	w.t.Helper()
	id := threadid.ID(uint64(1_757_500_000_000)<<16 | seq)
	th, err := w.cache.Create(id, uint64(w.forum), w.clock)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cache.Release(th)
	return id, w.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: uint64(w.users["alice"]),
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: "why-we-left-slack", ForumID: uint64(w.forum), Tags: tags}})
}

// append writes a record at the next tick of a fixed clock, projects it, and
// returns the log length.
func (w *world) append(id threadid.ID, a threadlog.Append) int64 {
	w.t.Helper()
	w.clock += 100_000
	if a.TimeMS == 0 {
		a.TimeMS = w.clock
	}
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	}); err != nil {
		w.t.Fatalf("appending %s: %v", a.Type, err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
	return res.LogLength
}

func (w *world) post(id threadid.ID, author string, src string, replyTo uint32) int64 {
	w.t.Helper()
	a := threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users[author]),
		Body: &threadlog.PostBody{Source: src, ReplyTo: replyTo}}
	if author == "scribe" {
		a.Flags = threadlog.FrameFlagMachineAuthored
	}
	return w.append(id, a)
}

// get requests a thread page as viewer and returns the response.
func (w *world) get(v domain.Viewer, path string, header ...string) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = r.WithContext(domain.WithViewer(r.Context(), v))
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, r)
	return rec
}

// page decodes a 200 strictly: a field the DTO does not have fails.
func page(t *testing.T, rec *httptest.ResponseRecorder) dto.ThreadPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var p dto.ThreadPage
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("decoding strictly into dto.ThreadPage: %v\n%s", err, rec.Body)
	}
	return p
}

// seed builds the thread the goldens show: an edited post, an agent's reply
// to the post above (so the reply link is suppressed), a moderator's post
// that was then deleted, a reply further back with a mention, a forum pin and
// two tags.
func (w *world) seed() (threadid.ID, int64) {
	w.t.Helper()
	id, _ := w.thread(1, "Why we left Slack", "meta", "tools")
	w.post(id, "alice", "The bill was the *smaller* half of it.", 0)
	w.post(id, "scribe", "Summarising the thread so far.", 1)
	w.post(id, "bob", "the text a moderator still needs to read", 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 1,
		Body: &threadlog.EditBody{Source: "The bill was the **smaller** half of it."}})
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 3,
		Body: &threadlog.DeleteBody{Reason: "off topic"}})
	w.post(id, "carol", "Agreed with @alice on this.", 1)
	end := w.append(id, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadPinnedBody{Scope: "forum", Until: 1_760_000_000_000}})
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(w.t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.users["carol"], ThreadID: int64(id), LastReadOrdinal: 2, LastReadAt: 1})
	})
	if err := w.db.Write(w.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(),
			`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (?, ?, 'mute', 1, 1)`,
			w.users["bob"], int64(id))
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id, end
}

// TestThreadPageGoldens is AC1: the response for each viewer class, decoded
// strictly into dto.ThreadPage and compared with a committed golden.
// Regenerate with `go test ./internal/web/api -update`.
func TestThreadPageGoldens(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	pages := map[string]string{}
	for class, handle := range map[string]string{"anon": "", "member": "carol", "moderator": "bob"} {
		t.Run(class, func(t *testing.T) {
			rec := w.get(w.viewer(handle), "/api/v1/threads/"+id.String())
			p := page(t, rec)
			got, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			pages[class] = string(got)
			path := filepath.Join("testdata", "golden", "thread."+class+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("the %s page differs from %s:\n%s", class, path, got)
			}
		})
	}
	if pages["anon"] == pages["member"] || pages["member"] == pages["moderator"] {
		t.Fatal("two viewer classes got identical pages; the distinction is not exercised")
	}
}

// TestTombstonesByClass is AC5: the deleted post is a tombstone with no HTML
// for anonymous viewers and members, and carries its content for a
// moderator.
func TestTombstonesByClass(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	for handle, wantHTML := range map[string]string{"": "", "carol": "", "bob": "<p>the text a moderator still needs to read</p>\n"} {
		p := page(t, w.get(w.viewer(handle), "/api/v1/threads/"+id.String()+"?page=1"))
		post := p.Posts[2]
		if post.Ordinal != 3 || !post.Deleted || post.HTML != wantHTML {
			t.Errorf("viewer %q: post 3 is %+v; want deleted with html %q", handle, post, wantHTML)
		}
	}
}

var etagShape = regexp.MustCompile(`^"[0-9a-z]+-\d+-r\d+-[0-9a-f]{16}"$`)

// TestETagAndNotModified is AC2: the ETag names the thread, its log length
// and the renderer version; a matching If-None-Match is a 304 with no body;
// an append, or a change to the viewer's own state, is a fresh 200.
func TestETagAndNotModified(t *testing.T) {
	w := newWorld(t)
	id, end := w.seed()
	path := "/api/v1/threads/" + id.String()
	carol := w.viewer("carol")

	first := w.get(carol, path)
	tag := first.Header().Get("ETag")
	want := fmt.Sprintf(`"%s-%d-r%d-`, id, end, render.Version)
	if !etagShape.MatchString(tag) || tag[:len(want)] != want {
		t.Fatalf("ETag %s, want the shape %s…", tag, want)
	}
	if first.Header().Get("Vary") != "Cookie" {
		t.Errorf("Vary is %q, want Cookie", first.Header().Get("Vary"))
	}

	for _, inm := range []string{tag, "W/" + tag, `"other", ` + tag, "*"} {
		rec := w.get(carol, path, "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != tag {
			t.Errorf("If-None-Match %s: status %d, %d body bytes, ETag %s", inm, rec.Code, rec.Body.Len(), rec.Header().Get("ETag"))
		}
	}
	if rec := w.get(carol, path, "If-None-Match", `"stale"`); rec.Code != http.StatusOK {
		t.Errorf("a stale tag got %d", rec.Code)
	}
	if other := w.get(w.viewer("bob"), path).Header().Get("ETag"); other == tag {
		t.Error("a moderator and a member share an ETag")
	}

	// The viewer's read position moves: same log, new tag.
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.users["carol"], ThreadID: int64(id), LastReadOrdinal: 4, LastReadAt: 2})
	})
	moved := w.get(carol, path, "If-None-Match", tag)
	if moved.Code != http.StatusOK || moved.Header().Get("ETag") == tag {
		t.Fatalf("after the read position moved: %d with ETag %s", moved.Code, moved.Header().Get("ETag"))
	}

	// An append: a new log length, a new tag.
	tag = moved.Header().Get("ETag")
	w.post(id, "alice", "one more", 0)
	if rec := w.get(carol, path, "If-None-Match", tag); rec.Code != http.StatusOK || rec.Header().Get("ETag") == tag {
		t.Fatalf("after an append: %d with ETag %s", rec.Code, rec.Header().Get("ETag"))
	}
}

// TestPermalinkPage is AC3: ?ordinal=N serves the page holding N.
func TestPermalinkPage(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 60 {
		w.post(id, "alice", fmt.Sprintf("post %d", i+1), 0)
	}
	for _, c := range []struct {
		ordinal, page, first int
	}{{1, 1, 1}, {25, 1, 1}, {26, 2, 26}, {50, 2, 26}, {51, 3, 51}, {60, 3, 51}} {
		p := page(t, w.get(domain.Anonymous(), fmt.Sprintf("/api/v1/threads/%s?ordinal=%d", id, c.ordinal)))
		if p.Page.Number != c.page || p.Page.TotalPages != 3 || p.Posts[0].Ordinal != c.first {
			t.Errorf("ordinal %d: page %d of %d starting at %d; want page %d starting at %d",
				c.ordinal, p.Page.Number, p.Page.TotalPages, p.Posts[0].Ordinal, c.page, c.first)
		}
	}
	if rec := w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()+"?ordinal=61"); rec.Code != http.StatusNotFound {
		t.Errorf("an ordinal past the end got %d", rec.Code)
	}
}

// TestPageFor covers the page arithmetic at a second page size, and the
// landing rule for a returning member (docs/06).
func TestPageFor(t *testing.T) {
	for _, c := range []struct {
		name     string
		q        threads.Query
		n, size  int
		member   bool
		lastRead int64
		want     int
	}{
		{"ordinal 10 at size 10", threads.Query{Ordinal: 10}, 30, 10, false, 0, 1},
		{"ordinal 11 at size 10", threads.Query{Ordinal: 11}, 30, 10, false, 0, 2},
		{"explicit page", threads.Query{Page: 3}, 30, 10, true, 5, 3},
		{"ordinal wins over page", threads.Query{Page: 1, Ordinal: 25}, 30, 10, false, 0, 3},
		{"no page, anonymous", threads.Query{}, 30, 10, false, 25, 1},
		{"no page, never read", threads.Query{}, 30, 10, true, 0, 1},
		{"no page, read to 10: lands on 11's page", threads.Query{}, 30, 10, true, 10, 2},
		{"no page, read to 9", threads.Query{}, 30, 10, true, 9, 1},
		{"no page, read everything: last page", threads.Query{}, 30, 10, true, 30, 3},
		{"empty thread", threads.Query{}, 0, 25, true, 0, 1},
	} {
		got, err := threads.PageFor(c.q, c.n, c.size, c.member, c.lastRead)
		if err != nil || got != c.want {
			t.Errorf("%s: got %d, %v; want %d", c.name, got, err, c.want)
		}
	}
}

// TestLandingPage: a member with a read position and no page in the URL gets
// the page holding their first unread post.
func TestLandingPage(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 40 {
		w.post(id, "alice", fmt.Sprintf("post %d", i+1), 0)
	}
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.users["carol"], ThreadID: int64(id), LastReadOrdinal: 30, LastReadAt: 1})
	})
	if p := page(t, w.get(w.viewer("carol"), "/api/v1/threads/"+id.String())); p.Page.Number != 2 {
		t.Errorf("carol, read to 30, landed on page %d", p.Page.Number)
	}
	if p := page(t, w.get(domain.Anonymous(), "/api/v1/threads/"+id.String())); p.Page.Number != 1 {
		t.Errorf("an anonymous viewer landed on page %d", p.Page.Number)
	}
}

// TestAfterLogLength is AC4: after_log_length=L keeps the page's posts whose
// current revision was written at or after L. New posts and an edit of an
// older post on the same page are returned, and nothing else.
func TestAfterLogLength(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Long")
	var l int64
	for i := range 30 {
		l = w.post(id, "alice", fmt.Sprintf("post %d", i+1), 0)
	}
	for i := range 3 {
		w.post(id, "scribe", fmt.Sprintf("new %d", i+1), 27)
	}
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 27,
		Body: &threadlog.EditBody{Source: "post 27, fixed"}})
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 3,
		Body: &threadlog.EditBody{Source: "post 3, on page one"}})

	p := page(t, w.get(domain.Anonymous(), fmt.Sprintf("/api/v1/threads/%s?page=2&after_log_length=%d", id, l)))
	var got []int
	for _, post := range p.Posts {
		got = append(got, post.Ordinal)
	}
	if fmt.Sprint(got) != "[27 31 32 33]" {
		t.Fatalf("after %d, page 2 returned ordinals %v; want [27 31 32 33]", l, got)
	}
	if p.Posts[0].ReplyCount != 3 {
		t.Fatalf("post 27, replied to three times, has reply_count %d", p.Posts[0].ReplyCount)
	}
	if p.Thread.PostCount != 33 || p.Thread.LogLength <= l {
		t.Fatalf("the header says %d posts at length %d", p.Thread.PostCount, p.Thread.LogLength)
	}
	// At the current length nothing is newer.
	p = page(t, w.get(domain.Anonymous(), fmt.Sprintf("/api/v1/threads/%s?page=2&after_log_length=%d", id, p.Thread.LogLength)))
	if len(p.Posts) != 0 {
		t.Fatalf("after the current length, %d posts came back", len(p.Posts))
	}
}

// TestAfterLogLengthUnderConcurrentAppends: while posts are appended, every
// response's posts are at or after the length asked for, and its header
// never claims a length its index did not cover (run under -race).
func TestAfterLogLengthUnderConcurrentAppends(t *testing.T) {
	w := newWorld(t)
	id, start := w.thread(1, "Busy")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 60 {
			if err := w.cache.With(id, func(th *threadcache.Thread) error {
				_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
					TimeMS: int64(1_800_000_000_000 + i), Body: &threadlog.PostBody{Source: "x"}})
				return err
			}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	seen := start
	for {
		select {
		case <-done:
			return
		default:
		}
		p := page(t, w.get(domain.Anonymous(), fmt.Sprintf("/api/v1/threads/%s?page=1&after_log_length=%d", id, seen)))
		if p.Thread.LogLength < seen {
			t.Fatalf("the log length went backwards: %d after %d", p.Thread.LogLength, seen)
		}
		if n := min(p.Thread.PostCount, 25); n > 0 && len(p.Posts) > n {
			t.Fatalf("%d posts on a page of %d", len(p.Posts), n)
		}
		seen = p.Thread.LogLength
	}
}

// TestAccess: hidden threads are 404, a members forum is 401 to anonymous
// viewers, and malformed or unsupported parameters are 400.
func TestAccess(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	base := "/api/v1/threads/" + id.String()
	for _, c := range []struct {
		name string
		v    domain.Viewer
		path string
		want int
	}{
		{"no such thread", domain.Anonymous(), "/api/v1/threads/" + threadid.ID(12345).String(), http.StatusNotFound},
		{"malformed id", domain.Anonymous(), "/api/v1/threads/not-an-id", http.StatusBadRequest},
		{"a lens", domain.Anonymous(), base + "?lens=author:alice", http.StatusBadRequest},
		{"page zero", domain.Anonymous(), base + "?page=0", http.StatusBadRequest},
		{"page not a number", domain.Anonymous(), base + "?page=two", http.StatusBadRequest},
		{"negative after", domain.Anonymous(), base + "?after_log_length=-1", http.StatusBadRequest},
		{"page past the end", domain.Anonymous(), base + "?page=2", http.StatusNotFound},
		{"an unknown API path", domain.Anonymous(), "/api/v1/nothing", http.StatusNotFound},
	} {
		if rec := w.get(c.v, c.path); rec.Code != c.want {
			t.Errorf("%s: got %d, want %d: %s", c.name, rec.Code, c.want, rec.Body)
		}
	}

	w.write(func(q *dbq.Queries) error {
		return q.SetForumVisibility(t.Context(), dbq.SetForumVisibilityParams{
			Visibility: domain.VisibilityMembers, UpdatedAt: 2, ID: w.forum})
	})
	// A fresh handler, so no cached permission from the requests above.
	if rec := newHandlerFor(w).get(domain.Anonymous(), base); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous on a members forum: got %d", rec.Code)
	}
	w.write(func(q *dbq.Queries) error {
		return q.SetForumVisibility(t.Context(), dbq.SetForumVisibilityParams{
			Visibility: domain.VisibilityRestricted, UpdatedAt: 3, ID: w.forum})
	})
	if rec := newHandlerFor(w).get(w.viewer("carol"), base); rec.Code != http.StatusNotFound {
		t.Errorf("a member on a restricted forum without read: got %d", rec.Code)
	}
}

// newHandlerFor returns w with a fresh Evaluator, so a test that changes
// permissions behind the cache's back sees the change.
func newHandlerFor(w *world) *world {
	svc := &threads.Service{DB: w.db, Perm: perm.New(w.db), Threads: w.cache, Render: render.NewCache(1 << 20)}
	c := *w
	c.handler = New(Deps{Threads: svc})
	return &c
}

// TestInactiveViewerIsAnonymous: a suspended account's session is served as
// an anonymous viewer, with no read position and no reply.
func TestInactiveViewerIsAnonymous(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	v := w.viewer("carol")
	v.Active = false
	p := page(t, w.get(v, "/api/v1/threads/"+id.String()))
	if p.Viewer != (dto.ThreadViewer{}) {
		t.Fatalf("an inactive member got the viewer block %+v", p.Viewer)
	}
	if tag, anon := w.get(v, "/api/v1/threads/"+id.String()).Header().Get("ETag"),
		w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()).Header().Get("ETag"); tag != anon {
		t.Fatalf("an inactive member's ETag is %s, an anonymous viewer's %s", tag, anon)
	}
}

// TestLockedThread: a lock takes the reply away from members and leaves it
// with moderators.
func TestLockedThread(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadLockedBody{Reason: "cooling off"}})
	for handle, want := range map[string]bool{"carol": false, "bob": true} {
		p := page(t, w.get(w.viewer(handle), "/api/v1/threads/"+id.String()))
		if !p.Thread.Locked || p.Viewer.CanReply != want {
			t.Errorf("%s on a locked thread: locked=%v can_reply=%v", handle, p.Thread.Locked, p.Viewer.CanReply)
		}
	}
}

// TestHeaderCountsFromTheIndex: a post the projection has not reached yet is
// counted, so post_count agrees with log_length and the page.
func TestHeaderCountsFromTheIndex(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
			TimeMS: w.clock + 1, Body: &threadlog.PostBody{Source: "not projected yet"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p := page(t, w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()))
	if p.Thread.PostCount != 5 || p.Thread.LogLength != res.LogLength || len(p.Posts) != 5 {
		t.Fatalf("header says %d posts at %d with %d on the page; want 5 at %d",
			p.Thread.PostCount, p.Thread.LogLength, len(p.Posts), res.LogLength)
	}
}

// TestQuoteOfADeletedPost: the page renders with the thread's index as the
// quote resolver, so quoting a deleted post shows a tombstone rather than the
// quoted text.
func TestQuoteOfADeletedPost(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", "> [quote=#3]\n> what bob said\n\nand my reply", 0)
	w.post(id, "alice", "> [quote=#1]\n> what I said\n\nand my reply", 0)
	p := page(t, w.get(domain.Anonymous(), "/api/v1/threads/"+id.String()))
	deleted, live := p.Posts[4].HTML, p.Posts[5].HTML
	if strings.Contains(deleted, "what bob said") || !strings.Contains(live, "what I said") {
		t.Fatalf("quoting deleted #3 rendered %q; quoting live #1 rendered %q", deleted, live)
	}
}

// TestCachedQuoteOfAPostDeletedLater: a quote rendered while its target was
// live is cached, and deleting the target afterwards turns it into a
// tombstone on the next request rather than serving the cached quotation.
func TestCachedQuoteOfAPostDeletedLater(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "carol", "> [quote=#1]\n> what alice said\n\nquite", 0)
	path := "/api/v1/threads/" + id.String()
	if p := page(t, w.get(domain.Anonymous(), path)); !strings.Contains(p.Posts[4].HTML, "what alice said") {
		t.Fatalf("the live quote rendered as %q", p.Posts[4].HTML)
	}
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 1,
		Body: &threadlog.DeleteBody{}})
	if p := page(t, w.get(domain.Anonymous(), path)); strings.Contains(p.Posts[4].HTML, "what alice said") {
		t.Fatalf("after #1 was deleted the quote still reads %q", p.Posts[4].HTML)
	}
}

// TestETagCoversAuthorsAndForum: an author's profile or the forum's name
// changes the page without moving the log, and the ETag moves with it.
func TestETagCoversAuthorsAndForum(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	for _, change := range []struct{ name, sql string }{
		{"a display name", `UPDATE users SET display_name = 'Alice N.' WHERE handle = 'alice'`},
		{"a role", `UPDATE users SET role = 'moderator' WHERE handle = 'scribe'`},
		{"the forum name", `UPDATE forums SET name = 'Town square' WHERE slug = 'general'`},
	} {
		tag := w.get(domain.Anonymous(), path).Header().Get("ETag")
		if err := w.db.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), change.sql)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if rec := w.get(domain.Anonymous(), path, "If-None-Match", tag); rec.Code != http.StatusOK {
			t.Errorf("after changing %s the old ETag still got %d", change.name, rec.Code)
		}
	}
}

// TestETagCoversPageSize: page 1 at two page sizes is two different pages,
// and neither's ETag may revalidate the other (review 393).
func TestETagCoversPageSize(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 30 {
		w.post(id, "alice", fmt.Sprintf("post %d", i+1), 0)
	}
	v := domain.Anonymous()
	small, err := w.svc.View(t.Context(), v, id, threads.Query{Page: 1, PageSize: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	large, err := w.svc.View(t.Context(), v, id, threads.Query{Page: 1, PageSize: 25}, small.ETag)
	if err != nil {
		t.Fatal(err)
	}
	if large.NotModified || large.ETag == small.ETag || len(large.Page.Posts) != 25 {
		t.Fatalf("size 25 against size 10's tag: not-modified=%v, tags %s and %s", large.NotModified, small.ETag, large.ETag)
	}
	again, err := w.svc.View(t.Context(), v, id, threads.Query{Page: 1, PageSize: 10}, small.ETag)
	if err != nil || !again.NotModified || again.ETag != small.ETag || len(again.Page.Posts) != 0 {
		t.Fatalf("size 10 against its own tag: %+v, %v", again, err)
	}
}
