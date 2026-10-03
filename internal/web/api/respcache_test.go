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
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
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
	c.handler = New(Deps{Threads: svc, Users: &users.Service{DB: w.db, Perm: perm.New(w.db)}, Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}, Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db}, Perm: perm.New(w.db)})
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

// TestResponseCacheAfterRoleChange: role changes made through the admin API
// (TKT-01M29CXK1X) apply on the changed user's next request, and the author
// badge moves for everyone, with no append to the thread. carol, made admin,
// promotes alice through the API; alice then demotes carol, and carol's next
// page is a member's.
func TestResponseCacheAfterRoleChange(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	for _, who := range []string{"", "alice", "carol"} {
		w.same(w.session(who), path)
	}
	if ok, err := perm.New(w.db).Promote(t.Context(), w.users["carol"], perm.Promotion{}); err != nil || !ok {
		t.Fatalf("promote: %v, %v", ok, err)
	}
	role := func(as, handle, to string) {
		t.Helper()
		rec := w.sendAs(w.session(as), "PUT", "/api/v1/admin/users/"+handle+"/role", `{"role":"`+to+`"}`)
		if rec.Code != 200 {
			t.Fatalf("%s setting %s to %s: %d %s", as, handle, to, rec.Code, rec.Body)
		}
	}
	badge := func(who, handle string) string {
		t.Helper()
		for _, p := range page(t, w.get(w.session(who), path)).Posts {
			if p.Author.Handle == handle {
				return p.Author.Role
			}
		}
		t.Fatalf("%s has no post on the page", handle)
		return ""
	}

	role("carol", "alice", domain.RoleAdmin)
	if body := w.same(w.session("alice"), path); !strings.Contains(body, secret) {
		t.Error("alice, promoted through the API, did not get the deleted text on her next request")
	}
	if b := badge("", "alice"); b != domain.RoleAdmin {
		t.Errorf("an anonymous reader sees alice's badge as %q", b)
	}

	role("alice", "carol", domain.RoleMember)
	carol := w.session("carol")
	if p := page(t, w.get(carol, path)); strings.Contains(p.Posts[2].HTML, secret) || p.Viewer.CanModerate {
		t.Error("carol, demoted through the API, still moderates on her next request")
	}
	w.same(carol, path)
	for _, who := range []string{"", "alice"} {
		if b := badge(who, "carol"); b != domain.RoleMember {
			t.Errorf("%q still sees carol's badge as %q", who, b)
		}
		w.same(w.session(who), path)
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

// TestDisplayNameChangeMissesTheResponseCache is TKT-01M3V6XVVG criterion
// 4, the response-cache key invariant: the key holds a digest of the page's
// authors (threads.authorsDigest), so once an author changes their display
// name through PATCH /settings/profile's service, the next request for a
// page cached before it misses, and carries the new name, with no
// invalidation call.
func TestDisplayNameChangeMissesTheResponseCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	name := func(v domain.Viewer) string {
		t.Helper()
		for _, p := range page(t, w.get(v, path)).Posts {
			if p.Author.Handle == "alice" {
				return p.Author.DisplayName
			}
		}
		t.Fatal("alice has no post on the page")
		return ""
	}
	carol := w.session("carol")
	w.same(carol, path)
	w.same(carol, path)
	before := w.svc.Responses.Stats()
	if before.Hits == 0 {
		t.Fatalf("stats %+v: the second request was not served from the cache", before)
	}

	u := &users.Service{DB: w.db, Perm: perm.New(w.db)}
	if _, err := u.UpdateProfile(t.Context(), w.session("alice"),
		users.ProfilePatch{DisplayName: []byte(`"Alice the Renamed"`)}); err != nil {
		t.Fatal(err)
	}
	if got := name(carol); got != "Alice the Renamed" {
		t.Errorf("after the change the page names alice %q", got)
	}
	if s := w.svc.Responses.Stats(); s.Hits != before.Hits || s.Inserts != before.Inserts+1 {
		t.Errorf("the page after the change was not a fresh draw: %+v then %+v", before, s)
	}
	w.same(carol, path)
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
	// The page reads the service clock (a pin's expiry, TKT-01M29CKRGS), and
	// w.clock is a plain field w.post advances. Freeze the readers' clock so
	// only the thread moves under them.
	frozen := time.UnixMilli(w.clock)
	w.svc.Now = func() time.Time { return frozen }
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

// TestLockMovesTheResponseCacheKey is AC6, the response cache invariant for
// moderation (docs/13 "Invariants"): a page cached before a lock is not served
// after it. The lock is an append, so it moves the log length in the key, and
// the next GET is a miss drawn under the new length, whose header says
// locked. Unlock, pin and unpin move it the same way.
func TestLockMovesTheResponseCacheKey(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	path := "/api/v1/threads/" + id.String()
	classes := []string{"", "carol", "bob"}
	for _, step := range []struct {
		action, body string
		check        func(dto.ThreadHeader) bool
	}{
		{"lock", `{"reason":"cooling off"}`, func(h dto.ThreadHeader) bool { return h.Locked }},
		{"unlock", ``, func(h dto.ThreadHeader) bool { return !h.Locked }},
		{"unpin", `{"scope":"forum"}`, func(h dto.ThreadHeader) bool { return h.Pinned == nil }},
		{"pin", `{"scope":"global"}`, func(h dto.ThreadHeader) bool { return h.Pinned != nil && h.Pinned.Scope == "global" }},
	} {
		for _, who := range classes {
			w.same(w.viewer(who), path)
		}
		before := w.svc.Responses.Stats()
		for _, who := range classes {
			w.same(w.viewer(who), path)
		}
		if s := w.svc.Responses.Stats(); s.Hits != before.Hits+3 || s.Inserts != before.Inserts {
			t.Fatalf("before the %s the second round was not served from the cache: %+v then %+v", step.action, before, s)
		}

		h := header(t, w.sendAs(w.viewer("bob"), http.MethodPost, modPath(id, step.action), step.body))
		before = w.svc.Responses.Stats()
		for _, who := range classes {
			rec := w.get(w.viewer(who), path)
			p := page(t, rec)
			if p.Thread.LogLength != h.LogLength || !step.check(p.Thread) {
				t.Errorf("after the %s %q got the header %+v", step.action, who, p.Thread)
			}
			if tag := rec.Header().Get("ETag"); !strings.HasPrefix(tag, fmt.Sprintf(`"%s-%d-`, id, h.LogLength)) {
				t.Errorf("after the %s %q got ETag %s, not one under the new log length %d", step.action, who, tag, h.LogLength)
			}
		}
		if s := w.svc.Responses.Stats(); s.Misses != before.Misses+3 || s.Inserts != before.Inserts+3 || s.Hits != before.Hits {
			t.Errorf("after the %s the three classes' pages were not fresh draws: %+v then %+v", step.action, before, s)
		}
	}
}

// TestResponseCacheSeparatesTheHiddenPostsAuthor is the response cache
// invariant for TKT-01M29CKRJ9 (docs/13 "Invariants"): a page holding a post
// a moderator hid is drawn for its author under a class of their own, so in
// either request order, and again once both entries are warm, the other
// member's answer never carries the hidden post's html and the author's
// always does, from two entries. A page that holds none of the viewer's
// hidden posts keeps the shared member entry.
func TestResponseCacheSeparatesTheHiddenPostsAuthor(t *testing.T) {
	for _, order := range [][]string{{"alice", "carol", "alice"}, {"carol", "alice", "carol"}} {
		t.Run(strings.Join(order, ","), func(t *testing.T) {
			w := newWorld(t)
			id, _ := w.seedHidden()
			path := "/api/v1/threads/" + id.String()
			for round := range 2 {
				for _, who := range order {
					body := w.same(w.viewer(who), path)
					if author := who == "alice"; strings.Contains(body, hiddenSecret) != author {
						t.Fatalf("round %d: %q (author=%v) got a page with the hidden text=%v", round, who, author, !author)
					}
				}
			}
			// Two keys: alice's and the member one. Each was inserted by
			// its first request, and the other four requests hit.
			if s := w.svc.Responses.Stats(); s.Inserts != 2 || s.Entries != 2 || s.Hits != 4 {
				t.Fatalf("stats %+v: want two entries, one per key, and the rest hits", s)
			}
			// scribe, another member, shares carol's entry.
			if body := w.same(w.viewer("scribe"), path); strings.Contains(body, hiddenSecret) {
				t.Fatal("another member got the hidden text")
			}
			if s := w.svc.Responses.Stats(); s.Inserts != 2 || s.Hits != 5 {
				t.Fatalf("stats %+v: another member did not share the member entry", s)
			}
		})
	}

	// A page without alice's hidden post is the member page for her too.
	w := newWorld(t)
	id := w.longThread(1, 30)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["bob"]), Ordinal: 2,
		Body: &threadlog.DeleteBody{ByModerator: true}})
	base := "/api/v1/threads/" + id.String()
	w.same(w.viewer("carol"), base+"?page=2")
	before := w.svc.Responses.Stats()
	w.same(w.viewer("alice"), base+"?page=2")
	if s := w.svc.Responses.Stats(); s.Inserts != before.Inserts || s.Hits != before.Hits+1 {
		t.Errorf("alice's page 2, which holds none of her hidden posts, was not the shared member entry: %+v then %+v", before, s)
	}
	for _, who := range []string{"carol", "alice"} {
		w.same(w.viewer(who), base+"?page=1")
		p := page(t, w.get(w.viewer(who), base+"?page=1"))
		if got := p.Posts[1]; got.Ordinal != 2 || !got.Deleted || (got.HTML != "") != (who == "alice") {
			t.Errorf("%s's page 1 has alice's hidden post 2 as %+v", who, got)
		}
	}
}
