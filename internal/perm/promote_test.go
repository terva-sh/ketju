package perm

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Review 1128: an admin with no promotion on record whom bootstrap matches
// gets the record, so once demoted they stay demoted at the next match.
func TestPromoteOnceRecordsAnExistingAdmin(t *testing.T) {
	f := newFixture(t)
	v := f.viewer(domain.RoleAdmin)
	once := Promotion{Once: true, Details: map[string]string{"via": "bootstrap"}}
	if got, err := f.ev.Promote(t.Context(), v.UserID, once); err != nil || got {
		t.Fatalf("Promote = %v, %v; want no change", got, err)
	}
	rows := f.promotions(v.UserID)
	if len(rows) != 1 || rows[0].details["already"] != domain.RoleAdmin || rows[0].details["via"] != "bootstrap" {
		t.Fatalf("audit rows %+v, want one marked already admin", rows)
	}
	f.viewer(domain.RoleAdmin) // another admin, so v may be demoted
	if err := f.ev.ChangeRole(t.Context(), audit.By{}, v.UserID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if got, err := f.ev.Promote(t.Context(), v.UserID, once); err != nil || got {
		t.Fatalf("after the demotion, Promote = %v, %v; want no change", got, err)
	}
	if r := f.role(v.UserID); r != domain.RoleMember {
		t.Errorf("role %s after the demotion and a second match, want member", r)
	}
	if n := len(f.promotions(v.UserID)); n != 1 {
		t.Errorf("%d audit rows, want 1", n)
	}
}

// Review 1128: a promotion made by another process, as ketju admin promote
// beside a running server is, cannot clear the server's cache, and need
// not. The server's evaluator here sits on its own database handle and so
// its own generation counter, as a separate process would, and holds a
// cached decision for the member. The member's next request builds its
// Viewer from the users row, as the session middleware does, and gets the
// admin answer.
func TestPromoteFromAnotherProcess(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	v := f.viewer(domain.RoleMember)
	if d := f.check(v, forum, domain.PermModerate); d != Deny {
		t.Fatalf("before: %v", d)
	}

	other, err := db.Open(t.Context(), db.Options{Path: f.path})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	gen := f.ev.gen.Load()
	if _, err := New(other).Promote(t.Context(), v.UserID, Promotion{}); err != nil {
		t.Fatal(err)
	}
	if f.ev.gen.Load() != gen {
		t.Fatal("the other handle shares the server's generation; this test no longer models another process")
	}

	next := v
	next.Role = f.role(v.UserID)
	if d := f.check(next, forum, domain.PermModerate); d != Allow {
		t.Errorf("the promoted member's next request: %v, want allow", d)
	}
}

type auditRow struct {
	actor   sql.NullInt64
	details map[string]string
}

func (f *fixture) promotions(userID int64) []auditRow {
	f.t.Helper()
	var out []auditRow
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT actor_id, details_json FROM audit_log
			WHERE action = ? AND target_type = 'user' AND target_id = ? ORDER BY id`,
			ActionPromoted, strconv.FormatInt(userID, 10))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			var dj string
			if err := rows.Scan(&r.actor, &dj); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(dj), &r.details); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) role(userID int64) string {
	f.t.Helper()
	var role string
	if err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(f.t.Context(), userID)
		role = u.Role
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return role
}

// Promote's rules: it promotes and records once, leaves an admin alone, and
// with Once leaves alone anyone ever promoted, whatever their role is now.
func TestPromote(t *testing.T) {
	cases := []struct {
		name string
		// demoted: promoted, then demoted, before the Promote under test.
		role    string
		demoted bool
		once    bool
		want    bool
		rows    int
		after   string
	}{
		{name: "member", role: domain.RoleMember, want: true, rows: 1, after: domain.RoleAdmin},
		{name: "member, once", role: domain.RoleMember, once: true, want: true, rows: 1, after: domain.RoleAdmin},
		{name: "moderator", role: domain.RoleModerator, want: true, rows: 1, after: domain.RoleAdmin},
		{name: "already admin", role: domain.RoleAdmin, want: false, rows: 0, after: domain.RoleAdmin},
		{name: "already admin, once", role: domain.RoleAdmin, once: true, want: false, rows: 1, after: domain.RoleAdmin},
		{name: "demoted, once", role: domain.RoleMember, demoted: true, once: true, want: false, rows: 1, after: domain.RoleMember},
		{name: "demoted, by hand", role: domain.RoleMember, demoted: true, want: true, rows: 2, after: domain.RoleAdmin},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			v := f.viewer(c.role)
			if c.demoted {
				if ok, err := f.ev.Promote(t.Context(), v.UserID, Promotion{}); err != nil || !ok {
					t.Fatalf("first promotion = %v, %v", ok, err)
				}
				f.viewer(domain.RoleAdmin) // another admin, so v may be demoted
				if err := f.ev.ChangeRole(t.Context(), audit.By{}, v.UserID, domain.RoleMember); err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.ev.Promote(t.Context(), v.UserID, Promotion{Once: c.once, Actor: 0,
				Details: map[string]string{"via": "test", "role": "overridden"}})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("Promote = %v, want %v", got, c.want)
			}
			if r := f.role(v.UserID); r != c.after {
				t.Errorf("role %s, want %s", r, c.after)
			}
			rows := f.promotions(v.UserID)
			if len(rows) != c.rows {
				t.Fatalf("%d audit rows, want %d", len(rows), c.rows)
			}
			if c.rows > 0 && !c.demoted {
				r := rows[0]
				if r.actor.Valid || r.details["role"] != domain.RoleAdmin || r.details["via"] != "test" {
					t.Errorf("audit row %+v", r)
				}
			}
		})
	}
}

// The audit row names the actor when there is one.
func TestPromoteRecordsTheActor(t *testing.T) {
	f := newFixture(t)
	by := f.viewer(domain.RoleAdmin)
	v := f.viewer(domain.RoleMember)
	if _, err := f.ev.Promote(t.Context(), v.UserID, Promotion{Actor: by.UserID}); err != nil {
		t.Fatal(err)
	}
	rows := f.promotions(v.UserID)
	if len(rows) != 1 || rows[0].actor.Int64 != by.UserID {
		t.Errorf("audit rows %+v, want one naming %d", rows, by.UserID)
	}
}

func TestPromoteNobody(t *testing.T) {
	f := newFixture(t)
	if _, err := f.ev.Promote(t.Context(), 9999, Promotion{}); !errors.Is(err, ErrNoUser) {
		t.Errorf("Promote = %v, want ErrNoUser", err)
	}
}

// A promotion clears the cache, as every other decision write does.
func TestPromoteInvalidates(t *testing.T) {
	f := newFixture(t)
	v := f.viewer(domain.RoleMember)
	gen := f.ev.gen.Load()
	if _, err := f.ev.Promote(t.Context(), v.UserID, Promotion{}); err != nil {
		t.Fatal(err)
	}
	if f.ev.gen.Load() == gen {
		t.Error("Promote did not invalidate the cache")
	}
}
