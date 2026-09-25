package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// send makes a JSON request through h as who (see chain).
func send(h http.Handler, who, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if who != "" {
		r.Header.Set("X-Test-Viewer", who)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// created decodes a 201 strictly.
func created[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(rec.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding %T: %v", v, err)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	return v
}

func postsPath(id threadid.ID) string { return "/api/v1/threads/" + id.String() + "/posts" }

// logState is what a refused write must leave alone: the log's size on disk,
// the index's length, and how many events were published.
type logState struct {
	size   int64
	posts  int
	events int
}

func (w *world) logState(id threadid.ID) logState {
	w.t.Helper()
	fi, err := os.Stat(id.LogPath(w.root))
	if err != nil {
		w.t.Fatal(err)
	}
	var n int
	if err := w.cache.With(id, func(th *threadcache.Thread) error { n = th.Index.Len(); return nil }); err != nil {
		w.t.Fatal(err)
	}
	return logState{size: fi.Size(), posts: n, events: len(w.events.list())}
}

// TestReplyIsDurable is AC1: with the log synced on every append, the reply
// the response describes is on disk when the response arrives. A second
// cache over the same directory, after the first is closed, reads it back
// at the ordinal, revision and log length the response gave, and exactly
// one event announced it.
func TestReplyIsDurable(t *testing.T) {
	w := newWorldSync(t, threadlog.SyncAlways)
	id, _ := w.seed()
	res := created[dto.CreatedPost](t, send(w.chain(), "carol", http.MethodPost, postsPath(id),
		`{"source":"Durable, @alice and @Alice.","reply_to":2}`))
	if res.Ordinal != 5 || res.Revision != 1 || res.Post.Ordinal != 5 {
		t.Fatalf("created %+v", res)
	}
	if err := w.cache.Close(); err != nil {
		t.Fatal(err)
	}

	c, err := threadcache.New(w.root, threadcache.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.With(id, func(th *threadcache.Thread) error {
		if got := th.Actor.LogLength(); got != res.LogLength {
			t.Errorf("log length on disk %d, response %d", got, res.LogLength)
		}
		if th.Index.Len() != int(res.Ordinal) {
			t.Errorf("index holds %d posts, response ordinal %d", th.Index.Len(), res.Ordinal)
		}
		p, err := th.Reader.One(res.Ordinal)
		if err != nil {
			return err
		}
		if p.Source != "Durable, @alice and @Alice." || p.ReplyTo != 2 || uint32(p.Revision) != res.Revision ||
			p.Author != uint64(w.users["carol"]) || !slices.Equal(p.Mentions, []uint64{uint64(w.users["alice"])}) {
			t.Errorf("read back %+v", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if evs := w.events.list(); len(evs) != 1 || evs[0].Thread != id || evs[0].Ordinal != res.Ordinal ||
		evs[0].LogLength != res.LogLength {
		t.Errorf("published %+v", evs)
	}
}

// TestReplyMatchesALaterRead is AC4: the post in the response is the post a
// later read serves, byte for byte, from a handler whose render cache has
// never seen it, both as a single post and on the thread page. The source
// exercises what depends on the thread and the users table: a quote, a
// mention, a reply_to, and an agent author.
func TestReplyMatchesALaterRead(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	for _, c := range []struct{ who, body string }{
		{"carol", `{"source":"> [quote=#1]\n> smaller half\n\n> [quote=#3]\n> gone\n\nAgreed, @Alice and @nobody.","reply_to":1}`},
		{"scribe", `{"source":"A summary, **so far**."}`},
		{"bob", `{"source":"Noted.","reply_to":4}`},
	} {
		res := created[dto.CreatedPost](t, send(w.chain(), c.who, http.MethodPost, postsPath(id), c.body))
		if res.Post.MachineAuthored != (c.who == "scribe") {
			t.Errorf("%s: machine_authored %v", c.who, res.Post.MachineAuthored)
		}
		cold := newHandlerFor(w)
		v := w.viewer(c.who)
		rec := cold.get(v, fmt.Sprintf("%s/%d", postsPath(id), res.Ordinal))
		if got, want := rec.Body.String(), renderJSON(t, res.Post); got != want {
			t.Errorf("%s: GET the post\n got %s\nwant %s", c.who, got, want)
		}
		p := page(t, cold.get(v, fmt.Sprintf("/api/v1/threads/%s?ordinal=%d", id, res.Ordinal)))
		i := slices.IndexFunc(p.Posts, func(q dto.Post) bool { return uint32(q.Ordinal) == res.Ordinal })
		if i < 0 || renderJSON(t, p.Posts[i]) != renderJSON(t, res.Post) {
			t.Errorf("%s: the page's post differs from the response's", c.who)
		}
		if p.Thread.LogLength != res.LogLength {
			t.Errorf("%s: page log length %d, response %d", c.who, p.Thread.LogLength, res.LogLength)
		}
	}
}

// TestReplyRefusals is AC2: a locked thread, a missing permission and an
// active slow mode each refuse with their own code, and a moderator passes
// the lock and the slow mode.
func TestReplyRefusals(t *testing.T) {
	t.Run("locked", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		w.append(id, threadlog.Append{Type: threadlog.TypeThreadLocked, Author: uint64(w.users["bob"]),
			Body: &threadlog.ThreadLockedBody{Reason: "cooling off"}})
		before := w.logState(id)
		if rec := send(w.chain(), "carol", http.MethodPost, postsPath(id), `{"source":"hi"}`); problemCode(t, rec) != "conflict" {
			t.Errorf("a member on a locked thread: %d %s", rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("a refused post changed the log: %+v then %+v", before, after)
		}
		created[dto.CreatedPost](t, send(w.chain(), "bob", http.MethodPost, postsPath(id), `{"source":"moderator here"}`))

		// Archived is read-only for everyone, moderators included.
		w.cache.MarkArchived(id)
		before = w.logState(id)
		if rec := send(w.chain(), "bob", http.MethodPost, postsPath(id), `{"source":"too late"}`); problemCode(t, rec) != "conflict" {
			t.Errorf("a moderator on an archived thread: %d %s", rec.Code, rec.Body)
		}
		if after := w.logState(id); after != before {
			t.Errorf("a refused post changed the log: %+v then %+v", before, after)
		}
	})

	t.Run("permission", func(t *testing.T) {
		w := newWorld(t)
		// Members may read the forum and nothing else: a per-forum row for
		// the members group replaces its global read, reply, create_thread.
		w.write(func(q *dbq.Queries) error {
			return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
				ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
		})
		id, _ := w.seed()
		for who, want := range map[string]string{"carol": "forbidden", "": "unauthenticated", "pre-user": "forbidden"} {
			if rec := send(w.chain(), who, http.MethodPost, postsPath(id), `{"source":"hi"}`); problemCode(t, rec) != want {
				t.Errorf("%q: %d %s, want %s", who, rec.Code, rec.Body, want)
			}
		}
		// Restricted with no per-forum row for them hides it from members.
		w.hideForum()
		w.revokeForumRead()
		if rec := send(newHandlerFor(w).chain(), "carol", http.MethodPost, postsPath(id), `{"source":"hi"}`); problemCode(t, rec) != "not-found" {
			t.Errorf("a hidden thread: %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("slow mode", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		if err := settings.Set(t.Context(), w.db, settings.SlowModeDefaultSeconds.Name, json.RawMessage(`30`), 0, time.UnixMilli(1)); err != nil {
			t.Fatal(err)
		}
		h := w.chain()
		created[dto.CreatedPost](t, send(h, "carol", http.MethodPost, postsPath(id), `{"source":"one"}`))
		for _, c := range []struct {
			advance time.Duration
			retry   string
		}{{0, "30"}, {29_500 * time.Millisecond, "1"}} {
			w.clock += c.advance.Milliseconds()
			before := w.logState(id)
			rec := send(h, "carol", http.MethodPost, postsPath(id), `{"source":"two"}`)
			if problemCode(t, rec) != "rate-limited" || rec.Header().Get("Retry-After") != c.retry {
				t.Errorf("after %s: %d Retry-After %q %s, want rate-limited and %s", c.advance, rec.Code,
					rec.Header().Get("Retry-After"), rec.Body, c.retry)
			}
			if after := w.logState(id); after != before {
				t.Errorf("a refused post changed the log: %+v then %+v", before, after)
			}
		}
		w.clock += 500
		created[dto.CreatedPost](t, send(h, "carol", http.MethodPost, postsPath(id), `{"source":"two"}`))
		// Another member's clock is their own, and a moderator has none.
		created[dto.CreatedPost](t, send(h, "alice", http.MethodPost, postsPath(id), `{"source":"mine"}`))
		created[dto.CreatedPost](t, send(h, "bob", http.MethodPost, postsPath(id), `{"source":"a"}`))
		created[dto.CreatedPost](t, send(h, "bob", http.MethodPost, postsPath(id), `{"source":"b"}`))
	})
}

// TestReplyRefusedBeforeAppend is AC3: a post that does not render, or that
// the request gets wrong, is refused with the log, the index and the event
// stream as they were.
func TestReplyRefusedBeforeAppend(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	before := w.logState(id)
	for _, c := range []struct{ name, body, want string }{
		{"a reply to no post", `{"source":"hi","reply_to":99}`, "validation-failed"},
		{"a reply to ordinal 0", `{"source":"hi","reply_to":0}`, "validation-failed"},
		{"an empty source", `{"source":" \n\t "}`, "validation-failed"},
		{"a source over the limit", `{"source":"` + strings.Repeat("a", render.MaxSource+1) + `"}`, "payload-too-large"},
		{"an attachment", `{"source":"hi","attachments":["a1b2"]}`, "validation-failed"},
		{"no source", `{"reply_to":1}`, "bad-request"},
		{"an unknown field", `{"source":"hi","title":"x"}`, "bad-request"},
		{"two objects", `{"source":"hi"}{}`, "bad-request"},
	} {
		if rec := send(w.chain(), "carol", http.MethodPost, postsPath(id), c.body); problemCode(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if rec := send(w.chain(), "carol", http.MethodPost, "/api/v1/threads/0000000000c1s/posts", `{"source":"hi"}`); problemCode(t, rec) != "not-found" {
		t.Errorf("no such thread: %d %s", rec.Code, rec.Body)
	}
	if after := w.logState(id); after != before {
		t.Errorf("a refused post changed the log: %+v then %+v", before, after)
	}
}

// TestConcurrentReplies: fifty replies at once get fifty dense ordinals,
// each announced once. Run under -race.
func TestConcurrentReplies(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	const n = 50
	got := make([]uint32, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			rec := send(h, "carol", http.MethodPost, postsPath(id), fmt.Sprintf(`{"source":"reply %d"}`, i))
			if rec.Code != http.StatusCreated {
				t.Errorf("reply %d: %d %s", i, rec.Code, rec.Body)
				return
			}
			var res dto.CreatedPost
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Error(err)
				return
			}
			got[i] = res.Ordinal
		})
	}
	wg.Wait()
	slices.Sort(got)
	for i, o := range got {
		if o != uint32(5+i) {
			t.Fatalf("ordinals %v are not 5..%d", got, 4+n)
		}
	}
	var announced []uint32
	for _, ev := range w.events.list() {
		announced = append(announced, ev.Ordinal)
	}
	slices.Sort(announced)
	if !slices.Equal(announced, got) {
		t.Errorf("announced %v, want %v", announced, got)
	}
}

func threadsPath(forum string) string { return "/api/v1/forums/" + forum + "/threads" }

// TestCreateThread: the response is the thread page's header and first post
// as a later read serves them, the thread is readable at once, and the two
// records were each announced.
func TestCreateThread(t *testing.T) {
	w := newWorld(t)
	rec := send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"  Café au lait: ÆON — 2026!  ","source":"Hello, @alice.","tags":[]}`)
	res := created[dto.CreatedThread](t, rec)
	id, err := threadid.Parse(res.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/threads/"+id.String() {
		t.Errorf("Location %q", loc)
	}
	h := res.Thread
	if h.Title != "Café au lait: ÆON — 2026!" || h.Slug != "cafe-au-lait-aeon-2026" || h.PostCount != 1 ||
		h.Forum.Slug != "general" || h.Locked || h.Pinned != nil || len(h.Tags) != 0 || int64(h.CreatedAt) != w.clock {
		t.Errorf("header %+v", h)
	}
	if res.Post.Ordinal != 1 || res.Post.Revision != 1 || res.Post.LogLength != h.LogLength ||
		!strings.Contains(res.Post.Post.HTML, `href="/u/alice"`) {
		t.Errorf("first post %+v", res.Post)
	}

	cold := newHandlerFor(w)
	p := page(t, cold.get(w.viewer("carol"), "/api/v1/threads/"+id.String()))
	if renderJSON(t, p.Thread) != renderJSON(t, res.Thread) {
		t.Errorf("GET header\n got %s\nwant %s", renderJSON(t, p.Thread), renderJSON(t, res.Thread))
	}
	if len(p.Posts) != 1 || renderJSON(t, p.Posts[0]) != renderJSON(t, res.Post.Post) {
		t.Errorf("GET posts %s, want %s", renderJSON(t, p.Posts), renderJSON(t, res.Post.Post))
	}
	if evs := w.events.list(); len(evs) != 2 || evs[0].Ordinal != 0 || evs[1].Ordinal != 1 ||
		evs[1].LogLength != res.Post.LogLength || evs[0].Thread != id || evs[1].Thread != id {
		t.Errorf("published %+v", evs)
	}

	// An agent's thread is machine-authored, both records.
	res = created[dto.CreatedThread](t, send(w.chain(), "scribe", http.MethodPost, threadsPath("general"),
		`{"title":"Digest","source":"Today."}`))
	if !res.Post.Post.MachineAuthored {
		t.Error("an agent's first post is not machine-authored")
	}
}

// TestCreateThreadRefusals: every refusal comes before the thread's files
// exist, and each says what kind of refusal it is.
func TestCreateThreadRefusals(t *testing.T) {
	w := newWorld(t)
	for _, c := range []struct{ name, who, forum, body, want string }{
		{"anonymous", "", "general", `{"title":"t","source":"s"}`, "unauthenticated"},
		{"a pre-user", "pre-user", "general", `{"title":"t","source":"s"}`, "forbidden"},
		{"no such forum", "carol", "nowhere", `{"title":"t","source":"s"}`, "not-found"},
		{"an empty title", "carol", "general", `{"title":"  ","source":"s"}`, "validation-failed"},
		{"a long title", "carol", "general", `{"title":"` + strings.Repeat("é", threads.MaxTitle+1) + `","source":"s"}`, "validation-failed"},
		{"a control character", "carol", "general", `{"title":"a\u0007b","source":"s"}`, "validation-failed"},
		{"a tag", "carol", "general", `{"title":"t","source":"s","tags":["meta"]}`, "validation-failed"},
		{"an empty source", "carol", "general", `{"title":"t","source":""}`, "validation-failed"},
		{"a source over the limit", "carol", "general", `{"title":"t","source":"` + strings.Repeat("a", render.MaxSource+1) + `"}`, "payload-too-large"},
		{"no title", "carol", "general", `{"source":"s"}`, "bad-request"},
		{"a reply_to", "carol", "general", `{"title":"t","source":"s","reply_to":1}`, "bad-request"},
	} {
		if rec := send(w.chain(), c.who, http.MethodPost, threadsPath(c.forum), c.body); problemCode(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, rec.Body, c.want)
		}
	}
	// A title of exactly the limit, in characters rather than bytes, is fine.
	created[dto.CreatedThread](t, send(w.chain(), "carol", http.MethodPost, threadsPath("general"),
		`{"title":"`+strings.Repeat("é", threads.MaxTitle)+`","source":"s"}`))
	if n := len(w.events.list()); n != 2 {
		t.Errorf("the refusals published: %d events, want only the accepted thread's 2", n)
	}

	w.write(func(q *dbq.Queries) error {
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
			ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
	})
	if rec := send(newHandlerFor(w).chain(), "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`); problemCode(t, rec) != "forbidden" {
		t.Errorf("without create_thread: %d %s", rec.Code, rec.Body)
	}
	w.hideForum()
	w.revokeForumRead()
	if rec := send(newHandlerFor(w).chain(), "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`); problemCode(t, rec) != "not-found" {
		t.Errorf("a hidden forum: %d %s", rec.Code, rec.Body)
	}
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE forums SET archived = 1, visibility = 'public' WHERE id = ?`, w.forum)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if rec := send(newHandlerFor(w).chain(), "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`); problemCode(t, rec) != "conflict" {
		t.Errorf("an archived forum: %d %s", rec.Code, rec.Body)
	}
	if n := len(w.events.list()); n != 2 {
		t.Errorf("%d events after the refusals, want 2", n)
	}
	dirs := 0
	if err := walkLogs(w.root, func() { dirs++ }); err != nil {
		t.Fatal(err)
	}
	if dirs != 1 {
		t.Errorf("%d thread logs on disk, want the one accepted", dirs)
	}
}

// TestThreadWithNoPostsIsNotFound: a ThreadCreated with no first post, as a
// crash between CreateThread's two appends leaves, is served as not-found.
func TestThreadWithNoPostsIsNotFound(t *testing.T) {
	w := newWorld(t)
	id, _ := w.thread(9, "Half made")
	if rec := w.get(w.viewer("carol"), "/api/v1/threads/"+id.String()); rec.Code != http.StatusNotFound {
		t.Errorf("a thread with no posts: %d %s", rec.Code, rec.Body)
	}
	if rec := send(w.chain(), "carol", http.MethodPost, postsPath(id), `{"source":"hi"}`); problemCode(t, rec) != "not-found" {
		t.Errorf("a reply to a thread with no posts: %d %s", rec.Code, rec.Body)
	}
}

// walkLogs calls fn for each thread log under root.
func walkLogs(root string, fn func()) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if _, perr := threadid.FromLogPath(path); perr == nil && filepath.Ext(path) == threadid.LogExt {
				fn()
			}
		}
		return err
	})
}

// revokeForumRead removes the members group's per-forum read row that the
// permission tests grant, so the forum falls back to the global rows.
func (w *world) revokeForumRead() {
	w.write(func(q *dbq.Queries) error {
		return q.RevokePermission(w.t.Context(), dbq.RevokePermissionParams{
			ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead)})
	})
}

// TestWriteServiceRefusals holds the service to what the handlers check
// first, so a caller that is not the API is refused the same way: a
// stranger is unauthenticated rather than forbidden, and a source the
// renderer refuses is refused before anything is written, whatever body
// limit sits in front.
func TestWriteServiceRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	before := w.logState(id)
	big := strings.Repeat("a", render.MaxSource+1)
	for _, c := range []struct {
		name string
		v    domain.Viewer
		src  string
		want string
	}{
		{"anonymous", domain.Anonymous(), "hi", "unauthenticated"},
		{"too large", w.viewer("carol"), big, "payload-too-large"},
	} {
		_, err := w.svc.Reply(t.Context(), c.v, id, threads.ReplyRequest{Source: c.src})
		if got := string(problem.FromError(err)); got != c.want {
			t.Errorf("reply, %s: %v (%s), want %s", c.name, err, got, c.want)
		}
		_, err = w.svc.CreateThread(t.Context(), c.v, "general", threads.CreateThreadRequest{Title: "t", Source: c.src})
		if got := string(problem.FromError(err)); got != c.want {
			t.Errorf("create, %s: %v (%s), want %s", c.name, err, got, c.want)
		}
	}
	if after := w.logState(id); after != before {
		t.Errorf("a refused post changed the log: %+v then %+v", before, after)
	}
	logs := 0
	if err := walkLogs(w.root, func() { logs++ }); err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Errorf("%d thread logs, want the seeded one", logs)
	}
}
