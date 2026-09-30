package identity

import (
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Handle changes (TKT-01M3NRP3W9, docs/05 "Changing a handle").

// renaming is the registry with alice (sub-1), bob (sub-2) and an admin
// (sub-3, root) registered, and a clock the test moves.
type renaming struct {
	t     *testing.T
	s     *Service
	d     *db.DB
	clock time.Time
	ids   map[string]int64
}

func newRenaming(t *testing.T) *renaming {
	t.Helper()
	s, d := registry(t)
	r := &renaming{t: t, s: s, d: d, clock: time.UnixMilli(1_757_500_000_000), ids: map[string]int64{}}
	s.Now = func() time.Time { return r.clock }
	for sub, h := range map[string]string{"sub-1": "alice", "sub-2": "bob", "sub-3": "root"} {
		if _, err := s.Register(t.Context(), claims(sub), h, session.Meta{Provider: "fake"}, ""); err != nil {
			t.Fatal(err)
		}
		r.ids[h] = accountOf(t, d, sub)
	}
	return r
}

// member is the viewer a session of the account registered as h builds.
func (r *renaming) member(h string) domain.Viewer {
	return domain.Viewer{UserID: r.ids[h], Role: domain.RoleMember, Active: true}
}

func (r *renaming) admin() domain.Viewer {
	return domain.Viewer{UserID: r.ids["root"], Role: domain.RoleAdmin, Active: true}
}

func (r *renaming) user(h string) dbq.User {
	r.t.Helper()
	var u dbq.User
	if err := r.d.Read(r.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		u, err = q.GetUser(r.t.Context(), r.ids[h])
		return err
	}); err != nil {
		r.t.Fatal(err)
	}
	return u
}

func (r *renaming) count(query string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.d.Read(r.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(r.t.Context(), query, args...).Scan(&n)
	}); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// fieldCode is the handle field's code in a validation-failed refusal, or
// "" when err is not one.
func fieldCode(err error) string {
	if problem.FromError(err) != problem.CodeValidationFailed {
		return ""
	}
	for _, f := range problem.FieldErrorsOf(err) {
		if f.Field == "handle" {
			return f.Code
		}
	}
	return ""
}

const day = 24 * time.Hour

// TestRenameCooldown is AC1's service half: a member's second change inside
// 30 days is conflict carrying available_at, the first change plus 30 days,
// and at that instant it succeeds. An admin's rename inside the window
// succeeds and does not move handle_changed_at.
func TestRenameCooldown(t *testing.T) {
	r := newRenaming(t)
	t0 := r.clock
	res, err := r.s.ChangeHandle(t.Context(), r.member("alice"), "198.51.100.7", "Alicia")
	if err != nil {
		t.Fatal(err)
	}
	wantNext := dto.Millis(t0.Add(RenameCooldown).UnixMilli())
	if res.Handle != "alicia" || res.Previous != "alice" || res.NextChangeAt == nil || *res.NextChangeAt != wantNext {
		t.Fatalf("the first change answered %+v", res)
	}

	r.clock = t0.Add(day)
	_, err = r.s.ChangeHandle(t.Context(), r.member("alice"), "", "alina")
	if at, ok := problem.AvailableAtOf(err); problem.FromError(err) != problem.CodeConflict || !ok || at != int64(wantNext) {
		t.Fatalf("a second change a day later: %v (available_at %d, %t)", err, at, ok)
	}

	res, err = r.s.RenameMember(t.Context(), r.admin(), "", "alicia", "alina")
	if err != nil {
		t.Fatalf("an admin inside the member's window: %v", err)
	}
	if got := r.user("alice").HandleChangedAt; got.Int64 != t0.UnixMilli() {
		t.Errorf("the admin's rename moved handle_changed_at to %v", got)
	}
	if res.NextChangeAt == nil || *res.NextChangeAt != wantNext {
		t.Errorf("the admin's rename answered next_change_at %v, want the member's own %d", res.NextChangeAt, wantNext)
	}

	r.clock = t0.Add(RenameCooldown - time.Millisecond)
	if _, err := r.s.ChangeHandle(t.Context(), r.member("alice"), "", "alix"); problem.FromError(err) != problem.CodeConflict {
		t.Fatalf("a millisecond before the window ends: %v", err)
	}
	r.clock = t0.Add(RenameCooldown)
	if _, err := r.s.ChangeHandle(t.Context(), r.member("alice"), "", "alix"); err != nil {
		t.Fatalf("as the window ends: %v", err)
	}

	// An admin's rename never starts the cooldown: bob, renamed by one, may
	// change his own handle at once.
	res, err = r.s.RenameMember(t.Context(), r.admin(), "", "bob", "robert")
	if err != nil || res.NextChangeAt != nil {
		t.Fatalf("an admin renaming bob: %+v, %v", res, err)
	}
	if _, err := r.s.ChangeHandle(t.Context(), r.member("bob"), "", "bobby"); err != nil {
		t.Fatalf("bob changing his own after an admin's rename: %v", err)
	}
}

// TestRenameChecksTheHandle is AC2's service half: each refusal carries the
// one code that means it, on validation-failed, and a handle a rename
// released within 90 days is taken, never reserved, for anyone, its former
// owner included.
func TestRenameChecksTheHandle(t *testing.T) {
	r := newRenaming(t)
	if _, err := r.s.RenameMember(t.Context(), r.admin(), "", "alice", "alicia"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		who, to, want string
	}{
		{"bob", "b", CodeInvalidHandle},
		{"bob", "bob!", CodeInvalidHandle},
		{"bob", "trailing-", CodeInvalidHandle},
		{"bob", "admin", CodeReserved},
		{"bob", "API", CodeReserved},
		{"bob", "deleted-user-9", CodeReserved},
		{"bob", "ROOT", CodeTaken},  // held, in another case
		{"bob", "alice", CodeTaken}, // released within 90 days
		{"bob", "Alice", CodeTaken},
		{"alice", "alice", CodeTaken}, // its former owner too
		{"bob", "Bob", CodeSame},
	} {
		_, err := r.s.ChangeHandle(t.Context(), r.member(c.who), "", c.to)
		if got := fieldCode(err); got != c.want {
			t.Errorf("%s to %q: code %q (%v), want %q", c.who, c.to, got, err, c.want)
		}
	}
	if n := r.count(`SELECT count(*) FROM handle_releases`); n != 1 {
		t.Errorf("%d release rows after refused renames, want the one", n)
	}
	// After the 90 days the handle is free again.
	r.clock = r.clock.Add(domain.HandleReservation)
	if _, err := r.s.ChangeHandle(t.Context(), r.member("bob"), "", "alice"); err != nil {
		t.Errorf("bob taking alice once the reservation ended: %v", err)
	}
}

// TestRenameIsOneTransaction is AC4's other half: an audit insert that
// fails leaves the handle, handle_changed_at and handle_releases as they
// were.
func TestRenameIsOneTransaction(t *testing.T) {
	r := newRenaming(t)
	if err := r.d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `CREATE TRIGGER no_audit BEFORE INSERT ON audit_log
			BEGIN SELECT RAISE(ABORT, 'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.ChangeHandle(t.Context(), r.member("alice"), "", "alicia"); err == nil {
		t.Fatal("a rename succeeded with no way to write its audit row")
	}
	u := r.user("alice")
	if u.Handle != "alice" || u.HandleChangedAt.Valid {
		t.Errorf("the failed rename left %q, changed at %v", u.Handle, u.HandleChangedAt)
	}
	if n := r.count(`SELECT count(*) FROM handle_releases`); n != 0 {
		t.Errorf("the failed rename left %d release rows", n)
	}
}

// TestRenameWritesOneAuditRow is AC4: each rename writes one user.renamed
// row against the account, by whoever made it, with {from, to}, and the
// release row that reserves the old handle names the account for 90 days.
func TestRenameWritesOneAuditRow(t *testing.T) {
	r := newRenaming(t)
	if _, err := r.s.ChangeHandle(t.Context(), r.member("alice"), "198.51.100.7", "alicia"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.RenameMember(t.Context(), r.admin(), "203.0.113.9", "bob", "robert"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		who, actor, ip, from, to string
	}{
		{"alice", "alice", "198.51.100.7", "alice", "alicia"},
		{"bob", "root", "203.0.113.9", "bob", "robert"},
	} {
		var rows []struct {
			actor, ip, details string
		}
		if err := r.d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			res, err := tx.QueryContext(t.Context(), `SELECT actor_id, ip, details_json FROM audit_log
				WHERE action = ? AND target_type = 'user' AND target_id = ?`, ActionRenamed, r.ids[c.who])
			if err != nil {
				return err
			}
			defer res.Close()
			for res.Next() {
				var actor int64
				var row struct{ actor, ip, details string }
				if err := res.Scan(&actor, &row.ip, &row.details); err != nil {
					return err
				}
				if actor == r.ids[c.actor] {
					row.actor = c.actor
				}
				rows = append(rows, row)
			}
			return res.Err()
		}); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: %d user.renamed rows, want 1", c.who, len(rows))
		}
		var details map[string]string
		if err := json.Unmarshal([]byte(rows[0].details), &details); err != nil {
			t.Fatal(err)
		}
		if rows[0].actor != c.actor || rows[0].ip != c.ip || len(details) != 2 || details["from"] != c.from ||
			details["to"] != c.to {
			t.Errorf("%s's row is %+v", c.who, rows[0])
		}
		var user sql.NullInt64
		var released, until int64
		if err := r.d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT user_id, released_at, reserved_until FROM handle_releases
				WHERE handle = ?`, c.from).Scan(&user, &released, &until)
		}); err != nil {
			t.Fatal(err)
		}
		if user.Int64 != r.ids[c.who] || released != r.clock.UnixMilli() ||
			until-released != domain.HandleReservation.Milliseconds() {
			t.Errorf("%s's release: user %v, %d to %d", c.from, user, released, until)
		}
	}
}

// TestConcurrentRenamesProduceOne: two changes of one account's handle at
// once make one rename and one release; the other is conflict.
func TestConcurrentRenamesProduceOne(t *testing.T) {
	r := newRenaming(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, to := range []string{"alicia", "alina"} {
		wg.Go(func() {
			_, errs[i] = r.s.ChangeHandle(t.Context(), r.member("alice"), "", to)
		})
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case problem.FromError(err) == problem.CodeConflict:
			conflict++
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Errorf("%d renames and %d conflicts, want one of each", ok, conflict)
	}
	if n := r.count(`SELECT count(*) FROM handle_releases`); n != 1 {
		t.Errorf("%d release rows, want 1", n)
	}
}

// TestAnonymizedCannotBeRenamed is AC5: neither the owner nor an admin can
// rename an anonymized account, and the query that renames refuses one
// whatever its caller checked.
func TestAnonymizedCannotBeRenamed(t *testing.T) {
	r := newRenaming(t)
	alice := r.member("alice")
	if err := r.s.Perm.Anonymize(t.Context(), r.ids["alice"], audit.System("test"), nil); err != nil {
		t.Fatal(err)
	}
	placeholder := domain.DeletedMemberHandle(r.ids["alice"])
	if _, err := r.s.ChangeHandle(t.Context(), alice, "", "alicia"); problem.FromError(err) != problem.CodeConflict {
		t.Errorf("the owner renaming: %v", err)
	}
	if _, err := r.s.RenameMember(t.Context(), r.admin(), "", placeholder, "alicia"); problem.FromError(err) != problem.CodeConflict {
		t.Errorf("an admin renaming: %v", err)
	}
	var n int64
	if err := r.d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.RenameUser(t.Context(), dbq.RenameUserParams{Handle: "alicia", UpdatedAt: 1, ID: r.ids["alice"],
			OldHandle: placeholder})
		return err
	}); err != nil || n != 0 {
		t.Errorf("RenameUser on the anonymized row: %d rows, %v", n, err)
	}
	if err := r.d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET handle = 'alicia' WHERE id = ?`, r.ids["alice"])
		return err
	}); err == nil {
		t.Error("the schema let an anonymized account be renamed")
	}
	if got := r.user("alice").Handle; got != placeholder {
		t.Errorf("the handle is %q", got)
	}
	if n := r.count(`SELECT count(*) FROM audit_log WHERE action = ?`, ActionRenamed); n != 0 {
		t.Errorf("%d rename rows", n)
	}
}

// TestRenameNeedsAMemberOrAnAdmin: a stranger or a pre-user cannot change a
// handle, and a member cannot rename another account.
func TestRenameNeedsAMemberOrAnAdmin(t *testing.T) {
	r := newRenaming(t)
	for _, c := range []struct {
		v    domain.Viewer
		want problem.Code
	}{
		{domain.Anonymous(), problem.CodeUnauthenticated},
		{domain.Viewer{PreUser: true, IdentityID: 1, Active: true}, problem.CodeForbidden},
	} {
		if _, err := r.s.ChangeHandle(t.Context(), c.v, "", "zed"); problem.FromError(err) != c.want {
			t.Errorf("%+v: %v, want %s", c.v, err, c.want)
		}
	}
	mod := r.member("bob")
	mod.Role = domain.RoleModerator
	if _, err := r.s.RenameMember(t.Context(), mod, "", "alice", "zed"); problem.FromError(err) != problem.CodeForbidden {
		t.Errorf("a moderator renaming: %v", err)
	}
	if _, err := r.s.RenameMember(t.Context(), r.admin(), "", "nobody", "zed"); problem.FromError(err) != problem.CodeNotFound {
		t.Errorf("renaming nobody: %v", err)
	}
	if n := r.count(`SELECT count(*) FROM audit_log WHERE action = ?`, ActionRenamed); n != 0 {
		t.Errorf("%d rename rows", n)
	}
}
