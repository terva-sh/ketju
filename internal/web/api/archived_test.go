package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for archived forums (TKT-01M3N24RHD, docs/04 "permissions"): posting
// is refused to everyone but an admin, as conflict; reading and moderating
// are not.

// newAdmin makes an admin account in w and returns it as a viewer. The chain's
// X-Test-Viewer names only members and bob, so an admin's requests go
// through sendAs.
func (w *world) newAdmin() domain.Viewer {
	w.t.Helper()
	var id int64
	w.write(func(q *dbq.Queries) error {
		var err error
		id, err = q.CreateUser(w.t.Context(), dbq.CreateUserParams{Handle: "dora", DisplayName: "Dora Admin",
			Status: "active", Role: domain.RoleAdmin, Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	w.users["dora"] = id
	return domain.Viewer{UserID: id, Role: domain.RoleAdmin, Active: true, SessionKey: "s-dora"}
}

// archive archives w's forum through perm, as the admin forum screens will.
func (w *world) archive() {
	w.t.Helper()
	if err := perm.New(w.db).SetForumArchived(w.t.Context(), audit.By{}, w.forum, true); err != nil {
		w.t.Fatal(err)
	}
}

// refused sends as who and wants the problem code want with the log of id
// left as it was.
func (w *world) refused(t *testing.T, id threadid.ID, h http.Handler, who, method, path, body, want string) {
	t.Helper()
	before := w.logState(id)
	rec := send(h, who, method, path, body)
	if got := problemCode(t, rec); got != want {
		t.Errorf("%s %s %s: %d %s, want %s", who, method, path, rec.Code, rec.Body, want)
	}
	if after := w.logState(id); after != before {
		t.Errorf("%s %s %s: a refusal changed the log: %+v then %+v", who, method, path, before, after)
	}
}

// TestArchivedForumConflicts is the second and fifth criteria. In an
// archived forum a new thread and a reply are conflict to a member and to a
// moderator, and 201 to an admin; a member's edit, delete and restore of
// their own post are conflict, and so is a moderator's edit of their own
// post; reading the thread and the list is unchanged. A restricted archived
// forum is still not-found to a viewer without read. No refusal appends.
func TestArchivedForumConflicts(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.post(id, "carol", "one I will take down", 0) // 5
	w.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: uint64(w.users["carol"]), Ordinal: 5,
		Body: &threadlog.DeleteBody{}})
	w.post(id, "bob", "a moderator's own words", 0) // 6
	admin := w.newAdmin()
	h := w.chain()

	threadPath := "/api/v1/threads/" + id.String()
	pageBefore := page(t, w.get(w.viewer("carol"), threadPath))
	listBefore := w.get(w.viewer("carol"), listPath("general", "", ""))
	if !pageBefore.Viewer.CanReply {
		t.Fatal("before the archive carol may not reply; the test needs her to")
	}

	w.archive()
	for _, c := range []struct{ who, method, path, body, want string }{
		{"carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`, "conflict"},
		{"bob", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`, "conflict"},
		{"carol", http.MethodPost, postsPath(id), `{"source":"late"}`, "conflict"},
		{"bob", http.MethodPost, postsPath(id), `{"source":"late"}`, "conflict"},
		{"", http.MethodPost, postsPath(id), `{"source":"late"}`, "unauthenticated"},
		{"carol", http.MethodPatch, postPathN(id, 4), `{"source":"changed"}`, "conflict"},
		{"carol", http.MethodDelete, postPathN(id, 4), "", "conflict"},
		{"carol", http.MethodPost, postPathN(id, 5) + "/restore", "", "conflict"},
		{"bob", http.MethodPatch, postPathN(id, 6), `{"source":"changed"}`, "conflict"},
		{"bob", http.MethodDelete, postPathN(id, 6), "", "conflict"},
	} {
		w.refused(t, id, h, c.who, c.method, c.path, c.body, c.want)
	}

	// Reading is unchanged, but for the page no longer offering a reply.
	pageAfter := page(t, w.get(w.viewer("carol"), threadPath))
	if pageAfter.Viewer.CanReply {
		t.Error("the thread page offers carol a reply in an archived forum")
	}
	pb, _ := json.Marshal(pageBefore.Posts)
	pa, _ := json.Marshal(pageAfter.Posts)
	if string(pb) != string(pa) {
		t.Errorf("the posts changed with the archive:\n%s\n%s", pb, pa)
	}
	if listAfter := w.get(w.viewer("carol"), listPath("general", "", "")); listAfter.Code != http.StatusOK ||
		listAfter.Body.String() != listBefore.Body.String() {
		t.Errorf("the thread list changed with the archive: %d\n%s\n%s", listAfter.Code, listBefore.Body, listAfter.Body)
	}

	// An admin still posts.
	created[dto.CreatedPost](t, w.sendAs(admin, http.MethodPost, postsPath(id), `{"source":"an admin's note"}`))
	created[dto.CreatedThread](t, w.sendAs(admin, http.MethodPost, threadsPath("general"), `{"title":"Closing notes","source":"s"}`))

	// Restricted and archived: not-found to a member without read, before
	// anything says it is archived.
	w.hideForum()
	hidden := newHandlerFor(w).chain()
	w.refused(t, id, hidden, "carol", http.MethodPost, postsPath(id), `{"source":"late"}`, "not-found")
	w.refused(t, id, hidden, "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`, "not-found")
	w.refused(t, id, hidden, "carol", http.MethodPatch, postPathN(id, 4), `{"source":"changed"}`, "not-found")
}

// TestArchivedRefusalIgnoresAStaleCache is the sixth criterion. carol's
// reply Allow is cached, then the forum is archived by a write that goes
// around SetForumArchived, as another process's would, so no cache is
// cleared. The cached decision still says Allow, and her next reply and new
// thread are conflict all the same, appending nothing, because the refusal
// reads forums.archived on every request.
func TestArchivedRefusalIgnoresAStaleCache(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	h := w.chain()
	carol := w.viewer("carol")
	created[dto.CreatedPost](t, send(h, "carol", http.MethodPost, postsPath(id), `{"source":"before"}`))
	if ok, err := w.svc.Perm.Can(t.Context(), carol, w.forum, domain.PermCreateThread); !ok || err != nil {
		t.Fatalf("create_thread before the archive: %v, %v", ok, err)
	}

	if err := w.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE forums SET archived = 1 WHERE id = ?`, w.forum)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []domain.Permission{domain.PermReply, domain.PermCreateThread} {
		if ok, err := w.svc.Perm.Can(t.Context(), carol, w.forum, p); !ok || err != nil {
			t.Fatalf("%s after the stray archive: %v, %v; the test needs the stale Allow still cached", p, ok, err)
		}
	}
	w.refused(t, id, h, "carol", http.MethodPost, postsPath(id), `{"source":"after"}`, "conflict")
	w.refused(t, id, h, "carol", http.MethodPost, threadsPath("general"), `{"title":"t","source":"s"}`, "conflict")
	if page(t, w.get(carol, "/api/v1/threads/"+id.String())).Viewer.CanReply {
		t.Error("the thread page offers a reply from the stale cache")
	}
}

// TestModeratorsStillModerateInArchivedForums is the seventh criterion as far
// as main has routes for it: in an archived forum a moderator hides and
// restores a member's post, and locks, pins and unlocks the thread. Each is
// appended. Moving a thread belongs to TKT-01M29CKRH9, which has no route
// yet.
func TestModeratorsStillModerateInArchivedForums(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.archive()
	h := w.chain()
	for _, c := range []struct{ method, path, body string }{
		{http.MethodDelete, postPathN(id, 4), `{"reason":"found later"}`},
		{http.MethodPost, postPathN(id, 4) + "/restore", ""},
		{http.MethodPost, modPath(id, "lock"), `{"reason":"archived"}`},
		{http.MethodPost, modPath(id, "pin"), `{"scope":"global"}`},
		{http.MethodPost, modPath(id, "unlock"), ``},
	} {
		before := w.logState(id)
		if rec := send(h, "bob", c.method, c.path, c.body); rec.Code != http.StatusOK {
			t.Errorf("bob %s %s: %d %s, want 200", c.method, c.path, rec.Code, rec.Body)
			continue
		}
		if after := w.logState(id); after.size <= before.size {
			t.Errorf("bob %s %s: nothing appended", c.method, c.path)
		}
	}
	// And the member still may not.
	w.refused(t, id, h, "carol", http.MethodPost, postsPath(id), `{"source":"late"}`, "conflict")
}
