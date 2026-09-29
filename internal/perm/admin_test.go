package perm

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The admin screens' writes (TKT-01M29CXK1X): roles, memberships and grants,
// each audited in its own transaction.

// by is who the admin tests act as: an account made on first use, as a
// member so that it counts in no admin tally, and an address.
func (f *fixture) by() audit.By {
	f.t.Helper()
	if f.actor == 0 {
		f.actor = f.viewer(domain.RoleMember).UserID
	}
	return audit.By{Actor: f.actor, IP: "192.0.2.7"}
}

// adminRow is one audit_log row as these tests read it.
type adminRow struct {
	action, targetType, targetID string
	actor                        sql.NullInt64
	ip                           string
	details                      map[string]any
}

// rowsSince is every audit row after id, in order.
func (f *fixture) rowsSince(after int64) []adminRow {
	f.t.Helper()
	var out []adminRow
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(f.t.Context(), `SELECT action, target_type, target_id, actor_id, ip, details_json
			FROM audit_log WHERE id > ? ORDER BY id`, after)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r adminRow
			var dj string
			if err := rows.Scan(&r.action, &r.targetType, &r.targetID, &r.actor, &r.ip, &dj); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(dj), &r.details); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// lastAudit is the newest audit row's id, or 0.
func (f *fixture) lastAudit() int64 {
	f.t.Helper()
	var id sql.NullInt64
	if err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(f.t.Context(), `SELECT max(id) FROM audit_log`).Scan(&id)
	}); err != nil {
		f.t.Fatal(err)
	}
	return id.Int64
}

// failAudit makes every later audit insert fail.
func (f *fixture) failAudit() {
	f.exec(`CREATE TRIGGER test_audit_fails BEFORE INSERT ON audit_log
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`)
}

// TestRoleChangeAuditedInOneTransaction is AC4 for roles: each kind of role
// change writes its own action against the user, with the actor, address
// and from/to, and with the audit insert failing the role is unchanged and
// the cache kept, since nothing changed.
func TestRoleChangeAuditedInOneTransaction(t *testing.T) {
	for _, c := range []struct {
		name, from, to, action string
	}{
		{"promote", domain.RoleMember, domain.RoleAdmin, ActionPromoted},
		{"promote a moderator", domain.RoleModerator, domain.RoleAdmin, ActionPromoted},
		{"demote", domain.RoleAdmin, domain.RoleMember, ActionDemoted},
		{"demote to moderator", domain.RoleAdmin, domain.RoleModerator, ActionDemoted},
		{"member to moderator", domain.RoleMember, domain.RoleModerator, ActionRoleChanged},
		{"moderator to member", domain.RoleModerator, domain.RoleMember, ActionRoleChanged},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.viewer(domain.RoleAdmin) // the admin acting, so a demotion is not of the last one
			v := f.viewer(c.from)
			mark := f.lastAudit()
			if err := f.ev.ChangeRole(t.Context(), f.by(), v.UserID, c.to); err != nil {
				t.Fatal(err)
			}
			if got := f.role(v.UserID); got != c.to {
				t.Fatalf("role %s, want %s", got, c.to)
			}
			rows := f.rowsSince(mark)
			if len(rows) != 1 {
				t.Fatalf("%d audit rows, want 1: %+v", len(rows), rows)
			}
			r := rows[0]
			if r.action != c.action || r.targetType != audit.TypeUser || r.targetID != strconv.FormatInt(v.UserID, 10) ||
				r.actor.Int64 != f.by().Actor || r.ip != f.by().IP || r.details["from"] != c.from {
				t.Errorf("audit row %+v, want %s against user %d from %s by %+v", r, c.action, v.UserID, c.from, f.by())
			}
			if c.action != ActionPromoted && r.details["to"] != c.to {
				t.Errorf("audit details %v, want to=%s", r.details, c.to)
			}

			// Back again, with the audit insert failing: nothing changes.
			f.viewer(domain.RoleAdmin)
			fresh := f.viewer(domain.RoleMember)
			forum := f.forum(domain.VisibilityPublic)
			f.check(fresh, forum, domain.PermRead)
			gen := f.ev.gen.Load()
			f.failAudit()
			err := f.ev.ChangeRole(t.Context(), f.by(), v.UserID, c.from)
			if err == nil || !strings.Contains(err.Error(), "injected audit failure") {
				t.Fatalf("ChangeRole answered %v, want the injected failure", err)
			}
			if got := f.role(v.UserID); got != c.to {
				t.Errorf("after a failed audit insert the role is %s, want %s unchanged", got, c.to)
			}
			if f.ev.gen.Load() != gen || cached(f.ev) == 0 {
				t.Error("a failed role change cleared the cache")
			}
		})
	}
}

// A change to the role a user already has writes nothing, and a role that
// is not one is refused before anything is read.
func TestChangeRoleNoOpAndInvalid(t *testing.T) {
	f := newFixture(t)
	v := f.viewer(domain.RoleModerator)
	mark := f.lastAudit()
	if err := f.ev.ChangeRole(t.Context(), f.by(), v.UserID, domain.RoleModerator); err != nil {
		t.Fatal(err)
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("a no-op wrote %+v", rows)
	}
	if err := f.ev.ChangeRole(t.Context(), f.by(), v.UserID, "owner"); !errors.Is(err, ErrInvalidRole) {
		t.Errorf("an unknown role: %v, want ErrInvalidRole", err)
	}
	if err := f.ev.ChangeRole(t.Context(), f.by(), 9999, domain.RoleMember); !errors.Is(err, ErrNoUser) {
		t.Errorf("nobody: %v, want ErrNoUser", err)
	}
}

// TestDemotionLeavesPromotionRows is AC5: demoting a bootstrap admin writes
// user.demoted and leaves their user.promoted row where it is, so the
// bootstrap's promotion, which is Promote with Once, does not make them
// admin again at the next sign-in. internal/web/auth's
// TestBootstrapPromotesOnceAtSignIn drives the same through a real sign-in.
func TestDemotionLeavesPromotionRows(t *testing.T) {
	f := newFixture(t)
	f.viewer(domain.RoleAdmin)
	v := f.viewer(domain.RoleMember)
	bootstrap := Promotion{Once: true, Details: map[string]string{"via": "bootstrap"}}
	if ok, err := f.ev.Promote(t.Context(), v.UserID, bootstrap); err != nil || !ok {
		t.Fatalf("bootstrap: %v, %v", ok, err)
	}
	if err := f.ev.ChangeRole(t.Context(), f.by(), v.UserID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if n := len(f.promotions(v.UserID)); n != 1 {
		t.Fatalf("%d user.promoted rows after the demotion, want the 1 bootstrap wrote", n)
	}
	if ok, err := f.ev.Promote(t.Context(), v.UserID, bootstrap); err != nil || ok {
		t.Fatalf("the next sign-in's bootstrap promoted again: %v, %v", ok, err)
	}
	if r := f.role(v.UserID); r != domain.RoleMember {
		t.Errorf("role %s after the demotion and a sign-in, want member", r)
	}
}

// TestCannotDemoteTheLastAdmin is AC5: the last active admin cannot be
// demoted, to either role, and a suspended admin does not count as one.
// With a second active admin the demotion goes through, and then the other
// is the last.
func TestCannotDemoteTheLastAdmin(t *testing.T) {
	f := newFixture(t)
	a := f.viewer(domain.RoleAdmin)
	suspended := f.viewer(domain.RoleAdmin)
	f.exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, suspended.UserID)
	mark := f.lastAudit()
	for _, to := range []string{domain.RoleMember, domain.RoleModerator} {
		if err := f.ev.ChangeRole(t.Context(), f.by(), a.UserID, to); !errors.Is(err, ErrLastAdmin) {
			t.Errorf("demoting the last active admin to %s: %v, want ErrLastAdmin", to, err)
		}
	}
	if r := f.role(a.UserID); r != domain.RoleAdmin {
		t.Fatalf("the last admin is now %s", r)
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("a refused demotion wrote %+v", rows)
	}
	// An inactive admin may be demoted: it leaves the active count alone.
	if err := f.ev.ChangeRole(t.Context(), f.by(), suspended.UserID, domain.RoleMember); err != nil {
		t.Errorf("demoting a suspended admin: %v", err)
	}

	b := f.viewer(domain.RoleAdmin)
	if err := f.ev.ChangeRole(t.Context(), f.by(), a.UserID, domain.RoleMember); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}
	if err := f.ev.ChangeRole(t.Context(), f.by(), b.UserID, domain.RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demoting the remaining admin: %v, want ErrLastAdmin", err)
	}
}

// TestMembershipWritesAudited is AC4 for memberships: an add and a remove
// each write their row against the user, with the actor, address and group,
// in the transaction that changes the membership; with the audit insert
// failing, the membership is unchanged. Adding a member already in the
// group, or removing one who is not, changes and records nothing.
func TestMembershipWritesAudited(t *testing.T) {
	f := newFixture(t)
	g := f.newGroup()
	v := f.viewer(domain.RoleMember)
	mark := f.lastAudit()
	if err := f.ev.AddMember(t.Context(), f.by(), g, v.UserID); err != nil {
		t.Fatal(err)
	}
	if err := f.ev.AddMember(t.Context(), f.by(), g, v.UserID); err != nil {
		t.Fatal(err)
	}
	if got := f.memberships(v.UserID)[f.slug(g)]; got != domain.MembershipAdmin {
		t.Fatalf("membership source %q, want admin", got)
	}
	if err := f.ev.RemoveMember(t.Context(), f.by(), g, v.UserID); err != nil {
		t.Fatal(err)
	}
	if err := f.ev.RemoveMember(t.Context(), f.by(), g, v.UserID); err != nil {
		t.Fatal(err)
	}
	rows := f.rowsSince(mark)
	if len(rows) != 2 || rows[0].action != ActionMemberAdded || rows[1].action != ActionMemberRemoved {
		t.Fatalf("audit rows %+v, want one add and one remove", rows)
	}
	for _, r := range rows {
		if r.targetType != audit.TypeUser || r.targetID != strconv.FormatInt(v.UserID, 10) || r.actor.Int64 != f.by().Actor ||
			r.ip != f.by().IP || r.details["group"] != f.slug(g) || r.details["source"] != domain.MembershipAdmin {
			t.Errorf("audit row %+v", r)
		}
	}
	if added := f.addedBy(g, v.UserID); added.Valid {
		t.Errorf("the removed membership still has an adder %v", added)
	}

	f.failAudit()
	if err := f.ev.AddMember(t.Context(), f.by(), g, v.UserID); err == nil {
		t.Fatal("an add with a failing audit insert succeeded")
	}
	if _, ok := f.memberships(v.UserID)[f.slug(g)]; ok {
		t.Error("an add whose audit insert failed left the membership")
	}

	// Implicit groups, and nobody, are refused.
	for _, slug := range []string{domain.GroupEveryone, domain.GroupMembers} {
		if err := f.ev.AddMember(t.Context(), f.by(), f.group(slug), v.UserID); !errors.Is(err, ErrImplicitGroup) {
			t.Errorf("adding to %s: %v, want ErrImplicitGroup", slug, err)
		}
	}
	if err := f.ev.AddMember(t.Context(), f.by(), 9999, v.UserID); !errors.Is(err, ErrNoGroup) {
		t.Errorf("no group: %v", err)
	}
	if err := f.ev.AddMember(t.Context(), f.by(), g, 9999); !errors.Is(err, ErrNoUser) {
		t.Errorf("no user: %v", err)
	}
}

// addedBy is the membership's added_by, invalid when there is no row.
func (f *fixture) addedBy(groupID, userID int64) sql.NullInt64 {
	f.t.Helper()
	var added sql.NullInt64
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		err := tx.QueryRowContext(f.t.Context(), `SELECT added_by FROM group_members WHERE group_id = ? AND user_id = ?`,
			groupID, userID).Scan(&added)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return added
}

// TestIdPMembershipNotEditable is AC2: removing a membership an IdP mapping
// made is ErrIdPMembership (conflict) and the row stays; adding over one is
// the same and its source stays idp. Neither writes an audit row.
func TestIdPMembershipNotEditable(t *testing.T) {
	f := newFixture(t)
	g := f.newGroup()
	v := f.viewer(domain.RoleMember)
	f.sync(v.UserID, IdPSync{Provider: "club", Slugs: []string{f.slug(g)}})
	mark := f.lastAudit()
	if err := f.ev.RemoveMember(t.Context(), f.by(), g, v.UserID); !errors.Is(err, ErrIdPMembership) {
		t.Errorf("removing an idp membership: %v, want ErrIdPMembership", err)
	}
	if err := f.ev.AddMember(t.Context(), f.by(), g, v.UserID); !errors.Is(err, ErrIdPMembership) {
		t.Errorf("adding over an idp membership: %v, want ErrIdPMembership", err)
	}
	if got := f.memberships(v.UserID)[f.slug(g)]; got != domain.MembershipIdP {
		t.Errorf("membership source %q, want idp left as it was", got)
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("refused edits wrote %+v", rows)
	}
}

// TestGrantsApplyInOneTransaction is AC1 and AC4 for grants: a batch
// applies every change with one audit row each, or, when any change is
// invalid or disagrees with the rows, none of them and no row.
func TestGrantsApplyInOneTransaction(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	g := f.newGroup()
	f.grant(0, f.members, domain.PermRead)

	good := []GrantChange{
		{ForumID: forum, GroupID: g, Permission: domain.PermRead, Grant: true},
		{ForumID: 0, GroupID: g, Permission: domain.PermUpload, Grant: true},
	}
	for name, third := range map[string]GrantChange{
		"unknown permission":  {ForumID: forum, GroupID: g, Permission: "admin", Grant: true},
		"unknown forum":       {ForumID: 9999, GroupID: g, Permission: domain.PermReply, Grant: true},
		"unknown group":       {ForumID: forum, GroupID: 9999, Permission: domain.PermReply, Grant: true},
		"the same row twice":  {ForumID: forum, GroupID: g, Permission: domain.PermRead, Grant: false},
		"a grant that exists": {ForumID: 0, GroupID: f.members, Permission: domain.PermRead, Grant: true},
		"a revoke of nothing": {ForumID: forum, GroupID: f.members, Permission: domain.PermReply},
	} {
		mark := f.lastAudit()
		err := f.ev.ApplyGrants(t.Context(), f.by(), append(append([]GrantChange{}, good...), third))
		if !errors.Is(err, ErrInvalidChange) && !errors.Is(err, ErrGrantState) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
		if n := len(f.grid()); n != 1 {
			t.Errorf("%s: %d permission rows, want the 1 there before", name, n)
		}
		if rows := f.rowsSince(mark); len(rows) != 0 {
			t.Errorf("%s: a refused batch wrote %d audit rows", name, len(rows))
		}
	}

	mark := f.lastAudit()
	batch := append(append([]GrantChange{}, good...), GrantChange{ForumID: 0, GroupID: f.members, Permission: domain.PermRead})
	if err := f.ev.ApplyGrants(t.Context(), f.by(), batch); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{fmt.Sprintf("%d:%d:read", forum, g): true, fmt.Sprintf("0:%d:upload", g): true}
	if got := f.grid(); len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Errorf("rows %v, want %v", got, want)
	}
	rows := f.rowsSince(mark)
	if len(rows) != 3 {
		t.Fatalf("%d audit rows, want 3", len(rows))
	}
	wantRows := []struct{ action, id string }{
		{ActionPermissionGranted, fmt.Sprintf("%d:%d:read", forum, g)},
		{ActionPermissionGranted, fmt.Sprintf("global:%d:upload", g)},
		{ActionPermissionRevoked, fmt.Sprintf("global:%d:read", f.members)},
	}
	for i, r := range rows {
		if r.action != wantRows[i].action || r.targetType != audit.TypePermission || r.targetID != wantRows[i].id ||
			r.actor.Int64 != f.by().Actor || r.ip != f.by().IP {
			t.Errorf("audit row %d: %+v, want %+v", i, r, wantRows[i])
		}
	}

	// A failing audit insert takes the whole batch with it.
	f.failAudit()
	if err := f.ev.ApplyGrants(t.Context(), f.by(), []GrantChange{{ForumID: forum, GroupID: f.members,
		Permission: domain.PermReply, Grant: true}}); err == nil {
		t.Fatal("a batch with a failing audit insert succeeded")
	}
	if n := len(f.grid()); n != 2 {
		t.Errorf("%d rows after a failed batch, want 2", n)
	}
}

// Review 1416: the read that answers for a batch is in the batch's
// transaction, so a batch whose answer cannot be read is not made, and a
// read that succeeds sees the batch applied.
func TestApplyGrantsThenReadsInTheSameTransaction(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	v := f.viewer(domain.RoleMember)
	change := []GrantChange{{ForumID: forum, GroupID: f.members, Permission: domain.PermRead, Grant: true}}
	f.check(v, forum, domain.PermRead)
	mark := f.lastAudit()
	gen := f.ev.gen.Load()
	failed := errors.New("the read failed")
	if err := f.ev.ApplyGrantsThen(t.Context(), f.by(), change, func(*dbq.Queries) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("ApplyGrantsThen answered %v, want the read's error", err)
	}
	if n := len(f.grid()); n != 0 {
		t.Errorf("%d rows after a batch whose read failed, want none", n)
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("a batch whose read failed left %d audit rows", len(rows))
	}
	if f.ev.gen.Load() != gen {
		t.Error("a batch whose read failed cleared the cache")
	}

	var seen int
	if err := f.ev.ApplyGrantsThen(t.Context(), f.by(), change, func(q *dbq.Queries) error {
		rows, err := q.ListPermissions(t.Context())
		seen = len(rows)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Errorf("the read saw %d rows, want the 1 the batch made", seen)
	}
}

// A role or membership change whose read fails is not made, and the read
// sees the change it follows (Terva review 1423).
func TestRoleAndMembershipThenReadInTheSameTransaction(t *testing.T) {
	f := newFixture(t)
	f.viewer(domain.RoleAdmin)
	v := f.viewer(domain.RoleMember)
	g := f.newGroup()
	mark := f.lastAudit()
	failed := errors.New("the read failed")
	fail := func(*dbq.Queries) error { return failed }
	if err := f.ev.ChangeRoleThen(t.Context(), f.by(), v.UserID, domain.RoleModerator, fail); !errors.Is(err, failed) {
		t.Errorf("ChangeRoleThen answered %v, want the read's error", err)
	}
	if got := f.role(v.UserID); got != domain.RoleMember {
		t.Errorf("role %s after a change whose read failed, want member", got)
	}
	if err := f.ev.AddMemberThen(t.Context(), f.by(), g, v.UserID, fail); !errors.Is(err, failed) {
		t.Errorf("AddMemberThen answered %v, want the read's error", err)
	}
	if _, in := f.memberships(v.UserID)[f.slug(g)]; in {
		t.Error("a membership whose read failed was made")
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("changes whose read failed left %d audit rows", len(rows))
	}

	var role string
	if err := f.ev.ChangeRoleThen(t.Context(), f.by(), v.UserID, domain.RoleModerator, func(q *dbq.Queries) error {
		u, err := q.GetUser(t.Context(), v.UserID)
		role = u.Role
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if role != domain.RoleModerator {
		t.Errorf("the read saw role %s, want moderator", role)
	}
	count := func(n *int) func(q *dbq.Queries) error {
		return func(q *dbq.Queries) error {
			ms, err := q.GroupMembers(t.Context(), g)
			*n = len(ms)
			return err
		}
	}
	var added, removed int
	if err := f.ev.AddMemberThen(t.Context(), f.by(), g, v.UserID, count(&added)); err != nil {
		t.Fatal(err)
	}
	if err := f.ev.RemoveMemberThen(t.Context(), f.by(), g, v.UserID, count(&removed)); err != nil {
		t.Fatal(err)
	}
	if added != 1 || removed != 0 {
		t.Errorf("the reads saw %d members after the add and %d after the remove, want 1 and 0", added, removed)
	}
}

// PreviewThen reads in the transaction it decided in: the read runs, and
// its error is the preview's (Terva review 1424).
func TestPreviewThenReadsInTheSameTransaction(t *testing.T) {
	f := newFixture(t)
	f.forum(domain.VisibilityPublic)
	v := f.viewer(domain.RoleModerator)
	failed := errors.New("the read failed")
	if _, err := f.ev.PreviewThen(t.Context(), v.UserID, nil, func(*dbq.Queries) error { return failed }); !errors.Is(err, failed) {
		t.Errorf("PreviewThen answered %v, want the read's error", err)
	}
	var role string
	effects, err := f.ev.PreviewThen(t.Context(), v.UserID, nil, func(q *dbq.Queries) error {
		u, err := q.GetUser(t.Context(), v.UserID)
		role = u.Role
		return err
	})
	if err != nil || len(effects) != 1 || role != domain.RoleModerator {
		t.Errorf("PreviewThen: %d effects, role %q, %v; want 1, moderator, nil", len(effects), role, err)
	}
}

// grid is every permission row as "<forum or 0>:<group>:<permission>".
func (f *fixture) grid() []string {
	f.t.Helper()
	var out []string
	if err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		rows, err := q.ListPermissions(f.t.Context())
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%d:%d:%s", r.ForumID.Int64, r.GroupID, r.Permission))
		}
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// TestPreviewMatchesApply is AC3: for random grids and random batches over
// them, what Preview says a member could do after the batch is what Check
// says once ApplyGrants has applied it, per forum and permission, for
// members of assorted roles, groups and statuses; and its before is what
// Check says now.
func TestPreviewMatchesApply(t *testing.T) {
	seed := rand.Uint64()
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewPCG(seed, 0))
	for round := range 40 {
		f := newFixture(t)
		var forums []int64
		for range 2 + rng.IntN(4) {
			vis := []string{domain.VisibilityPublic, domain.VisibilityMembers, domain.VisibilityRestricted}[rng.IntN(3)]
			forums = append(forums, f.forum(vis))
		}
		groups := []int64{f.everyone, f.members, f.newGroup(), f.newGroup(), f.group(domain.GroupModerators)}
		places := append([]int64{0}, forums...)
		pick := func() rowKey {
			return rowKey{places[rng.IntN(len(places))], groups[rng.IntN(len(groups))],
				domain.AllPermissions[rng.IntN(len(domain.AllPermissions))]}
		}
		have := map[rowKey]bool{}
		for range rng.IntN(12) {
			k := pick()
			if !have[k] {
				f.grant(k.forum, k.group, k.p)
				have[k] = true
			}
		}
		roles := []string{domain.RoleMember, domain.RoleMember, domain.RoleModerator, domain.RoleAdmin}
		var mine []int64
		for _, g := range groups[2:] {
			if rng.IntN(2) == 0 {
				mine = append(mine, g)
			}
		}
		v := f.viewer(roles[rng.IntN(len(roles))], mine...)
		if rng.IntN(5) == 0 {
			f.exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, v.UserID)
			v.Active = false
		}

		var changes []GrantChange
		staged := map[rowKey]bool{}
		for range 1 + rng.IntN(6) {
			k := pick()
			if staged[k] {
				continue
			}
			staged[k] = true
			changes = append(changes, GrantChange{ForumID: k.forum, GroupID: k.group, Permission: k.p, Grant: !have[k]})
		}

		effects, err := f.ev.Preview(t.Context(), v.UserID, changes)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if len(effects) != len(forums) {
			t.Fatalf("round %d: %d effects for %d forums", round, len(effects), len(forums))
		}
		for _, fe := range effects {
			for _, p := range domain.AllPermissions {
				if got := f.check(v, fe.ForumID, p); fe.Before[p] != got {
					t.Errorf("round %d forum %d %s: preview before %v, Check %v", round, fe.ForumID, p, fe.Before[p], got)
				}
			}
		}
		if err := f.ev.ApplyGrants(t.Context(), f.by(), changes); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		for _, fe := range effects {
			for _, p := range domain.AllPermissions {
				if got := f.check(v, fe.ForumID, p); fe.After[p] != got {
					t.Errorf("round %d forum %d %s: preview after %v, Check after applying %v (changes %+v)",
						round, fe.ForumID, p, fe.After[p], got, changes)
				}
			}
		}
	}
}

// Preview refuses what ApplyGrants would refuse, and names nobody as
// ErrNoUser.
func TestPreviewRefusesWhatApplyRefuses(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	v := f.viewer(domain.RoleMember)
	if _, err := f.ev.Preview(t.Context(), v.UserID, []GrantChange{{ForumID: forum, GroupID: f.members,
		Permission: domain.PermRead}}); !errors.Is(err, ErrGrantState) {
		t.Errorf("a revoke of nothing: %v, want ErrGrantState", err)
	}
	if _, err := f.ev.Preview(t.Context(), 9999, nil); !errors.Is(err, ErrNoUser) {
		t.Errorf("nobody: %v, want ErrNoUser", err)
	}
}

// TestOnlyRoleWritesSetTheRole: inside this package, only promote and
// ChangeRoleThen (which ChangeRole calls) call SetUserRole, so no path
// changes a role without its audit row. TestMutationsGoThroughPerm keeps
// the call out of every other package.
func TestOnlyRoleWritesSetTheRole(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"promote": true, "ChangeRoleThen": true}
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "SetUserRole" {
					found[fn.Name.Name] = true
					if !allowed[fn.Name.Name] {
						t.Errorf("%s calls SetUserRole in %s; only promote and ChangeRoleThen may, since they audit it",
							fset.Position(call.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	for name := range allowed {
		if !found[name] {
			t.Errorf("%s no longer calls SetUserRole; the walk may be missing the file", name)
		}
	}
}

// The two overlay cases a random batch reaches only by luck: the everyone
// group's first row, which a members forum must still ignore, and a
// group's last row on a forum, whose revoke hands the group back to its
// global rows.
func TestPreviewEdgeCases(t *testing.T) {
	f := newFixture(t)
	members := f.forum(domain.VisibilityMembers)
	public := f.forum(domain.VisibilityPublic)
	g := f.newGroup()
	v := f.viewer(domain.RoleMember, g)
	f.grant(0, g, domain.PermReply)
	f.grant(public, g, domain.PermRead)

	changes := []GrantChange{
		{ForumID: 0, GroupID: f.everyone, Permission: domain.PermRead, Grant: true},
		{ForumID: public, GroupID: g, Permission: domain.PermRead},
	}
	effects, err := f.ev.Preview(t.Context(), v.UserID, changes)
	if err != nil {
		t.Fatal(err)
	}
	after := map[int64]map[domain.Permission]Decision{}
	for _, fe := range effects {
		after[fe.ForumID] = fe.After
	}
	if d := after[members][domain.PermRead]; d != Deny {
		t.Errorf("everyone's first global read reached a members forum: %v", d)
	}
	if d := after[public][domain.PermReply]; d != Allow {
		t.Errorf("revoking the group's last row on the forum did not fall back to its global reply: %v", d)
	}
	if d := after[public][domain.PermRead]; d != Allow {
		t.Errorf("everyone's global read on a public forum: %v", d)
	}
	if err := f.ev.ApplyGrants(t.Context(), f.by(), changes); err != nil {
		t.Fatal(err)
	}
	for id, want := range after {
		for p, d := range want {
			if got := f.check(v, id, p); got != d {
				t.Errorf("forum %d %s: preview %v, applied %v", id, p, d, got)
			}
		}
	}
}
