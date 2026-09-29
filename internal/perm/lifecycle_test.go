package perm

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The account lifecycle (TKT-01M29CKRFS): every status change in one
// transaction with its audit row and the deletion of the account's sessions.

// account is what these tests read back of a users row and what hangs off
// it.
type account struct {
	handle, display, status, reason string
	until                           sql.NullInt64
	anonymized                      int
	sessions, identities            int
}

func (f *fixture) account(id int64) account {
	f.t.Helper()
	var a account
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(f.t.Context(), `SELECT handle, display_name, status, status_reason, status_until,
			anonymized FROM users WHERE id = ?`, id).Scan(&a.handle, &a.display, &a.status, &a.reason, &a.until,
			&a.anonymized); err != nil {
			return err
		}
		if err := tx.QueryRowContext(f.t.Context(), `SELECT count(*) FROM sessions WHERE user_id = ?
			OR identity_id IN (SELECT id FROM identities WHERE user_id = ?)`, id, id).Scan(&a.sessions); err != nil {
			return err
		}
		return tx.QueryRowContext(f.t.Context(), `SELECT count(*) FROM identities WHERE user_id = ?`, id).Scan(&a.identities)
	}); err != nil {
		f.t.Fatal(err)
	}
	return a
}

// member makes an account of role and status with an identity, two
// sessions on it, and a pre-user session from before it had a user, and
// returns its id.
func (f *fixture) member(role, status string) int64 {
	f.t.Helper()
	id := f.viewer(role).UserID
	f.exec(`UPDATE users SET status = ? WHERE id = ?`, status, id)
	f.exec(`INSERT INTO identities (user_id, provider_key, subject, issuer, created_at, updated_at)
		VALUES (?, 'main', ?, 'https://idp.example', 1, 1)`, id, "sub-"+strconv.FormatInt(id, 10))
	for range 2 {
		f.exec(`INSERT INTO sessions (id_hash, user_id, identity_id, kind, created_at, expires_at, last_used_at)
			VALUES (randomblob(32), ?, (SELECT max(id) FROM identities), 'user', 1, 9000000000000, 1)`, id)
	}
	f.exec(`INSERT INTO sessions (id_hash, identity_id, kind, created_at, expires_at, last_used_at)
		VALUES (randomblob(32), (SELECT max(id) FROM identities), 'pre_user', 1, 9000000000000, 1)`)
	return id
}

// transition is one lifecycle change, as a test drives it.
type transition struct {
	name, from, to, action string
}

var lifecycle = []transition{
	{"deactivate", domain.StatusActive, domain.StatusDeactivated, ActionDeactivated},
	{"suspend", domain.StatusActive, domain.StatusSuspended, ActionSuspended},
	{"suspend a deactivated account", domain.StatusDeactivated, domain.StatusSuspended, ActionSuspended},
	{"unsuspend", domain.StatusSuspended, domain.StatusActive, ActionUnsuspended},
	{"reactivate", domain.StatusDeactivated, domain.StatusActive, ActionReactivated},
	{"anonymize", domain.StatusActive, domain.StatusDeactivated, ActionAnonymized},
	{"anonymize a suspended account", domain.StatusSuspended, domain.StatusDeactivated, ActionAnonymized},
}

// run makes c to id as f.by().
func (f *fixture) run(c transition, id int64) error {
	if c.action == ActionAnonymized {
		return f.ev.Anonymize(f.t.Context(), id, f.by(), nil)
	}
	return f.ev.ChangeStatus(f.t.Context(), id, StatusChange{By: f.by(), Action: c.action, Reason: "spam",
		Until: 1_800_000_000_000})
}

// TestChangeStatusIsOneTransaction is AC2's rollback half: with the audit
// insert failing, every transition leaves the status, the reason, the end,
// the sessions and the identities exactly as they were.
func TestChangeStatusIsOneTransaction(t *testing.T) {
	for _, c := range lifecycle {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			id := f.member(domain.RoleMember, c.from)
			f.exec(`UPDATE users SET status_reason = 'before', status_until = 7 WHERE id = ?`, id)
			before := f.account(id)
			f.failAudit()
			if err := f.run(c, id); err == nil || !strings.Contains(err.Error(), "injected audit failure") {
				t.Fatalf("with the audit insert failing: %v", err)
			}
			if after := f.account(id); after != before {
				t.Errorf("the failed change left %+v, want %+v", after, before)
			}
		})
	}
}

// TestEachTransitionWritesItsAuditRow is AC1 and AC2 at the source: each
// transition writes exactly its row, with the actor, address, target and
// details, and every one that leaves active deletes the sessions. The
// anonymization row names no handle, and no role row is written.
func TestEachTransitionWritesItsAuditRow(t *testing.T) {
	for _, c := range lifecycle {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			id := f.member(domain.RoleMember, c.from)
			old := f.account(id).handle
			mark := f.lastAudit()
			if err := f.run(c, id); err != nil {
				t.Fatal(err)
			}
			a := f.account(id)
			if a.status != c.to {
				t.Errorf("status %s, want %s", a.status, c.to)
			}
			if leaves := c.to != domain.StatusActive; leaves && a.sessions != 0 {
				t.Errorf("%d sessions survived", a.sessions)
			} else if !leaves && a.sessions != 3 {
				t.Errorf("coming back touched the sessions: %d, want 3", a.sessions)
			}
			rows := f.rowsSince(mark)
			if len(rows) != 1 {
				t.Fatalf("%d audit rows, want 1: %+v", len(rows), rows)
			}
			r := rows[0]
			if r.action != c.action || r.targetType != audit.TypeUser || r.targetID != strconv.FormatInt(id, 10) ||
				r.actor.Int64 != f.actor || r.ip != "192.0.2.7" || r.details["from"] != c.from {
				t.Errorf("row %+v", r)
			}
			switch c.action {
			case ActionSuspended:
				if r.details["reason"] != "spam" || r.details["until"] != float64(1_800_000_000_000) ||
					a.reason != "spam" || a.until.Int64 != 1_800_000_000_000 {
					t.Errorf("suspension: details %v, stored %q %v", r.details, a.reason, a.until)
				}
			case ActionAnonymized:
				if len(r.details) != 1 || strings.Contains(fmt.Sprint(r.details), old) {
					t.Errorf("the anonymization row says more than where it came from: %v", r.details)
				}
				if a.handle != fmt.Sprintf("deleted-user-%d", id) || a.display != "" || a.anonymized != 1 ||
					a.identities != 0 {
					t.Errorf("anonymized account %+v", a)
				}
			default:
				if a.reason != "" || a.until.Valid {
					t.Errorf("%s left a reason %q and end %v", c.action, a.reason, a.until)
				}
			}
		})
	}
}

// TestUnsuspendClearsTheReason: a suspension's reason and end go when it
// ends.
func TestUnsuspendClearsTheReason(t *testing.T) {
	f := newFixture(t)
	id := f.member(domain.RoleMember, domain.StatusActive)
	for _, c := range []transition{lifecycle[1], lifecycle[3]} {
		if err := f.run(c, id); err != nil {
			t.Fatal(err)
		}
	}
	if a := f.account(id); a.status != domain.StatusActive || a.reason != "" || a.until.Valid {
		t.Errorf("after unsuspending: %+v", a)
	}
}

// TestLastAdminCannotLeave is AC6's guard: the only active admin cannot be
// suspended, deactivated, anonymized or demoted, whoever asks (a suspended
// admin does not count), and nothing is written. With a second active admin
// each change goes through, and then the one left is the last.
func TestLastAdminCannotLeave(t *testing.T) {
	leave := []func(f *fixture, id int64) error{
		func(f *fixture, id int64) error { return f.run(lifecycle[0], id) },
		func(f *fixture, id int64) error { return f.run(lifecycle[1], id) },
		func(f *fixture, id int64) error { return f.run(lifecycle[5], id) },
		func(f *fixture, id int64) error { return f.ev.ChangeRole(f.t.Context(), f.by(), id, domain.RoleMember) },
		func(f *fixture, id int64) error {
			return f.ev.ChangeRole(f.t.Context(), f.by(), id, domain.RoleModerator)
		},
	}
	for i, fn := range leave {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			f := newFixture(t)
			admin := f.member(domain.RoleAdmin, domain.StatusActive)
			f.member(domain.RoleAdmin, domain.StatusSuspended)
			before, mark := f.account(admin), f.lastAudit()
			if err := fn(f, admin); !errors.Is(err, ErrLastAdmin) {
				t.Fatalf("the last active admin left: %v", err)
			}
			if after := f.account(admin); after != before || f.role(admin) != domain.RoleAdmin {
				t.Errorf("the refusal changed %+v to %+v", before, after)
			}
			if rows := f.rowsSince(mark); len(rows) != 0 {
				t.Errorf("the refusal wrote %+v", rows)
			}

			other := f.member(domain.RoleAdmin, domain.StatusActive)
			if err := fn(f, admin); err != nil {
				t.Fatalf("with another active admin: %v", err)
			}
			if err := fn(f, other); !errors.Is(err, ErrLastAdmin) {
				t.Errorf("the second admin, now the last, left: %v", err)
			}
		})
	}
}

// TestBadTransitionsAreRefused: an anonymized account changes no further,
// and a change the status does not allow is refused. Neither writes.
func TestBadTransitionsAreRefused(t *testing.T) {
	f := newFixture(t)
	gone := f.member(domain.RoleMember, domain.StatusActive)
	if err := f.run(lifecycle[5], gone); err != nil {
		t.Fatal(err)
	}
	for _, c := range lifecycle {
		mark := f.lastAudit()
		if err := f.run(c, gone); !errors.Is(err, ErrAnonymized) {
			t.Errorf("%s of an anonymized account: %v", c.name, err)
		}
		if rows := f.rowsSince(mark); len(rows) != 0 {
			t.Errorf("%s of an anonymized account wrote %+v", c.name, rows)
		}
	}

	for _, c := range []struct {
		from, action string
	}{
		{domain.StatusSuspended, ActionDeactivated},
		{domain.StatusSuspended, ActionSuspended},
		{domain.StatusSuspended, ActionReactivated},
		{domain.StatusPending, ActionReactivated},
		{domain.StatusPending, ActionSuspended},
		{domain.StatusActive, ActionUnsuspended},
		{domain.StatusActive, ActionReactivated},
		{domain.StatusDeactivated, ActionDeactivated},
		{domain.StatusDeactivated, ActionUnsuspended},
		{domain.StatusActive, "user.promoted"},
	} {
		id := f.member(domain.RoleMember, c.from)
		before, mark := f.account(id), f.lastAudit()
		err := f.ev.ChangeStatus(t.Context(), id, StatusChange{By: f.by(), Action: c.action})
		if !errors.Is(err, ErrBadTransition) {
			t.Errorf("%s from %s: %v", c.action, c.from, err)
		}
		if after := f.account(id); after != before || len(f.rowsSince(mark)) != 0 {
			t.Errorf("%s from %s wrote something", c.action, c.from)
		}
	}
	if err := f.run(lifecycle[0], 999_999); !errors.Is(err, ErrNoUser) {
		t.Errorf("an id nobody has: %v", err)
	}
}

// TestAllowSeesTheAccountAsChanged: the caller's authority rule is asked
// about the row read in the transaction that changes it, and its refusal
// writes nothing.
func TestAllowSeesTheAccountAsChanged(t *testing.T) {
	f := newFixture(t)
	id := f.member(domain.RoleMember, domain.StatusActive)
	// Promoted after any read a caller made beforehand.
	f.exec(`UPDATE users SET role = 'moderator' WHERE id = ?`, id)
	refused := errors.New("a moderator may not")
	var saw string
	allow := func(u dbq.User) error {
		saw = u.Role
		if u.Role != domain.RoleMember {
			return refused
		}
		return nil
	}
	before, mark := f.account(id), f.lastAudit()
	if err := f.ev.ChangeStatus(t.Context(), id, StatusChange{By: f.by(), Action: ActionSuspended, Reason: "x",
		Allow: allow}); !errors.Is(err, refused) {
		t.Fatalf("suspend: %v", err)
	}
	if err := f.ev.Anonymize(t.Context(), id, f.by(), allow); !errors.Is(err, refused) {
		t.Fatalf("anonymize: %v", err)
	}
	if saw != domain.RoleModerator {
		t.Errorf("allow saw role %q", saw)
	}
	if after := f.account(id); after != before || len(f.rowsSince(mark)) != 0 {
		t.Error("a refused change wrote something")
	}
}

// TestAnonymizeKeepsTheApplication: an application that named one of the
// identities is kept, with its identity_id cleared by the foreign key.
func TestAnonymizeKeepsTheApplication(t *testing.T) {
	f := newFixture(t)
	id := f.member(domain.RoleMember, domain.StatusActive)
	f.exec(`INSERT INTO applications (identity_id, form_id, form_version, answers_json, status, decided_at, created_at)
		VALUES ((SELECT id FROM identities WHERE user_id = ?), (SELECT id FROM forms LIMIT 1), 1, '{}', 'withdrawn', 1, 1)`, id)
	if err := f.run(lifecycle[5], id); err != nil {
		t.Fatal(err)
	}
	var n int
	var identity sql.NullInt64
	if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*), max(identity_id) FROM applications`).Scan(&n, &identity)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 || identity.Valid {
		t.Errorf("%d applications, identity %v", n, identity)
	}
}

// TestChangeStatusInvalidates: a status change clears the cache once it has
// committed, as every decision write does.
func TestChangeStatusInvalidates(t *testing.T) {
	f := newFixture(t)
	id := f.member(domain.RoleMember, domain.StatusActive)
	gen := f.ev.gen.Load()
	if err := f.run(lifecycle[1], id); err != nil {
		t.Fatal(err)
	}
	if f.ev.gen.Load() == gen {
		t.Error("the suspension left the cache generation alone")
	}
}
