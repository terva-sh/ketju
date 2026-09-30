package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The admin users, groups and permissions routes (TKT-01M29CXK1X), through
// the real session middleware, as the admin world's viewers.

// call makes a request as viewer, with body as JSON when it is not empty.
func (w *adminWorld) call(viewer, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := w.tokens[viewer]; tok != "" {
		req.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec
}

// decodeAs decodes a 2xx answer strictly into T: a field the DTO lacks
// fails.
func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if rec.Code/100 != 2 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decoding %T: %v\n%s", v, err, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", cc)
	}
	return v
}

// problemType is the answer's problem type, the last segment of its URI.
func problemType(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("status %d, not a problem: %s", rec.Code, rec.Body)
	}
	return p.Type[strings.LastIndex(p.Type, "/")+1:]
}

// auditCount is how many audit rows have this action.
func (w *adminWorld) auditCount(action string) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT count(*) FROM audit_log WHERE action = ?`, action).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *adminWorld) exec(q string, args ...any) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), q, args...)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// TestUsersScreenAccess is AC6: a member is refused the list; a moderator
// may list accounts and read one, with no identities or sessions, and is
// refused every change and every groups and permissions route; an admin may
// do all of it and sees the identities.
func TestUsersScreenAccess(t *testing.T) {
	w := newAdminWorld(t)
	w.exec(`INSERT INTO identities (user_id, provider_key, subject, issuer, email, email_verified, created_at,
		updated_at) VALUES (1, 'github', 'member-1', 'https://github.com', 'member@example.org', 1, 1, 1)`)

	if rec := w.call("member", "GET", "/api/v1/admin/users", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a member listed accounts: %d", rec.Code)
	}

	list := decodeAs[dto.AdminUserList](t, w.call("moderator", "GET", "/api/v1/admin/users", ""))
	var handles []string
	for _, u := range list.Users {
		handles = append(handles, u.User.Handle+":"+u.Status)
	}
	if got := strings.Join(handles, " "); got != "admin:active member:active moderator:active scoped:active suspended:suspended" {
		t.Errorf("the moderator's list: %s", got)
	}

	mod := decodeAs[dto.AdminUserDetail](t, w.call("moderator", "GET", "/api/v1/admin/users/member", ""))
	if mod.Identities != nil || mod.Sessions != nil {
		t.Errorf("a moderator saw identities %v and sessions %v", mod.Identities, mod.Sessions)
	}
	if strings.Contains(w.call("moderator", "GET", "/api/v1/admin/users/member", "").Body.String(), "member@example.org") {
		t.Error("the moderator's answer carries the email")
	}
	adm := decodeAs[dto.AdminUserDetail](t, w.call("admin", "GET", "/api/v1/admin/users/member", ""))
	if len(adm.Identities) != 1 || adm.Identities[0].Email == nil || *adm.Identities[0].Email != "member@example.org" ||
		adm.Sessions == nil || *adm.Sessions != 1 {
		t.Errorf("the admin's detail: identities %+v, sessions %v", adm.Identities, adm.Sessions)
	}

	refused := []struct{ method, path, body string }{
		{"PUT", "/api/v1/admin/users/member/role", `{"role":"moderator"}`},
		{"GET", "/api/v1/admin/groups", ""},
		{"POST", "/api/v1/admin/groups", `{"slug":"x","name":"X","description":""}`},
		{"POST", fmt.Sprintf("/api/v1/admin/groups/%d/members", w.group(domain.GroupModerators)), `{"handle":"member"}`},
		{"GET", "/api/v1/admin/permissions", ""},
		{"POST", "/api/v1/admin/permissions", `{"changes":[]}`},
		{"POST", "/api/v1/admin/permissions/preview", `{"handle":"member","changes":[]}`},
	}
	for _, r := range refused {
		if rec := w.call("moderator", r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("a moderator's %s %s: %d, want 403", r.method, r.path, rec.Code)
		}
	}
	if role := w.role(1); role != domain.RoleMember {
		t.Errorf("after the moderator's attempt the member is %s", role)
	}

	got := decodeAs[dto.AdminUserDetail](t, w.call("admin", "PUT", "/api/v1/admin/users/member/role", `{"role":"moderator"}`))
	if got.User.User.Role != domain.RoleModerator || w.role(1) != domain.RoleModerator {
		t.Errorf("the admin's role change answered %s; stored %s", got.User.User.Role, w.role(1))
	}
	decodeAs[dto.GroupList](t, w.call("admin", "GET", "/api/v1/admin/groups", ""))
	decodeAs[dto.PermissionGrid](t, w.call("admin", "GET", "/api/v1/admin/permissions", ""))
}

func (w *adminWorld) role(id int64) string {
	w.t.Helper()
	var role string
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(w.t.Context(), id)
		role = u.Role
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return role
}

// The list's filters and paging: a filter value that is not one is
// validation-failed, and a page ends with a cursor that picks up after it.
func TestAdminUsersFiltersAndPages(t *testing.T) {
	w := newAdminWorld(t)
	for i := range 60 {
		w.exec(`INSERT INTO users (handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
			VALUES (?, 'Bot', 'active', 'member', 'agent', '{}', 1, 1)`, fmt.Sprintf("bot%02d", i))
	}
	agents := decodeAs[dto.AdminUserList](t, w.call("admin", "GET", "/api/v1/admin/users?kind=agent", ""))
	if len(agents.Users) != 50 || agents.NextCursor == "" {
		t.Fatalf("first page: %d users, cursor %q", len(agents.Users), agents.NextCursor)
	}
	rest := decodeAs[dto.AdminUserList](t, w.call("admin", "GET", "/api/v1/admin/users?kind=agent&cursor="+agents.NextCursor, ""))
	if len(rest.Users) != 10 || rest.NextCursor != "" || rest.Users[0].User.Handle != "bot50" {
		t.Errorf("second page: %d users from %s, cursor %q", len(rest.Users), rest.Users[0].User.Handle, rest.NextCursor)
	}
	pre := decodeAs[dto.AdminUserList](t, w.call("admin", "GET", "/api/v1/admin/users?prefix=BOT0&status=active", ""))
	if len(pre.Users) != 10 {
		t.Errorf("prefix BOT0: %d users, want 10", len(pre.Users))
	}
	for _, q := range []string{"role=owner", "status=gone", "kind=robot"} {
		if rec := w.call("admin", "GET", "/api/v1/admin/users?"+q, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("?%s: %d, want 422", q, rec.Code)
		}
	}
	if rec := w.call("admin", "GET", "/api/v1/admin/users?cursor=***", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed cursor: %d, want 400", rec.Code)
	}
}

// AC5 through the API: the only active admin cannot be demoted (the
// suspended admin does not count), and the refusal is conflict.
func TestCannotDemoteTheLastAdminThroughTheAPI(t *testing.T) {
	w := newAdminWorld(t)
	rec := w.call("admin", "PUT", "/api/v1/admin/users/admin/role", `{"role":"member"}`)
	if rec.Code != http.StatusConflict || problemType(t, rec) != "conflict" {
		t.Fatalf("demoting the last admin: %d %s", rec.Code, rec.Body)
	}
	if w.role(4) != domain.RoleAdmin {
		t.Fatal("the last admin was demoted")
	}
	if rec := w.call("admin", "PUT", "/api/v1/admin/users/member/role", `{"role":"owner"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown role: %d, want 422", rec.Code)
	}
	if rec := w.call("admin", "PUT", "/api/v1/admin/users/nobody/role", `{"role":"member"}`); rec.Code != http.StatusNotFound {
		t.Errorf("nobody: %d, want 404", rec.Code)
	}
}

// TestIdPMembershipNotEditable is AC2 through the API: the detail marks an
// IdP membership as such, and adding over it or removing it is conflict
// with the row left as it was.
func TestIdPMembershipNotEditableThroughTheAPI(t *testing.T) {
	w := newAdminWorld(t)
	staff := decodeAs[dto.Group](t, w.call("admin", "POST", "/api/v1/admin/groups", `{"slug":"staff","name":"Staff","description":""}`))
	if _, err := w.perm.SyncIdPGroups(t.Context(), 1, perm.IdPSync{Provider: "github", Slugs: []string{"staff"}}); err != nil {
		t.Fatal(err)
	}
	d := decodeAs[dto.AdminUserDetail](t, w.call("admin", "GET", "/api/v1/admin/users/member", ""))
	if len(d.Groups) != 1 || d.Groups[0].Source != domain.MembershipIdP || d.Groups[0].AddedBy != nil {
		t.Fatalf("the member's groups: %+v", d.Groups)
	}
	path := fmt.Sprintf("/api/v1/admin/groups/%d/members", staff.ID)
	for _, rec := range []*httptest.ResponseRecorder{
		w.call("admin", "POST", path, `{"handle":"member"}`),
		w.call("admin", "DELETE", path+"/member", ""),
	} {
		if rec.Code != http.StatusConflict {
			t.Errorf("editing an idp membership: %d, want 409", rec.Code)
		}
	}
	g := decodeAs[dto.GroupDetail](t, w.call("admin", "GET", fmt.Sprintf("/api/v1/admin/groups/%d", staff.ID), ""))
	if len(g.Members) != 1 || g.Members[0].Source != domain.MembershipIdP {
		t.Errorf("after refused edits: %+v", g.Members)
	}
}

// TestGroupDeleteOnlyWhenEmpty: a group with a member or a permission row,
// or a system group, is conflict and stays; an empty one is deleted with
// its audit row. Create and rename are audited too.
func TestGroupDeleteOnlyWhenEmpty(t *testing.T) {
	w := newAdminWorld(t)
	actions := []string{"group.created", "group.updated", "group.deleted", "group.member_added",
		"group.member_removed", "permission.granted", "permission.revoked"}
	before := map[string]int{} // the fixture's own writes are audited too
	for _, a := range actions {
		before[a] = w.auditCount(a)
	}
	g := decodeAs[dto.Group](t, w.call("admin", "POST", "/api/v1/admin/groups",
		`{"slug":"book-club","name":"  Book club ","description":"Reads."}`))
	if g.Name != "Book club" || g.Slug != "book-club" {
		t.Fatalf("created %+v", g)
	}
	path := fmt.Sprintf("/api/v1/admin/groups/%d", g.ID)
	if rec := w.call("admin", "POST", "/api/v1/admin/groups", `{"slug":"book-club","name":"Again","description":""}`); rec.Code != http.StatusConflict {
		t.Errorf("a taken slug: %d, want 409", rec.Code)
	}
	for _, bad := range []string{`{"slug":"Book Club","name":"B","description":""}`, `{"slug":"b","name":" ","description":""}`} {
		if rec := w.call("admin", "POST", "/api/v1/admin/groups", bad); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422", bad, rec.Code)
		}
	}
	renamed := decodeAs[dto.Group](t, w.call("admin", "PATCH", path, `{"name":"Readers"}`))
	if renamed.Name != "Readers" || renamed.Description != "Reads." {
		t.Errorf("renamed %+v", renamed)
	}

	decodeAs[dto.GroupDetail](t, w.call("admin", "POST", path+"/members", `{"handle":"member"}`))
	if rec := w.call("admin", "DELETE", path, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting a group with a member: %d, want 409", rec.Code)
	}
	decodeAs[dto.GroupDetail](t, w.call("admin", "DELETE", path+"/members/member", ""))
	change := fmt.Sprintf(`{"changes":[{"forum_id":null,"group_id":%d,"permission":"upload","grant":%%t}]}`, g.ID)
	decodeAs[dto.PermissionGrid](t, w.call("admin", "POST", "/api/v1/admin/permissions", fmt.Sprintf(change, true)))
	if rec := w.call("admin", "DELETE", path, ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting a group with a grant: %d, want 409", rec.Code)
	}
	decodeAs[dto.PermissionGrid](t, w.call("admin", "POST", "/api/v1/admin/permissions", fmt.Sprintf(change, false)))
	if rec := w.call("admin", "DELETE", path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the empty group: %d %s", rec.Code, rec.Body)
	}
	if rec := w.call("admin", "GET", path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("the deleted group: %d, want 404", rec.Code)
	}
	if rec := w.call("admin", "DELETE", fmt.Sprintf("/api/v1/admin/groups/%d", w.group(domain.GroupAdmins)), ""); rec.Code != http.StatusConflict {
		t.Errorf("deleting a system group: %d, want 409", rec.Code)
	}
	for _, a := range actions {
		if n := w.auditCount(a) - before[a]; n != 1 {
			t.Errorf("%d %s rows, want 1", n, a)
		}
	}
}

// AC4 for groups: a group change whose audit insert fails is not made.
func TestGroupWritesAuditedInOneTransaction(t *testing.T) {
	w := newAdminWorld(t)
	g := decodeAs[dto.Group](t, w.call("admin", "POST", "/api/v1/admin/groups", `{"slug":"staff","name":"Staff","description":""}`))
	path := fmt.Sprintf("/api/v1/admin/groups/%d", g.ID)
	w.exec(`CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
	for _, r := range []struct{ method, path, body string }{
		{"POST", "/api/v1/admin/groups", `{"slug":"crew","name":"Crew","description":""}`},
		{"PATCH", path, `{"name":"Renamed"}`},
		{"DELETE", path, ""},
	} {
		if rec := w.call("admin", r.method, r.path, r.body); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s with a failing audit insert: %d, want 500", r.method, r.path, rec.Code)
		}
	}
	w.exec(`DROP TRIGGER test_audit_fails`)
	list := decodeAs[dto.GroupList](t, w.call("admin", "GET", "/api/v1/admin/groups", ""))
	var names []string
	for _, x := range list.Groups {
		names = append(names, x.Slug+"="+x.Name)
	}
	if got := strings.Join(names, " "); got != "everyone=Everyone members=Members moderators=Moderators admins=Admins staff=Staff" {
		t.Errorf("after the failed writes the groups are %s", got)
	}
}

// TestPermissionChangeAppliesOnNextRequest is AC1: an admin's grid change
// applies to a member's next request, with no wait for the cache. The
// member reads the forum, the admin gives the forum rows of its own that
// leave read out, and the member's next request is refused; the preview
// said so first.
func TestPermissionChangeAppliesOnNextRequest(t *testing.T) {
	w := newAdminWorld(t)
	list := "/api/v1/forums/general/threads"
	if rec := w.call("member", "GET", list, ""); rec.Code != http.StatusOK {
		t.Fatalf("before: %d %s", rec.Code, rec.Body)
	}
	members := w.group(domain.GroupMembers)
	everyone := w.group(domain.GroupEveryone)
	body := fmt.Sprintf(`{"changes":[{"forum_id":%d,"group_id":%d,"permission":"reply","grant":true},
		{"forum_id":%d,"group_id":%d,"permission":"reply","grant":true}]}`, w.forum, members, w.forum, everyone)

	preview := decodeAs[dto.PermissionPreview](t, w.call("admin", "POST", "/api/v1/admin/permissions/preview",
		`{"handle":"member",`+strings.TrimPrefix(body, "{")))
	if len(preview.Forums) != 1 || preview.Forums[0].Before.Read != "allow" || preview.Forums[0].After.Read != "deny" {
		t.Fatalf("the preview: %+v", preview.Forums)
	}

	grid := decodeAs[dto.PermissionGrid](t, w.call("admin", "POST", "/api/v1/admin/permissions", body))
	local := 0
	for _, g := range grid.Grants {
		if g.ForumID != nil && *g.ForumID == w.forum {
			local++
		}
	}
	if local != 3 { // the fixture's moderate, and the two replies
		t.Errorf("%d rows on the forum after the batch, want 3", local)
	}
	if rec := w.call("member", "GET", list, ""); rec.Code != http.StatusForbidden {
		t.Errorf("after: %d, want 403 on the next request", rec.Code)
	}
	if rec := w.call("admin", "POST", "/api/v1/admin/permissions", body); rec.Code != http.StatusConflict {
		t.Errorf("the same batch again, against rows that moved: %d, want 409", rec.Code)
	}
	for _, bad := range []string{
		`{"changes":[{"forum_id":0,"group_id":1,"permission":"read","grant":true}]}`,
		`{"changes":[{"forum_id":null,"group_id":1,"permission":"admin","grant":true}]}`,
		`{"changes":[]}`,
	} {
		if rec := w.call("admin", "POST", "/api/v1/admin/permissions", bad); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422", bad, rec.Code)
		}
	}
}

// TestAdminDetailCarriesTheSuspension (TKT-01M3QMRE47): a suspended
// account's detail carries the suspension's reason and end, to a moderator
// and to an admin alike, no-store. A suspension with no end has until null,
// and the account's detail has suspension null once it is lifted, as an
// account never suspended has.
func TestAdminDetailCarriesTheSuspension(t *testing.T) {
	w := newWorld(t)
	bob := w.viewer("bob")
	admin := domain.Viewer{UserID: w.users["alice"], Role: domain.RoleAdmin, Active: true, SessionKey: "s-admin"}
	const path = "/api/v1/admin/users/carol"
	read := func(v domain.Viewer) dto.AdminUserDetail {
		t.Helper()
		return decodeAs[dto.AdminUserDetail](t, w.get(v, path))
	}

	for _, v := range []domain.Viewer{bob, admin} {
		if s := read(v).Suspension; s != nil {
			t.Errorf("%s reads an active account's suspension as %+v, want null", v.Role, *s)
		}
	}

	end := time.Now().Add(7 * 24 * time.Hour).UnixMilli()
	w.admin(bob, http.MethodPost, carolPosts+"suspend", fmt.Sprintf(`{"reason":"  spam, again ","until":%d}`, end))
	for _, v := range []domain.Viewer{bob, admin} {
		d := read(v)
		if d.User.Status != domain.StatusSuspended || d.Suspension == nil || d.Suspension.Reason != "spam, again" ||
			d.Suspension.Until == nil || int64(*d.Suspension.Until) != end {
			t.Errorf("%s reads the suspension as %+v (status %s), want the trimmed reason until %d", v.Role, d.Suspension,
				d.User.Status, end)
		}
	}

	w.admin(bob, http.MethodPost, carolPosts+"unsuspend", `{}`)
	if s := read(admin).Suspension; s != nil {
		t.Errorf("after the unsuspension the suspension is %+v, want null", *s)
	}

	w.admin(bob, http.MethodPost, carolPosts+"suspend", `{"reason":"until we talk","until":null}`)
	for _, v := range []domain.Viewer{bob, admin} {
		if s := read(v).Suspension; s == nil || s.Reason != "until we talk" || s.Until != nil {
			t.Errorf("%s reads a suspension with no end as %+v, want until null", v.Role, s)
		}
	}
}
