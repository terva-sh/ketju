package applications

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The review world adds a moderator (2) and a member (3) to the admin (1)
// newWorld seeds.
var (
	moderator = domain.Viewer{UserID: 2, Role: domain.RoleModerator, Active: true}
	admin     = domain.Viewer{UserID: 1, Role: domain.RoleAdmin, Active: true}
	aMember   = domain.Viewer{UserID: 3, Role: domain.RoleMember, Active: true}
)

func newReviewWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (2, 'mod', 'Moderator', 'active', 'moderator', 'human', '{}', 1, 1),
		       (3, 'plain', 'Member', 'active', 'member', 'human', '{}', 1, 1)`)
	return w
}

// pendingApp submits an application for a new applicant and returns the
// applicant and the application's id.
func (w *world) pendingApp(subject string) (domain.Viewer, int64) {
	w.t.Helper()
	v := w.applicant(subject)
	if err := w.submit(v); err != nil {
		w.t.Fatal(err)
	}
	var id int64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT id FROM applications WHERE identity_id = ? AND status = 'pending'`,
			v.IdentityID).Scan(&id)
	}); err != nil {
		w.t.Fatal(err)
	}
	return v, id
}

func (w *world) status(id int64) string {
	w.t.Helper()
	var s string
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT status FROM applications WHERE id = ?`, id).Scan(&s)
	}); err != nil {
		w.t.Fatal(err)
	}
	return s
}

// auditRows are the audit rows with action about target, as (actor, details).
type auditRow struct {
	actor   sql.NullInt64
	details map[string]any
	ip      string
}

func (w *world) audits(action, targetType string, targetID int64) []auditRow {
	w.t.Helper()
	var out []auditRow
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(w.t.Context(), `SELECT actor_id, details_json, ip FROM audit_log
			WHERE action = ? AND target_type = ? AND target_id = ?`, action, targetType, strconv.FormatInt(targetID, 10))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			var dj string
			if err := rows.Scan(&r.actor, &dj, &r.ip); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(dj), &r.details); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (w *world) allAudits() int { return w.count(`SELECT count(*) FROM audit_log`) }

// fieldCode is the one field error err carries, as "field code".
func fieldCode(err error) string {
	var inv *forms.Invalid
	if !errors.As(err, &inv) || len(inv.Fields) != 1 {
		return fmt.Sprintf("not one field error: %v", err)
	}
	return inv.Fields[0].Field + " " + inv.Fields[0].Code
}

// AC1 and AC8: an approval creates the user (active, member, human), links
// the identity, and marks the application approved with the user's id, with
// one audit row naming the moderator and the note. When the audit insert
// fails, none of it is left. A success at all proves the decision UPDATE
// runs after the user exists: the table's CHECK refuses an approved row
// with no user_id (review 1344).
func TestApproveIsOneTransactionOrNone(t *testing.T) {
	w := newReviewWorld(t)

	// First the failure: a trigger refuses the approval's audit row.
	v, id := w.pendingApp("failing")
	w.exec(`CREATE TRIGGER fail_approval_audit BEFORE INSERT ON audit_log WHEN NEW.action = 'application.approved'
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	users := w.count(`SELECT count(*) FROM users`)
	if _, err := w.svc.Approve(t.Context(), moderator, id, "failing", "known", "192.0.2.9"); err == nil {
		t.Fatal("the approval succeeded with its audit row refused")
	}
	if n := w.count(`SELECT count(*) FROM users`); n != users {
		t.Errorf("users went from %d to %d", users, n)
	}
	if n := w.count(`SELECT count(*) FROM identities WHERE id = ? AND user_id IS NULL`, v.IdentityID); n != 1 {
		t.Error("the identity was linked")
	}
	if s := w.status(id); s != StatusPending {
		t.Errorf("the application is %s", s)
	}
	if n := w.allAudits(); n != 0 {
		t.Errorf("%d audit rows", n)
	}
	w.exec(`DROP TRIGGER fail_approval_audit`)

	// Then the success.
	out, err := w.svc.Approve(t.Context(), moderator, id, " Failing ", "known to alice", "192.0.2.9")
	if err != nil {
		t.Fatal(err)
	}
	var u struct {
		id                                  int64
		handle, display, status, role, kind string
		appUser, identityUser, decidedBy    sql.NullInt64
	}
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT u.id, u.handle, u.display_name, u.status, u.role, u.kind,
			a.user_id, i.user_id, a.decided_by
			FROM applications a JOIN identities i ON i.id = a.identity_id JOIN users u ON u.handle = 'failing'
			WHERE a.id = ?`, id).Scan(&u.id, &u.handle, &u.display, &u.status, &u.role, &u.kind, &u.appUser,
			&u.identityUser, &u.decidedBy)
	}); err != nil {
		t.Fatal(err)
	}
	if u.status != domain.StatusActive || u.role != domain.RoleMember || u.kind != domain.KindHuman {
		t.Errorf("the user is %s %s %s, want active member human", u.status, u.role, u.kind)
	}
	if u.display != "Carol failing" {
		t.Errorf("display name %q, want the IdP's name claim", u.display)
	}
	if u.appUser.Int64 != u.id || u.identityUser.Int64 != u.id || u.decidedBy.Int64 != moderator.UserID {
		t.Errorf("application user %v, identity user %v, decided by %v; want user %d by %d",
			u.appUser, u.identityUser, u.decidedBy, u.id, moderator.UserID)
	}
	if s := w.status(id); s != StatusApproved {
		t.Errorf("the application is %s", s)
	}
	rows := w.audits(ActionApproved, "application", id)
	if len(rows) != 1 || rows[0].actor.Int64 != moderator.UserID || rows[0].details["note"] != "known to alice" ||
		rows[0].details["handle"] != "failing" || rows[0].details["user_id"] != float64(u.id) ||
		rows[0].details["identity_id"] != float64(v.IdentityID) || rows[0].ip != "192.0.2.9" {
		t.Errorf("audit rows: %+v", rows)
	}
	if out.Status != StatusApproved || out.Decision == nil || out.Decision.User == nil || out.Decision.User.Handle != "failing" ||
		out.Decision.By == nil || out.Decision.By.Handle != "mod" || out.Decision.Note != "known to alice" {
		t.Errorf("the answer: %+v", out)
	}
}

// AC2, as the maintainer decided on TKT-01M3Q2V25E: approval adds no session
// and deletes the applicant's pre-user rows; the old cookie resolves as
// nobody and is cleared; the member session comes from the next sign-in,
// through session.IssueUser.
func TestApproveIssuesNoSession(t *testing.T) {
	w := newReviewWorld(t)
	store := &session.Store{DB: w.db, Now: func() time.Time { return w.now }}
	v, id := w.pendingApp("carol")
	token, _, err := store.IssuePreUser(t.Context(), v.IdentityID, session.Meta{IP: "192.0.2.1", Provider: "github"})
	if err != nil {
		t.Fatal(err)
	}
	// A second applicant's pre-user session, which approval must not touch.
	other, _ := w.pendingApp("dave")
	if _, _, err := store.IssuePreUser(t.Context(), other.IdentityID, session.Meta{Provider: "github"}); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM sessions`); n != 2 {
		t.Fatalf("%d sessions before approval", n)
	}

	if _, err := w.svc.Approve(t.Context(), moderator, id, "carol", "", ""); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM sessions`); n != 1 {
		t.Errorf("%d sessions after approval, want only the other applicant's", n)
	}
	if n := w.count(`SELECT count(*) FROM sessions WHERE identity_id = ? AND kind = 'pre_user'`, other.IdentityID); n != 1 {
		t.Error("approval removed another applicant's session")
	}

	var seen domain.Viewer
	h := session.Middleware(store, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = domain.ViewerFrom(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apply/status", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != (domain.Viewer{}) {
		t.Errorf("the retired pre-user session resolved as %+v", seen)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].Name != session.CookieName || c[0].MaxAge >= 0 {
		t.Errorf("the cookie was not cleared: %v", c)
	}

	ids := &identity.Service{DB: w.db, Sessions: store, Perm: perm.New(w.db), Now: func() time.Time { return w.now }}
	res, err := ids.Login(t.Context(), &oidc.Identity{Provider: "github", Issuer: "https://github.com", Subject: "carol"},
		domain.Viewer{}, session.Meta{Provider: "github"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != identity.SignedIn || res.Token == "" || res.Session.Kind != session.KindUser {
		t.Errorf("the sign-in after approval: %+v", res)
	}
	if n := w.count(`SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.kind = 'user' AND u.handle = 'carol'`); n != 1 {
		t.Errorf("%d member sessions for carol after signing in", n)
	}
}

// AC3: an approved member suspended before they sign in is refused by
// IssueUser, and gets no session.
func TestApprovedThenSuspendedCannotSignIn(t *testing.T) {
	w := newReviewWorld(t)
	_, id := w.pendingApp("erin")
	if _, err := w.svc.Approve(t.Context(), moderator, id, "erin", "", ""); err != nil {
		t.Fatal(err)
	}
	w.exec(`UPDATE users SET status = 'suspended', status_version = status_version + 1 WHERE handle = 'erin'`)
	store := &session.Store{DB: w.db, Now: func() time.Time { return w.now }}
	ids := &identity.Service{DB: w.db, Sessions: store, Perm: perm.New(w.db), Now: func() time.Time { return w.now }}
	res, err := ids.Login(t.Context(), &oidc.Identity{Provider: "github", Issuer: "https://github.com", Subject: "erin"},
		domain.Viewer{}, session.Meta{Provider: "github"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != identity.Unavailable || res.Status != domain.StatusSuspended || res.Token != "" {
		t.Errorf("a suspended member signed in: %+v", res)
	}
	if n := w.count(`SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("%d sessions", n)
	}
}

// AC3's second half: approval makes a member and never another role, and
// writes no user.promoted row. The API refuses a role field (400) before
// this is reached.
func TestApproveNeverSetsARole(t *testing.T) {
	w := newReviewWorld(t)
	for _, by := range []domain.Viewer{moderator, admin} {
		sub := "role" + strconv.FormatInt(by.UserID, 10)
		_, id := w.pendingApp(sub)
		if _, err := w.svc.Approve(t.Context(), by, id, sub, "", ""); err != nil {
			t.Fatal(err)
		}
		if n := w.count(`SELECT count(*) FROM users WHERE handle = ? AND role = 'member'`, sub); n != 1 {
			t.Errorf("approved by %s: the user is not a member", by.Role)
		}
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE action = 'user.promoted'`); n != 0 {
		t.Errorf("%d user.promoted rows", n)
	}
}

// The handle passes registration's checks, HandleFree's included, in the
// approval's transaction. Every refusal is validation-failed on the handle
// field, and leaves the application pending with nothing written.
func TestApproveChecksTheHandle(t *testing.T) {
	w := newReviewWorld(t)
	_, id := w.pendingApp("frank")
	w.exec(`INSERT INTO handle_releases (handle, user_id, released_at, reserved_until) VALUES ('gone', NULL, 1, ?)`,
		w.now.Add(time.Hour).UnixMilli())
	for _, c := range []struct{ handle, want string }{
		{"a", "handle invalid_handle"},
		{"has space", "handle invalid_handle"},
		{"admin", "handle reserved"},
		{"deleted-user-9", "handle reserved"},
		{"ROOT", "handle taken"},
		{"Mod", "handle taken"},
		{"gone", "handle taken"},
	} {
		_, err := w.svc.Approve(t.Context(), moderator, id, c.handle, "", "")
		if got := fieldCode(err); got != c.want || problem.FromError(err) != problem.CodeValidationFailed {
			t.Errorf("%q: %s (%s), want %s", c.handle, got, problem.FromError(err), c.want)
		}
	}
	if s := w.status(id); s != StatusPending {
		t.Errorf("the application is %s", s)
	}
	if n := w.count(`SELECT count(*) FROM users`); n != 3 {
		t.Errorf("%d users", n)
	}
	if n := w.allAudits(); n != 0 {
		t.Errorf("%d audit rows", n)
	}
}

// AC4: deny with block sets the marker with user_id NULL, and the identity's
// form and submit are then forbidden.
func TestDenyWithBlockRefusesReapplication(t *testing.T) {
	w := newReviewWorld(t)
	v, id := w.pendingApp("gina")
	if _, err := w.svc.Deny(t.Context(), moderator, id, "spam", true, ""); err != nil {
		t.Fatal(err)
	}
	if n := w.count(`SELECT count(*) FROM identities WHERE id = ? AND blocked_at = ? AND blocked_by = ? AND user_id IS NULL`,
		v.IdentityID, w.now.UnixMilli(), moderator.UserID); n != 1 {
		t.Error("the identity is not blocked as it should be")
	}
	w.now = w.now.Add(48 * time.Hour)
	_, err := w.svc.Form(t.Context(), v)
	wantCode(t, "the form", err, ErrBlocked, problem.CodeForbidden)
	err = w.submit(v)
	wantCode(t, "the submit", err, ErrBlocked, problem.CodeForbidden)
}

// A denial needs a note, of at most 2000 characters; an approval's note is
// optional but bounded the same.
func TestDenyRequiresANote(t *testing.T) {
	w := newReviewWorld(t)
	_, id := w.pendingApp("hana")
	long := strings.Repeat("é", NoteMaxLen+1)
	for _, c := range []struct{ note, want string }{
		{"", "note required"}, {"  \n ", "note required"}, {long, "note too_long"}, {"bell\a", "note invalid_text"},
	} {
		_, err := w.svc.Deny(t.Context(), moderator, id, c.note, false, "")
		if got := fieldCode(err); got != c.want {
			t.Errorf("deny with %q: %s, want %s", c.note[:min(len(c.note), 8)], got, c.want)
		}
	}
	if _, err := w.svc.Approve(t.Context(), moderator, id, "hana", long, ""); fieldCode(err) != "note too_long" {
		t.Errorf("approve with a long note: %v", err)
	}
	if s := w.status(id); s != StatusPending || w.allAudits() != 0 {
		t.Errorf("the application is %s with %d audit rows", s, w.allAudits())
	}
	if _, err := w.svc.Deny(t.Context(), moderator, id, strings.Repeat("é", NoteMaxLen), false, ""); err != nil {
		t.Errorf("a note of exactly %d characters: %v", NoteMaxLen, err)
	}
}

// AC11: a block needs exactly one identity with no user. When the identity
// was removed, or has a user, deny with block is conflict and rolls back,
// and a plain deny of the same application still succeeds.
func TestBlockNeedsAnIdentity(t *testing.T) {
	w := newReviewWorld(t)
	for _, c := range []struct {
		name   string
		break_ func(v domain.Viewer)
	}{
		{"removed", func(v domain.Viewer) { w.exec(`DELETE FROM identities WHERE id = ?`, v.IdentityID) }},
		{"linked", func(v domain.Viewer) { w.exec(`UPDATE identities SET user_id = 3 WHERE id = ?`, v.IdentityID) }},
	} {
		v, id := w.pendingApp("ivy-" + c.name)
		c.break_(v)
		before := w.allAudits()
		_, err := w.svc.Deny(t.Context(), moderator, id, "no", true, "")
		wantCode(t, c.name, err, ErrIdentityGone, problem.CodeConflict)
		if s := w.status(id); s != StatusPending {
			t.Errorf("%s: the application is %s", c.name, s)
		}
		if n := w.count(`SELECT count(*) FROM identities WHERE blocked_at IS NOT NULL`); n != 0 {
			t.Errorf("%s: %d blocked identities", c.name, n)
		}
		if w.allAudits() != before {
			t.Errorf("%s: an audit row was written", c.name)
		}
		if _, err := w.svc.Deny(t.Context(), moderator, id, "no", false, ""); err != nil {
			t.Fatalf("%s: the plain deny: %v", c.name, err)
		}
		if rows := w.audits(ActionDenied, "application", id); len(rows) != 1 || rows[0].details["blocked"] != false {
			t.Errorf("%s: audit rows %+v", c.name, rows)
		}
	}
}

// AC9: staff lift a block; the application stays denied, one
// identity.unblocked row names the actor, the form answers under the
// 24-hour rule instead of 403, and a second unblock is conflict. A member
// is forbidden.
func TestUnblockLiftsTheBlock(t *testing.T) {
	w := newReviewWorld(t)
	v, id := w.pendingApp("jo")
	if _, err := w.svc.Deny(t.Context(), moderator, id, "spam", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Unblock(t.Context(), aMember, id, ""); problem.FromError(err) != problem.CodeForbidden {
		t.Errorf("a member's unblock: %v", err)
	}

	out, err := w.svc.Unblock(t.Context(), admin, id, "192.0.2.4")
	if err != nil {
		t.Fatal(err)
	}
	if out.Identity == nil || out.Identity.Blocked || out.Status != StatusDenied {
		t.Errorf("the answer: %+v", out)
	}
	if n := w.count(`SELECT count(*) FROM identities WHERE id = ? AND blocked_at IS NULL AND blocked_by IS NULL`, v.IdentityID); n != 1 {
		t.Error("the block is still there")
	}
	if s := w.status(id); s != StatusDenied {
		t.Errorf("the application is %s", s)
	}
	rows := w.audits(ActionUnblocked, "identity", v.IdentityID)
	if len(rows) != 1 || rows[0].actor.Int64 != admin.UserID || rows[0].details["application_id"] != float64(id) {
		t.Errorf("audit rows: %+v", rows)
	}
	_, err = w.svc.Form(t.Context(), v)
	wantCode(t, "the form within 24 hours", err, ErrTooSoon, problem.CodeRateLimited)
	w.now = w.now.Add(CooldownAfter)
	if _, err := w.svc.Form(t.Context(), v); err != nil {
		t.Errorf("the form after 24 hours: %v", err)
	}

	before := w.allAudits()
	_, err = w.svc.Unblock(t.Context(), moderator, id, "")
	wantCode(t, "a second unblock", err, ErrNotBlocked, problem.CodeConflict)
	if w.allAudits() != before {
		t.Error("the second unblock wrote an audit row")
	}
}

// AC5: every decision writes one row with the moderator as actor and the
// note in its details.
func TestEveryDecisionIsAudited(t *testing.T) {
	w := newReviewWorld(t)
	_, a := w.pendingApp("kim")
	_, d := w.pendingApp("lee")
	_, b := w.pendingApp("max")
	if _, err := w.svc.Approve(t.Context(), moderator, a, "kim", "approved note", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Deny(t.Context(), moderator, d, "denied note", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.Deny(t.Context(), admin, b, "blocked note", true, ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		action  string
		id      int64
		by      int64
		note    string
		blocked any
	}{
		{ActionApproved, a, moderator.UserID, "approved note", nil},
		{ActionDenied, d, moderator.UserID, "denied note", false},
		{ActionDenied, b, admin.UserID, "blocked note", true},
	} {
		rows := w.audits(c.action, "application", c.id)
		if len(rows) != 1 || !rows[0].actor.Valid || rows[0].actor.Int64 != c.by || rows[0].details["note"] != c.note ||
			rows[0].details["blocked"] != c.blocked {
			t.Errorf("%s of %d: %+v", c.action, c.id, rows)
		}
	}
	if n := w.allAudits(); n != 3 {
		t.Errorf("%d audit rows, want 3", n)
	}
}

// AC6: two decisions racing on one application make one decision, one
// audit row, and a conflict for the other: approve against deny, and
// approve against the applicant's withdrawal.
func TestConcurrentDecisionsProduceOne(t *testing.T) {
	w := newReviewWorld(t)
	race := func(round int, first, second func() error) (error, error) {
		var wg sync.WaitGroup
		var e1, e2 error
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; e1 = first() }()
		go func() { defer wg.Done(); <-start; e2 = second() }()
		close(start)
		wg.Wait()
		return e1, e2
	}
	conflict := func(err error) bool { return problem.FromError(err) == problem.CodeConflict }

	rounds := 200
	if testing.Short() {
		rounds = 20
	}
	for i := range rounds {
		sub := fmt.Sprintf("race%03d", i)
		_, id := w.pendingApp(sub)
		ea, ed := race(i,
			func() error { _, err := w.svc.Approve(t.Context(), moderator, id, sub, "", ""); return err },
			func() error { _, err := w.svc.Deny(t.Context(), admin, id, "no", false, ""); return err })
		if (ea == nil) == (ed == nil) || !(conflict(ea) || conflict(ed)) {
			t.Fatalf("round %d: approve %v, deny %v; want one success and one conflict", i, ea, ed)
		}
		if !errors.Is(ea, ErrAlreadyDecided) && !errors.Is(ed, ErrAlreadyDecided) {
			t.Fatalf("round %d: the loser was not ErrAlreadyDecided: %v, %v", i, ea, ed)
		}
		n := w.count(`SELECT count(*) FROM audit_log WHERE target_type = 'application' AND target_id = ?`, strconv.FormatInt(id, 10))
		if n != 1 {
			t.Fatalf("round %d: %d audit rows", i, n)
		}
		users := w.count(`SELECT count(*) FROM users WHERE handle = ?`, sub)
		if (ea == nil) != (users == 1) {
			t.Fatalf("round %d: approve %v and %d users", i, ea, users)
		}
	}
	for i := range rounds / 2 {
		sub := fmt.Sprintf("wd%03d", i)
		v, id := w.pendingApp(sub)
		ea, ew := race(i,
			func() error { _, err := w.svc.Approve(t.Context(), moderator, id, sub, "", ""); return err },
			func() error { _, err := w.svc.Withdraw(t.Context(), v); return err })
		if (ea == nil) == (ew == nil) || !(conflict(ea) || conflict(ew)) {
			t.Fatalf("withdraw round %d: approve %v, withdraw %v", i, ea, ew)
		}
		want := StatusWithdrawn
		audits := 0
		if ea == nil {
			want, audits = StatusApproved, 1
		}
		if s := w.status(id); s != want {
			t.Fatalf("withdraw round %d: %s, want %s", i, s, want)
		}
		if n := w.count(`SELECT count(*) FROM audit_log WHERE target_type = 'application' AND target_id = ?`, strconv.FormatInt(id, 10)); n != audits {
			t.Fatalf("withdraw round %d: %d audit rows, want %d", i, n, audits)
		}
	}
}

// AC7's second half: an application answered against version 1 renders
// against version 1 after version 2 is active.
func TestOldVersionStillRenders(t *testing.T) {
	w := newReviewWorld(t)
	_, id := w.pendingApp("nia")
	w.saveVersion2()
	out, err := w.svc.Get(t.Context(), moderator, id)
	if err != nil {
		t.Fatal(err)
	}
	if out.Form.Version != 1 || len(out.Form.Fields) != 4 || out.Form.Fields[2].Key != "who" {
		t.Errorf("the form: version %d, %d fields", out.Form.Version, len(out.Form.Fields))
	}
	var answers map[string]any
	if err := json.Unmarshal(out.Answers, &answers); err != nil || answers["who"] != "A friend" || answers["name"] != "Carol" {
		t.Errorf("the answers: %s", out.Answers)
	}
	if out.SuggestedHandle != "nia" || out.Identity == nil || out.Identity.Email == nil || *out.Identity.Email != "nia@example.org" {
		t.Errorf("the identity and suggestion: %+v, %q", out.Identity, out.SuggestedHandle)
	}
}

// Only staff review: anonymous and inactive viewers are unauthenticated, and
// a pre-user or a member forbidden, on every method, before anything is
// read or written.
func TestOnlyStaffReview(t *testing.T) {
	w := newReviewWorld(t)
	applicant, id := w.pendingApp("otto")
	suspended := domain.Viewer{UserID: 2, Role: domain.RoleModerator, Active: false}
	for _, c := range []struct {
		name string
		v    domain.Viewer
		code problem.Code
	}{
		{"anonymous", domain.Viewer{}, problem.CodeUnauthenticated},
		{"suspended moderator", suspended, problem.CodeUnauthenticated},
		{"pre-user", applicant, problem.CodeForbidden},
		{"member", aMember, problem.CodeForbidden},
	} {
		calls := map[string]error{}
		_, calls["list"] = w.svc.List(t.Context(), c.v, "", "")
		_, calls["get"] = w.svc.Get(t.Context(), c.v, id)
		_, calls["approve"] = w.svc.Approve(t.Context(), c.v, id, "otto", "", "")
		_, calls["deny"] = w.svc.Deny(t.Context(), c.v, id, "no", true, "")
		_, calls["unblock"] = w.svc.Unblock(t.Context(), c.v, id, "")
		_, calls["note"] = w.svc.AddNote(t.Context(), c.v, id, "hi")
		for what, err := range calls {
			if got := problem.FromError(err); got != c.code {
				t.Errorf("%s %s: %s (%v), want %s", c.name, what, got, err, c.code)
			}
		}
	}
	if s := w.status(id); s != StatusPending || w.allAudits() != 0 || w.count(`SELECT count(*) FROM application_notes`) != 0 {
		t.Error("a refused viewer changed something")
	}
}

// The list pages newest first, filters by status, and refuses a status or
// cursor that is not one.
func TestListPagesAndFilters(t *testing.T) {
	w := newReviewWorld(t)
	var ids []int64
	for i := range PageSize + 3 {
		_, id := w.pendingApp(fmt.Sprintf("page%02d", i))
		ids = append(ids, id)
		w.now = w.now.Add(time.Second)
	}
	if _, err := w.svc.Deny(t.Context(), moderator, ids[0], "no", false, ""); err != nil {
		t.Fatal(err)
	}
	first, err := w.svc.List(t.Context(), moderator, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Applications) != PageSize || first.NextCursor == "" || first.Applications[0].ID != ids[len(ids)-1] {
		t.Fatalf("first page: %d rows from %d, cursor %q", len(first.Applications), first.Applications[0].ID, first.NextCursor)
	}
	rest, err := w.svc.List(t.Context(), moderator, StatusPending, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Applications) != 2 || rest.NextCursor != "" || rest.Applications[1].ID != ids[1] {
		t.Errorf("second page: %+v", rest)
	}
	denied, _ := w.svc.List(t.Context(), moderator, StatusDenied, "")
	all, _ := w.svc.List(t.Context(), moderator, "all", "")
	if len(denied.Applications) != 1 || denied.Applications[0].ID != ids[0] || len(all.Applications) != PageSize {
		t.Errorf("denied %d, all %d", len(denied.Applications), len(all.Applications))
	}
	if r := denied.Applications[0]; r.Provider == nil || *r.Provider != "github" || r.Email == nil || !r.EmailVerified {
		t.Errorf("the row's identity: %+v", r)
	}
	if _, err := w.svc.List(t.Context(), moderator, "open", ""); problem.FromError(err) != problem.CodeValidationFailed {
		t.Errorf("status=open: %v", err)
	}
	if _, err := w.svc.List(t.Context(), moderator, "", "***"); problem.FromError(err) != problem.CodeBadRequest {
		t.Errorf("a malformed cursor: %v", err)
	}
}

// AC10 at the service: after a denial with a distinctive note, nothing the
// applicant's session can ask for carries it. The API test walks every
// route as the pre-user.
func TestStatusNeverCarriesTheDecisionNote(t *testing.T) {
	w := newReviewWorld(t)
	v, id := w.pendingApp("pia")
	const note = "zebra-quartz-7731 moderators only"
	if _, err := w.svc.Deny(t.Context(), moderator, id, note, false, ""); err != nil {
		t.Fatal(err)
	}
	st, err := w.svc.Status(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "zebra") || st.Status != StatusDenied {
		t.Errorf("the status: %s", b)
	}
	// The staff view does carry it, so the check above is not vacuous.
	out, err := w.svc.Get(t.Context(), moderator, id)
	if err != nil || out.Decision == nil || out.Decision.Note != note {
		t.Errorf("the staff view: %+v, %v", out.Decision, err)
	}
}

// An approval needs an identity it can link: removed, already linked to an
// account, or blocked, the approval is conflict and writes nothing.
func TestApproveNeedsAUsableIdentity(t *testing.T) {
	w := newReviewWorld(t)
	for _, c := range []struct {
		name   string
		break_ func(v domain.Viewer)
	}{
		{"removed", func(v domain.Viewer) { w.exec(`DELETE FROM identities WHERE id = ?`, v.IdentityID) }},
		{"linked", func(v domain.Viewer) { w.exec(`UPDATE identities SET user_id = 3 WHERE id = ?`, v.IdentityID) }},
		{"blocked", func(v domain.Viewer) {
			w.exec(`UPDATE identities SET blocked_at = 1, blocked_by = 2 WHERE id = ?`, v.IdentityID)
		}},
	} {
		v, id := w.pendingApp("una-" + c.name)
		c.break_(v)
		users := w.count(`SELECT count(*) FROM users`)
		_, err := w.svc.Approve(t.Context(), moderator, id, "una-"+c.name, "", "")
		wantCode(t, c.name, err, ErrIdentityGone, problem.CodeConflict)
		if s := w.status(id); s != StatusPending || w.count(`SELECT count(*) FROM users`) != users || w.allAudits() != 0 {
			t.Errorf("%s: the application is %s, users %d to %d, %d audit rows", c.name, s, users,
				w.count(`SELECT count(*) FROM users`), w.allAudits())
		}
	}
}

// The guarded statements themselves, since the service's reads before them
// would hide a guard that stopped guarding: each writes nothing when its
// row is not in the state it requires.
func TestGuardedStatements(t *testing.T) {
	w := newReviewWorld(t)
	v, id := w.pendingApp("vic")
	if _, err := w.svc.Deny(t.Context(), moderator, id, "no", true, ""); err != nil {
		t.Fatal(err)
	}
	linked, _ := w.pendingApp("wes")
	w.exec(`UPDATE identities SET user_id = 3 WHERE id = ?`, linked.IdentityID)
	if err := w.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		ctx := t.Context()
		for name, f := range map[string]func() (int64, error){
			"decide a denied application": func() (int64, error) {
				return q.DecideApplication(ctx, dbq.DecideApplicationParams{Status: StatusApproved,
					DecidedBy: sql.NullInt64{Int64: 2, Valid: true}, DecidedAt: sql.NullInt64{Int64: 9, Valid: true},
					UserID: sql.NullInt64{Int64: 3, Valid: true}, ID: id})
			},
			"link a blocked identity": func() (int64, error) {
				return q.LinkIdentityToUser(ctx, dbq.LinkIdentityToUserParams{UserID: sql.NullInt64{Int64: 3, Valid: true},
					UpdatedAt: 9, ID: v.IdentityID})
			},
			"link a linked identity": func() (int64, error) {
				return q.LinkIdentityToUser(ctx, dbq.LinkIdentityToUserParams{UserID: sql.NullInt64{Int64: 2, Valid: true},
					UpdatedAt: 9, ID: linked.IdentityID})
			},
			"block a linked identity": func() (int64, error) {
				return q.BlockIdentity(ctx, dbq.BlockIdentityParams{BlockedAt: sql.NullInt64{Int64: 9, Valid: true},
					BlockedBy: sql.NullInt64{Int64: 2, Valid: true}, UpdatedAt: 9, ID: linked.IdentityID})
			},
			"unblock an unblocked identity": func() (int64, error) {
				return q.UnblockIdentity(ctx, dbq.UnblockIdentityParams{UpdatedAt: 9, ID: linked.IdentityID})
			},
		} {
			if n, err := f(); err != nil || n != 0 {
				t.Errorf("%s: %d rows, %v", name, n, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if s := w.status(id); s != StatusDenied {
		t.Errorf("the denied application is %s", s)
	}
}
