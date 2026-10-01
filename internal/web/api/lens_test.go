package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the lens framework (TKT-01M29CRJ1P). These serve fixed filters
// through threads.Service.TestLenses, which wins over a kind's resolver: the
// framework is the same whichever lens chose the ordinals. Each lens ticket
// adds its kind's rows to TestLensPostsMatchCanonical, and its own tests
// beside this file.

// lensAs serves kind with f on w's service, replacing whatever it served.
func (w *world) lensAs(kind lens.Kind, f lens.Filter) {
	w.svc.TestLenses = map[lens.Kind]lens.Filter{kind: f}
}

// odd and even are ordinals 1..n that are odd, and even.
func odd(n int) []uint32 {
	var out []uint32
	for o := 1; o <= n; o += 2 {
		out = append(out, uint32(o))
	}
	return out
}

func even(n int) []uint32 {
	var out []uint32
	for o := 2; o <= n; o += 2 {
		out = append(out, uint32(o))
	}
	return out
}

// gaps is a lens's gaps, from after and count pairs.
func gaps(pairs ...int) []dto.LensGap {
	out := []dto.LensGap{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, dto.LensGap{After: pairs[i], Count: pairs[i+1]})
	}
	return out
}

func ordinalsOf(posts []dto.Post) []int {
	out := make([]int, len(posts))
	for i, p := range posts {
		out[i] = p.Ordinal
	}
	return out
}

// TestUnknownLensServesCanonical: a value that is not a decided lens, the
// empty one included, answers 200 with lens: null and exactly the canonical
// page, body and ETag, for every viewer class (docs/07, decided 2026-10-01).
func TestUnknownLensServesCanonical(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seedHidden()
	base := threadPath(id)
	for _, who := range []string{"", "carol", "alice", "bob"} {
		want := w.get(w.viewer(who), base)
		if want.Code != http.StatusOK {
			t.Fatalf("%q: the canonical page is %d", who, want.Code)
		}
		for _, q := range []string{"?lens=", "?lens=nonsense", "?lens=chain:abc", "?lens=chain:0", "?lens=unread:-1",
			"?lens=author:", "?lens=since:yesterday", "?lens=mod:all", "?lens=digest", "?lens=%00", "?lens=" + strings.Repeat("x", 5000)} {
			got := w.get(w.viewer(who), base+q)
			if got.Code != http.StatusOK {
				t.Errorf("%q %s: %d %s", who, q, got.Code, got.Body)
				continue
			}
			if p := page(t, got); p.Lens != nil {
				t.Errorf("%q %s: lens %+v, want null", who, q, p.Lens)
			}
			if got.Body.String() != want.Body.String() || got.Header().Get("ETag") != want.Header().Get("ETag") {
				t.Errorf("%q %s: not the canonical page\n got %s\nwant %s", who, q, got.Body, want.Body)
			}
		}
	}
}

// TestLensOnHiddenThreadIsNotFound: access runs before the lens, so a lens
// on a thread in a forum hidden from the viewer is not-found and one on a
// signed-in-only forum is unauthenticated to an anonymous viewer, as the
// canonical page is, and a lens the build does not serve answers the same.
func TestLensOnHiddenThreadIsNotFound(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.lensAs(lens.Chain, lens.Ordinals(1, 2))
	paths := []string{threadPath(id) + "?lens=chain:1", threadPath(id) + "?lens=chain:1&ordinal=2",
		threadPath(id) + "?lens=nonsense"}

	w.write(func(q *dbq.Queries) error {
		return q.SetForumVisibility(t.Context(), dbq.SetForumVisibilityParams{
			Visibility: domain.VisibilityMembers, UpdatedAt: 2, ID: w.forum})
	})
	members := newHandlerFor(w)
	members.lensAs(lens.Chain, lens.Ordinals(1, 2))
	for _, path := range paths {
		if rec := members.get(domain.Anonymous(), path); rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous on a members forum, %s: %d", path, rec.Code)
		}
	}
	// A member still reads it, through the lens.
	if p := page(t, members.get(w.viewer("carol"), paths[0])); p.Lens == nil || !slices.Equal(ordinalsOf(p.Posts), []int{1, 2}) {
		t.Errorf("a member's lens page on a members forum: lens %+v, posts %v", p.Lens, ordinalsOf(p.Posts))
	}

	w.hideForum()
	hidden := newHandlerFor(w)
	hidden.lensAs(lens.Chain, lens.Ordinals(1, 2))
	for _, who := range []string{"", "carol"} {
		for _, path := range paths {
			if rec := hidden.get(w.viewer(who), path); rec.Code != http.StatusNotFound {
				t.Errorf("%q on a restricted forum, %s: %d %s", who, path, rec.Code, rec.Body)
			}
		}
	}
}

// TestLensPageGolden fixes what the endpoint serves for a lens page: a member
// reading the odd posts of seedHidden's thread, which keeps #3, deleted by
// its author, and #5, hidden by a moderator, as tombstones. The test filter
// is served as chain:1, so the golden names that kind. Regenerate with
// `go test ./internal/web/api -update`.
func TestLensPageGolden(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seedHidden()
	w.lensAs(lens.Chain, lens.Ordinals(odd(5)...))
	p := page(t, w.get(w.viewer("carol"), threadPath(id)+"?lens=chain:1"))
	got, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "golden", "thread.lens.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the lens page differs from %s:\n%s", path, got)
	}
	l := p.Lens
	if l == nil || l.Kind != "chain" || l.Value != "1" || l.Shown != 3 || l.Hidden != 2 ||
		!slices.Equal(l.Gaps, []dto.LensGap{{After: 1, Count: 1}, {After: 3, Count: 1}}) {
		t.Fatalf("lens %+v", l)
	}
	if !slices.Equal(ordinalsOf(p.Posts), []int{1, 3, 5}) || p.Page != (dto.Pagination{Number: 1, Size: 25, TotalPages: 1}) {
		t.Fatalf("posts %v, page %+v", ordinalsOf(p.Posts), p.Page)
	}
}

// TestLensPaging is the paging rule under a lens (docs/07): page and
// total_pages count the lens's posts, ?ordinal= serves the lens page holding
// the post or else the next one the lens keeps, the landing rule does not
// apply, and past the end is not-found.
func TestLensPaging(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(1, 30)
	w.lensAs(lens.Chain, lens.Ordinals(odd(30)...)) // fifteen posts
	w.write(func(q *dbq.Queries) error {
		return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{
			UserID: w.users["carol"], ThreadID: int64(id), LastReadOrdinal: 24, LastReadAt: 1})
	})
	view := func(who string, q threads.Query) (dto.ThreadPage, error) {
		q.Lens, q.PageSize = "chain:1", 5
		res, err := w.svc.View(t.Context(), w.viewer(who), id, q, "")
		return res.Body, err
	}
	for _, c := range []struct {
		name  string
		q     threads.Query
		page  int
		posts []int
		gaps  []dto.LensGap
	}{
		{"no page, a returning member", threads.Query{}, 1, []int{1, 3, 5, 7, 9},
			gaps(1, 1, 3, 1, 5, 1, 7, 1, 9, 1)},
		{"page 2", threads.Query{Page: 2}, 2, []int{11, 13, 15, 17, 19},
			gaps(9, 1, 11, 1, 13, 1, 15, 1, 17, 1, 19, 1)},
		{"page 3", threads.Query{Page: 3}, 3, []int{21, 23, 25, 27, 29},
			gaps(19, 1, 21, 1, 23, 1, 25, 1, 27, 1, 29, 1)},
		{"a kept ordinal", threads.Query{Ordinal: 13}, 2, []int{11, 13, 15, 17, 19}, nil},
		{"a skipped ordinal", threads.Query{Ordinal: 20}, 3, []int{21, 23, 25, 27, 29}, nil},
		{"the last post, skipped", threads.Query{Ordinal: 30}, 3, []int{21, 23, 25, 27, 29}, nil},
		{"the ordinal wins over the page", threads.Query{Ordinal: 2, Page: 3}, 1, []int{1, 3, 5, 7, 9}, nil},
	} {
		p, err := view("carol", c.q)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if p.Page != (dto.Pagination{Number: c.page, Size: 5, TotalPages: 3}) || !slices.Equal(ordinalsOf(p.Posts), c.posts) {
			t.Errorf("%s: page %+v posts %v, want page %d posts %v", c.name, p.Page, ordinalsOf(p.Posts), c.page, c.posts)
		}
		if p.Lens == nil || p.Lens.Shown != 15 || p.Lens.Hidden != 15 || c.gaps != nil && !slices.Equal(p.Lens.Gaps, c.gaps) {
			t.Errorf("%s: lens %+v", c.name, p.Lens)
		}
		if p.Thread.PostCount != 30 {
			t.Errorf("%s: post_count %d, want the thread's 30", c.name, p.Thread.PostCount)
		}
	}
	for _, q := range []threads.Query{{Page: 4}, {Ordinal: 31}} {
		if _, err := view("carol", q); problem.FromError(err) != problem.CodeNotFound {
			t.Errorf("%+v: %v, want not-found", q, err)
		}
	}

	// A lens that keeps nothing has one empty page, and its one gap is the
	// whole thread.
	w.lensAs(lens.Chain, lens.Ordinals())
	p, err := view("", threads.Query{})
	if err != nil || len(p.Posts) != 0 || p.Page.TotalPages != 1 || p.Lens.Shown != 0 || p.Lens.Hidden != 30 ||
		!slices.Equal(p.Lens.Gaps, gaps(0, 30)) {
		t.Fatalf("the empty lens: %+v, %v", p, err)
	}
	if _, err := view("", threads.Query{Ordinal: 7}); err != nil {
		t.Fatalf("a permalink into the empty lens: %v", err)
	}
}

// TestLensAfterLogLength: after_log_length filters a lens page's posts as it
// filters a canonical page's, and the lens block still counts the whole
// lens.
func TestLensAfterLogLength(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(1, 6)
	w.lensAs(lens.Chain, lens.Ordinals(odd(9)...))
	length := w.post(id, "carol", "seventh", 0)
	w.post(id, "carol", "eighth", 0)
	w.post(id, "carol", "ninth", 0)
	p := page(t, w.get(w.viewer("carol"), fmt.Sprintf("%s?lens=chain:1&after_log_length=%d", threadPath(id), length)))
	if !slices.Equal(ordinalsOf(p.Posts), []int{9}) || p.Lens == nil || p.Lens.Shown != 5 {
		t.Fatalf("after %d: posts %v lens %+v; want only #9, and the whole lens counted", length, ordinalsOf(p.Posts), p.Lens)
	}
}

// accessThread is a thread holding a post in every state the canonical page
// treats differently (TKT-01M29CKRJ9, TKT-01M29CP2PJ, TKT-01M29CXJZ9,
// TKT-01M29CP2QK), built on seedHidden:
//
//	#1 alice's, edited          #6 carol's, hidden pending review
//	#2 scribe's, an agent's     #7 dave's, hidden by his suspension
//	#3 bob's, he deleted it     #8 alice's, hard-redacted
//	#4 carol's, a mention       #9 carol's, moved out of the thread
//	#5 alice's, a moderator hid #10 erin's, a reply to #5 quoting it
//
// dave is suspended. The moved flag is set in the sidecar index, since no
// record moves a post yet (TKT-01M29CXJZZ).
func (w *world) accessThread() threadid.ID {
	w.t.Helper()
	w.addUsers("suspended", "dave")
	w.addUsers("active", "erin")
	id, _ := w.seedHidden()
	bob := uint64(w.users["bob"])
	w.post(id, "carol", "a post three members flagged", 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 6,
		Body: &threadlog.DeleteBody{Reason: "flagged", Auto: true}})
	w.post(id, "dave", "a post a suspension hid", 0)
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: bob, Ordinal: 7,
		Body: &threadlog.DeleteBody{Reason: "spam", ByModerator: true, Suspension: 1}})
	w.post(id, "alice", redactedSecret, 0)
	w.post(id, "carol", "a post split into another thread", 0)
	w.post(id, "erin", "> [quote=#5]\n> "+hiddenSecret+"\n\nre that", 5)
	w.redactOffline(id, 8)
	w.flagOffline(id, 9, threadlog.EntryMoved)
	for o, want := range map[uint32]threadlog.EntryFlags{
		3: threadlog.EntryDeleted, 5: threadlog.EntryHiddenByMod, 6: threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden,
		7: threadlog.EntryHiddenByMod, 8: threadlog.EntryRedacted, 9: threadlog.EntryMoved,
	} {
		if e := w.entry(id, o); !e.Flags.Has(want) {
			w.t.Fatalf("#%d has flags %b, want %b", o, e.Flags, want)
		}
	}
	return id
}

// flagOffline sets flag on ordinal's entry in id's sidecar index, with the
// thread closed, and reopens the world's stores on it. The index header's log
// length still matches the log, so the reopened thread trusts it.
func (w *world) flagOffline(id threadid.ID, ordinal uint32, flag threadlog.EntryFlags) {
	w.t.Helper()
	w.entryOffline(id, ordinal, func(e *threadlog.IndexEntry) { e.Flags |= flag })
}

// entryOffline changes ordinal's entry in id's sidecar index with edit, as
// flagOffline does: with the thread closed, and the stores reopened on it.
func (w *world) entryOffline(id threadid.ID, ordinal uint32, edit func(*threadlog.IndexEntry)) {
	w.t.Helper()
	if err := w.cache.Close(); err != nil {
		w.t.Fatal(err)
	}
	path := id.IndexPath(w.root)
	b, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatal(err)
	}
	off, err := threadlog.EntryFileOffset(ordinal)
	if err != nil {
		w.t.Fatal(err)
	}
	e, err := threadlog.ParseIndexEntry(b[off:])
	if err != nil {
		w.t.Fatal(err)
	}
	edit(&e)
	copy(b[off:], e.AppendTo(nil))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		w.t.Fatal(err)
	}
	c, err := threadcache.New(w.root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { c.Close() })
	w.cache, w.svc.Threads = c, c
	w.svc.Render, w.svc.Responses = render.NewCache(1<<20), respcache.New(1<<20)
}

// forumModerator makes handle a member who moderates forum alone, through a
// group of their own, and returns their viewer.
func (w *world) forumModerator(handle string, forum int64) domain.Viewer {
	w.t.Helper()
	w.addUsers("active", handle)
	var g int64
	w.write(func(q *dbq.Queries) error {
		var err error
		g, err = q.CreateGroup(w.t.Context(), dbq.CreateGroupParams{Name: "Mods " + handle, Slug: "mods-" + handle,
			CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	e := perm.New(w.db)
	if err := e.AddMember(w.t.Context(), audit.By{}, g, w.users[handle]); err != nil {
		w.t.Fatal(err)
	}
	if err := e.Grant(w.t.Context(), audit.By{}, forum, g, domain.PermModerate); err != nil {
		w.t.Fatal(err)
	}
	return w.viewer(handle)
}

// TestLensPostsMatchCanonical is the lens framework's access invariant
// (docs/14 "Invariants, added"): every post a lens page serves is, byte for
// byte, the post the canonical page serves the same viewer at ?ordinal=.
// A lens chooses ordinals and never what a post shows.
//
// Every viewer class reads every post of accessThread under several lenses:
// each post alone, the odd posts, the even posts, and all of them, so each
// post is drawn on pages whose other posts, and so whose viewer class
// (pageClass), differ. Each served kind adds its own rows: the unread lens
// over several boundaries and over each viewer's own position, the author
// lens for every author in the thread and for none, the reply chain of
// every post, since from several moments, mentions over mentions put in
// the projection, and the moderator lens, which every viewer who may not
// moderate the forum is refused with nothing of the thread. Each lens is
// read twice in two orders of viewers, so a page the response cache stored
// for one viewer is offered to every other.
func TestLensPostsMatchCanonical(t *testing.T) {
	w := newWorld(t)
	id := w.accessThread()
	elsewhere := w.forumAt("elsewhere", domain.VisibilityPublic, false)
	viewers := []struct {
		name string
		v    domain.Viewer
		// sees is what this viewer must read of the hidden post #5: its
		// body or a tombstone. It proves the matrix reaches both.
		sees bool
		// read is the viewer's read position, which a bare unread lens
		// starts after; -1 is none. A viewer who is not a member has no
		// position to read from, whatever its row says.
		read int64
	}{
		{"anonymous", domain.Anonymous(), false, -1},
		{"member", w.viewer("erin"), false, -1},
		{"author of a post a moderator hid", w.viewer("alice"), true, 4},
		{"author of a post hidden pending review", w.viewer("carol"), false, 6},
		{"moderator by role", w.viewer("bob"), true, 7},
		{"moderator by group grant", w.groupModerator(w.forum), true, 2},
		{"moderator of another forum", w.forumModerator("hank", elsewhere), false, 5},
		{"admin", w.newAdmin(), true, 10},
		{"suspended, the author of a post his suspension hid", w.session("dave"), false, 1},
	}
	const n = 10
	for _, vw := range viewers {
		if vw.read >= 0 {
			w.readTo(vw.v.UserID, id, vw.read)
		}
	}
	canonical := make([]map[int]string, len(viewers))
	for i, vw := range viewers {
		canonical[i] = map[int]string{}
		for o := 1; o <= n; o++ {
			for _, p := range page(t, w.get(vw.v, fmt.Sprintf("%s?ordinal=%d", threadPath(id), o))).Posts {
				b, err := json.Marshal(p)
				if err != nil {
					t.Fatal(err)
				}
				canonical[i][p.Ordinal] = string(b)
			}
		}
		if len(canonical[i]) != n {
			t.Fatalf("%s: the canonical pages hold %d posts, want %d", vw.name, len(canonical[i]), n)
		}
		if got := strings.Contains(canonical[i][5], "only its author"); got != vw.sees {
			t.Fatalf("%s reads #5's body=%v on the canonical page, want %v", vw.name, got, vw.sees)
		}
		for _, o := range []int{8, 9} {
			var p dto.Post
			if err := json.Unmarshal([]byte(canonical[i][o]), &p); err != nil || p.HTML != "" {
				t.Fatalf("%s reads #%d with html %q: a redacted or moved post has none", vw.name, o, p.HTML)
			}
		}
	}

	// A case is a served lens, raw, whose posts for viewer i are want(i), or
	// else the test filter keeping keep, served as chain.
	type lensCase struct {
		name string
		keep []uint32
		raw  string
		want func(i int) []int
	}
	lenses := []lensCase{{name: "odd", keep: odd(n)}, {name: "even", keep: even(n)},
		{name: "all", keep: append(even(n), odd(n)...)}}
	for o := uint32(1); o <= n; o++ {
		lenses = append(lenses, lensCase{name: fmt.Sprintf("#%d alone", o), keep: []uint32{o}})
	}
	// The unread lens (TKT-01M29CTY4J): ranges that start before, on and
	// after the removed posts, an empty one, and each viewer's own position.
	for _, b := range []int{0, 2, 4, 6, 9, 10} {
		lenses = append(lenses, lensCase{name: fmt.Sprintf("unread:%d", b), raw: fmt.Sprintf("unread:%d", b),
			want: func(int) []int { return span(b+1, n) }})
	}
	lenses = append(lenses, lensCase{name: "bare unread", raw: "unread", want: func(i int) []int {
		if !viewers[i].v.Member() {
			return []int{}
		}
		return span(int(max(viewers[i].read, 0))+1, n)
	}})
	// The author lens (TKT-01M29CTY58): each author's posts shown as
	// themselves, which is the same for every viewer. dave's one post his
	// suspension hid, and bob's he deleted, leave them empty, as are a
	// member with no posts here and a handle nobody holds.
	for raw, want := range map[string][]int{"author:alice": {1}, "author:ALICE": {1}, "author:scribe": {2},
		"author:bob": {}, "author:carol": {4}, "author:dave": {}, "author:erin": {10}, "author:gina": {},
		"author:nobody": {}} {
		lenses = append(lenses, lensCase{name: raw, raw: raw, want: func(int) []int { return want }})
	}
	// The reply chain lens (TKT-01M29CTY5T) of every post: #2 and #4 reply
	// to #1, and #10 to #5, which a moderator hid, so chain:10 runs through
	// a removed post. The chain is the same for every viewer.
	for n, want := range map[int][]int{1: {1, 2, 4}, 2: {1, 2}, 3: {3}, 4: {1, 4}, 5: {5, 10}, 6: {6}, 7: {7},
		8: {8}, 9: {9}, 10: {5, 10}} {
		raw := fmt.Sprintf("chain:%d", n)
		lenses = append(lenses, lensCase{name: raw, raw: raw, want: func(int) []int { return want }})
	}
	// The since lens (TKT-01M29CTY6E): from the first post, from a removed
	// one, from the last, and from after it. The same for every viewer.
	for _, from := range []uint32{1, 3, 7, 10} {
		raw := fmt.Sprintf("since:%d", w.entry(id, from).CreatedMS)
		lenses = append(lenses, lensCase{name: raw, raw: raw, want: func(int) []int { return span(int(from), n) }})
	}
	lenses = append(lenses, lensCase{name: "since after the last post",
		raw: fmt.Sprintf("since:%d", w.entry(id, n).CreatedMS+1), want: func(int) []int { return []int{} }})
	// The mentions lens (TKT-01M29CTY6E). accessThread's posts were appended
	// without mentions, so these rows put some in the projection: erin at
	// #4, bob at #6 (hidden pending review), gina at #2 and at #8
	// (redacted). alice's posts #1 and #5 have the replies #2, #4 and #10.
	for _, m := range [][2]int64{{4, w.users["erin"]}, {6, w.users["bob"]}, {2, w.users["gina"]}, {8, w.users["gina"]}} {
		w.exec(`INSERT INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (?, ?, ?)`, int64(id), m[0], m[1])
	}
	lenses = append(lenses, lensCase{name: "mentions", raw: "mentions", want: func(i int) []int {
		if !viewers[i].v.Member() {
			return []int{}
		}
		switch viewers[i].v.UserID {
		case w.users["erin"]:
			return []int{4}
		case w.users["alice"]:
			return []int{2, 4, 10}
		case w.users["gina"]:
			return []int{2}
		}
		return []int{}
	}})
	// The moderator lens (TKT-01M29CTY6E): the removed posts, to a viewer
	// who may moderate this forum, and refused, nil here, to anyone else.
	lenses = append(lenses, lensCase{name: "mod", raw: "mod", want: func(i int) []int {
		if !viewers[i].sees || viewers[i].v.UserID == w.users["alice"] {
			return nil
		}
		return []int{3, 5, 6, 7, 8, 9}
	}})
	order := make([]int, len(viewers))
	for i := range order {
		order[i] = i
	}
	for round := range 2 {
		for li, lc := range lenses {
			// A served lens is read through its resolver, which the test
			// filter would otherwise shadow for the chain kind.
			w.svc.TestLenses = nil
			path := threadPath(id) + "?lens=" + lc.raw
			if lc.raw == "" {
				w.lensAs(lens.Chain, lens.Ordinals(lc.keep...))
				path = fmt.Sprintf("%s?lens=chain:%d", threadPath(id), li+1)
			}
			for _, i := range order {
				vw := viewers[i]
				rec := w.get(vw.v, path)
				var want []int
				if lc.want != nil {
					if want = lc.want(i); want == nil {
						// Refused: an error, and nothing of the thread.
						if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "only its author") {
							t.Fatalf("round %d, %s, %s: %d %s, want a refusal", round, lc.name, vw.name, rec.Code, rec.Body)
						}
						continue
					}
				} else {
					want = []int{}
					for _, o := range slices.Sorted(slices.Values(lc.keep)) {
						want = append(want, int(o))
					}
				}
				p := page(t, rec)
				if got := ordinalsOf(p.Posts); !slices.Equal(got, want) {
					t.Fatalf("round %d, %s, %s: the lens served %v, want %v", round, lc.name, vw.name, got, want)
				}
				for _, post := range p.Posts {
					b, err := json.Marshal(post)
					if err != nil {
						t.Fatal(err)
					}
					if string(b) != canonical[i][post.Ordinal] {
						t.Errorf("round %d, lens %s, %s: #%d differs from the canonical page's\nlens      %s\ncanonical %s",
							round, lc.name, vw.name, post.Ordinal, b, canonical[i][post.Ordinal])
					}
				}
			}
		}
		slices.Reverse(order)
	}
	if s := w.svc.Responses.Stats(); s.Hits == 0 {
		t.Fatalf("stats %+v: the second round should have been served from the cache", s)
	}
}

// TestLensResponseCacheKeysSelection: two lenses, a lens and the canonical
// page, and the same lens string over two selections each land on page 1 of
// the same thread at the same length, and none of them shares an entry with
// another. alice wrote every post, so the authors' digest is the same for
// every selection and only the lens and the ordinals keep them apart. A
// moderator's lens page never answers a member. Every page equals a draw
// from a service with no cache.
func TestLensResponseCacheKeysSelection(t *testing.T) {
	w := newWorld(t)
	id := w.longThread(2, 6)
	base := threadPath(id)
	fresh := uncached(w)
	check := func(who, path string, kind lens.Kind, f lens.Filter, want []int) string {
		t.Helper()
		w.lensAs(kind, f)
		fresh.svc.TestLenses = w.svc.TestLenses
		got, draw := w.get(w.viewer(who), path), fresh.get(w.viewer(who), path)
		if got.Body.String() != draw.Body.String() {
			t.Fatalf("%q %s: the cached handler served\n%s\na fresh draw is\n%s", who, path, got.Body, draw.Body)
		}
		if p := page(t, got); !slices.Equal(ordinalsOf(p.Posts), want) {
			t.Fatalf("%q %s: posts %v, want %v", who, path, ordinalsOf(p.Posts), want)
		}
		return got.Body.String()
	}
	all := lens.Ordinals(1, 2, 3, 4, 5, 6)
	steps := []struct {
		path string
		kind lens.Kind
		f    lens.Filter
		want []int
	}{
		{base, lens.Chain, nil, []int{1, 2, 3, 4, 5, 6}},
		{base + "?lens=chain:1", lens.Chain, all, []int{1, 2, 3, 4, 5, 6}},
		{base + "?lens=chain:1", lens.Chain, lens.Ordinals(odd(6)...), []int{1, 3, 5}},
		{base + "?lens=chain:1", lens.Chain, lens.Ordinals(even(6)...), []int{2, 4, 6}},
		{base + "?lens=since:0", lens.Since, lens.Ordinals(odd(6)...), []int{1, 3, 5}},
	}
	for round := range 2 {
		before := w.svc.Responses.Stats()
		for _, s := range steps {
			check("carol", s.path, s.kind, s.f, s.want)
		}
		after := w.svc.Responses.Stats()
		switch round {
		case 0:
			if after.Inserts-before.Inserts != uint64(len(steps)) || after.Hits != before.Hits {
				t.Fatalf("round 0: %d inserts and %d hits for %d different pages: two shared an entry",
					after.Inserts-before.Inserts, after.Hits-before.Hits, len(steps))
			}
		case 1:
			if after.Inserts != before.Inserts {
				t.Fatalf("round 1 inserted %d entries: a page's key moved between rounds", after.Inserts-before.Inserts)
			}
		}
	}

	// On seedHidden's thread, bob's lens page carries the bodies of #3 and
	// #5; carol, alice and an anonymous viewer reading the same lens next get
	// their own.
	hidden, _ := w.seedHidden()
	base = threadPath(hidden)
	modOnly := lens.Ordinals(3, 5)
	if body := check("bob", base+"?lens=chain:9", lens.Chain, modOnly, []int{3, 5}); !strings.Contains(body, secret) ||
		!strings.Contains(body, hiddenSecret) {
		t.Fatalf("the moderator's lens page lacks the removed bodies: %s", body)
	}
	for who, sees := range map[string]bool{"carol": false, "": false, "alice": true} {
		body := check(who, base+"?lens=chain:9", lens.Chain, modOnly, []int{3, 5})
		if strings.Contains(body, secret) || strings.Contains(body, hiddenSecret) != sees {
			t.Fatalf("%q read the moderator's lens page: %s", who, body)
		}
	}
}

// TestLensPageStatements: a lens page runs exactly the statements the
// canonical page runs, on a thread of 10 posts and one of 2000, for an
// anonymous viewer and a member. The selection is made from the in-memory
// index, so a lens costs no query and none per post.
func TestLensPageStatements(t *testing.T) {
	st := &statements{}
	w := newWorldTraced(t, threadlog.SyncNever, st.trace)
	w.lensAs(lens.Chain, lens.Ordinals(odd(2000)...))
	counts := map[string][]int{}
	for seq, n := range map[uint64]int{1: 10, 2: 2000} {
		id := w.quickThread(seq, n)
		canonical, lensed := threadPath(id)+"?page=1", threadPath(id)+"?lens=chain:1&page=1"
		for _, who := range []string{"", "carol"} {
			v := w.viewer(who)
			// Warm the permission cache and the pool, so neither request
			// pays for the other's first time.
			w.get(v, canonical)
			w.get(v, lensed)
			var c, l *httptest.ResponseRecorder
			ranC := st.during(func() { c = w.get(v, canonical) })
			ranL := st.during(func() { l = w.get(v, lensed) })
			if c.Code != http.StatusOK || l.Code != http.StatusOK {
				t.Fatalf("%d posts, %q: %d and %d", n, who, c.Code, l.Code)
			}
			if !slices.Equal(ranC, ranL) {
				t.Errorf("%d posts, %q: the canonical page ran %d statements, the lens page %d\ncanonical %q\nlens      %q",
					n, who, len(ranC), len(ranL), ranC, ranL)
			}
			if p := page(t, l); p.Lens == nil || p.Lens.Shown != (n+1)/2 {
				t.Fatalf("%d posts: lens %+v", n, p.Lens)
			}
			counts[who] = append(counts[who], len(ranL))
		}
	}
	for who, c := range counts {
		if len(c) != 2 || c[0] != c[1] {
			t.Errorf("%q: the lens page ran %v statements on the two threads; the length changed the count", who, c)
		}
	}
}

// quickThread is a thread of n posts by alice, appended in one go and
// projected once.
func (w *world) quickThread(seq uint64, n int) threadid.ID {
	w.t.Helper()
	id, _ := w.thread(seq, "A long one")
	var length int64
	if err := w.cache.With(id, func(th *threadcache.Thread) error {
		for i := range n {
			w.clock += 1000
			res, err := th.Actor.Append(threadlog.Append{Type: threadlog.TypePost, Author: uint64(w.users["alice"]),
				TimeMS: w.clock, Body: &threadlog.PostBody{Source: fmt.Sprintf("post %d", i+1)}})
			if err != nil {
				return err
			}
			length = res.LogLength
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.proj.ProjectThread(w.t.Context(), id, length); err != nil {
		w.t.Fatal(err)
	}
	return id
}
