package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

func listPath(slug, cursor, sort string) string {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if sort != "" {
		q.Set("sort", sort)
	}
	p := "/api/v1/forums/" + slug + "/threads"
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p
}

// threadList GETs a page and decodes it strictly.
func (w *forumWorld) threadList(handle, slug, cursor, sort string) dto.ThreadList {
	w.t.Helper()
	rec := w.get(w.viewer(handle), listPath(slug, cursor, sort))
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET %s as %q: %d %s", listPath(slug, cursor, sort), handle, rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		w.t.Errorf("Cache-Control %q", cc)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	var l dto.ThreadList
	if err := dec.Decode(&l); err != nil {
		w.t.Fatalf("decoding strictly into dto.ThreadList: %v\n%s", err, rec.Body)
	}
	return l
}

func (w *forumWorld) pin(id threadid.ID, scope string, until int64) {
	w.t.Helper()
	w.append(id, threadlog.Append{Type: threadlog.TypeThreadPinned, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadPinnedBody{Scope: scope, Until: until}})
}

func rowIDs(l dto.ThreadList) []string {
	var out []string
	for _, r := range l.Threads {
		out = append(out, r.ID)
	}
	return out
}

func strs(ids ...threadid.ID) []string {
	var out []string
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// TestThreadListPins is AC1: the first page opens with global pins from the
// forums the viewer may read, then the forum's own pins, and a pin whose
// pinned_until has passed sorts with the rest and is shown unpinned.
func TestThreadListPins(t *testing.T) {
	w := newForumWorld(t)
	plain := w.threadIn("general", 1, "Plain")
	w.post(plain, "alice", "p", 0)
	forumPin := w.threadIn("general", 2, "Forum pin")
	w.post(forumPin, "alice", "p", 0)
	w.pin(forumPin, "forum", 0)
	expiring := w.threadIn("general", 3, "Expiring pin")
	w.post(expiring, "alice", "p", 0)
	// Far enough ahead to outlast the appends below, which each move the
	// clock on.
	until := w.clock + 100_000_000
	w.pin(expiring, "forum", until)
	global := w.threadIn("zeta", 4, "Global from zeta")
	w.post(global, "alice", "p", 0)
	w.pin(global, "global", 0)
	hidden := w.threadIn("mods", 5, "Global from mods")
	w.post(hidden, "bob", "p", 0)
	w.pin(hidden, "global", 0)
	elsewhere := w.threadIn("g-b", 6, "Forum pin in g-b")
	w.post(elsewhere, "alice", "p", 0)
	w.pin(elsewhere, "forum", 0)
	w.post(plain, "carol", "the newest post in general", 0)

	for _, c := range []struct {
		viewer string
		want   []string
	}{
		{"alice", strs(global, expiring, forumPin, plain)},
		{"", strs(global, expiring, forumPin, plain)},
		{"carol", strs(hidden, global, expiring, forumPin, plain)},
	} {
		l := w.threadList(c.viewer, "general", "", "")
		if got := rowIDs(l); !slices.Equal(got, c.want) {
			t.Errorf("viewer %q: %v, want %v", c.viewer, got, c.want)
		}
		for _, r := range l.Threads {
			if pinned := r.ID != plain.String(); (r.Pinned != nil) != pinned {
				t.Errorf("viewer %q: %s pinned %+v", c.viewer, r.Title, r.Pinned)
			}
		}
	}
	l := w.threadList("alice", "general", "", "")
	if p := l.Threads[1].Pinned; p == nil || p.Scope != "forum" || p.Until == nil || int64(*p.Until) != until {
		t.Errorf("the expiring pin: %+v", p)
	}
	if p := l.Threads[0].Pinned; p == nil || p.Scope != "global" || p.Until != nil {
		t.Errorf("the global pin: %+v", p)
	}

	// At pinned_until the pin is over: the thread sorts by its last post,
	// below plain's newer one, and is shown unpinned.
	w.clock = until
	l = w.threadList("alice", "general", "", "")
	if got, want := rowIDs(l), strs(global, forumPin, plain, expiring); !slices.Equal(got, want) {
		t.Errorf("after the expiry: %v, want %v", got, want)
	}
	if l.Threads[3].Pinned != nil {
		t.Errorf("an expired pin is shown: %+v", l.Threads[3].Pinned)
	}
}

// TestThreadListPaging is AC2 through the handler: 25 a page, pins not
// counted, a next_cursor until the end, and a thread bumped between pages
// moves up rather than appearing twice.
func TestThreadListPaging(t *testing.T) {
	w := newForumWorld(t)
	var all []threadid.ID
	for i := range 31 {
		id := w.threadIn("general", uint64(100+i), "Thread")
		w.post(id, "alice", "p", 0)
		all = append(all, id)
	}
	w.pin(all[0], "forum", 0)

	for _, sort := range []string{"", "created"} {
		page1 := w.threadList("alice", "general", "", sort)
		if len(page1.Threads) != 26 || page1.NextCursor == "" {
			t.Fatalf("sort %q: page 1 has %d rows (want a pin and 25), cursor %q", sort, len(page1.Threads), page1.NextCursor)
		}
		page2 := w.threadList("alice", "general", page1.NextCursor, sort)
		if len(page2.Threads) != 5 || page2.NextCursor != "" {
			t.Fatalf("sort %q: page 2 has %d rows, cursor %q", sort, len(page2.Threads), page2.NextCursor)
		}
		seen := map[string]bool{}
		for _, id := range append(rowIDs(page1), rowIDs(page2)...) {
			if seen[id] {
				t.Errorf("sort %q: %s twice", sort, id)
			}
			seen[id] = true
		}
		if len(seen) != 31 {
			t.Errorf("sort %q: %d threads seen, want 31", sort, len(seen))
		}
	}

	// Exactly a page: no cursor, because there is nothing after it.
	w2 := newForumWorld(t)
	for i := range threads.ListPageSize {
		id := w2.threadIn("zeta", uint64(200+i), "Thread")
		w2.post(id, "alice", "p", 0)
	}
	if l := w2.threadList("alice", "zeta", "", ""); len(l.Threads) != threads.ListPageSize || l.NextCursor != "" {
		t.Errorf("a full page and no more: %d rows, cursor %q", len(l.Threads), l.NextCursor)
	}

	// Bump the thread at the top of page 2 after page 1 is read: it moves
	// above the cursor, and page 2 is the other four.
	page1 := w.threadList("alice", "general", "", "")
	page2 := w.threadList("alice", "general", page1.NextCursor, "")
	bumped := page2.Threads[0].ID
	id, _ := threadid.Parse(bumped)
	w.post(id, "carol", "bump", 0)
	after := w.threadList("alice", "general", page1.NextCursor, "")
	if got, want := rowIDs(after), rowIDs(page2)[1:]; !slices.Equal(got, want) {
		t.Errorf("after a bump: %v, want %v", got, want)
	}
	if top := w.threadList("alice", "general", "", ""); top.Threads[1].ID != bumped {
		t.Errorf("the bumped thread is not first after the pin: %v", rowIDs(top))
	}
}

// TestThreadListRow: a row's fields, and the unread badge per viewer (AC3):
// a position behind, no position at all, and an anonymous viewer.
func TestThreadListRow(t *testing.T) {
	w := newForumWorld(t)
	id := w.threadIn("general", 1, "Tagged thread")
	w.post(id, "alice", "one", 0)
	w.post(id, "bob", "two", 0)
	w.post(id, "carol", "three", 0)
	w.write(func(q *dbq.Queries) error {
		if err := q.InsertThreadTag(t.Context(), dbq.InsertThreadTagParams{ThreadID: int64(id), Tag: "meta"}); err != nil {
			return err
		}
		return q.UpsertReadPosition(t.Context(), dbq.UpsertReadPositionParams{UserID: w.users["alice"],
			ThreadID: int64(id), LastReadOrdinal: 1, LastReadAt: 1})
	})
	carol := dto.Author{Handle: "carol", DisplayName: "Carol Ruiz", Role: domain.RoleMember, Kind: "human"}
	alice := dto.Author{Handle: "alice", DisplayName: "Alice Nakamura", Role: domain.RoleMember, Kind: "human"}
	base := dto.ThreadRow{ID: id.String(), Slug: "tagged-thread", Title: "Tagged thread", Tags: []string{"meta"},
		Author: alice, ReplyCount: 2, LastPostAuthor: carol, LastPostAt: dto.Millis(w.clock)}
	for _, c := range []struct {
		viewer           string
		unread, lastRead int
	}{
		{"alice", 2, 1},
		{"bob", 3, 0}, // no read position: every post
		{"", 0, 0},
	} {
		l := w.threadList(c.viewer, "general", "", "")
		want := base
		want.Unread, want.LastReadOrdinal = c.unread, c.lastRead
		if len(l.Threads) != 1 || !rowEqual(l.Threads[0], want) {
			t.Errorf("viewer %q: %+v\nwant %+v", c.viewer, l.Threads, want)
		}
		if l.Forum == nil || *l.Forum != (dto.ForumRef{Slug: "general", Name: "General"}) {
			t.Errorf("viewer %q: forum %+v", c.viewer, l.Forum)
		}
	}
	// A thread with no tags has an empty array, not null.
	bare := w.threadIn("general", 2, "Bare")
	w.post(bare, "alice", "p", 0)
	if rec := w.get(w.viewer("alice"), listPath("general", "", "")); !strings.Contains(rec.Body.String(), `"tags":[]`) {
		t.Errorf("a thread with no tags: %s", rec.Body)
	}
	// A forum with no threads: an empty array.
	if l := w.threadList("alice", "zeta", "", ""); l.Threads == nil || len(l.Threads) != 0 || l.NextCursor != "" {
		t.Errorf("empty forum: %+v", l)
	}
}

func rowEqual(a, b dto.ThreadRow) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

// TestThreadListAccess: the list refuses exactly as a thread page in the
// same forum does, for every viewer, so a list never shows what the page
// would not, nor names a forum the page would call missing.
func TestThreadListAccess(t *testing.T) {
	w := newForumWorld(t)
	for _, slug := range []string{"general", "g-secret", "g-a", "alpha", "mods", "mods-public"} {
		id := w.threadIn(slug, uint64(len(slug)), "In "+slug)
		w.post(id, "bob", "p", 0)
		for _, viewer := range []string{"", "alice", "carol"} {
			page := w.get(w.viewer(viewer), "/api/v1/threads/"+id.String())
			list := w.get(w.viewer(viewer), listPath(slug, "", ""))
			if page.Code != list.Code || (page.Code != http.StatusOK && problemCode(t, page) != problemCode(t, list)) {
				t.Errorf("%s as %q: the list answers %d %s, the page %d %s", slug, viewer,
					list.Code, list.Body, page.Code, page.Body)
			}
		}
	}
	// A suspended member is served as anonymous: their group's grant on a
	// restricted forum no longer admits them.
	suspended := w.viewer("carol")
	suspended.Active = false
	if rec := w.get(suspended, listPath("mods", "", "")); problemCode(t, rec) != "not-found" {
		t.Errorf("a suspended member of crew: %d %s", rec.Code, rec.Body)
	}
	if rec := w.get(w.viewer("alice"), listPath("no-such-forum", "", "")); problemCode(t, rec) != "not-found" {
		t.Errorf("an unknown forum: %d %s", rec.Code, rec.Body)
	}
}

// TestThreadListBadRequests: a cursor that is malformed, or was made for
// another forum or the other sort, and an unknown sort, are the client's
// mistake (review on PR #84: a cursor from one forum must not be replayed
// against another).
func TestThreadListBadRequests(t *testing.T) {
	w := newForumWorld(t)
	for i := range 27 {
		for j, forum := range []string{"general", "zeta"} {
			id := w.threadIn(forum, uint64(1000*j+i+1), "Thread")
			w.post(id, "alice", "p", 0)
		}
	}
	good := w.threadList("alice", "general", "", "").NextCursor
	created := w.threadList("alice", "general", "", "created").NextCursor
	zeta := w.threadList("alice", "zeta", "", "").NextCursor
	g := strconv.FormatInt(w.ids["general"], 10)
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, c := range map[string]struct{ cursor, sort string }{
		"not base64":                  {"!!!", ""},
		"padded":                      {good + "=", ""},
		"four fields":                 {enc("a|" + g + "|1757500000000|1"), ""},
		"a letter for a number":       {enc("a|" + g + "|x|1|"), ""},
		"a sign":                      {enc("a|" + g + "|+1757500000000|1|"), ""},
		"a zero id":                   {enc("a|" + g + "|1757500000000|0|"), ""},
		"a bad pin list":              {enc("a|" + g + "|1757500000000|1|2,,3"), ""},
		"a zero pin":                  {enc("a|" + g + "|1757500000000|1|0"), ""},
		"too many pins":               {enc("a|" + g + "|1757500000000|1|" + strings.Repeat("7,", views.MaxPinned) + "7"), ""},
		"another forum's cursor":      {zeta, ""},
		"the other sort's cursor":     {created, ""},
		"this sort's cursor, crossed": {good, "created"},
		"an unknown sort":             {"", "newest"},
	} {
		rec := w.get(w.viewer("alice"), listPath("general", c.cursor, c.sort))
		if code := problemCode(t, rec); rec.Code != http.StatusBadRequest || code != "bad-request" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	// A cursor written by hand in the right form is a position, not a
	// credential: it is honoured.
	if rec := w.get(w.viewer("alice"), listPath("general", enc("a|"+g+"|1757500000000|1|"), "")); rec.Code != http.StatusOK {
		t.Errorf("a well-formed cursor: %d %s", rec.Code, rec.Body)
	}
}

// TestThreadListPinsChangeBetweenPages: the first page's pins are fixed by
// its cursor. A pin that expires, or is removed, before the next page is not
// shown again lower down; a thread pinned after the first page stays where
// its activity puts it (review on PR #84).
func TestThreadListPinsChangeBetweenPages(t *testing.T) {
	w := newForumWorld(t)
	var all []threadid.ID
	for i := range 40 {
		id := w.threadIn("general", uint64(i+1), "Thread")
		w.post(id, "alice", "p", 0)
		all = append(all, id)
	}
	// The oldest threads, which sort onto page 2 once unpinned.
	w.pin(all[0], "forum", w.clock+100_000_000)
	w.pin(all[1], "forum", 0)
	page1 := w.threadList("alice", "general", "", "")
	if got := rowIDs(page1)[:2]; !slices.Equal(got, strs(all[1], all[0])) && !slices.Equal(got, strs(all[0], all[1])) {
		t.Fatalf("page 1 does not open with the pins: %v", rowIDs(page1))
	}
	// Between pages: one pin expires, one is removed, and a thread on
	// page 2 is pinned.
	w.clock += 200_000_000
	w.append(all[1], threadlog.Append{Type: threadlog.TypeThreadUnpinned, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadUnpinnedBody{Scope: "forum"}})
	w.pin(all[2], "forum", 0)

	seen := map[string]bool{}
	for _, id := range rowIDs(page1) {
		seen[id] = true
	}
	for c := page1.NextCursor; c != ""; {
		page := w.threadList("alice", "general", c, "")
		for _, id := range rowIDs(page) {
			if seen[id] {
				t.Errorf("%s again on a later page", id)
			}
			seen[id] = true
		}
		c = page.NextCursor
	}
	if !seen[all[2].String()] {
		t.Errorf("the thread pinned between pages was skipped")
	}
}

// TestThreadListOpensNoLog is AC4's behavioural half: a service with no
// thread cache at all answers the list, pins included, so nothing in it
// reaches for a thread log.
func TestThreadListOpensNoLog(t *testing.T) {
	w := newForumWorld(t)
	id := w.threadIn("general", 1, "Thread")
	w.post(id, "alice", "p", 0)
	w.pin(id, "forum", 0)
	svc := &threads.Service{DB: w.db, Perm: perm.New(w.db)} // no Threads, no Render
	c := *w.world
	c.handler = New(Deps{Threads: svc, Users: &users.Service{DB: w.db, Perm: perm.New(w.db)},
		Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}, Perm: perm.New(w.db)})
	f := &forumWorld{world: &c, ids: w.ids}
	if l := f.threadList("alice", "general", "", ""); len(l.Threads) != 1 || l.Threads[0].Pinned == nil {
		t.Errorf("%+v", l)
	}
}

// TestThreadListPinCap: a forum with more pins than views.MaxPinned shows
// that many at the top of its first page, and the rest where their activity
// puts them, each thread once across every page; no cursor outgrows the cap
// (review on PR #84).
func TestThreadListPinCap(t *testing.T) {
	w := newForumWorld(t)
	for i := range views.MaxPinned + 10 {
		id := w.threadIn("general", uint64(i+1), "Pinned")
		w.post(id, "alice", "p", 0)
		w.pin(id, "forum", 0)
	}
	page1 := w.threadList("alice", "general", "", "")
	pins := 0
	for _, r := range page1.Threads {
		if r.Pinned != nil {
			pins++
		}
	}
	if pins != views.MaxPinned {
		t.Errorf("page 1 shows %d pins, want %d", pins, views.MaxPinned)
	}
	seen := map[string]bool{}
	total := 0
	for l := page1; ; {
		for _, id := range rowIDs(l) {
			if seen[id] {
				t.Errorf("%s twice", id)
			}
			seen[id] = true
			total++
		}
		if l.NextCursor == "" {
			break
		}
		l = w.threadList("alice", "general", l.NextCursor, "")
	}
	if total != views.MaxPinned+10 {
		t.Errorf("%d threads across the pages, want %d", total, views.MaxPinned+10)
	}
}

// TestThreadListPinCapAfterAccess: pins the viewer may not see do not count
// toward the cap. More global pins than views.MaxPinned in a forum hidden
// from alice, all newer than the pins she may see, leave her pin block as it
// would be without them (review 3 on PR #84).
func TestThreadListPinCapAfterAccess(t *testing.T) {
	w := newForumWorld(t)
	visibleGlobal := w.threadIn("zeta", 1, "Visible global")
	w.post(visibleGlobal, "alice", "p", 0)
	w.pin(visibleGlobal, "global", 0)
	own := w.threadIn("general", 2, "Own pin")
	w.post(own, "alice", "p", 0)
	w.pin(own, "forum", 0)
	for i := range views.MaxPinned + 5 {
		id := w.threadIn("mods", uint64(100+i), "Hidden global")
		w.post(id, "bob", "p", 0)
		w.pin(id, "global", 0)
	}
	l := w.threadList("alice", "general", "", "")
	if got, want := rowIDs(l), strs(visibleGlobal, own); !slices.Equal(got, want) {
		t.Fatalf("alice's page: %v, want %v", got, want)
	}
	for _, r := range l.Threads {
		if r.Pinned == nil {
			t.Errorf("%s is not shown as pinned", r.Title)
		}
	}
	// carol may read mods: the cap is hers to hit, and her own forum's pin
	// is the one that falls to the rows, unbadged.
	l = w.threadList("carol", "general", "", "")
	pins := 0
	for _, r := range l.Threads {
		if r.Pinned != nil {
			pins++
		}
	}
	if pins != views.MaxPinned {
		t.Errorf("carol sees %d pins, want %d", pins, views.MaxPinned)
	}
}
