package api

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
	"github.com/terva-sh/ketju/internal/web"
)

// Tests for the cross-forum lists and mark all read (TKT-01M29CTY7K).

var crossLists = []string{"unread", "new", "participated", "watching"}

func crossPath(list, cursor string) string {
	p := "/api/v1/" + list
	if cursor != "" {
		p += "?cursor=" + cursor
	}
	return p
}

// crossPage GETs one page of list as who and decodes it strictly.
func (w *world) crossPage(who, list, cursor string) dto.ThreadList {
	w.t.Helper()
	rec := w.get(w.viewer(who), crossPath(list, cursor))
	l := ok[dto.ThreadList](w.t, rec)
	if l.Forum != nil || l.Watch != "" {
		w.t.Errorf("%s as %s: forum %+v, watch %q on a cross-forum list", list, who, l.Forum, l.Watch)
	}
	for _, r := range l.Threads {
		if r.Forum == nil || r.Forum.Slug == "" {
			w.t.Errorf("%s as %s: row %s names no forum", list, who, r.ID)
		}
		if r.Pinned != nil {
			w.t.Errorf("%s as %s: row %s is shown pinned", list, who, r.ID)
		}
	}
	return l
}

// crossAll is every row of list for who, page by page.
func (w *world) crossAll(who, list string) []dto.ThreadRow {
	w.t.Helper()
	var out []dto.ThreadRow
	cursor := ""
	for range 100 {
		l := w.crossPage(who, list, cursor)
		out = append(out, l.Threads...)
		if cursor = l.NextCursor; cursor == "" {
			return out
		}
	}
	w.t.Fatalf("%s as %s did not end", list, who)
	return nil
}

// crossIDs is the ids of every row of list for who, sorted.
func (w *world) crossIDs(who, list string) []string {
	w.t.Helper()
	var out []string
	for _, r := range w.crossAll(who, list) {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

func sortedIDs(ids ...threadid.ID) []string {
	out := strs(ids...)
	sort.Strings(out)
	return out
}

// meOf is GET /me for who, through the real session middleware, which /me
// needs to name the account.
func (w *world) meOf(who string) dto.Me {
	w.t.Helper()
	now := time.UnixMilli(w.clock)
	store := &session.Store{DB: w.db, Now: func() time.Time { return now }}
	pass := func(h http.Handler) http.Handler { return h }
	h := web.New(web.Groups{API: w.handler}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(sessiontest.Cookie(sessiontest.SignIn(w.t, w.db, w.users[who], now)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	me := decodeAs[dto.Me](w.t, rec)
	if me.User == nil || me.User.Handle != who {
		w.t.Fatalf("/me as %s answered %+v", who, me.User)
	}
	return me
}

// sql runs statements against w's database directly.
func (w *world) sql(stmts ...string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(w.t.Context(), s); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		w.t.Fatal(err)
	}
}

func i64(n int64) string { return strconv.FormatInt(n, 10) }

// readAs sets who's read position in id, as a beacon would.
func (w *world) readAs(who string, id threadid.ID, ordinal int) dto.ReadPosition {
	w.t.Helper()
	return ok[dto.ReadPosition](w.t, send(w.chain(), who, http.MethodPut, "/api/v1/threads/"+id.String()+"/read",
		`{"ordinal":`+strconv.Itoa(ordinal)+`}`))
}

// watchAs sets who's level on thread id.
func (w *world) watchAs(who string, id threadid.ID, level string) {
	w.t.Helper()
	ok[dto.ThreadWatch](w.t, send(w.chain(), who, http.MethodPost, watchPath(id), levelBody(level)))
}

// withQuarantine rebuilds w's handler with ids quarantined, for the thread
// endpoints and for /me, as serve wires both from the projection writer.
func (w *world) withQuarantine(ids ...threadid.ID) {
	w.t.Helper()
	held := func(id threadid.ID) bool { return slices.Contains(ids, id) }
	w.svc.Quarantined = held
	w.handler = New(Deps{Threads: w.svc,
		Users:  &users.Service{DB: w.db, Perm: perm.New(w.db), Quarantined: func() []threadid.ID { return ids }},
		Forums: &forums.Service{DB: w.db, Perm: perm.New(w.db)}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db}, Perm: perm.New(w.db)})
}

// checkBadges holds every /me badge to its list for who: unread and new
// count their lists' rows, participated and watching their lists' unread
// rows (docs/06 "Thread list views").
func (w *world) checkBadges(who, when string) dto.UnreadCounts {
	w.t.Helper()
	me := w.meOf(who).Unread
	unreadRows := func(rows []dto.ThreadRow) int {
		n := 0
		for _, r := range rows {
			if r.Unread > 0 {
				n++
			}
		}
		return n
	}
	got := dto.UnreadCounts{
		Threads:      len(w.crossAll(who, "unread")),
		New:          len(w.crossAll(who, "new")),
		Participated: unreadRows(w.crossAll(who, "participated")),
		Watching:     unreadRows(w.crossAll(who, "watching")),
	}
	if me != got {
		w.t.Errorf("%s, %s: /me unread %+v, the lists %+v", when, who, me, got)
	}
	for _, r := range w.crossAll(who, "unread") {
		if r.Unread == 0 {
			w.t.Errorf("%s, %s: unread row %s has no unread post", when, who, r.ID)
		}
	}
	return me
}

// TestCrossListsArePermissionFiltered is AC1: every list, and every badge,
// holds only threads the viewer may read on this request. A restricted forum
// the viewer has no grant for is never listed; a thread moved into such a
// forum, a forum made restricted, and a group grant removed from a moderator
// each drop rows on the next request. Each viewer has written in, and
// watches, every thread, so every list would hold every thread without the
// filter.
func TestCrossListsArePermissionFiltered(t *testing.T) {
	w := newForumWorld(t)
	var crew int64
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT id FROM groups WHERE slug = 'crew'`).Scan(&crew)
	}); err != nil {
		t.Fatal(err)
	}
	w.write(func(q *dbq.Queries) error {
		// carol moderates g-secret through crew, as well as reading it
		// (docs/08 "Decided 2026-09-30").
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{ForumID: sql.NullInt64{Int64: w.ids["g-secret"], Valid: true},
			GroupID: crew, Permission: string(domain.PermModerate), CreatedAt: 1})
	})
	pub := w.threadIn("general", 1, "Public")
	secret := w.threadIn("g-secret", 2, "Secret")
	moving := w.threadIn("general", 3, "Moving")
	closing := w.threadIn("zeta", 4, "Closing")
	for _, id := range []threadid.ID{pub, secret, moving, closing} {
		w.post(id, "bob", "a post", 0)
		w.post(id, "alice", "alice was here", 0)
		w.post(id, "carol", "carol was here", 0)
		w.post(id, "bob", "and after them", 0)
		for _, who := range []string{"alice", "carol"} {
			w.sql(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at) VALUES (` +
				i64(w.users[who]) + `, ` + i64(int64(id)) + `, 'watch', 1, 1)`)
		}
	}
	check := func(when string, want map[string][]threadid.ID) {
		t.Helper()
		for who, ids := range want {
			for _, list := range crossLists {
				if got := w.crossIDs(who, list); !slices.Equal(got, sortedIDs(ids...)) {
					t.Errorf("%s: %s's %s is %v, want %v", when, who, list, got, sortedIDs(ids...))
				}
			}
			if me := w.checkBadges(who, when); me.Threads != len(ids) {
				t.Errorf("%s: %s's unread badge is %d, want %d", when, who, me.Threads, len(ids))
			}
		}
	}
	check("at first", map[string][]threadid.ID{
		"alice": {pub, moving, closing},
		"carol": {pub, secret, moving, closing},
	})

	// Moved into mods, which crew reads and alice does not.
	w.append(moving, threadlog.Append{Type: threadlog.TypeThreadMoved, Author: uint64(w.users["bob"]),
		Body: &threadlog.ThreadMovedBody{ForumID: uint64(w.ids["mods"])}})
	check("after the move", map[string][]threadid.ID{
		"alice": {pub, closing},
		"carol": {pub, secret, moving, closing},
	})

	// zeta made restricted, with no grant to anyone here.
	if err := w.svc.Perm.SetVisibility(t.Context(), audit.By{Actor: w.users["bob"]}, w.ids["zeta"],
		domain.VisibilityRestricted); err != nil {
		t.Fatal(err)
	}
	check("after zeta closed", map[string][]threadid.ID{
		"alice": {pub},
		"carol": {pub, secret, moving},
	})

	// carol leaves crew: the moderator by group grant sees neither of its
	// forums on the next request.
	if err := w.svc.Perm.RemoveMember(t.Context(), audit.By{Actor: w.users["bob"]}, crew, w.users["carol"]); err != nil {
		t.Fatal(err)
	}
	check("after carol left crew", map[string][]threadid.ID{
		"alice": {pub},
		"carol": {pub},
	})
}

// TestListRowsCarryNoHiddenPost is AC2: a row is what a forum's list shows,
// and never a post's body. A thread whose last post a moderator hid, or that
// was redacted, shows its last poster and time, as the thread page's
// tombstone does; a redacted title is shown as its replacement; and a
// quarantined thread is in no list.
func TestListRowsCarryNoHiddenPost(t *testing.T) {
	w := newWorld(t)
	hidden, _ := w.seedHidden() // #5 is alice's, hidden by bob
	redacted, _ := w.thread(2, "A title somebody regretted")
	w.post(redacted, "bob", "a first post", 0)
	w.post(redacted, "alice", redactedSecret, 0)
	held, _ := w.thread(3, "Held")
	w.post(held, "bob", "in a thread startup found short", 0)
	w.redactOffline(redacted, 2)
	w.rewriteOffline(redacted, func(src *bytes.Reader, size int64, dst *bytes.Buffer, o threadlog.RedactOptions) error {
		_, err := threadlog.RedactTitle(src, size, dst, threadlog.ThreadRenamedBody{Title: "Title removed", Slug: "title-removed"}, o)
		return err
	})
	w.withQuarantine(held)

	for _, list := range crossLists {
		for _, who := range []string{"carol", "alice"} {
			rec := w.get(w.viewer(who), crossPath(list, ""))
			body := rec.Body.String()
			for _, text := range []string{hiddenSecret, redactedSecret, "regretted", "smaller", "Summarising",
				"Agreed with", "a first post", "startup found short"} {
				if strings.Contains(body, text) {
					t.Errorf("%s as %s carries %q: %s", list, who, text, body)
				}
			}
			if strings.Contains(body, held.String()) {
				t.Errorf("%s as %s lists the quarantined thread", list, who)
			}
		}
	}

	// carol has read hidden to #2, so it is unread to her, with its hidden
	// post last. The row names what the thread page's tombstone names.
	rows := map[string]dto.ThreadRow{}
	for _, r := range w.crossAll("carol", "unread") {
		rows[r.ID] = r
	}
	p := page(t, w.get(w.viewer("carol"), threadPath(hidden)))
	last := p.Posts[len(p.Posts)-1]
	if r, ok := rows[hidden.String()]; !ok || r.LastPostAuthor.Handle != last.Author.Handle || !last.Deleted ||
		r.Unread != 3 || r.LastReadOrdinal != 2 {
		t.Errorf("the hidden thread's row %+v; the page's last post %+v", r, last)
	}
	if r := rows[redacted.String()]; r.Title != "Title removed" || r.Slug != "title-removed" {
		t.Errorf("the redacted thread's row %+v", r)
	}
	if _, ok := rows[held.String()]; ok || len(rows) != 2 {
		t.Errorf("carol's unread rows %v", rows)
	}
}

// TestUnreadWithNoReadPositions is AC3: a member with no read_positions row
// has every thread with a post unread, until a mark; then exactly the
// threads with a post after it. A thread with no post is in no list.
func TestUnreadWithNoReadPositions(t *testing.T) {
	w := newWorld(t)
	old, _ := w.thread(1, "Old")
	w.post(old, "bob", "before", 0)
	mid, _ := w.thread(2, "Middle")
	w.post(mid, "bob", "before", 0)
	w.thread(3, "Empty")
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(old, mid)) {
		t.Errorf("with no mark: %v", got)
	}
	w.sql(`UPDATE users SET mark_all_read_at = ` + i64(w.clock) + ` WHERE id = ` + i64(w.users["alice"]))
	w.post(mid, "carol", "after", 0)
	fresh, _ := w.thread(4, "Fresh")
	w.post(fresh, "carol", "after", 0)
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(mid, fresh)) {
		t.Errorf("after the mark: %v", got)
	}
	if got := w.crossIDs("alice", "new"); !slices.Equal(got, sortedIDs(fresh)) {
		t.Errorf("new after the mark: %v", got)
	}
	for _, r := range w.crossAll("alice", "unread") {
		if r.Unread != 1 || r.LastReadOrdinal != map[string]int{mid.String(): 1, fresh.String(): 0}[r.ID] {
			t.Errorf("row %s: unread %d, last read %d", r.ID, r.Unread, r.LastReadOrdinal)
		}
	}
	w.checkBadges("alice", "after the mark")
}

// TestBadgesMatchLists is AC6: in one world holding every kind of thread the
// rules tell apart, each /me badge equals its list, counted page by page,
// before and after each kind of mark and with a thread quarantined.
func TestBadgesMatchLists(t *testing.T) {
	w := newForumWorld(t)
	var seq uint64
	thread := func(forum, title string, posts ...string) threadid.ID {
		seq++
		id := w.threadIn(forum, seq, title)
		for _, who := range posts {
			w.post(id, who, "a post by "+who, 0)
		}
		return id
	}
	read := thread("general", "Read", "bob", "bob")
	w.readAs("alice", read, 2)
	behind := thread("general", "Behind", "bob", "bob")
	w.readAs("alice", behind, 1)
	unopened := thread("general", "Never opened", "bob")
	mineUnread := thread("general", "Mine, unread", "alice", "bob")
	w.readAs("alice", mineUnread, 1)
	mineRead := thread("zeta", "Mine, read", "alice")
	w.readAs("alice", mineRead, 1)
	movedOut := thread("zeta", "Only a moved post of mine", "alice", "bob")
	w.sql(`UPDATE post_meta SET moved = 1 WHERE thread_id = ` + i64(int64(movedOut)) + ` AND ordinal = 1`)
	watchedUnread := thread("zeta", "Watched, unread", "bob")
	w.watchAs("alice", watchedUnread, "watch")
	watchedRead := thread("zeta", "Watched, read", "bob")
	w.watchAs("alice", watchedRead, "watch")
	w.readAs("alice", watchedRead, 1)
	muted := thread("general", "Muted", "alice", "bob")
	w.watchAs("alice", muted, "mute")
	inMuted := thread("alpha", "In a muted forum", "alice", "bob")
	watchedInMuted := thread("alpha", "Watched in a muted forum", "bob")
	w.watchAs("alice", watchedInMuted, "watch")
	ok[dto.ForumWatch](t, send(w.chain(), "alice", http.MethodPost, forumWatchPath("alpha"), levelBody("mute")))
	thread("g-secret", "Restricted", "alice", "bob")
	empty := thread("general", "No post yet")
	held := thread("general", "Quarantined", "alice", "bob")
	w.watchAs("alice", held, "watch")

	want := map[string][]threadid.ID{
		"unread":       {behind, unopened, mineUnread, movedOut, watchedUnread, watchedInMuted, held},
		"new":          {unopened, movedOut, watchedUnread, watchedInMuted, held},
		"participated": {mineUnread, mineRead, held},
		"watching":     {watchedUnread, watchedRead, watchedInMuted, held},
	}
	for list, ids := range want {
		if got := w.crossIDs("alice", list); !slices.Equal(got, sortedIDs(ids...)) {
			t.Errorf("%s is %v, want %v", list, got, sortedIDs(ids...))
		}
	}
	_ = inMuted
	_ = empty
	me := w.checkBadges("alice", "at first")
	if me != (dto.UnreadCounts{Threads: 7, New: 5, Participated: 2, Watching: 3}) {
		t.Errorf("/me unread %+v", me)
	}

	// Quarantined: in no list, and in no badge.
	w.withQuarantine(held)
	for list, ids := range want {
		ids = slices.DeleteFunc(slices.Clone(ids), func(id threadid.ID) bool { return id == held })
		if got := w.crossIDs("alice", list); !slices.Equal(got, sortedIDs(ids...)) {
			t.Errorf("quarantined: %s is %v, want %v", list, got, sortedIDs(ids...))
		}
	}
	me = w.checkBadges("alice", "quarantined")
	if me != (dto.UnreadCounts{Threads: 6, New: 4, Participated: 1, Watching: 2}) {
		t.Errorf("quarantined: /me unread %+v", me)
	}

	// The forum-level mark reads general's threads; the site-level mark,
	// then a post after it, leaves only that thread.
	ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, "/api/v1/forums/general/read", ""))
	w.checkBadges("alice", "after general's mark")
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(movedOut, watchedUnread, watchedInMuted)) {
		t.Errorf("unread after general's mark: %v", got)
	}
	ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, "/api/v1/me/read", ""))
	w.checkBadges("alice", "after the site's mark")
	w.post(watchedRead, "bob", "after the mark", 0)
	fresh := thread("zeta", "Started after the mark", "bob")
	// Started after the mark with no post yet: new to nobody (review 1751).
	thread("zeta", "Started after the mark, empty")
	w.checkBadges("alice", "after a post past the mark")
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(watchedRead, fresh)) {
		t.Errorf("unread after the site's mark: %v", got)
	}
	if got := w.crossIDs("alice", "new"); !slices.Equal(got, sortedIDs(fresh)) {
		t.Errorf("new after the site's mark: %v", got)
	}
}

// TestMarkAllRead is AC5. The forum level reads that forum's threads and no
// other's, and the site level everything written so far; each shows in /me
// and the lists on the next request, and neither moves a position or the
// mark back. After each, the thread page's landing page, its divider, its
// bare unread lens and the read routes' answers name one boundary. A
// forum the viewer cannot see is not-found.
func TestMarkAllRead(t *testing.T) {
	w := newForumWorld(t)
	g1 := w.threadIn("general", 1, "First")
	for range 3 {
		w.post(g1, "bob", "in general", 0)
	}
	g2 := w.threadIn("general", 2, "Second")
	w.post(g2, "bob", "in general", 0)
	w.post(g2, "bob", "in general", 0)
	far := w.threadIn("general", 3, "Read past its end")
	w.post(far, "bob", "in general", 0)
	z1 := w.threadIn("zeta", 4, "Elsewhere")
	w.post(z1, "bob", "in zeta", 0)
	w.post(z1, "bob", "in zeta", 0)
	w.readAs("alice", g1, 1)
	// A position past the last post, which only a projection that trails
	// the index can leave, and the forum's mark must not lower.
	w.sql(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES (` +
		i64(w.users["alice"]) + `, ` + i64(int64(far)) + `, 9, 77)`)

	agree := func(when string, id threadid.ID, boundary int, at *dto.Millis) {
		t.Helper()
		p := page(t, w.get(w.viewer("alice"), threadPath(id)))
		if p.Viewer.LastReadOrdinal != boundary || (at != nil && (p.Viewer.LastReadAt == nil || *p.Viewer.LastReadAt != *at)) {
			t.Errorf("%s: the divider is #%d at %v, want #%d at %v", when, p.Viewer.LastReadOrdinal, p.Viewer.LastReadAt,
				boundary, at)
		}
		lensed := page(t, w.get(w.viewer("alice"), threadPath(id)+"?lens=unread"))
		if lensed.Lens == nil || lensed.Lens.Value != strconv.Itoa(boundary) {
			t.Errorf("%s: the bare unread lens is %+v, want unread:%d", when, lensed.Lens, boundary)
		}
		for _, post := range lensed.Posts {
			if post.Ordinal <= boundary {
				t.Errorf("%s: the unread lens shows #%d at boundary %d", when, post.Ordinal, boundary)
			}
		}
		if want := min(boundary/p.Page.Size+1, p.Page.TotalPages); boundary > 0 && p.Page.Number != want {
			t.Errorf("%s: landed on page %d for boundary %d, want %d", when, p.Page.Number, boundary, want)
		}
		for _, r := range w.crossAll("alice", "unread") {
			if r.ID == id.String() && r.LastReadOrdinal != boundary {
				t.Errorf("%s: the unread row's last_read_ordinal is %d, want %d", when, r.LastReadOrdinal, boundary)
			}
		}
	}

	// The forum level.
	before := w.clock
	res := ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, "/api/v1/forums/general/read", "{}"))
	if int64(res.At) != before {
		t.Errorf("general marked at %d, want %d", res.At, before)
	}
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(z1)) {
		t.Errorf("unread after general's mark: %v", got)
	}
	if got := w.crossIDs("alice", "new"); !slices.Equal(got, sortedIDs(z1)) {
		t.Errorf("new after general's mark: %v", got)
	}
	if me := w.checkBadges("alice", "after general's mark"); me.Threads != 1 || me.New != 1 {
		t.Errorf("/me after general's mark: %+v", me)
	}
	if w.position("alice", g1) != 3 || w.position("alice", g2) != 2 || w.position("alice", far) != 9 {
		t.Errorf("positions after general's mark: %d %d %d", w.position("alice", g1), w.position("alice", g2),
			w.position("alice", far))
	}
	at := dto.Millis(before)
	agree("after general's mark", g1, 3, &at)
	// carol's state is hers.
	if got := w.crossIDs("carol", "unread"); len(got) != 4 {
		t.Errorf("carol's unread after alice's mark: %v", got)
	}

	// The site level, after a post in zeta and one in general.
	w.post(z1, "carol", "third in zeta", 0)
	w.post(g2, "carol", "third in general", 0)
	mark := w.clock + 1
	w.clock = mark
	res = ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, "/api/v1/me/read", ""))
	if int64(res.At) != mark {
		t.Errorf("the site marked at %d, want %d", res.At, mark)
	}
	for _, list := range []string{"unread", "new"} {
		if got := w.crossIDs("alice", list); len(got) != 0 {
			t.Errorf("%s after the site's mark: %v", list, got)
		}
	}
	if me := w.checkBadges("alice", "after the site's mark"); me != (dto.UnreadCounts{}) {
		t.Errorf("/me after the site's mark: %+v", me)
	}
	markAt := dto.Millis(mark)
	// No position moved: the mark moved the boundary past them.
	if w.position("alice", z1) != -1 || w.position("alice", g2) != 2 {
		t.Errorf("positions after the site's mark: %d %d", w.position("alice", z1), w.position("alice", g2))
	}
	agree("after the site's mark", z1, 3, &markAt)
	agree("after the site's mark", g2, 3, &markAt)

	// A post after the mark: the boundary is the post before it, and the
	// read routes answer it.
	w.post(z1, "bob", "after the mark", 0)
	if got := w.crossIDs("alice", "unread"); !slices.Equal(got, sortedIDs(z1)) {
		t.Errorf("unread after a post past the mark: %v", got)
	}
	agree("after a post past the mark", z1, 3, &markAt)
	if got := w.readAs("alice", z1, 1); got.LastReadOrdinal != 3 {
		t.Errorf("a beacon below the mark answered %d, want 3", got.LastReadOrdinal)
	}
	unread := ok[dto.ReadPosition](t, send(w.chain(), "alice", http.MethodDelete, "/api/v1/threads/"+z1.String()+"/read", ""))
	if unread.LastReadOrdinal != 3 {
		t.Errorf("marking the thread unread answered %d, want 3: the mark keeps 1 to 3 read", unread.LastReadOrdinal)
	}
	agree("after marking it unread", z1, 3, &markAt)
	if got := w.readAs("alice", z1, 4); got.LastReadOrdinal != 4 {
		t.Errorf("reading to the end answered %d", got.LastReadOrdinal)
	}
	agree("after reading to the end", z1, 4, nil)
	w.checkBadges("alice", "after reading to the end")

	// Neither moves back: a mark from a clock behind keeps the later one.
	w.clock = mark - 50_000
	res = ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, "/api/v1/me/read", ""))
	if int64(res.At) != mark {
		t.Errorf("a mark from behind answered %d, want %d", res.At, mark)
	}

	// A forum alice cannot see, or that is not there, is not-found; a
	// stranger is refused before the forum is looked at.
	for _, slug := range []string{"g-secret", "nowhere"} {
		if rec := send(w.chain(), "alice", http.MethodPut, "/api/v1/forums/"+slug+"/read", ""); problemCode(t, rec) != "not-found" {
			t.Errorf("marking %s: %d %s", slug, rec.Code, rec.Body)
		}
	}
	if rec := send(w.chain(), "alice", http.MethodPut, "/api/v1/me/read", `{"at":1}`); problemCode(t, rec) != "bad-request" {
		t.Errorf("a body with a field: %d %s", rec.Code, rec.Body)
	}
}

// TestCrossListCursor is AC7's paging: 25 to a page, keyset by cursor, a
// thread bumped between pages not shown twice, and a cursor from another
// list, from a forum's list, or of no shape is bad-request.
func TestCrossListCursor(t *testing.T) {
	w := newWorld(t)
	var all []threadid.ID
	for i := range 30 {
		id, _ := w.thread(uint64(i+1), "Thread "+strconv.Itoa(i))
		w.post(id, "bob", "a post", 0)
		all = append(all, id)
	}
	first := w.crossPage("alice", "unread", "")
	if len(first.Threads) != 25 || first.NextCursor == "" {
		t.Fatalf("first page: %d rows, cursor %q", len(first.Threads), first.NextCursor)
	}
	// The newest thread, on page 1, is bumped: it moves above the cursor.
	w.post(all[29], "carol", "a bump", 0)
	second := w.crossPage("alice", "unread", first.NextCursor)
	if len(second.Threads) != 5 || second.NextCursor != "" {
		t.Fatalf("second page: %d rows, cursor %q", len(second.Threads), second.NextCursor)
	}
	seen := map[string]bool{}
	for _, r := range append(first.Threads, second.Threads...) {
		if seen[r.ID] {
			t.Errorf("%s shown twice", r.ID)
		}
		seen[r.ID] = true
	}
	if len(seen) != 30 {
		t.Errorf("%d threads seen, want 30", len(seen))
	}

	newCursor := w.crossPage("alice", "new", "").NextCursor
	forumCursor := w.get(w.viewer("alice"), listPath("general", "", ""))
	fc := ok[dto.ThreadList](t, forumCursor).NextCursor
	for name, c := range map[string]string{
		"another list's":  newCursor,
		"a forum list's":  fc,
		"not base64":      "!!",
		"too few parts":   base64.RawURLEncoding.EncodeToString([]byte("unread|5")),
		"a zero id":       base64.RawURLEncoding.EncodeToString([]byte("unread|5|0")),
		"a non-canonical": base64.RawURLEncoding.EncodeToString([]byte("unread|05|1")),
	} {
		if c == "" {
			t.Fatalf("%s cursor is empty", name)
		}
		if rec := w.get(w.viewer("alice"), crossPath("unread", c)); problemCode(t, rec) != "bad-request" {
			t.Errorf("%s cursor: %d %s", name, rec.Code, rec.Body)
		}
	}
}

// TestCrossListRefusals is AC7's refusals: every route is a member's. An
// anonymous or inactive viewer is unauthenticated, and a pre-user is
// forbidden.
func TestCrossListRefusals(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	_ = id
	h := w.chain()
	type route struct{ method, path string }
	var routes []route
	for _, l := range crossLists {
		routes = append(routes, route{http.MethodGet, crossPath(l, "")})
	}
	routes = append(routes, route{http.MethodPut, "/api/v1/me/read"}, route{http.MethodPut, "/api/v1/forums/general/read"})
	for _, r := range routes {
		for who, want := range map[string]string{"": "unauthenticated", "inactive": "unauthenticated", "pre-user": "forbidden"} {
			if rec := send(h, who, r.method, r.path, ""); problemCode(t, rec) != want {
				t.Errorf("%s %s as %q: %d %s, want %s", r.method, r.path, who, rec.Code, rec.Body, want)
			}
		}
		if rec := send(h, "alice", r.method, r.path, ""); rec.Code != http.StatusOK {
			t.Errorf("%s %s as a member: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
	}
}

// TestCrossListsStayOutOfTheResponseCache: the lists and the mark routes
// are per viewer, so none of them looks in or fills the shared response
// cache, and each answer is no-store (docs/13 "The response cache"). Two
// members with different read state get different answers.
func TestCrossListsStayOutOfTheResponseCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed() // carol has read to #2; alice has no position
	path := threadPath(id)
	w.get(w.viewer("carol"), path) // fills the cache, so its counters are live
	before := w.svc.Responses.Stats()
	if before.Inserts == 0 {
		t.Fatal("the thread page did not fill the cache: the counters prove nothing")
	}
	bodies := map[string]string{}
	for _, list := range crossLists {
		for _, who := range []string{"alice", "carol"} {
			rec := w.get(w.viewer(who), crossPath(list, ""))
			if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != "" {
				t.Errorf("%s as %s: %d, Cache-Control %q, ETag %q", list, who, rec.Code,
					rec.Header().Get("Cache-Control"), rec.Header().Get("ETag"))
			}
			bodies[list+who] = rec.Body.String()
		}
	}
	for _, p := range []string{"/api/v1/me/read", "/api/v1/forums/general/read"} {
		ok[dto.MarkedRead](t, send(w.chain(), "alice", http.MethodPut, p, ""))
	}
	if after := w.svc.Responses.Stats(); after.Hits+after.Misses != before.Hits+before.Misses ||
		after.Inserts != before.Inserts || after.Entries != before.Entries {
		t.Errorf("a cross-forum list touched the response cache: %+v then %+v", before, after)
	}
	if bodies["unreadalice"] == bodies["unreadcarol"] {
		t.Errorf("alice and carol got one unread answer: %s", bodies["unreadalice"])
	}
}
