package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the admin forum routes (TKT-01M29CXK19).

const adminForums = "/api/v1/admin/forums"

func forumPath(id int64, rest string) string { return fmt.Sprintf("%s/%d%s", adminForums, id, rest) }

// managedShape is the list as "slug(parent slug)" in order, with the flags that
// differ from a fresh forum's, for comparing whole lists in one line.
func managedShape(l dto.ManagedForumList) string {
	slugs := map[int64]string{}
	for _, f := range l.Forums {
		slugs[f.ID] = f.Slug
	}
	var out []string
	for _, f := range l.Forums {
		s := f.Slug
		if f.ParentID != nil {
			s += "(" + slugs[*f.ParentID] + ")"
		}
		if f.Archived {
			s += "+archived"
		}
		s += "/" + f.Visibility
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func (l forumLookup) id(slug string) int64 {
	for _, f := range l.Forums {
		if f.Slug == slug {
			return f.ID
		}
	}
	return 0
}

type forumLookup dto.ManagedForumList

// TestAdminForumsCRUD is AC1 through the routes: an admin creates a root and
// a child, renames one, moves one under a root and reorders the roots,
// archives and unarchives, and each write is audited. A second level of
// nesting is validation-failed, and the refusals say what they refuse.
func TestAdminForumsCRUD(t *testing.T) {
	w := newAdminWorld(t)
	list := decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))
	if got := managedShape(list); got != "general/public" {
		t.Fatalf("the fixture's forums: %s", got)
	}

	rec := w.call("admin", "POST", adminForums, `{"slug":"lounge","name":" Lounge ","description":"Off topic.","parent_id":null,"visibility":"members"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	list = decodeAs[dto.ManagedForumList](t, rec)
	lounge := forumLookup(list).id("lounge")
	// No route reads one forum, so the answer names none (review 1571).
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("Location %q, want none", loc)
	}
	body := fmt.Sprintf(`{"slug":"books","name":"Books","description":"","parent_id":%d,"visibility":""}`, w.forum)
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "POST", adminForums, body))
	books := forumLookup(list).id("books")
	if got := managedShape(list); got != "general/public books(general)/members lounge/members" {
		t.Errorf("after creating: %s", got)
	}
	for _, f := range list.Forums {
		if f.Slug == "lounge" && (f.Name != "Lounge" || f.Description != "Off topic.") {
			t.Errorf("lounge was stored as %+v", f)
		}
	}

	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "PATCH", forumPath(books, ""), `{"name":"Reading"}`))
	for _, f := range list.Forums {
		if f.ID == books && (f.Name != "Reading" || f.Slug != "books") {
			t.Errorf("after the rename: %+v", f)
		}
	}
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "POST", forumPath(lounge, "/move"),
		fmt.Sprintf(`{"parent_id":null,"before":%d}`, w.forum)))
	if got := managedShape(list); got != "lounge/members general/public books(general)/members" {
		t.Errorf("after reordering the roots: %s", got)
	}
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "POST", forumPath(books, "/move"),
		fmt.Sprintf(`{"parent_id":%d}`, lounge)))
	if got := managedShape(list); got != "lounge/members books(lounge)/members general/public" {
		t.Errorf("after moving under a root: %s", got)
	}
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "PUT", forumPath(lounge, "/archived"), `{"archived":true}`))
	if got := managedShape(list); got != "lounge+archived/members books(lounge)/members general/public" {
		t.Errorf("after archiving: %s", got)
	}
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "PUT", forumPath(lounge, "/archived"), `{"archived":false}`))
	if got := managedShape(list); got != "lounge/members books(lounge)/members general/public" {
		t.Errorf("after unarchiving: %s", got)
	}
	list = decodeAs[dto.ManagedForumList](t, w.call("admin", "PUT", forumPath(books, "/visibility"), `{"visibility":"restricted"}`))
	if got := managedShape(list); got != "lounge/members books(lounge)/restricted general/public" {
		t.Errorf("after restricting: %s", got)
	}
	for action, want := range map[string]int{forums.ActionCreated: 2, forums.ActionUpdated: 1, forums.ActionMoved: 2,
		perm.ActionForumArchived: 1, perm.ActionForumUnarchived: 1, perm.ActionForumVisibilityChanged: 1} {
		if got := w.auditCount(action); got != want {
			t.Errorf("%d %s rows, want %d", got, action, want)
		}
	}

	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"a second level", "POST", adminForums, fmt.Sprintf(`{"slug":"deep","name":"Deep","parent_id":%d}`, books), 422},
		{"a forum with children under a root", "POST", forumPath(lounge, "/move"), fmt.Sprintf(`{"parent_id":%d}`, w.forum), 422},
		{"a taken slug", "POST", adminForums, `{"slug":"lounge","name":"Again"}`, 409},
		{"a bad slug", "POST", adminForums, `{"slug":"Lounge!","name":"L"}`, 422},
		{"a slug ending in -", "POST", adminForums, `{"slug":"lounge-","name":"L"}`, 422},
		{"no name", "POST", adminForums, `{"slug":"empty","name":"  "}`, 422},
		{"an unknown visibility", "POST", adminForums, `{"slug":"secret","name":"S","visibility":"private"}`, 422},
		{"an unknown field", "POST", adminForums, `{"slug":"x","name":"X","position":1}`, 400},
		{"an empty edit", "PATCH", forumPath(books, ""), `{}`, 422},
		{"a slug edit", "PATCH", forumPath(books, ""), `{"slug":"novels"}`, 400},
		{"no such forum", "PATCH", forumPath(999, ""), `{"name":"X"}`, 404},
		{"no such forum to move", "POST", forumPath(999, "/move"), `{"parent_id":null}`, 404},
		// A move must name its parent: an omitted parent_id is not the top
		// level (review 1571).
		{"a move with no parent_id", "POST", forumPath(books, "/move"), fmt.Sprintf(`{"before":%d}`, books), 400},
		{"a move with an empty body", "POST", forumPath(books, "/move"), `{}`, 400},
		{"a parent_id that is not an id", "POST", forumPath(books, "/move"), `{"parent_id":"lounge"}`, 400},
		// Ids are positive: 0 and below are malformed, not missing (review
		// 1574).
		{"a parent_id of 0", "POST", forumPath(books, "/move"), `{"parent_id":0}`, 400},
		{"a negative parent_id", "POST", forumPath(books, "/move"), `{"parent_id":-1}`, 400},
		{"a negative before", "POST", forumPath(books, "/move"), `{"parent_id":null,"before":-1}`, 400},
		{"a negative after", "POST", forumPath(books, "/move"), `{"parent_id":null,"after":-1}`, 400},
		// And on create (review 1576).
		{"a create under parent 0", "POST", adminForums, `{"slug":"zero","name":"Z","parent_id":0}`, 400},
		{"a create under a negative parent", "POST", adminForums, `{"slug":"neg","name":"N","parent_id":-1}`, 400},
		{"no such forum to restrict", "PUT", forumPath(999, "/visibility"), `{"visibility":"public"}`, 404},
		{"no such forum to archive", "PUT", forumPath(999, "/archived"), `{"archived":true}`, 404},
		{"a bad id", "PATCH", adminForums + "/x", `{"name":"X"}`, 400},
		{"an unknown visibility change", "PUT", forumPath(books, "/visibility"), `{"visibility":"private"}`, 422},
		{"no visibility", "PUT", forumPath(books, "/visibility"), `{}`, 400},
		{"no archived", "PUT", forumPath(books, "/archived"), `{}`, 400},
	} {
		if rec := w.call("admin", c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if got := managedShape(decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))); got !=
		"lounge/members books(lounge)/restricted general/public" {
		t.Errorf("refusals changed the forums: %s", got)
	}
}

// TestAdminForumsAreAdminOnly is AC5: every /api/v1/admin/forums route is an
// admin's. A member, a moderator by role, and a member who holds moderate in
// the forum through the moderators group are each forbidden, and none of
// their writes changes anything. Admin screens keep their role gates under
// the forum-grant decision of 2026-09-30 (docs/08). TestAdminRoutesAreGated
// walks the same routes with the rest.
func TestAdminForumsAreAdminOnly(t *testing.T) {
	w := newAdminWorld(t)
	before := decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))
	calls := map[string]string{
		"GET " + adminForums:                       "",
		"POST " + adminForums:                      `{"slug":"lounge","name":"Lounge"}`,
		"PATCH " + forumPath(w.forum, ""):          `{"name":"Taken over"}`,
		"POST " + forumPath(w.forum, "/move"):      `{"parent_id":null}`,
		"PUT " + forumPath(w.forum, "/visibility"): `{"visibility":"restricted"}`,
		"PUT " + forumPath(w.forum, "/archived"):   `{"archived":true}`,
	}
	routes := 0
	for _, r := range w.h.adminRoutes {
		if strings.Contains(r.pattern, "/admin/forums") {
			routes++
			if r.gate.name != adminGate.name {
				t.Errorf("%s has the %s gate, want admin", r.pattern, r.gate.name)
			}
		}
	}
	if routes != len(calls) {
		t.Errorf("%d forum routes registered, and this test calls %d", routes, len(calls))
	}
	for call, body := range calls {
		method, path, _ := strings.Cut(call, " ")
		for _, viewer := range []string{"member", "moderator", "scoped"} {
			if rec := w.call(viewer, method, path, body); rec.Code != http.StatusForbidden {
				t.Errorf("%s as %s: %d, want 403", call, viewer, rec.Code)
			}
		}
		if rec := w.call("anonymous", method, path, body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymously: %d, want 401", call, rec.Code)
		}
	}
	after := decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))
	if b, a := renderJSON(t, before), renderJSON(t, after); b != a {
		t.Errorf("refused writes changed the forums:\n%s\n%s", b, a)
	}
}

// TestForumWritesAuditInTheSameTransaction is AC4: with the audit insert
// failing, a create, an edit, a move, a visibility change and an archive
// each fail, and the forums are as they were. perm's
// TestForumDecisionWritesAudited shows the decision cache is kept too.
func TestForumWritesAuditInTheSameTransaction(t *testing.T) {
	w := newAdminWorld(t)
	decodeAs[dto.ManagedForumList](t, w.call("admin", "POST", adminForums, `{"slug":"lounge","name":"Lounge"}`))
	before := decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))
	w.exec(`CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
	for _, r := range []struct{ method, path, body string }{
		{"POST", adminForums, `{"slug":"crew","name":"Crew"}`},
		{"PATCH", forumPath(w.forum, ""), `{"name":"Renamed","description":"New words."}`},
		{"POST", forumPath(w.forum, "/move"), `{"parent_id":null}`},
		{"PUT", forumPath(w.forum, "/visibility"), `{"visibility":"restricted"}`},
		{"PUT", forumPath(w.forum, "/archived"), `{"archived":true}`},
	} {
		if rec := w.call("admin", r.method, r.path, r.body); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s with a failing audit insert: %d, want 500", r.method, r.path, rec.Code)
		}
	}
	w.exec(`DROP TRIGGER test_audit_fails`)
	after := decodeAs[dto.ManagedForumList](t, w.call("admin", "GET", adminForums, ""))
	if b, a := renderJSON(t, before), renderJSON(t, after); b != a {
		t.Errorf("failed writes changed the forums:\n%s\n%s", b, a)
	}
}

// TestVisibilityChangeAppliesOnNextRequest is AC3's server half: a member
// lists the public forum and reads its threads, the admin makes it
// restricted, and the member's next requests neither list it nor read it,
// with no wait for the decision cache's minute.
func TestVisibilityChangeAppliesOnNextRequest(t *testing.T) {
	w := newAdminWorld(t)
	listed := func() bool {
		var tree dto.ForumTree
		rec := w.call("member", "GET", "/api/v1/forums", "")
		if err := json.Unmarshal(rec.Body.Bytes(), &tree); rec.Code != http.StatusOK || err != nil {
			t.Fatalf("GET /forums: %d %v %s", rec.Code, err, rec.Body)
		}
		for _, f := range tree.Forums {
			if f.Slug == "general" {
				return true
			}
		}
		return false
	}
	threads := "/api/v1/forums/general/threads"
	if !listed() {
		t.Fatal("before: the member does not see general")
	}
	if rec := w.call("member", "GET", threads, ""); rec.Code != http.StatusOK {
		t.Fatalf("before: %d %s", rec.Code, rec.Body)
	}
	decodeAs[dto.ManagedForumList](t, w.call("admin", "PUT", forumPath(w.forum, "/visibility"), `{"visibility":"restricted"}`))
	if listed() {
		t.Error("after: the member's next GET /forums still lists general")
	}
	if rec := w.call("member", "GET", threads, ""); rec.Code != http.StatusNotFound {
		t.Errorf("after: the member's next read is %d, want 404", rec.Code)
	}
}

// TestArchivedForumEffect is AC7's server half, through the admin route. In
// the archived forum: whoever read it still does, and its thread still
// reads; a member's and a moderator's new thread and reply are conflict; a
// moderator still locks, pins, hides a post and moves a thread out; moving a
// thread in is refused to the moderator and allowed to an admin; an admin
// still posts. Unarchiving restores posting.
func TestArchivedForumEffect(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	admin := w.newAdmin()
	h := w.chain()
	w.forumAt("lounge", "public", false)
	elsewhere := created[dto.CreatedThread](t, w.sendAs(admin, http.MethodPost, threadsPath("lounge"),
		`{"title":"Elsewhere","source":"s"}`))
	other, err := threadid.Parse(elsewhere.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	archived := func(on bool) {
		t.Helper()
		l := decodeAs[dto.ManagedForumList](t, w.sendAs(admin, http.MethodPut, forumPath(w.forum, "/archived"),
			fmt.Sprintf(`{"archived":%t}`, on)))
		for _, f := range l.Forums {
			if f.ID == w.forum && f.Archived != on {
				t.Fatalf("the list says general is archived=%t, want %t", f.Archived, on)
			}
		}
	}
	archived(true)

	// Read access is unchanged.
	var tree dto.ForumTree
	if err := json.Unmarshal(w.get(w.viewer("carol"), "/api/v1/forums").Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree.Forums) == 0 || tree.Forums[0].Slug != "general" || !tree.Forums[0].Archived {
		t.Errorf("carol's forum list after the archive: %+v", tree.Forums)
	}
	page(t, w.get(w.viewer("carol"), threadPath(id)))
	page(t, w.get(domain.Anonymous(), threadPath(id)))

	// Posting stops, moderators included.
	for _, who := range []string{"carol", "bob"} {
		w.refused(t, id, h, who, http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`, "conflict")
		w.refused(t, id, h, who, http.MethodPost, postsPath(id), `{"source":"late"}`, "conflict")
	}

	// Moderation does not.
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, modPath(id, "lock"), `{"reason":"archived"}`},
		{http.MethodPost, modPath(id, "pin"), `{"scope":"global"}`},
		{http.MethodDelete, postPathN(id, 4), `{"reason":"found later"}`},
	} {
		if rec := send(h, "bob", c.method, c.path, c.body); rec.Code != http.StatusOK {
			t.Errorf("bob %s %s in the archived forum: %d %s, want 200", c.method, c.path, rec.Code, rec.Body)
		}
	}
	w.refused(t, other, h, "bob", http.MethodPost, modPath(other, "move"), moveTo("general"), "conflict")
	if got := header(t, send(h, "bob", http.MethodPost, modPath(id, "move"), moveTo("lounge"))); got.Forum.Slug != "lounge" {
		t.Errorf("bob's move out of the archived forum answered %+v", got)
	}
	if got := header(t, w.sendAs(admin, http.MethodPost, modPath(other, "move"), moveTo("general"))); got.Forum.Slug != "general" {
		t.Errorf("the admin's move into the archived forum answered %+v", got)
	}
	created[dto.CreatedPost](t, w.sendAs(admin, http.MethodPost, postsPath(other), `{"source":"an admin's note"}`))
	created[dto.CreatedThread](t, w.sendAs(admin, http.MethodPost, threadsPath("general"), `{"title":"Closing notes","source":"s"}`))

	archived(false)
	created[dto.CreatedPost](t, send(h, "carol", http.MethodPost, postsPath(other), `{"source":"open again"}`))
	created[dto.CreatedThread](t, send(h, "carol", http.MethodPost, threadsPath("general"), `{"title":"Back","source":"s"}`))
}
