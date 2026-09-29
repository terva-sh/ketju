package perm

import (
	"database/sql"
	"github.com/terva-sh/ketju/internal/audit"
	"strconv"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func (f *fixture) moderatesAny(v domain.Viewer) bool {
	f.t.Helper()
	ok, err := f.ev.ModeratesAny(f.t.Context(), v)
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

// ModeratesAny's rules: staff by role, a member granted moderate on a forum
// they can see, and nobody else.
func TestModeratesAny(t *testing.T) {
	f := newFixture(t)
	public := f.forum(domain.VisibilityPublic)
	restricted := f.forum(domain.VisibilityRestricted)
	f.grant(0, f.members, domain.PermRead)

	mods := f.newGroup()
	blind := f.newGroup()
	global := f.newGroup()
	f.grant(public, mods, domain.PermModerate)
	// moderate in a restricted forum the group cannot read moderates nothing:
	// the forum is hidden from them.
	f.grant(restricted, blind, domain.PermModerate)
	f.grant(0, global, domain.PermModerate)

	suspended := f.viewer(domain.RoleAdmin)
	suspended.Active = false
	for _, c := range []struct {
		name string
		v    domain.Viewer
		want bool
	}{
		{"anonymous", domain.Anonymous(), false},
		{"pre-user", domain.Viewer{PreUser: true, IdentityID: 3, Active: true}, false},
		{"inactive admin", suspended, false},
		{"member", f.viewer(domain.RoleMember), false},
		{"moderator by role", f.viewer(domain.RoleModerator), true},
		{"admin", f.viewer(domain.RoleAdmin), true},
		{"granted on one forum", f.viewer(domain.RoleMember, mods), true},
		{"granted where hidden", f.viewer(domain.RoleMember, blind), false},
		{"granted globally", f.viewer(domain.RoleMember, global), true},
	} {
		if got := f.moderatesAny(c.v); got != c.want {
			t.Errorf("%s: ModeratesAny = %v, want %v", c.name, got, c.want)
		}
	}
}

// A revoked grant applies on the next call, and a new one too: the answer is
// cached with the viewer's other decisions and cleared by the same writes.
func TestModeratesAnyFollowsGrants(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	mods := f.newGroup()
	v := f.viewer(domain.RoleMember, mods)
	if f.moderatesAny(v) {
		t.Fatal("before the grant")
	}
	f.grant(forum, mods, domain.PermModerate)
	if !f.moderatesAny(v) {
		t.Fatal("after the grant")
	}
	if err := f.ev.Revoke(t.Context(), audit.By{}, forum, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	if f.moderatesAny(v) {
		t.Fatal("after the revoke")
	}
}

// A warm answer costs no statement, so the gate and /me can both ask.
func TestModeratesAnyIsCached(t *testing.T) {
	var mu sync.Mutex
	counting, ran := false, 0
	f := newFixtureWith(t, func(sql string) {
		mu.Lock()
		defer mu.Unlock()
		if counting {
			ran++
		}
	})
	f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	v := f.viewer(domain.RoleMember)
	f.moderatesAny(v)
	mu.Lock()
	counting = true
	mu.Unlock()
	if f.moderatesAny(v) {
		t.Fatal("a plain member moderates")
	}
	mu.Lock()
	defer mu.Unlock()
	if ran != 0 {
		t.Errorf("a warm ModeratesAny ran %d statements", ran)
	}
}

// TestPromoteUnchanged: moving Promote onto internal/audit changed nothing
// it writes. The rows are compared whole, column by column.
func TestPromoteUnchanged(t *testing.T) {
	f := newFixture(t)
	by := f.viewer(domain.RoleAdmin)
	member := f.viewer(domain.RoleMember)
	admin := f.viewer(domain.RoleAdmin)
	if _, err := f.ev.Promote(t.Context(), member.UserID, Promotion{Details: map[string]string{"via": "cli"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ev.Promote(t.Context(), admin.UserID, Promotion{Once: true, Actor: by.UserID,
		Details: map[string]string{"via": "bootstrap"}}); err != nil {
		t.Fatal(err)
	}
	type row struct {
		at                           int64
		actor                        sql.NullInt64
		action, typ, id, details, ip string
	}
	var rows []row
	if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rs, err := tx.QueryContext(t.Context(), `SELECT at, actor_id, action, target_type, target_id, details_json, ip
			FROM audit_log ORDER BY id`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.at, &r.actor, &r.action, &r.typ, &r.id, &r.details, &r.ip); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		return rs.Err()
	}); err != nil {
		t.Fatal(err)
	}
	at := f.clock.now().UnixMilli()
	want := []row{
		{at, sql.NullInt64{}, ActionPromoted, "user", strconv.FormatInt(member.UserID, 10), `{"role":"admin","via":"cli"}`, ""},
		{at, sql.NullInt64{Int64: by.UserID, Valid: true}, ActionPromoted, "user", strconv.FormatInt(admin.UserID, 10),
			`{"already":"admin","role":"admin","via":"bootstrap"}`, ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d: %+v, want %+v", i, rows[i], want[i])
		}
	}
}
