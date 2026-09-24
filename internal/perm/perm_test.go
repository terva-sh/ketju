package perm

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// fixture is a migrated database with the seeded grants removed, so every
// test states the rows it depends on.
type fixture struct {
	t     testing.TB
	db    *db.DB
	ev    *Evaluator
	clock *fakeClock
	n     int

	everyone, members int64
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, db: d, clock: &fakeClock{t: time.UnixMilli(1_790_000_000_000)}}
	f.exec("DELETE FROM permissions")
	f.ev = New(d)
	f.ev.now = f.clock.now
	f.everyone = f.group(domain.GroupEveryone)
	f.members = f.group(domain.GroupMembers)
	return f
}

// exec writes behind the Evaluator's back, as a stray write would.
func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(f.t.Context(), query, args...)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) group(slug string) int64 {
	f.t.Helper()
	var id int64
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		g, err := q.GetGroupBySlug(f.t.Context(), slug)
		id = g.ID
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) newGroup() int64 {
	f.t.Helper()
	f.n++
	var id int64
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateGroup(f.t.Context(), dbq.CreateGroupParams{
			Name: fmt.Sprintf("Group %d", f.n), Slug: fmt.Sprintf("group-%d", f.n), CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) forum(visibility string) int64 {
	f.t.Helper()
	f.n++
	var id int64
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateForum(f.t.Context(), dbq.CreateForumParams{
			Slug: fmt.Sprintf("forum-%d", f.n), Name: "Forum", Position: int64(f.n),
			Visibility: visibility, CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

// viewer creates a user and returns them as a signed-in, active viewer in
// each of groups.
func (f *fixture) viewer(role string, groups ...int64) domain.Viewer {
	f.t.Helper()
	f.n++
	var id int64
	err := f.db.Write(f.t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateUser(f.t.Context(), dbq.CreateUserParams{
			Handle: fmt.Sprintf("user%d", f.n), DisplayName: "User", Status: "active",
			Role: role, Kind: "human", CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, g := range groups {
		if err := f.ev.AddMember(f.t.Context(), g, id, 0); err != nil {
			f.t.Fatal(err)
		}
	}
	return domain.Viewer{UserID: id, Role: role, Active: true, SessionKey: fmt.Sprintf("session-%d", id)}
}

func (f *fixture) grant(forumID, groupID int64, p domain.Permission) {
	f.t.Helper()
	if err := f.ev.Grant(f.t.Context(), forumID, groupID, p); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) check(v domain.Viewer, forumID int64, p domain.Permission) Decision {
	f.t.Helper()
	d, err := f.ev.Check(f.t.Context(), v, forumID, p)
	if err != nil {
		f.t.Fatal(err)
	}
	return d
}

// TestUnionWithGlobalFallback is AC1. Each case grants rows to the implicit
// groups or to an extra group X, on the forum or globally, and states by hand
// what an anonymous viewer, a plain member, and a member of X get. It runs
// for every permission, with "other" a different permission, so a grant of
// one never leaks into another.
//
// Fallback is per group (settled in grooming): a group with any row on the
// forum uses those rows, and a group without falls back to its global rows,
// whatever other groups have on the forum.
func TestUnionWithGlobalFallback(t *testing.T) {
	type row struct {
		global bool
		group  string // "everyone", "members", or "x"
		other  bool   // grant the other permission rather than the one checked
	}
	cases := []struct {
		name                string
		rows                []row
		anon, member, xUser Decision
	}{
		{"no rows", nil, Deny, Deny, Deny},
		{"global members row", []row{{global: true, group: "members"}}, Deny, Allow, Allow},
		{"global everyone row", []row{{global: true, group: "everyone"}}, Allow, Allow, Allow},
		{"forum row for x", []row{{group: "x"}}, Deny, Deny, Allow},
		{"global row for x", []row{{global: true, group: "x"}}, Deny, Deny, Allow},
		{"forum and global rows for x", []row{{group: "x"}, {global: true, group: "x"}}, Deny, Deny, Allow},
		{"only the other permission", []row{{group: "x", other: true}, {global: true, group: "members", other: true}},
			Deny, Deny, Deny},
		// x has a row on the forum, so its global row no longer applies.
		{"forum row overrides x's global row", []row{{group: "x", other: true}, {global: true, group: "x"}},
			Deny, Deny, Deny},
		// members has a row on the forum; x has none there and falls back.
		{"fallback is per group", []row{{group: "members", other: true}, {global: true, group: "x"}},
			Deny, Deny, Allow},
		// members' forum row overrides its own global row, for everyone in it.
		{"forum row overrides members' global row", []row{{group: "members", other: true}, {global: true, group: "members"}},
			Deny, Deny, Deny},
		{"everyone's forum row overrides its global row", []row{{group: "everyone", other: true}, {global: true, group: "everyone"}},
			Deny, Deny, Deny},
		{"union across groups", []row{{group: "everyone", other: true}, {group: "x"}}, Deny, Deny, Allow},
	}
	for i, p := range domain.AllPermissions {
		other := domain.AllPermissions[(i+1)%len(domain.AllPermissions)]
		for _, c := range cases {
			t.Run(string(p)+"/"+c.name, func(t *testing.T) {
				f := newFixture(t)
				x := f.newGroup()
				forum := f.forum(domain.VisibilityPublic)
				ids := map[string]int64{"everyone": f.everyone, "members": f.members, "x": x}
				for _, r := range c.rows {
					at, grant := forum, p
					if r.global {
						at = 0
					}
					if r.other {
						grant = other
					}
					f.grant(at, ids[r.group], grant)
				}
				member, xUser := f.viewer(domain.RoleMember), f.viewer(domain.RoleMember, x)
				for name, got := range map[string][2]Decision{
					"anonymous": {f.check(domain.Anonymous(), forum, p), c.anon},
					"member":    {f.check(member, forum, p), c.member},
					"x member":  {f.check(xUser, forum, p), c.xUser},
				} {
					if got[0] != got[1] {
						t.Errorf("%s: got %v, want %v", name, got[0], got[1])
					}
				}
			})
		}
	}
}

// TestAdminPassesEverything is AC2: an active admin, in no group and with no
// rows anywhere, is allowed everything, even on a restricted forum. The
// database is closed before the checks, so an admin check that touched it
// would fail rather than pass.
func TestAdminPassesEverything(t *testing.T) {
	f := newFixture(t)
	restricted := f.forum(domain.VisibilityRestricted)
	public := f.forum(domain.VisibilityPublic)
	admin := f.viewer(domain.RoleAdmin)
	f.db.Close()
	for _, forum := range []int64{restricted, public, 999} {
		for _, p := range domain.AllPermissions {
			d, err := f.ev.Check(t.Context(), admin, forum, p)
			if err != nil || d != Allow {
				t.Errorf("admin, forum %d, %s: got %v, %v; want allow", forum, p, d, err)
			}
		}
	}
	// The same check for a member does reach the database, which is what
	// makes the closed database above evidence.
	if _, err := f.ev.Check(t.Context(), f.memberViewerNoDB(), public, domain.PermRead); err == nil {
		t.Fatal("a member's check succeeded against a closed database; the admin test proves nothing")
	}
}

func (f *fixture) memberViewerNoDB() domain.Viewer {
	return domain.Viewer{UserID: 1, Role: domain.RoleMember, Active: true, SessionKey: "no-db"}
}

// TestInactiveCountsAsAnonymous: a suspended admin or moderator has no role
// and no member rights, only what everyone has.
func TestInactiveCountsAsAnonymous(t *testing.T) {
	f := newFixture(t)
	f.grant(0, f.members, domain.PermRead)
	f.grant(0, f.everyone, domain.PermReply)
	public := f.forum(domain.VisibilityPublic)
	restricted := f.forum(domain.VisibilityRestricted)
	for _, role := range []string{domain.RoleAdmin, domain.RoleModerator, domain.RoleMember} {
		v := f.viewer(role)
		v.Active = false
		if d := f.check(v, public, domain.PermRead); d != Deny {
			t.Errorf("inactive %s, read on public: got %v, want deny (no members grant)", role, d)
		}
		if d := f.check(v, public, domain.PermReply); d != Allow {
			t.Errorf("inactive %s, reply on public: got %v, want allow (the everyone grant)", role, d)
		}
		if d := f.check(v, public, domain.PermModerate); d != Deny {
			t.Errorf("inactive %s, moderate: got %v, want deny", role, d)
		}
		if d := f.check(v, restricted, domain.PermRead); d != Hidden {
			t.Errorf("inactive %s, restricted: got %v, want hidden", role, d)
		}
	}
}

// TestRestrictedIsHiddenNotForbidden is AC3. Without read, a restricted forum
// is Hidden for every permission, so the caller answers 404 and the forum's
// existence does not leak. A public forum the viewer can read but not reply
// in is Deny, a 403. Global rows do not open a restricted forum; only rows on
// the forum itself do.
func TestRestrictedIsHiddenNotForbidden(t *testing.T) {
	f := newFixture(t)
	x := f.newGroup()
	for _, p := range domain.AllPermissions {
		f.grant(0, f.members, p)
		f.grant(0, f.everyone, p)
	}
	restricted := f.forum(domain.VisibilityRestricted)
	public := f.forum(domain.VisibilityPublic)
	f.grant(restricted, x, domain.PermRead)
	f.exec("DELETE FROM permissions WHERE forum_id IS NULL AND permission = 'reply'")
	f.ev.Invalidate()

	member, xUser := f.viewer(domain.RoleMember), f.viewer(domain.RoleMember, x)
	for _, p := range domain.AllPermissions {
		if d := f.check(member, restricted, p); d != Hidden {
			t.Errorf("member, restricted, %s: got %v, want hidden", p, d)
		}
		if d := f.check(domain.Anonymous(), restricted, p); d != Hidden {
			t.Errorf("anonymous, restricted, %s: got %v, want hidden", p, d)
		}
	}
	if d := f.check(member, public, domain.PermReply); d != Deny {
		t.Errorf("member, reply on public without the grant: got %v, want deny", d)
	}
	if d := f.check(xUser, restricted, domain.PermRead); d != Allow {
		t.Errorf("x member, read on restricted with the forum row: got %v, want allow", d)
	}
	if d := f.check(xUser, restricted, domain.PermCreateThread); d != Deny {
		t.Errorf("x member, create_thread on restricted with only read: got %v, want deny", d)
	}
	if d := f.check(member, 12345, domain.PermRead); d != Hidden {
		t.Errorf("a forum that does not exist: got %v, want hidden", d)
	}

	for _, c := range []struct {
		v    domain.Viewer
		want []int64
	}{{member, []int64{public}}, {xUser, []int64{restricted, public}}} {
		got, err := f.ev.ReadableForums(t.Context(), c.v)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("ReadableForums for user %d: got %v, want %v", c.v.UserID, got, c.want)
		}
	}
}

// TestMembersVisibility: a members forum ignores the everyone grant, so an
// anonymous viewer is denied (not hidden: a members forum's existence is not
// secret) while a member gets the members grant.
func TestMembersVisibility(t *testing.T) {
	f := newFixture(t)
	f.grant(0, f.everyone, domain.PermRead)
	f.grant(0, f.members, domain.PermReply)
	forum := f.forum(domain.VisibilityMembers)
	member := f.viewer(domain.RoleMember)
	if d := f.check(domain.Anonymous(), forum, domain.PermRead); d != Deny {
		t.Errorf("anonymous, read on members forum: got %v, want deny", d)
	}
	if d := f.check(member, forum, domain.PermRead); d != Deny {
		t.Errorf("member, read on members forum with only the everyone grant: got %v, want deny", d)
	}
	if d := f.check(member, forum, domain.PermReply); d != Allow {
		t.Errorf("member, reply on members forum: got %v, want allow", d)
	}
}

// TestModeratorRole: role=moderator grants moderate on any forum the viewer
// can see, without the moderators group, and nothing else. It does not open a
// restricted forum.
func TestModeratorRole(t *testing.T) {
	f := newFixture(t)
	public := f.forum(domain.VisibilityPublic)
	restricted := f.forum(domain.VisibilityRestricted)
	mod := f.viewer(domain.RoleModerator)
	if d := f.check(mod, public, domain.PermModerate); d != Allow {
		t.Errorf("moderator, moderate on public: got %v, want allow", d)
	}
	if d := f.check(mod, public, domain.PermReply); d != Deny {
		t.Errorf("moderator, reply with no rows: got %v, want deny", d)
	}
	if d := f.check(mod, restricted, domain.PermModerate); d != Hidden {
		t.Errorf("moderator, restricted without read: got %v, want hidden", d)
	}
	if d := f.check(f.viewer(domain.RoleMember), public, domain.PermModerate); d != Deny {
		t.Errorf("member, moderate: got %v, want deny", d)
	}
}
