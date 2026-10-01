package forums

import (
	"database/sql"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The front page footer's member count (TKT-01M3S3ZRYD, docs/06 "Thread
// list views (forum index)").

// user adds an account and returns its id.
func (f *adminFixture) user(handle, status, kind string) int64 {
	f.t.Helper()
	var id int64
	if err := f.db.Write(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateUser(f.t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle, Status: status,
			Role: domain.RoleMember, Kind: kind, CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		f.t.Fatalf("creating %s: %v", handle, err)
	}
	return id
}

// members is Tree's members field for v: -1 for null.
func (f *adminFixture) members(v domain.Viewer) int {
	f.t.Helper()
	tree, err := f.s.Tree(f.t.Context(), v)
	if err != nil {
		f.t.Fatal(err)
	}
	if tree.Members == nil {
		return -1
	}
	return *tree.Members
}

// TestForumTreeMembersOnlyForMembers: the count goes to a member session
// only, the viewers the mention lookup serves. An anonymous viewer, a
// pre-user and a signed-in account that is not active are given null, on a
// tree that lists a forum and on one that lists none.
func TestForumTreeMembersOnlyForMembers(t *testing.T) {
	f := newAdminFixture(t)
	alice := f.user("alice", domain.StatusActive, "human")
	gus := f.user("gus", domain.StatusSuspended, "human")
	member := domain.Viewer{UserID: alice, Role: domain.RoleMember, Active: true, SessionKey: "s-alice"}
	notMembers := map[string]domain.Viewer{
		"anonymous": domain.Anonymous(),
		"pre-user":  {PreUser: true, IdentityID: 7, Active: true, SessionKey: "s-pre"},
		"suspended": {UserID: gus, Role: domain.RoleMember, Active: false, SessionKey: "s-gus"},
	}
	check := func(when string) {
		t.Helper()
		if got := f.members(member); got != 2 {
			t.Errorf("%s: alice is told %d members, want 2 (dora and alice)", when, got)
		}
		for name, v := range notMembers {
			if got := f.members(v); got != -1 {
				t.Errorf("%s: the %s viewer is told %d members, want null", when, name, got)
			}
		}
	}
	check("with no forum")
	f.create("general", 0)
	check("with a public forum")
}

// TestForumTreeMembersCountsActiveAccounts: the count is exactly the
// accounts the mention lookup can offer. A pending, suspended, deactivated
// or anonymized account is not counted, nor an active one whose handle ends
// in '-'; an agent is. The count equals the distinct accounts
// UsersByHandlePrefix returns over every prefix a handle can start with,
// which is the parity the decision rests on, and it moves when a member is
// suspended.
func TestForumTreeMembersCountsActiveAccounts(t *testing.T) {
	f := newAdminFixture(t) // dora, an active admin
	f.user("alice", domain.StatusActive, "human")
	f.user("scribe", domain.StatusActive, "agent")
	f.user("trailing-", domain.StatusActive, "human")
	f.user("pat", domain.StatusPending, "human")
	f.user("gus", domain.StatusSuspended, "human")
	f.user("dee", domain.StatusDeactivated, "human")
	gone := f.user("erin", domain.StatusActive, "human")
	carl := f.user("carl", domain.StatusActive, "human")
	if err := f.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		return q.AnonymizeUser(t.Context(), dbq.AnonymizeUserParams{ID: gone, UpdatedAt: 2})
	}); err != nil {
		t.Fatal(err)
	}
	viewer := domain.Viewer{UserID: carl, Role: domain.RoleMember, Active: true, SessionKey: "s-carl"}

	// dora, alice, scribe and carl.
	if got := f.members(viewer); got != 4 {
		t.Errorf("members = %d, want 4: dora, alice, scribe (an agent) and carl", got)
	}
	if got, want := f.members(viewer), f.lookupable(); got != want {
		t.Errorf("members = %d, but the mention lookup offers %d accounts", got, want)
	}

	if err := f.db.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		return q.SetUserStatus(t.Context(), dbq.SetUserStatusParams{Status: domain.StatusSuspended, ID: carl, UpdatedAt: 3})
	}); err != nil {
		t.Fatal(err)
	}
	dora := f.admin
	if got := f.members(dora); got != 3 {
		t.Errorf("after carl's suspension members = %d, want 3", got)
	}
	if got, want := f.members(dora), f.lookupable(); got != want {
		t.Errorf("after carl's suspension members = %d, but the mention lookup offers %d accounts", got, want)
	}
}

// lookupable is the distinct accounts the mention lookup returns over every
// prefix a handle can start with: a letter or a digit (0002's handle rule).
func (f *adminFixture) lookupable() int {
	f.t.Helper()
	seen := map[string]bool{}
	if err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		for _, c := range "abcdefghijklmnopqrstuvwxyz0123456789" {
			rows, err := q.UsersByHandlePrefix(f.t.Context(), dbq.UsersByHandlePrefixParams{
				Lo: string(c), Hi: string(c) + "{", Lim: 1 << 20})
			if err != nil {
				return err
			}
			for _, r := range rows {
				seen[r.Handle] = true
			}
		}
		return nil
	}); err != nil {
		f.t.Fatal(err)
	}
	return len(seen)
}
