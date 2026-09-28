package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the response cache (TKT-01M29CCBY0). Every world runs with it on;
// these prove that a page served from it is the page a service without it
// draws, for each viewer class, and stays so after each change that moves
// what a page shows.

// uncached is w over a service with no response cache and a fresh Evaluator:
// what every page must equal, byte for byte.
func uncached(w *world) *world {
	svc := &threads.Service{DB: w.db, Perm: perm.New(w.db), Threads: w.cache, Render: render.NewCache(1 << 20)}
	w.writes(svc)
	c := *w
	c.svc = svc
	c.handler = New(Deps{Threads: svc, Users: &users.Service{DB: w.db, Perm: perm.New(w.db)}, Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}})
	return &c
}

// session is the viewer the session middleware would build for handle now:
// its role and status read from the users row, as session.Lookup does on
// every request, so a change another process made is seen.
func (w *world) session(handle string) domain.Viewer {
	w.t.Helper()
	if handle == "" {
		return domain.Anonymous()
	}
	var u dbq.User
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		u, err = q.GetUser(w.t.Context(), w.users[handle])
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return domain.Viewer{UserID: u.ID, Role: u.Role, Active: u.Status == "active", SessionKey: "s-" + handle}
}

// same asserts that the cached handler's page for v equals the uncached
// one's, headers that matter included, and returns the body.
func (w *world) same(v domain.Viewer, path string) string {
	w.t.Helper()
	got, want := w.get(v, path), uncached(w).get(v, path)
	if got.Code != http.StatusOK || want.Code != http.StatusOK {
		w.t.Fatalf("%s as %+v: status %d cached, %d uncached: %s", path, v, got.Code, want.Code, got.Body)
	}
	if got.Body.String() != want.Body.String() || got.Header().Get("ETag") != want.Header().Get("ETag") {
		w.t.Fatalf("%s as user %d (%s): the cached page differs from a fresh draw\n got %s\nwant %s",
			path, v.UserID, v.Role, got.Body, want.Body)
	}
	return got.Body.String()
}

const secret = "the text a moderator still needs to read"

// TestResponseCacheKeepsViewerClassesApart is AC2: whichever class asks
// first, a moderator never gets a member's or an anonymous viewer's page
// from the cache and neither of them gets the moderator's, and every page
// equals a fresh draw. Each order runs in its own world so the first
// request is the one that fills the cache.
func TestResponseCacheKeepsViewerClassesApart(t *testing.T) {
	orders := [][]string{
		{"", "carol", "bob"}, {"", "bob", "carol"}, {"carol", "", "bob"},
		{"carol", "bob", ""}, {"bob", "", "carol"}, {"bob", "carol", ""},
	}
	for _, order := range orders {
		t.Run(strings.Join(order, ","), func(t *testing.T) {
			w := newWorld(t)
			id, _ := w.seed()
			path := "/api/v1/threads/" + id.String()
			for round := range 3 {
				for _, who := range order {
					body := w.same(w.viewer(who), path)
					if moderator := who == "bob"; strings.Contains(body, secret) != moderator {
						t.Fatalf("round %d: %q (moderator=%v) got a page with the deleted text=%v",
							round, who, moderator, !moderator)
					}
				}
			}
			if s := w.svc.Responses.Stats(); s.Hits < 6 || s.Inserts != 3 {
				t.Fatalf("stats %+v: want the later rounds served from three entries", s)
			}
		})
	}
}

// TestResponseCacheAfterMutations: an edit, a delete and a restore through
// the endpoints (TKT-01M29CA9BP), and a moderator's delete and restore in
// the log, each move the log length, so each class's next page is a fresh
// draw that shows the change, with no invalidation call.
func TestResponseCacheAfterMutations(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	classes := []string{"", "carol", "bob"}
	warm := func() {
		for _, who := range classes {
			w.same(w.viewer(who), path)
		}
	}
	warm()

	steps := []struct {
		name  string
		do    func()
		check func(who, body string) bool
	}{
		{"an edit", func() {
			changed(t, w.sendAs(w.viewer("carol"), http.MethodPatch, postPathN(id, 4), `{"source":"Edited to say hello."}`))
		}, func(_, body string) bool { return strings.Contains(body, "Edited to say hello.") }},
		{"a delete", func() {
			changed(t, w.sendAs(w.viewer("carol"), http.MethodDelete, postPathN(id, 4), ``))
		}, func(who, body string) bool { return strings.Contains(body, "Edited to say hello.") == (who == "bob") }},
		{"a restore", func() {
			changed(t, w.sendAs(w.viewer("carol"), http.MethodPost, postPathN(id, 4)+"/restore", ``))
		}, func(_, body string) bool { return strings.Contains(body, "Edited to say hello.") }},
		{"a moderator's restore", func() {
			w.append(id, threadlog.Append{Type: threadlog.TypeRestore, Author: uint64(w.users["bob"]), Ordinal: 3,
				Body: &threadlog.RestoreBody{}})
		}, func(_, body string) bool { return strings.Contains(body, secret) }},
		{"a moderator's delete", func() {
			w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 3,
				Body: &threadlog.DeleteBody{Reason: "again"}})
		}, func(who, body string) bool { return strings.Contains(body, secret) == (who == "bob") }},
	}
	for _, step := range steps {
		before := w.svc.Responses.Stats().Inserts
		step.do()
		for _, who := range classes {
			if body := w.same(w.viewer(who), path); !step.check(who, body) {
				t.Errorf("after %s, %q got %s", step.name, who, body)
			}
		}
		if got := w.svc.Responses.Stats().Inserts - before; got != 3 {
			t.Errorf("after %s the three classes made %d new entries, want 3", step.name, got)
		}
		warm()
	}
}

// TestResponseCacheAfterPromotion: a member promoted by another process
// (TKT-01M29C800G: `ketju admin promote` writes the users row through its own
// Evaluator) is served the moderator page at once, and the author badge on
// their posts moves for everyone, with no append to the thread. A moderator
// demoted the same way loses the deleted text.
func TestResponseCacheAfterPromotion(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "carol", "bob"} {
		w.same(w.session(who), path)
	}
	if body := w.same(w.session("carol"), path); strings.Contains(body, secret) {
		t.Fatal("a member saw the deleted text")
	}

	other := perm.New(w.db) // another process's Evaluator
	if ok, err := other.Promote(t.Context(), w.users["carol"], perm.Promotion{}); err != nil || !ok {
		t.Fatalf("promote: %v, %v", ok, err)
	}
	carol := w.session("carol")
	if carol.Role != domain.RoleAdmin {
		t.Fatalf("carol's session role is %q after promotion", carol.Role)
	}
	if p := page(t, w.get(carol, path)); !strings.Contains(p.Posts[2].HTML, secret) || !p.Viewer.CanModerate {
		t.Fatalf("the promoted admin got post 3 as %+v", p.Posts[2])
	}
	w.same(carol, path)
	for _, who := range []string{"", "alice"} {
		p := page(t, w.get(w.session(who), path))
		if p.Posts[3].Author.Role != domain.RoleAdmin {
			t.Errorf("%q still sees carol's badge as %q", who, p.Posts[3].Author.Role)
		}
		if p.Posts[2].HTML != "" {
			t.Errorf("%q got the deleted text after carol's promotion", who)
		}
		w.same(w.session(who), path)
	}

	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET role = 'member' WHERE handle = 'bob'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if body := w.same(w.session("bob"), path); strings.Contains(body, secret) {
		t.Fatal("a demoted moderator still got the deleted text")
	}
}

// TestResponseCacheSeesARegisteredHandle: a page cached while @dana named
// nobody draws the mention as a link once dana registers (TKT-01M29C8005),
// though the thread did not move.
func TestResponseCacheSeesARegisteredHandle(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "alice", "Waiting for @dana to join.", 0)
	path := "/api/v1/threads/" + id.String()
	if body := w.same(domain.Anonymous(), path); strings.Contains(body, "/u/dana") {
		t.Fatalf("an unregistered handle rendered as a link: %s", body)
	}
	w.same(domain.Anonymous(), path) // a hit
	w.write(func(q *dbq.Queries) error {
		_, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: "dana", DisplayName: "Dana",
			Status: "active", Role: domain.RoleMember, Kind: "human", CreatedAt: 2, UpdatedAt: 2})
		return err
	})
	if body := w.same(domain.Anonymous(), path); !strings.Contains(body, "/u/dana") {
		t.Fatalf("after dana registered the mention is still text: %s", body)
	}
	if s := w.svc.Responses.Stats(); s.Stale != 1 {
		t.Fatalf("stats %+v: want the entry drawn before dana registered found stale once", s)
	}
}

// TestResponseCacheLeavesTheViewerOut is AC4 through the API: two members
// with different read positions and watch levels share one cached entry,
// and each gets their own viewer block.
func TestResponseCacheLeavesTheViewerOut(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String() + "?page=1"
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.users["alice"], ThreadID: int64(id), LastReadOrdinal: 4, LastReadAt: 1})
	})
	a := page(t, w.get(w.viewer("alice"), path))
	c := page(t, w.get(w.viewer("carol"), path))
	if a.Viewer.LastReadOrdinal != 4 || c.Viewer.LastReadOrdinal != 2 {
		t.Fatalf("viewer blocks %+v and %+v", a.Viewer, c.Viewer)
	}
	if renderJSON(t, a.Posts) != renderJSON(t, c.Posts) {
		t.Fatal("two members got different posts")
	}
	if s := w.svc.Responses.Stats(); s.Inserts != 1 || s.Hits != 1 {
		t.Fatalf("stats %+v: want one entry, filled by alice and served to carol", s)
	}
	// A read position moved by a beacon changes nothing the cache holds.
	ok[dto.ReadPosition](t, w.sendAs(w.viewer("carol"), http.MethodPut, readPath(id), `{"ordinal":4}`))
	if c := page(t, w.get(w.viewer("carol"), path)); c.Viewer.LastReadOrdinal != 4 {
		t.Fatalf("carol's read position is %d after the beacon", c.Viewer.LastReadOrdinal)
	}
}

// TestResponseCacheUnderConcurrency: the three classes read a thread while
// posts land, under -race. Every page a moderator gets
// carries the deleted text and no other page does.
func TestResponseCacheUnderConcurrency(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String() + "?page=1"
	var (
		wg     sync.WaitGroup
		served atomic.Int64
	)
	done := make(chan struct{})
	for _, who := range []string{"", "carol", "bob", "alice"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				rec := w.get(w.viewer(who), path)
				served.Add(1)
				if rec.Code != http.StatusOK {
					t.Errorf("%q: %d", who, rec.Code)
					return
				}
				if strings.Contains(rec.Body.String(), secret) != (who == "bob") {
					t.Errorf("%q got the wrong class's page: %s", who, rec.Body)
					return
				}
			}
		}()
	}
	// Post 3 stays deleted throughout; every append moves the key under the
	// readers' feet. Between appends the readers get a few rounds, so some
	// requests are hits and some race an append. A reader that fails stops,
	// so the wait gives up when the test has failed or a deadline passes
	// rather than hanging on progress that will not come (review 1150).
	deadline := time.Now().Add(30 * time.Second)
appends:
	for i := range 20 {
		for start := served.Load(); served.Load() < start+12; {
			if t.Failed() || time.Now().After(deadline) {
				break appends
			}
			runtime.Gosched()
		}
		w.post(id, "alice", fmt.Sprintf("post %d", i), 0)
	}
	close(done)
	wg.Wait()
	if !t.Failed() && time.Now().After(deadline) {
		t.Fatal("the readers stopped making progress")
	}
	if s := w.svc.Responses.Stats(); s.Hits == 0 {
		t.Fatalf("stats %+v: the readers never hit the cache", s)
	}
}
