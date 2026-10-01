package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	searchidx "github.com/terva-sh/ketju/internal/events/search"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for GET /api/v1/search (TKT-01M29CTY8T, docs/07 "Search"). The
// index is a real search.Indexer over the world's logs, driven by hand:
// index() brings it up to every log, as serve's catch-up does, and a test
// that wants the index to trail simply does not call it.

// index brings the search index up to the end of every log, with a fresh
// indexer over the world's thread cache as it now is (a redaction replaces
// the cache).
func (w *world) index() {
	w.t.Helper()
	x, err := searchidx.New(searchidx.Options{DB: w.db, Root: w.root, Threads: w.cache, Quarantined: w.svc.Quarantined})
	if err != nil {
		w.t.Fatal(err)
	}
	if err := x.Prepare(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	if err := x.CatchUp(w.t.Context()); err != nil {
		w.t.Fatal(err)
	}
	if lag := x.Lag(); lag.Threads != 0 {
		w.t.Fatalf("the index trails %d threads after a catch-up", lag.Threads)
	}
}

func searchPath(params ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(params); i += 2 {
		v.Set(params[i], params[i+1])
	}
	return "/api/v1/search?" + v.Encode()
}

// searchAs runs a search as v and decodes the 200 strictly.
func (w *world) searchAs(v domain.Viewer, params ...string) dto.SearchResults {
	w.t.Helper()
	return decodeAs[dto.SearchResults](w.t, w.get(v, searchPath(params...)))
}

// hitKeys names each result as thread/ordinal.
func hitKeys(res dto.SearchResults) []string {
	out := []string{}
	for _, r := range res.Results {
		out = append(out, fmt.Sprintf("%s/%d", r.Thread.ID, r.Ordinal))
	}
	return out
}

func hitKey(id threadid.ID, ordinal int) string { return fmt.Sprintf("%s/%d", id, ordinal) }

// pageShows is the oracle: whether v's thread page shows post ordinal of id
// as itself, not removed, redacted or moved.
func (w *world) pageShows(v domain.Viewer, id threadid.ID, ordinal int) bool {
	w.t.Helper()
	rec := w.get(v, fmt.Sprintf("%s?ordinal=%d", threadPath(id), ordinal))
	if rec.Code != http.StatusOK {
		return false
	}
	for _, p := range page(w.t, rec).Posts {
		if p.Ordinal == ordinal {
			return !p.Deleted && !p.Redacted && p.MovedTo == nil
		}
	}
	return false
}

// searchViewers are the viewers every access case is searched as: an
// anonymous reader, a member, the post's author, a moderator by role, a
// member who moderates the case's forum by a group grant, and an admin.
var searchViewers = []string{"anon", "member", "author", "moderator", "grant-mod", "admin"}

func (w *world) searchViewer(class string) domain.Viewer {
	switch class {
	case "anon":
		return domain.Anonymous()
	case "member":
		return w.viewer("carol")
	case "author":
		return w.viewer("alice")
	case "moderator":
		return w.viewer("bob")
	case "grant-mod":
		return w.viewer("gina")
	case "admin":
		return domain.Viewer{UserID: w.users["dana"], Role: domain.RoleAdmin, Active: true, SessionKey: "s-dana"}
	}
	w.t.Fatalf("no viewer %q", class)
	return domain.Viewer{}
}

// addAdmin adds dana, an admin.
func (w *world) addAdmin() {
	w.t.Helper()
	w.addUsers("active", "dana")
	w.exec(`UPDATE users SET role = 'admin' WHERE id = ?`, w.users["dana"])
}

// groupID is the id of the group with slug.
func (w *world) groupID(slug string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(w.t.Context(), slug)
		id = g.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// searchAccessCase is one way a post can stop being readable. Each runs in a
// world with forums open (public), members (signed-in only), vault
// (restricted, read by nobody but an admin) and noread (signed-in only,
// where the members group holds rows that do not include read). gina
// moderates open by a grant to the moderators group. The thread is in open
// unless start says otherwise, and its post 2 is alice's and holds the
// case's word.
type searchAccessCase struct {
	name  string
	start string // the forum the thread starts in; "" is open
	// act changes the world after the post is indexed. It may run searches
	// itself, and returns the viewers who must find the post afterwards.
	act  func(t *testing.T, w *world, sw *searchCaseWorld) []string
	want []string // who finds it when act returns nil
}

type searchCaseWorld struct {
	forums map[string]int64
	id     threadid.ID
	word   string
}

var everyone = searchViewers

// TestSearchNeverWidensAccess is the fifth criterion and the docs/14
// invariant: a result reaches a viewer only if that viewer's thread page
// shows the post as itself when the result is served. Each case states who
// finds the post, and every result any viewer gets is checked against that
// viewer's thread page, read after the search, so a search that widened
// access and one that answered too little both fail.
func TestSearchNeverWidensAccess(t *testing.T) {
	cases := []searchAccessCase{
		{name: "an open forum, the control", want: everyone},
		{name: "a hidden forum", start: "vault", want: []string{"admin"}},
		{name: "a signed-in-only forum", start: "members",
			want: []string{"member", "author", "moderator", "grant-mod", "admin"}},
		{name: "a forum without read for a member", start: "noread", want: []string{"admin"}},
		{name: "the author's delete", want: nil, act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.del(sw.id, 2, "alice")
			w.index()
			return nil
		}},
		{name: "a group-grant moderator's hide", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			changed(t, send(w.chain(), "gina", http.MethodDelete, postPathN(sw.id, 2), `{"reason":"rude"}`))
			if !w.entry(sw.id, 2).Flags.Has(threadlog.EntryHiddenByMod) {
				t.Fatal("gina's delete did not hide the post as a moderator")
			}
			w.index()
			return nil
		}},
		{name: "a moderator's hide by role", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			changed(t, send(w.chain(), "bob", http.MethodDelete, postPathN(sw.id, 2), `{"reason":"rude"}`))
			w.index()
			return nil
		}},
		{name: "an auto-hide pending review", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.addUsers("active", "rep1", "rep2", "rep3")
			for _, rep := range []string{"rep1", "rep2", "rep3"} {
				if rec := send(w.chain(), rep, http.MethodPost, threadPath(sw.id)+"/flags",
					`{"ordinal":2,"reason":"spam"}`); rec.Code != http.StatusOK {
					t.Fatalf("%s's flag: %d %s", rep, rec.Code, rec.Body)
				}
			}
			if !w.entry(sw.id, 2).Flags.Has(threadlog.EntryAutoHidden) {
				t.Fatal("three flags did not auto-hide the post")
			}
			w.index()
			return nil
		}},
		{name: "a redaction", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.redactOffline(sw.id, 2)
			w.index()
			return nil
		}},
		{name: "a move to a hidden forum, projected", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.append(sw.id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
				Body: &threadlog.ThreadMovedBody{ForumID: uint64(sw.forums["vault"])}})
			return []string{"admin"}
		}},
		{name: "a move to a hidden forum, the projection held behind", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			// The statement reads the row, still in open, and returns the
			// hit to everyone; only the row read again under the thread,
			// once the move is projected, refuses it.
			w.appendUnprojected(sw.id, sw.forums["open"], sw.forums["vault"])
			return []string{"admin"}
		}},
		{name: "a move to a hidden forum that cannot be projected", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.appendUnprojected(sw.id, sw.forums["open"], sw.forums["vault"])
			project := w.svc.Project
			w.svc.Project = func(context.Context, threadid.ID, int64) error { return errors.New("injected") }
			t.Cleanup(func() { w.svc.Project = project })
			// Fails closed for everyone, the admin included, until the
			// projection catches up.
			for _, class := range searchViewers {
				if got := hitKeys(w.searchAs(w.searchViewer(class), "q", sw.word)); len(got) != 0 {
					t.Errorf("%s found %v in a thread whose move could not be projected", class, got)
				}
			}
			// A service built with no Project cannot bring the row up
			// either, and fails closed the same way.
			w.svc.Project = nil
			if got := hitKeys(w.searchAs(w.searchViewer("member"), "q", sw.word)); len(got) != 0 {
				t.Errorf("with no Project, the member found %v", got)
			}
			w.svc.Project = project
			return []string{"admin"}
		}},
		{name: "a move back from a hidden forum, projected", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			for _, f := range []string{"vault", "open"} {
				w.append(sw.id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
					Body: &threadlog.ThreadMovedBody{ForumID: uint64(sw.forums[f])}})
			}
			return everyone
		}},
		{name: "a move back from a hidden forum, the projection held behind", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.append(sw.id, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
				Body: &threadlog.ThreadMovedBody{ForumID: uint64(sw.forums["vault"])}})
			w.appendUnprojected(sw.id, sw.forums["vault"], sw.forums["open"])
			// Until the move back is projected, the statement sees vault,
			// and only the admin's search reaches the thread, which then
			// projects it.
			for _, class := range searchViewers {
				got := hitKeys(w.searchAs(w.searchViewer(class), "q", sw.word))
				if class != "admin" && len(got) != 0 {
					t.Errorf("%s found %v before the move back was projected", class, got)
				}
			}
			return everyone
		}},
		{name: "a suspended author whose posts were hidden", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.admin(w.viewer("bob"), http.MethodPost, "/api/v1/admin/users/alice/suspend", `{"reason":"spam","hide_posts":true}`)
			if got := w.hidden(sw.id, 2); len(got) != 1 {
				t.Fatal("the suspension did not hide post 2")
			}
			w.index()
			return nil
		}},
		{name: "a suspended author whose posts were not hidden", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			w.admin(w.viewer("bob"), http.MethodPost, "/api/v1/admin/users/alice/suspend", `{"reason":"spam"}`)
			w.index()
			return everyone
		}},
		{name: "a group grant revoked between two requests", start: "vault", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			mods := w.groupID(domain.GroupModerators)
			for _, p := range []domain.Permission{domain.PermRead, domain.PermModerate} {
				if err := w.svc.Perm.Grant(t.Context(), audit.By{}, sw.forums["vault"], mods, p); err != nil {
					t.Fatal(err)
				}
			}
			if got := hitKeys(w.searchAs(w.searchViewer("grant-mod"), "q", sw.word)); !slices.Equal(got, []string{hitKey(sw.id, 2)}) {
				t.Fatalf("gina, granted read on vault, found %v", got)
			}
			if err := w.svc.Perm.Revoke(t.Context(), audit.By{}, sw.forums["vault"], mods, domain.PermRead); err != nil {
				t.Fatal(err)
			}
			return []string{"admin"}
		}},
		{name: "a quarantined thread", act: func(t *testing.T, w *world, sw *searchCaseWorld) []string {
			id := sw.id
			w.svc.Quarantined = func(x threadid.ID) bool { return x == id }
			return nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.addAdmin()
			sw := &searchCaseWorld{forums: map[string]int64{"open": w.forum}, word: "zebrafinch"}
			sw.forums["members"] = w.forumAt("members", domain.VisibilityMembers, false)
			sw.forums["vault"] = w.forumAt("vault", domain.VisibilityRestricted, false)
			sw.forums["noread"] = w.forumAt("noread", domain.VisibilityMembers, false)
			if err := perm.New(w.db).Grant(t.Context(), audit.By{}, sw.forums["noread"],
				w.groupID(domain.GroupMembers), domain.PermReply); err != nil {
				t.Fatal(err)
			}
			w.groupModerator(w.forum)
			start := sw.forums["open"]
			if c.start != "" {
				start = sw.forums[c.start]
			}
			sw.id = w.threadIn(1, start, "A thread about birds", "alice")
			w.post(sw.id, "alice", "I saw a "+sw.word+" at the feeder today.", 0)
			w.post(sw.id, "carol", "Nothing to see here.", 0)
			w.index()

			want := c.want
			if c.act != nil {
				want = c.act(t, w, sw)
			}
			for _, class := range searchViewers {
				v := w.searchViewer(class)
				res := w.searchAs(v, "q", sw.word)
				got := hitKeys(res)
				shows := w.pageShows(v, sw.id, 2)
				found := slices.Equal(got, []string{hitKey(sw.id, 2)})
				if len(got) != 0 && !found {
					t.Errorf("%s found %v, which is not post 2", class, got)
				}
				if found != slices.Contains(want, class) {
					t.Errorf("%s: found %t, want %t", class, found, !found)
				}
				if found && !shows {
					t.Errorf("%s was served post 2, which their thread page does not show them", class)
				}
				if !found && shows {
					t.Errorf("%s's thread page shows post 2, and search did not find it", class)
				}
			}
			// The thread page agrees with the case's verdict for each viewer
			// it refuses, so the expectation is not wrong in both places.
			if c.start == "noread" {
				if got := problemCode(t, w.get(w.viewer("carol"), threadPath(sw.id))); got != "forbidden" {
					t.Errorf("carol reads the noread thread as %s, want forbidden", got)
				}
			}
		})
	}
}

// TestSearchIsFreshWhenServed is the sixth criterion: with the index left
// where it was after indexing, a post deleted, hidden, redacted or edited,
// and a thread moved or renamed, is not served with what the index holds.
// The rename is held behind the projection too, so the title is judged at
// the log length the request read. Once the index catches up, the edited
// post is served with its new text, and the renamed title by its new words.
func TestSearchIsFreshWhenServed(t *testing.T) {
	w := newWorld(t)
	vault := w.forumAt("vault", domain.VisibilityRestricted, false)
	carol := w.viewer("carol")
	id := w.threadIn(1, w.forum, "Kingfisher sightings", "alice")
	for _, src := range []string{"deleteword", "hideword", "editword before", "redactword", "keepword"} {
		w.post(id, "alice", src+" by the river", 0)
	}
	moved := w.threadIn(2, w.forum, "Heron sightings", "alice")
	w.post(moved, "alice", "moveword by the lake", 0)
	linked := w.threadIn(3, w.forum, "Osprey sightings", "alice")
	w.post(linked, "alice", "linkword by the sea", 0)
	w.index()

	// A post's result links its thread by the title the thread has at the
	// request's log length, not the one the statement joined from a
	// projection that has not heard of the rename.
	w.renameUnprojected(linked, "Tern sightings", "tern-sightings")
	res := w.searchAs(carol, "q", "linkword")
	if len(res.Results) != 1 || res.Results[0].Thread.Title != "Tern sightings" || res.Results[0].Thread.Slug != "tern-sightings" {
		t.Errorf("linkword's result links %+v, want the thread's title now", res.Results)
	}

	for _, q := range []string{"deleteword", "hideword", "editword", "redactword", "moveword"} {
		if got := hitKeys(w.searchAs(carol, "q", q)); len(got) != 1 {
			t.Fatalf("before any change, %q found %v", q, got)
		}
	}
	// Post 1 says "Kingfisher sightings, the first post." and goes on
	// saying it; only the title's own hit must go with the rename.
	title := hitKey(id, 0)
	if got := hitKeys(w.searchAs(carol, "q", "kingfisher")); !slices.Contains(got, title) {
		t.Fatalf("before the rename, the title is not found: %v", got)
	}

	w.del(id, 2, "alice")
	changed(t, send(w.chain(), "bob", http.MethodDelete, postPathN(id, 3), `{"reason":"rude"}`))
	w.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: uint64(w.users["alice"]), Ordinal: 4,
		Body: &threadlog.EditBody{Source: "editword after, rewritten"}})
	w.appendUnprojected(moved, w.forum, vault)
	w.renameUnprojected(id, "Wren sightings", "wren-sightings")
	// First, while the projection still holds the old title, so the
	// statement's join agrees with the index and only the row read again at
	// the request's log length refuses the hit.
	if got := hitKeys(w.searchAs(carol, "q", "kingfisher")); slices.Contains(got, title) {
		t.Errorf("the old title, with the index and the projection behind, found %v", got)
	}
	if row := w.threadRow(id); row.Title != "Wren sightings" {
		t.Errorf("judging the title did not bring the projection up: %q", row.Title)
	}
	for _, q := range []string{"deleteword", "hideword", "editword", "before", "moveword"} {
		if got := hitKeys(w.searchAs(carol, "q", q)); len(got) != 0 {
			t.Errorf("%q, with the index behind, found %v", q, got)
		}
	}
	res = w.searchAs(carol, "q", "keepword")
	if len(res.Results) != 1 || res.Results[0].Thread.Title != "Wren sightings" {
		t.Errorf("keepword's result links %+v, want the thread's title now", res.Results)
	}
	w.redactOffline(id, 5)
	if got := hitKeys(w.searchAs(carol, "q", "redactword")); len(got) != 0 {
		t.Errorf("a redacted post, with the index behind, was found: %v", got)
	}

	w.index()
	res = w.searchAs(carol, "q", "editword")
	if got := hitKeys(res); !slices.Equal(got, []string{hitKey(id, 4)}) {
		t.Fatalf("after indexing, editword found %v", got)
	}
	if text := snippetText(res.Results[0].Snippet); !strings.Contains(text, "rewritten") || strings.Contains(text, "before") {
		t.Errorf("the edited post's snippet is %q, want the new revision's text", text)
	}
	if got := hitKeys(w.searchAs(carol, "q", "before")); len(got) != 0 {
		t.Errorf("the old revision's word still finds %v", got)
	}
	if got := hitKeys(w.searchAs(carol, "q", "wren")); !slices.Equal(got, []string{hitKey(id, 0)}) {
		t.Errorf("after indexing, the new title finds %v", got)
	}
	for _, q := range []string{"deleteword", "hideword", "redactword", "moveword"} {
		if got := hitKeys(w.searchAs(carol, "q", q)); len(got) != 0 {
			t.Errorf("%q, after indexing, found %v", q, got)
		}
	}
	if got := hitKeys(w.searchAs(carol, "q", "kingfisher")); slices.Contains(got, title) {
		t.Errorf("the old title, after indexing, found %v", got)
	}
}

// renameUnprojected appends a ThreadRenamed to id's log by its author and
// does not project it, and fails unless the row still has its old title.
func (w *world) renameUnprojected(id threadid.ID, title, slug string) {
	w.t.Helper()
	old := w.threadRow(id).Title
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		_, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: uint64(w.users["alice"]),
			TimeMS: w.clock, Body: &threadlog.ThreadRenamedBody{Title: title, Slug: slug}})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	if row := w.threadRow(id); row.Title != old {
		w.t.Fatalf("the rename was projected already: %q", row.Title)
	}
}

func snippetText(parts []dto.SnippetPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// bulkPosts appends n posts by author to id, each holding text and its
// number, and projects them once.
func (w *world) bulkPosts(id threadid.ID, author, text string, n int) {
	w.t.Helper()
	var res threadlog.Result
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		for i := range n {
			w.clock += 1000
			var err error
			if res, err = th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users[author]),
				TimeMS: w.clock, Body: &threadlog.PostBody{Source: fmt.Sprintf("%s number %d", text, i)}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), id, res.LogLength); err != nil {
		w.t.Fatal(err)
	}
}

// pages follows next_cursor from the first page and returns every page.
func (w *world) pages(v domain.Viewer, params ...string) []dto.SearchResults {
	w.t.Helper()
	var out []dto.SearchResults
	cursor := ""
	for range 100 {
		p := params
		if cursor != "" {
			p = append(slices.Clone(params), "cursor", cursor)
		}
		res := w.searchAs(v, p...)
		out = append(out, res)
		if cursor = res.NextCursor; cursor == "" {
			return out
		}
	}
	w.t.Fatal("more than 100 pages")
	return nil
}

// TestSearchPageLengthIgnoresUnreadableForums is the first criterion: 30
// matches in a forum the viewer cannot read and one in a forum they can
// give one result and no cursor.
func TestSearchPageLengthIgnoresUnreadableForums(t *testing.T) {
	w := newWorld(t)
	vault := w.forumAt("vault", domain.VisibilityRestricted, false)
	hidden := w.threadIn(1, vault, "Vault", "alice")
	w.bulkPosts(hidden, "alice", "needle", 30)
	open := w.threadIn(2, w.forum, "Open", "alice")
	w.post(open, "alice", "a needle in the open", 0)
	w.index()
	res := w.searchAs(w.viewer("carol"), "q", "needle")
	if got := hitKeys(res); !slices.Equal(got, []string{hitKey(open, 2)}) || res.NextCursor != "" {
		t.Errorf("got %v, cursor %q; want only the open post and no cursor", got, res.NextCursor)
	}
}

// TestSearchPagesIgnoreUnreadableForums is the seventh criterion: a
// member's pages over two readable forums, in order and content, are the
// same before and after 200 newer matching posts land in a forum they
// cannot read (Terva review 1728).
func TestSearchPagesIgnoreUnreadableForums(t *testing.T) {
	w := newWorld(t)
	lounge := w.forumAt("lounge", domain.VisibilityPublic, false)
	vault := w.forumAt("vault", domain.VisibilityRestricted, false)
	a := w.threadIn(1, w.forum, "General talk", "alice")
	b := w.threadIn(2, lounge, "Lounge talk", "carol")
	for i := range 20 {
		w.post(a, "alice", fmt.Sprintf("needle in general %d", i), 0)
		w.post(b, "carol", fmt.Sprintf("needle in the lounge %d", i), 0)
	}
	w.index()
	carol := w.viewer("carol")
	before := w.pages(carol, "q", "needle")
	if len(before) != 2 || len(before[0].Results) != 25 || len(before[1].Results) != 15 {
		t.Fatalf("before: %d pages", len(before))
	}
	hidden := w.threadIn(3, vault, "Vault talk", "alice")
	w.bulkPosts(hidden, "alice", "needle in the vault", 200)
	w.index()
	if got := hitKeys(w.searchAs(w.searchAdmin(), "q", "vault")); len(got) != 25 {
		t.Fatalf("the admin finds %d of the vault's posts on a page; the test needs them indexed", len(got))
	}
	after := w.pages(carol, "q", "needle")
	if !jsonEqual(t, before, after) {
		t.Errorf("carol's pages changed when posts landed in a forum she cannot read")
	}
}

func (w *world) searchAdmin() domain.Viewer {
	if _, ok := w.users["dana"]; !ok {
		w.addAdmin()
	}
	return domain.Viewer{UserID: w.users["dana"], Role: domain.RoleAdmin, Active: true, SessionKey: "s-dana"}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}

// TestSearchPagesFill is the third criterion's paging: 61 matches, 10 of
// them deleted after indexing so the index still returns them, give full
// pages of 25 with no repeats, newest first, and a short page only at the
// end. And a search reaches 1000 results and no further: its 40th page has
// no cursor.
func TestSearchPagesFill(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(1, w.forum, "Counting", "alice")
	w.bulkPosts(id, "alice", "needle", 61)
	w.index()
	stale := map[string]bool{}
	for _, o := range []int{2, 3, 20, 26, 27, 28, 29, 50, 51, 62} {
		w.del(id, uint32(o), "alice")
		stale[hitKey(id, o)] = true
	}
	carol := w.viewer("carol")
	pages := w.pages(carol, "q", "needle")
	var sizes []int
	seen := map[string]bool{}
	var order []int
	for _, p := range pages {
		sizes = append(sizes, len(p.Results))
		for _, r := range p.Results {
			k := fmt.Sprintf("%s/%d", r.Thread.ID, r.Ordinal)
			if seen[k] {
				t.Errorf("%s is on two pages", k)
			}
			if stale[k] {
				t.Errorf("%s was deleted and was served", k)
			}
			seen[k] = true
			order = append(order, int(r.Ordinal))
		}
	}
	if !slices.Equal(sizes, []int{25, 25, 1}) {
		t.Errorf("page sizes %v, want 25, 25, 1", sizes)
	}
	if !slices.IsSortedFunc(order, func(a, b int) int { return b - a }) {
		t.Errorf("results are not newest first: %v", order)
	}

	// The reach: 1100 matches give 40 pages, and the 40th has no cursor.
	more := w.threadIn(2, w.forum, "More counting", "alice")
	w.bulkPosts(more, "alice", "haystack", 1100)
	w.index()
	pages = w.pages(carol, "q", "haystack")
	if len(pages) != 40 {
		t.Fatalf("%d pages, want 40", len(pages))
	}
	for i, p := range pages {
		if len(p.Results) != 25 {
			t.Errorf("page %d holds %d", i+1, len(p.Results))
		}
	}
	if pages[39].NextCursor != "" {
		t.Errorf("the 40th page has a cursor")
	}
}

// TestSearchSnippetIsNeverMarkup is the second criterion: a post whose text
// holds markup characters that render.Plain keeps, as a code span and as
// escaped text, has a snippet carrying them as literal text, and the JSON
// escapes them. Its raw HTML is dropped before indexing, so it is never in
// a snippet (Terva review 1713).
func TestSearchSnippetIsNeverMarkup(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(1, w.forum, "Markup", "alice")
	w.post(id, "alice", "needle `<script>alert(1)</script>` and \\<mark\\> and &amp;amp; and <b>bold</b>", 0)
	w.index()
	rec := w.get(w.viewer("carol"), searchPath("q", "needle"))
	res := decodeAs[dto.SearchResults](t, rec)
	if len(res.Results) != 1 {
		t.Fatalf("got %v", hitKeys(res))
	}
	parts := res.Results[0].Snippet
	text := snippetText(parts)
	for _, want := range []string{"<script>alert(1)</script>", "<mark>", "&amp;"} {
		if !strings.Contains(text, want) {
			t.Errorf("the snippet %q lacks the literal text %q", text, want)
		}
	}
	if strings.Contains(text, "<b>") {
		t.Errorf("the snippet %q carries the post's raw HTML", text)
	}
	var matched []string
	for _, p := range parts {
		if p.Match {
			matched = append(matched, p.Text)
		}
	}
	if !slices.Equal(matched, []string{"needle"}) {
		t.Errorf("matched segments %q, want only needle", matched)
	}
	body := rec.Body.String()
	for _, raw := range []string{"<script>", "<mark>", "&amp;"} {
		if strings.Contains(body, raw) {
			t.Errorf("the JSON carries %q unescaped", raw)
		}
	}
}

// TestSearchQueryRules is q's rules: what is refused, and that nothing but
// words and quoted phrases is syntax.
func TestSearchQueryRules(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(1, w.forum, "Rules", "alice")
	w.post(id, "alice", "alpha or not near title x and a caret", 0)
	w.index()
	carol := w.viewer("carol")
	for _, c := range []struct{ name, q, code string }{
		{"absent", "", "required"},
		{"white space", " \t ", "required"},
		{"201 characters", strings.Repeat("a", 201), "too_long"},
		{"17 words", strings.Repeat("w ", 17), "too_many"},
	} {
		params := []string{}
		if c.name != "absent" {
			params = []string{"q", c.q}
		}
		rec := w.get(carol, searchPath(params...))
		var p problem.Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Type != problem.CodeValidationFailed ||
			len(p.Errors) != 1 || p.Errors[0] != (problem.FieldError{Field: "q", Code: c.code}) {
			t.Errorf("%s: %d %s, want validation-failed on q, %s", c.name, rec.Code, rec.Body, c.code)
		}
	}
	post := []string{hitKey(id, 2)}
	for _, c := range []struct {
		q    string
		want []string
	}{
		{"OR", post},
		{"NOT", post},
		{"NEAR(title x)", post}, // the phrase "near title" and x
		{"*", nil},
		{"^x", post},
		{"title:x", post}, // the phrase "title x"
		{`"near title`, post},
		{`"near title"`, post},
		{`"title near"`, nil}, // a phrase matches only in order
		{"alpha caret", post},
		{"alpha zebra", nil}, // every word must match
	} {
		got := hitKeys(w.searchAs(carol, "q", c.q))
		if !slices.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%q found %v, want %v", c.q, got, c.want)
		}
	}
}

// TestSearchParams is the third criterion's parameters: forum access as the
// thread list answers it, author by handle, after and before as Unix ms,
// the cursor, and every filter alone and together.
func TestSearchParams(t *testing.T) {
	w := newWorld(t)
	members := w.forumAt("members", domain.VisibilityMembers, false)
	w.forumAt("vault", domain.VisibilityRestricted, false)
	noread := w.forumAt("noread", domain.VisibilityMembers, false)
	if err := perm.New(w.db).Grant(t.Context(), audit.By{}, noread, w.groupID(domain.GroupMembers), domain.PermReply); err != nil {
		t.Fatal(err)
	}
	a := w.threadIn(1, w.forum, "General", "alice")
	w.post(a, "alice", "needle one", 0) // #2
	w.post(a, "carol", "needle two", 0) // #3
	b := w.threadIn(2, members, "Members", "alice")
	w.post(b, "alice", "needle three", 0) // #2
	w.post(b, "carol", "needle four", 0)  // #3
	w.index()
	carol, anon := w.viewer("carol"), domain.Anonymous()
	at := map[string]dto.Millis{}
	for _, r := range w.searchAs(carol, "q", "needle").Results {
		at[fmt.Sprintf("%s/%d", r.Thread.ID, r.Ordinal)] = r.CreatedAt
	}
	if len(at) != 4 {
		t.Fatalf("carol finds %d posts", len(at))
	}
	ms := func(k string) string { return fmt.Sprint(int64(at[k])) }
	a2, a3, b2, b3 := hitKey(a, 2), hitKey(a, 3), hitKey(b, 2), hitKey(b, 3)

	for _, c := range []struct {
		name   string
		v      domain.Viewer
		params []string
		want   []string
	}{
		{"none", carol, nil, []string{b3, b2, a3, a2}},
		{"anonymous", anon, nil, []string{a3, a2}},
		{"forum", carol, []string{"forum", "members"}, []string{b3, b2}},
		{"author", carol, []string{"author", "carol"}, []string{b3, a3}},
		{"author in any case", carol, []string{"author", "ALICE"}, []string{b2, a2}},
		{"an unknown author", carol, []string{"author", "nobody"}, nil},
		{"after includes its edge", carol, []string{"after", ms(b2)}, []string{b3, b2}},
		{"before excludes its edge", carol, []string{"before", ms(a3)}, []string{a2}},
		{"after and before", carol, []string{"after", ms(a3), "before", ms(b3)}, []string{b2, a3}},
		{"all four", carol, []string{"forum", "general", "author", "alice", "after", ms(a2), "before", ms(b3)}, []string{a2}},
		{"after past before", carol, []string{"after", ms(b3), "before", ms(a2)}, nil},
	} {
		got := hitKeys(w.searchAs(c.v, append([]string{"q", "needle"}, c.params...)...))
		if !slices.Equal(got, c.want) && !(len(got) == 0 && len(c.want) == 0) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	for _, c := range []struct {
		name   string
		v      domain.Viewer
		params []string
		code   string
	}{
		{"an unknown forum", carol, []string{"forum", "nowhere"}, "not-found"},
		{"a hidden forum", carol, []string{"forum", "vault"}, "not-found"},
		{"a signed-in-only forum, anonymously", anon, []string{"forum", "members"}, "unauthenticated"},
		{"a forum without read", carol, []string{"forum", "noread"}, "forbidden"},
		{"after, not a number", carol, []string{"after", "yesterday"}, "bad-request"},
		{"after, negative", carol, []string{"after", "-1"}, "bad-request"},
		{"before, a fraction", carol, []string{"before", "1.5"}, "bad-request"},
		{"a cursor that is not base64", carol, []string{"cursor", "!!"}, "bad-request"},
		{"a cursor for offset 0", carol, []string{"cursor", "MA"}, "bad-request"},
		{"a non-canonical cursor", carol, []string{"cursor", "MDE"}, "bad-request"},
		{"a cursor past the reach", carol, []string{"cursor", "MTAwMA"}, "bad-request"},
	} {
		rec := w.get(c.v, searchPath(append([]string{"q", "needle"}, c.params...)...))
		if got := problemCode(t, rec); got != c.code {
			t.Errorf("%s: %s, want %s", c.name, got, c.code)
		}
	}
	// A cursor the server issues works: offset 1, the page after the first
	// result.
	if got := hitKeys(w.searchAs(carol, "q", "needle", "cursor", "MQ")); !slices.Equal(got, []string{b2, a3, a2}) {
		t.Errorf("from offset 1: %v", got)
	}

	// forum is judged by the forum the log puts the thread in: a move from
	// general to members that the projection has not heard of leaves a's
	// posts out of a search of general, though both forums are carol's.
	w.appendUnprojected(a, w.forum, members)
	if got := hitKeys(w.searchAs(carol, "q", "needle", "forum", "general")); len(got) != 0 {
		t.Errorf("forum=general, after an unprojected move out of it: %v", got)
	}
	if got := hitKeys(w.searchAs(carol, "q", "needle", "forum", "members")); !slices.Equal(got, []string{b3, b2, a3, a2}) {
		t.Errorf("forum=members, once the move is projected: %v", got)
	}
}

// TestSearchAnonymizedAuthor: an anonymized account's posts show its
// placeholder, as every author block does, its old handle finds nothing,
// and the placeholder finds its posts.
func TestSearchAnonymizedAuthor(t *testing.T) {
	w := newWorld(t)
	dana := w.searchAdmin()
	id := w.threadIn(1, w.forum, "Thread", "alice")
	w.post(id, "carol", "needle from carol", 0)
	w.index()
	w.admin(dana, http.MethodPost, "/api/v1/admin/users/carol/anonymize", "")
	placeholder := fmt.Sprintf("deleted-user-%d", w.users["carol"])
	res := w.searchAs(w.viewer("alice"), "q", "needle")
	if len(res.Results) != 1 || res.Results[0].Author.Handle != placeholder || res.Results[0].Author.DisplayName != "" ||
		res.Results[0].Author.Avatar != "" {
		t.Fatalf("the anonymized author is drawn as %+v", res.Results)
	}
	if got := hitKeys(w.searchAs(w.viewer("alice"), "q", "needle", "author", "carol")); len(got) != 0 {
		t.Errorf("the old handle found %v", got)
	}
	if got := hitKeys(w.searchAs(w.viewer("alice"), "q", "needle", "author", placeholder)); !slices.Equal(got, []string{hitKey(id, 2)}) {
		t.Errorf("the placeholder found %v", got)
	}
}

// TestSearchRoute is the fourth criterion's route: no-store; a pre-user is
// refused by the gate before the handler; an inactive viewer is served as
// anonymous; and an agent with an empty write bucket still searches, since
// a read is not charged.
func TestSearchRoute(t *testing.T) {
	w := newWorld(t)
	members := w.forumAt("members", domain.VisibilityMembers, false)
	open := w.threadIn(1, w.forum, "Open", "alice")
	w.post(open, "alice", "needle open", 0)
	closed := w.threadIn(2, members, "Members", "alice")
	w.post(closed, "alice", "needle members", 0)
	w.index()
	h := w.chain()
	rec := send(h, "carol", http.MethodGet, searchPath("q", "needle"), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != "" {
		t.Errorf("a member's search: %d, Cache-Control %q, ETag %q", rec.Code, rec.Header().Get("Cache-Control"), rec.Header().Get("ETag"))
	}
	if got := problemCode(t, send(h, "pre-user", http.MethodGet, searchPath("q", "needle"), "")); got != "forbidden" {
		t.Errorf("a pre-user: %s, want forbidden", got)
	}
	// Every refusal is no-store too, whichever check refuses it (Terva
	// review 1765).
	for _, c := range []struct {
		who, path, code string
	}{
		{"carol", searchPath("q", ""), "validation-failed"},
		{"carol", searchPath("q", "needle", "cursor", "!!"), "bad-request"},
		{"carol", searchPath("q", "needle", "forum", "nowhere"), "not-found"},
		{"", searchPath("q", "needle", "forum", "members"), "unauthenticated"},
		{"pre-user", searchPath("q", "needle"), "forbidden"},
	} {
		rec := send(h, c.who, http.MethodGet, c.path, "")
		if got := problemCode(t, rec); got != c.code || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s as %q: %s with Cache-Control %q, want %s and no-store", c.path, c.who, got,
				rec.Header().Get("Cache-Control"), c.code)
		}
	}
	var res dto.SearchResults
	rec = send(h, "inactive", http.MethodGet, searchPath("q", "needle"), "")
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || !slices.Equal(hitKeys(res), []string{hitKey(open, 2)}) {
		t.Errorf("an inactive viewer: %d %s, want the anonymous answer", rec.Code, rec.Body)
	}
	if got := problemCode(t, send(h, "", http.MethodPost, searchPath("q", "needle"), "")); got == "" {
		t.Errorf("POST was answered")
	}

	aw := newAgentWorld(t)
	id, _ := aw.seed()
	token := aw.issue("scribe")
	for i := range 20 {
		if rec := aw.bearer(token, http.MethodPost, postsPath(id), reply(fmt.Sprintf("post %d", i))); rec.Code != http.StatusCreated {
			t.Fatalf("write %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	if rec := aw.bearer(token, http.MethodPost, postsPath(id), reply("one too many")); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the bucket is not empty: %d", rec.Code)
	}
	for range 3 {
		if rec := aw.bearer(token, http.MethodGet, searchPath("q", "post"), ""); rec.Code != http.StatusOK {
			t.Errorf("an agent's search with an empty bucket: %d %s", rec.Code, rec.Body)
		}
	}
}

// TestSearchIsNotCached is the fourth criterion's cache half: a search
// neither looks in nor fills the response cache, whoever asks.
func TestSearchIsNotCached(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(1, w.forum, "Open", "alice")
	w.post(id, "alice", "needle", 0)
	w.index()
	before := w.svc.Responses.Stats()
	for _, v := range []domain.Viewer{domain.Anonymous(), w.viewer("carol"), w.viewer("bob")} {
		for range 2 {
			w.searchAs(v, "q", "needle")
		}
	}
	if after := w.svc.Responses.Stats(); after != before {
		t.Errorf("the response cache moved for searches: before %+v, after %+v", before, after)
	}
	// The control: a thread page does move it.
	w.get(w.viewer("carol"), threadPath(id))
	if after := w.svc.Responses.Stats(); after == before {
		t.Fatal("a thread page did not touch the cache, so the check above proves nothing")
	}
}

// TestSearchIndexingFlag: indexing is the indexer's Indexing, true until a
// catch-up finishes and false after, and false with no indexer wired.
func TestSearchIndexingFlag(t *testing.T) {
	w := newWorld(t)
	id := w.threadIn(1, w.forum, "Open", "alice")
	w.post(id, "alice", "needle", 0)
	carol := w.viewer("carol")
	if res := w.searchAs(carol, "q", "needle"); res.Indexing {
		t.Error("with no indexer wired, indexing is true")
	}
	x, err := searchidx.New(searchidx.Options{DB: w.db, Root: w.root, Threads: w.cache})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.svc.SearchIndexing = x.Indexing
	if res := w.searchAs(carol, "q", "needle"); !res.Indexing || len(res.Results) != 0 {
		t.Errorf("before the catch-up: indexing %t, %v", res.Indexing, hitKeys(res))
	}
	if err := x.CatchUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if res := w.searchAs(carol, "q", "needle"); res.Indexing || len(res.Results) != 1 {
		t.Errorf("after the catch-up: indexing %t, %v", res.Indexing, hitKeys(res))
	}
}
