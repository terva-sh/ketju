package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The application review routes (TKT-01M29CP2MG), through the real session
// middleware, as the admin world's viewers. The admin world's identity 1 is
// the pre-user's; each test gives it the applications it needs.

// application inserts an application for identity 1 and returns its id.
func (w *adminWorld) application(status string, createdAt int64) int64 {
	w.t.Helper()
	decided := "NULL"
	if status != "pending" {
		decided = fmt.Sprint(createdAt + 1)
	}
	w.exec(`INSERT INTO applications (identity_id, form_id, form_version, answers_json, status, created_at, decided_at)
		VALUES (1, 1, 1, '{"who":"a friend"}', ?, ?, `+decided+`)`, status, createdAt)
	return int64(w.countRows(`SELECT max(id) FROM applications`))
}

func (w *adminWorld) countRows(q string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), q, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// reviewRoutes are the six routes, for application id.
func reviewRoutes(id int64) []struct{ method, path, body string } {
	p := fmt.Sprintf("/api/v1/admin/applications/%d", id)
	return []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/applications", ""},
		{"GET", p, ""},
		{"POST", p + "/notes", `{"body":"a note"}`},
		{"POST", p + "/approve", `{"handle":"newcomer"}`},
		{"POST", p + "/deny", `{"note":"no","block":true}`},
		{"POST", p + "/unblock", ``},
	}
}

// AC7: the review routes are staff's. Anonymous and a suspended moderator
// have no member session and are 401 (member.Require; TestAdminRoutesAreGated
// holds every admin route to it); a member, a pre-user and a member who
// moderates one forum by group are 403. Nothing a refused viewer sends is
// written, and every refusal is no-store.
func TestReviewIsStaffOnly(t *testing.T) {
	w := newAdminWorld(t)
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (6, 'suspmod', 'suspmod', 'suspended', 'moderator', 'human', '{}', 1, 1)`)
	w.tokens["suspended-moderator"] = sessiontest.SignIn(t, w.db, 6, w.now)
	id := w.application("pending", 1)
	for _, viewer := range []string{"anonymous", "suspended-moderator", "pre-user", "member", "scoped"} {
		want := http.StatusForbidden
		if viewer == "anonymous" || viewer == "suspended-moderator" {
			want = http.StatusUnauthorized
		}
		for _, r := range reviewRoutes(id) {
			rec := w.call(viewer, r.method, r.path, r.body)
			if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s as %s: %d (%s), want %d no-store", r.method, r.path, viewer, rec.Code,
					rec.Header().Get("Cache-Control"), want)
			}
		}
	}
	if w.countRows(`SELECT count(*) FROM applications WHERE status = 'pending'`) != 1 ||
		w.countRows(`SELECT count(*) FROM application_notes`) != 0 ||
		w.countRows(`SELECT count(*) FROM identities WHERE blocked_at IS NOT NULL`) != 0 {
		t.Error("a refused viewer changed something")
	}
	for _, viewer := range []string{"moderator", "admin"} {
		decodeAs[dto.AdminApplicationList](t, w.call(viewer, "GET", "/api/v1/admin/applications", ""))
		decodeAs[dto.AdminApplication](t, w.call(viewer, "GET", fmt.Sprintf("/api/v1/admin/applications/%d", id), ""))
	}
}

// Every answer the review routes give is no-store, a success included, and
// each success decodes strictly into its DTO.
func TestReviewResponsesAreNoStore(t *testing.T) {
	w := newAdminWorld(t)
	id := w.application("pending", 1)
	path := fmt.Sprintf("/api/v1/admin/applications/%d", id)
	decodeAs[dto.AdminApplicationList](t, w.call("moderator", "GET", "/api/v1/admin/applications?status=all", ""))
	decodeAs[dto.AdminApplication](t, w.call("moderator", "GET", path, ""))
	note := w.call("moderator", "POST", path+"/notes", `{"body":"known to the book club"}`)
	if note.Code != http.StatusCreated {
		t.Fatalf("the note: %d %s", note.Code, note.Body)
	}
	decodeAs[dto.ApplicationNote](t, note)
	denied := decodeAs[dto.AdminApplication](t, w.call("moderator", "POST", path+"/deny", `{"note":"spam","block":true}`))
	if denied.Identity == nil || !denied.Identity.Blocked || denied.Status != "denied" {
		t.Errorf("the denial: %+v", denied)
	}
	unblocked := decodeAs[dto.AdminApplication](t, w.call("admin", "POST", path+"/unblock", ""))
	if unblocked.Identity == nil || unblocked.Identity.Blocked {
		t.Errorf("the unblock: %+v", unblocked.Identity)
	}
	again := w.call("admin", "POST", path+"/unblock", "{}")
	if again.Code != http.StatusConflict || again.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a second unblock: %d %s", again.Code, again.Header().Get("Cache-Control"))
	}
	decided := w.call("admin", "POST", path+"/approve", `{"handle":"late"}`)
	if decided.Code != http.StatusConflict || problemType(t, decided) != "conflict" {
		t.Errorf("approving a denied application: %d %s", decided.Code, decided.Body)
	}
}

// AC3 at the API: approval takes no role. A body naming one is bad-request,
// and nothing is written.
func TestApproveRefusesARole(t *testing.T) {
	w := newAdminWorld(t)
	id := w.application("pending", 1)
	before := w.auditCount("application.approved")
	rec := w.call("admin", "POST", fmt.Sprintf("/api/v1/admin/applications/%d/approve", id), `{"handle":"newcomer","role":"admin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a role in the body: %d %s", rec.Code, rec.Body)
	}
	if w.countRows(`SELECT count(*) FROM users WHERE handle = 'newcomer'`) != 0 || w.auditCount("application.approved") != before {
		t.Error("the refused approval wrote something")
	}
	// The control: without the role it approves, as a member.
	out := decodeAs[dto.AdminApplication](t, w.call("admin", "POST", fmt.Sprintf("/api/v1/admin/applications/%d/approve", id),
		`{"handle":"newcomer"}`))
	if out.Decision == nil || out.Decision.User == nil || out.Decision.User.Role != "member" {
		t.Errorf("the approval: %+v", out.Decision)
	}
	if got := w.auditCount("application.approved") - before; got != 1 {
		t.Errorf("%d approval rows", got)
	}
	if w.auditCount("user.promoted") != 0 {
		t.Error("approval wrote user.promoted")
	}
	// A second applicant asking for the handle in another case.
	w.exec(`INSERT INTO identities (id, provider_key, subject, issuer, created_at, updated_at)
		VALUES (2, 'github', 'second', 'https://github.com', 1, 1)`)
	w.exec(`INSERT INTO applications (identity_id, form_id, form_version, answers_json, status, created_at)
		VALUES (2, 1, 1, '{}', 'pending', 5)`)
	second := w.countRows(`SELECT id FROM applications WHERE identity_id = 2`)
	taken := w.call("admin", "POST", fmt.Sprintf("/api/v1/admin/applications/%d/approve", second), `{"handle":"NEWCOMER"}`)
	var p problem.Problem
	if taken.Code != http.StatusUnprocessableEntity || json.Unmarshal(taken.Body.Bytes(), &p) != nil ||
		len(p.Errors) != 1 || p.Errors[0] != (problem.FieldError{Field: "handle", Code: "taken"}) {
		t.Errorf("a taken handle: %d %s", taken.Code, taken.Body)
	}
}

// The detail shows the identity's other applications, newest first, and
// the moderators' notes with their authors, oldest first. A note must say
// something.
func TestPriorApplicationsAndNotes(t *testing.T) {
	w := newAdminWorld(t)
	w.exec(`UPDATE identities SET email = 'applicant@example.org', email_verified = 1 WHERE id = 1`)
	old := w.application("withdrawn", 1)
	older := w.application("denied", 0)
	id := w.application("pending", 5)
	path := fmt.Sprintf("/api/v1/admin/applications/%d", id)
	for _, body := range []string{`{"body":"first"}`, `{"body":"second"}`} {
		if rec := w.call("moderator", "POST", path+"/notes", body); rec.Code != http.StatusCreated {
			t.Fatalf("note %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	empty := w.call("moderator", "POST", path+"/notes", `{"body":"  "}`)
	var p problem.Problem
	if empty.Code != http.StatusUnprocessableEntity || json.Unmarshal(empty.Body.Bytes(), &p) != nil ||
		len(p.Errors) != 1 || p.Errors[0] != (problem.FieldError{Field: "body", Code: "required"}) {
		t.Errorf("an empty note: %d %s", empty.Code, empty.Body)
	}
	got := decodeAs[dto.AdminApplication](t, w.call("admin", "GET", path, ""))
	if len(got.Prior) != 2 || got.Prior[0].ID != old || got.Prior[1].ID != older || got.Prior[0].Status != "withdrawn" {
		t.Errorf("prior: %+v", got.Prior)
	}
	if len(got.Notes) != 2 || got.Notes[0].Body != "first" || got.Notes[1].Body != "second" ||
		got.Notes[0].Author.Handle != "moderator" {
		t.Errorf("notes: %+v", got.Notes)
	}
	if got.Identity == nil || got.Identity.Provider != "github" || got.Identity.Email == nil ||
		*got.Identity.Email != "applicant@example.org" || !got.Identity.EmailVerified {
		t.Errorf("identity: %+v", got.Identity)
	}
	if rec := w.call("moderator", "GET", "/api/v1/admin/applications/999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an application that does not exist: %d", rec.Code)
	}
	if rec := w.call("moderator", "GET", "/api/v1/admin/applications/x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("an id that is not one: %d", rec.Code)
	}
}

// AC10: after a denial with a distinctive note, no route answers the
// applicant's pre-user session with it. Every route the API serves is
// walked as the pre-user, through the server's chain; the pre-user gate
// refuses most, and the few it lets through must not carry the note. The
// staff view carries it, so the search is not vacuous.
func TestTheNoteNeverReachesTheApplicant(t *testing.T) {
	w := newAdminWorld(t)
	id := w.application("pending", 1)
	const note = "zebra-quartz-7731 for the moderators"
	path := fmt.Sprintf("/api/v1/admin/applications/%d", id)
	if rec := w.call("moderator", "POST", path+"/deny", `{"note":"`+note+`","block":false}`); rec.Code != http.StatusOK {
		t.Fatalf("the denial: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(w.call("moderator", "GET", path, "").Body.String(), "zebra-quartz-7731") {
		t.Fatal("the staff view does not carry the note, so the search below proves nothing")
	}

	status := w.call("pre-user", "GET", "/api/v1/apply/status", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"status":"denied"`) {
		t.Fatalf("the applicant's status: %d %s", status.Code, status.Body)
	}
	walked := 0
	for _, rt := range w.h.routes() {
		for _, body := range []string{"", "{}"} {
			rec := w.call("pre-user", rt.Method, pathParam.ReplaceAllString(rt.Path, fmt.Sprint(id)), body)
			walked++
			if strings.Contains(rec.Body.String(), "zebra") {
				t.Errorf("%s %s answered the pre-user with the note: %s", rt.Method, rt.Path, rec.Body)
			}
		}
	}
	for _, a := range w.h.adminRoutes {
		method, p, _ := strings.Cut(a.pattern, " ")
		rec := w.call("pre-user", method, pathParam.ReplaceAllString(p, fmt.Sprint(id)), "{}")
		walked++
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "zebra") {
			t.Errorf("%s as the pre-user: %d %s", a.pattern, rec.Code, rec.Body)
		}
	}
	if walked < 40 {
		t.Errorf("walked only %d requests", walked)
	}
}
