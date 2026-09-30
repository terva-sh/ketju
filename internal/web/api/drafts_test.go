package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the new-thread draft, /api/v1/forums/{slug}/draft, and for the
// revision every draft carries (TKT-01M3K9XBJA, docs/07 "Drafts").

func forumDraftPath(slug string) string { return "/api/v1/forums/" + slug + "/draft" }

// noDraftRevision wants a 204 with no body, not cached, and returns its
// Draft-Revision.
func noDraftRevision(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d %q, want 204 and no body", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	rev := rec.Header().Get("Draft-Revision")
	if rev == "" {
		t.Fatal("a 204 from a draft route has no Draft-Revision")
	}
	return rev
}

// draftRows is how many drafts rows who has, tombstones included.
func (w *world) draftRows(who string) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT count(*) FROM drafts WHERE user_id = ?`, w.users[who]).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// TestForumDraftRoundTrip is AC1: a PUT is read back whole by a GET from
// another session of the same user, and after a DELETE the GET is 204.
func TestForumDraftRoundTrip(t *testing.T) {
	w := newWorld(t)
	laptop, phone := w.viewer("carol"), w.viewer("carol")
	laptop.SessionKey, phone.SessionKey = "s-carol-laptop", "s-carol-phone"
	path := forumDraftPath("general")

	noDraftRevision(t, w.sendAs(phone, http.MethodGet, path, ""))
	saved := ok[dto.ForumDraft](t, w.sendAs(laptop, http.MethodPut, path, `{"title":"Moving the club ","source":"We keep losing"}`))
	title := "Moving the club "
	want := dto.ForumDraft{Forum: "general", Title: &title, Source: "We keep losing", UpdatedAt: dto.Millis(w.clock), Revision: 1}
	if renderJSON(t, saved) != renderJSON(t, want) {
		t.Fatalf("saved %s, want %s", renderJSON(t, saved), renderJSON(t, want))
	}
	if got := ok[dto.ForumDraft](t, w.sendAs(phone, http.MethodGet, path, "")); renderJSON(t, got) != renderJSON(t, saved) {
		t.Fatalf("the other device reads %s, want %s", renderJSON(t, got), renderJSON(t, saved))
	}

	if rev := noDraftRevision(t, w.sendAs(phone, http.MethodDelete, path, "")); rev != "2" {
		t.Errorf("DELETE answered Draft-Revision %s, want 2", rev)
	}
	if rev := noDraftRevision(t, w.sendAs(laptop, http.MethodGet, path, "")); rev != "2" {
		t.Errorf("GET after DELETE answered Draft-Revision %s, want 2", rev)
	}
}

// TestForumDraftIsPerUserAndForum is AC1: alice's draft in general is not
// carol's, nor alice's in another forum, and a save in one leaves the other
// as it was.
func TestForumDraftIsPerUserAndForum(t *testing.T) {
	w := newWorld(t)
	w.forumAt("social", domain.VisibilityPublic, false)
	h := w.chain()

	ok[dto.ForumDraft](t, send(h, "alice", http.MethodPut, forumDraftPath("general"), `{"title":"Alice's","source":"in general"}`))
	noDraftRevision(t, send(h, "carol", http.MethodGet, forumDraftPath("general"), ""))
	noDraftRevision(t, send(h, "alice", http.MethodGet, forumDraftPath("social"), ""))

	ok[dto.ForumDraft](t, send(h, "carol", http.MethodPut, forumDraftPath("general"), `{"title":"Carol's","source":"hers"}`))
	ok[dto.ForumDraft](t, send(h, "alice", http.MethodPut, forumDraftPath("social"), `{"source":"in social"}`))
	got := ok[dto.ForumDraft](t, send(h, "alice", http.MethodGet, forumDraftPath("general"), ""))
	if got.Title == nil || *got.Title != "Alice's" || got.Source != "in general" || got.Forum != "general" {
		t.Errorf("alice's draft in general is now %s", renderJSON(t, got))
	}
	if got := ok[dto.ForumDraft](t, send(h, "alice", http.MethodGet, forumDraftPath("social"), "")); got.Source != "in social" || got.Forum != "social" {
		t.Errorf("alice's draft in social is %s", renderJSON(t, got))
	}

	// Carol discarding hers leaves alice's alone.
	noDraftRevision(t, send(h, "carol", http.MethodDelete, forumDraftPath("general"), ""))
	if got := ok[dto.ForumDraft](t, send(h, "alice", http.MethodGet, forumDraftPath("general"), "")); got.Source != "in general" {
		t.Errorf("carol's delete reached alice's draft: %s", renderJSON(t, got))
	}
	if n := w.draftRows("alice"); n != 2 {
		t.Errorf("%d draft rows for alice, want 2", n)
	}
}

// TestForumDraftPutReplacesTheTitle is AC1: a PUT replaces the whole draft,
// so one without a title clears the stored one, and the answer omits it.
func TestForumDraftPutReplacesTheTitle(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	path := forumDraftPath("general")
	ok[dto.ForumDraft](t, send(h, "carol", http.MethodPut, path, `{"title":"A title","source":"body"}`))
	rec := send(h, "carol", http.MethodPut, path, `{"source":"body, longer"}`)
	if strings.Contains(rec.Body.String(), `"title"`) {
		t.Errorf("a draft with no title still names one: %s", rec.Body)
	}
	if got := ok[dto.ForumDraft](t, rec); got.Title != nil || got.Source != "body, longer" {
		t.Errorf("after a PUT without a title: %s", renderJSON(t, got))
	}
	if got := ok[dto.ForumDraft](t, send(h, "carol", http.MethodGet, path, "")); got.Title != nil {
		t.Errorf("GET after a PUT without a title: %s", renderJSON(t, got))
	}
	// An empty title is a title, kept as typed.
	if got := ok[dto.ForumDraft](t, send(h, "carol", http.MethodPut, path, `{"title":"","source":""}`)); got.Title == nil || *got.Title != "" {
		t.Errorf("an empty title: %s", renderJSON(t, got))
	}
}

// TestForumDraftTitleRules is AC3: a title over MaxTitle characters or with a
// control character is validation-failed, and writes nothing; one of
// exactly MaxTitle characters, an empty one, and one with outer whitespace
// are kept as typed.
func TestForumDraftTitleRules(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	path := forumDraftPath("general")
	for _, c := range []struct{ name, body, want string }{
		{"a title over the limit", `{"title":"` + strings.Repeat("é", threads.MaxTitle+1) + `","source":"s"}`, "validation-failed"},
		{"a newline in the title", `{"title":"Topic\n","source":"s"}`, "validation-failed"},
		{"a tab in the title", `{"title":"a\tb","source":"s"}`, "validation-failed"},
		{"no source", `{"title":"t"}`, "bad-request"},
		{"an unknown field", `{"title":"t","source":"s","reply_to":1}`, "bad-request"},
		{"a title that is not a string", `{"title":3,"source":"s"}`, "bad-request"},
	} {
		if rec := send(h, "carol", http.MethodPut, path, c.body); problemCode(t, rec) != c.want {
			t.Errorf("%s: %d %s, want %s", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if n := w.draftRows("carol"); n != 0 {
		t.Fatalf("refused saves left %d draft rows", n)
	}
	for _, title := range []string{strings.Repeat("é", threads.MaxTitle), "", "  spaced out  "} {
		body := `{"title":` + renderJSON(t, title) + `,"source":"s"}`
		if got := ok[dto.ForumDraft](t, send(h, "carol", http.MethodPut, path, body)); got.Title == nil || *got.Title != title {
			t.Errorf("title %q came back as %s", title, renderJSON(t, got))
		}
	}
}

// TestForumDraftSaveNeedsCreateThread is AC3: a member who may read the
// forum but not start threads in it is forbidden to save a draft there, but
// may read and discard one.
func TestForumDraftSaveNeedsCreateThread(t *testing.T) {
	w := newWorld(t)
	path := forumDraftPath("general")
	ok[dto.ForumDraft](t, send(w.chain(), "carol", http.MethodPut, path, `{"title":"t","source":"s"}`))
	// A per-forum read row for members replaces their global read, reply
	// and create_thread there.
	w.write(func(q *dbq.Queries) error {
		return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
			ForumID: sql.NullInt64{Int64: w.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
	})
	h := newHandlerFor(w).chain()
	if rec := send(h, "carol", http.MethodPut, path, `{"title":"t","source":"changed"}`); problemCode(t, rec) != "forbidden" {
		t.Errorf("a save without create_thread: %d %s", rec.Code, rec.Body)
	}
	if got := ok[dto.ForumDraft](t, send(h, "carol", http.MethodGet, path, "")); got.Source != "s" || got.Revision != 1 {
		t.Errorf("reading without create_thread: %s", renderJSON(t, got))
	}
	if rev := noDraftRevision(t, send(h, "carol", http.MethodDelete, path, "")); rev != "2" {
		t.Errorf("discarding without create_thread: Draft-Revision %s, want 2", rev)
	}
}

// TestForumDraftInAnArchivedForumIsConflict is AC3: in an archived forum a
// member's and a moderator's save is conflict, as starting a thread is, and
// an admin's is saved. A member may still read and discard the draft they
// had.
func TestForumDraftInAnArchivedForumIsConflict(t *testing.T) {
	w := newWorld(t)
	admin := w.newAdmin()
	path := forumDraftPath("general")
	ok[dto.ForumDraft](t, send(w.chain(), "carol", http.MethodPut, path, `{"title":"before","source":"s"}`))
	w.archive()
	h := newHandlerFor(w)
	for _, who := range []string{"carol", "bob"} {
		if rec := send(h.chain(), who, http.MethodPut, path, `{"title":"after","source":"s"}`); problemCode(t, rec) != "conflict" {
			t.Errorf("%s in an archived forum: %d %s", who, rec.Code, rec.Body)
		}
	}
	if got := ok[dto.ForumDraft](t, send(h.chain(), "carol", http.MethodGet, path, "")); got.Title == nil || *got.Title != "before" {
		t.Errorf("carol's draft after the refusal: %s", renderJSON(t, got))
	}
	if got := ok[dto.ForumDraft](t, h.sendAs(admin, http.MethodPut, path, `{"title":"admin's","source":"s"}`)); got.Title == nil || *got.Title != "admin's" {
		t.Errorf("an admin's save: %s", renderJSON(t, got))
	}
	noDraftRevision(t, send(h.chain(), "carol", http.MethodDelete, path, ""))
}

// TestForumDraftInAHiddenForumIsNotFound is AC3: a forum hidden from the
// viewer, or one that does not exist, is not-found on every draft route,
// whether or not the viewer had a draft there.
func TestForumDraftInAHiddenForumIsNotFound(t *testing.T) {
	w := newWorld(t)
	ok[dto.ForumDraft](t, send(w.chain(), "carol", http.MethodPut, forumDraftPath("general"), `{"title":"t","source":"s"}`))
	w.hideForum()
	w.revokeForumRead()
	h := newHandlerFor(w).chain()
	for _, slug := range []string{"general", "nowhere"} {
		for _, r := range []struct{ method, body string }{
			{http.MethodGet, ""}, {http.MethodPut, `{"title":"t","source":"s"}`}, {http.MethodDelete, ""},
		} {
			if rec := send(h, "carol", r.method, forumDraftPath(slug), r.body); problemCode(t, rec) != "not-found" {
				t.Errorf("%s %s: %d %s", r.method, slug, rec.Code, rec.Body)
			}
		}
	}
}

// TestForumDraftNeedsAMemberSession is AC3: anonymous and inactive are
// unauthenticated, and a pre-user is forbidden, on every route and whatever
// the slug, and none of them writes.
func TestForumDraftNeedsAMemberSession(t *testing.T) {
	w := newWorld(t)
	h := w.chain()
	for who, want := range map[string]string{"": "unauthenticated", "inactive": "unauthenticated", "pre-user": "forbidden"} {
		for _, slug := range []string{"general", "nowhere"} {
			for _, r := range []struct{ method, body string }{
				{http.MethodGet, ""}, {http.MethodPut, `{"title":"t","source":"s"}`}, {http.MethodDelete, ""},
			} {
				if rec := send(h, who, r.method, forumDraftPath(slug), r.body); problemCode(t, rec) != want {
					t.Errorf("%q %s %s: %d %s, want %s", who, r.method, slug, rec.Code, rec.Body, want)
				}
			}
		}
	}
	if n := w.draftRows("alice"); n != 0 {
		t.Errorf("refused requests left alice %d draft rows", n)
	}
}

// draftKind is one kind of draft key: its path, and the body of a save.
type draftKind struct {
	name string
	path string
	body func(source string) string
}

func draftKinds(id threadid.ID) []draftKind {
	return []draftKind{
		{"reply", draftPath(id), func(s string) string { return `{"source":"` + s + `","reply_to":1}` }},
		{"new thread", forumDraftPath("general"), func(s string) string { return `{"title":"T","source":"` + s + `"}` }},
	}
}

// revisionOf is the revision in a 200's draft, of either kind.
func revisionOf(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var d struct {
		Revision *int64 `json:"revision"`
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || d.Revision == nil {
		t.Fatalf("no revision in %s: %v", rec.Body, err)
	}
	return *d.Revision
}

// TestDraftRevisionSurvivesDeletion is AC7: for each kind of draft, with
// the service clock fixed and then stepped back an hour between requests,
// PUT, PUT, DELETE, PUT answer revisions 1, 2, 3 (as Draft-Revision on the
// 204, which a GET then repeats) and 4. The row is kept as a tombstone, so
// the count never restarts, and nothing reads the clock to make it.
func TestDraftRevisionSurvivesDeletion(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	for _, k := range draftKinds(id) {
		t.Run(k.name, func(t *testing.T) {
			w.clock = 1_757_600_000_000
			h := newHandlerFor(w).chain()
			step := func() { w.clock -= 3_600_000 }

			if got := revisionOf(t, send(h, "carol", http.MethodPut, k.path, k.body("one"))); got != 1 {
				t.Errorf("first PUT: revision %d, want 1", got)
			}
			if got := revisionOf(t, send(h, "carol", http.MethodPut, k.path, k.body("two"))); got != 2 {
				t.Errorf("second PUT at the same instant: revision %d, want 2", got)
			}
			step()
			if rev := noDraftRevision(t, send(h, "carol", http.MethodDelete, k.path, "")); rev != "3" {
				t.Errorf("DELETE an hour earlier: Draft-Revision %s, want 3", rev)
			}
			if rev := noDraftRevision(t, send(h, "carol", http.MethodGet, k.path, "")); rev != "3" {
				t.Errorf("GET of the tombstone: Draft-Revision %s, want 3", rev)
			}
			step()
			rec := send(h, "carol", http.MethodPut, k.path, k.body("again"))
			if got := revisionOf(t, rec); got != 4 {
				t.Errorf("PUT after DELETE, an hour earlier still: revision %d, want 4", got)
			}
			if !strings.Contains(rec.Body.String(), `"source":"again"`) {
				t.Errorf("the revived draft: %s", rec.Body)
			}
		})
	}
	if n := w.draftRows("carol"); n != 2 {
		t.Errorf("%d draft rows for carol, want one per key, 2", n)
	}
}

// TestDraftRevisionIsPerKey is AC7: saving and deleting another thread's or
// forum's draft, or another user's, leaves this key's revision where it
// was.
func TestDraftRevisionIsPerKey(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	other, _ := w.thread(9, "Another thread")
	w.post(other, "alice", "first", 0)
	w.forumAt("social", domain.VisibilityPublic, false)
	h := w.chain()
	others := []draftKind{
		{"another thread", draftPath(other), func(s string) string { return `{"source":"` + s + `"}` }},
		{"another forum", forumDraftPath("social"), func(s string) string { return `{"source":"` + s + `"}` }},
	}
	for _, k := range draftKinds(id) {
		if got := revisionOf(t, send(h, "carol", http.MethodPut, k.path, k.body("mine"))); got != 1 {
			t.Fatalf("%s: first save answered %d", k.name, got)
		}
	}
	wrote := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code/100 != 2 {
			t.Fatalf("a write to another key failed, so the test proves nothing: %d %s", rec.Code, rec.Body)
		}
	}
	for range 3 {
		for _, o := range append(others, draftKinds(id)...) {
			wrote(send(h, "alice", http.MethodPut, o.path, o.body("hers")))
		}
		for _, o := range others {
			wrote(send(h, "carol", http.MethodPut, o.path, o.body("elsewhere")))
			wrote(send(h, "carol", http.MethodDelete, o.path, ""))
		}
	}
	for _, k := range draftKinds(id) {
		if got := revisionOf(t, send(h, "carol", http.MethodGet, k.path, "")); got != 1 {
			t.Errorf("%s: revision %d after writes to other keys, want 1", k.name, got)
		}
	}
}

// TestDeletingNothingKeepsTheRevision is AC7: a DELETE on a key with no row
// answers Draft-Revision 0 and writes no row, and a DELETE on a tombstone
// answers its revision unchanged.
func TestDeletingNothingKeepsTheRevision(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	for _, k := range draftKinds(id) {
		if rev := noDraftRevision(t, send(h, "carol", http.MethodDelete, k.path, "")); rev != "0" {
			t.Errorf("%s: DELETE with no row answered %s, want 0", k.name, rev)
		}
		if rev := noDraftRevision(t, send(h, "carol", http.MethodGet, k.path, "")); rev != "0" {
			t.Errorf("%s: GET with no row answered %s, want 0", k.name, rev)
		}
	}
	if n := w.draftRows("carol"); n != 0 {
		t.Fatalf("deleting nothing wrote %d rows", n)
	}
	for _, k := range draftKinds(id) {
		revisionOf(t, send(h, "carol", http.MethodPut, k.path, k.body("x")))
		for i := range 3 {
			if rev := noDraftRevision(t, send(h, "carol", http.MethodDelete, k.path, "")); rev != "2" {
				t.Errorf("%s: DELETE %d answered %s, want 2", k.name, i+1, rev)
			}
		}
	}
}
