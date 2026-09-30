package perm

import (
	"database/sql"
	"github.com/terva-sh/ketju/internal/audit"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestCacheExpiresAt60s is the TTL half of AC4. A write made behind the
// Evaluator's back is invisible while the cached decision lives and visible
// once it has expired, at most TTL later.
func TestCacheExpiresAt60s(t *testing.T) {
	if TTL != 60*time.Second {
		t.Fatalf("TTL is %v; docs/02 sets it at 60 s", TTL)
	}
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	member := f.viewer(domain.RoleMember)
	if d := f.check(member, forum, domain.PermRead); d != Allow {
		t.Fatalf("before the revoke: got %v, want allow", d)
	}

	f.exec("DELETE FROM permissions")
	f.clock.advance(59 * time.Second)
	if d := f.check(member, forum, domain.PermRead); d != Allow {
		t.Fatalf("59 s after filling: got %v, want the cached allow", d)
	}
	f.clock.advance(1 * time.Second)
	if d := f.check(member, forum, domain.PermRead); d != Deny {
		t.Fatalf("60 s after filling: got %v, want deny; a cached decision outlived the TTL", d)
	}
}

// TestWriteInvalidatesImmediately is the invalidation half of AC4: each
// mutation method, run while a decision is cached, changes the very next
// check with no time passing.
func TestWriteInvalidatesImmediately(t *testing.T) {
	type step struct {
		name   string
		mutate func(f *fixture, forum, x int64, v domain.Viewer) error
		want   Decision
	}
	cases := []struct {
		name  string
		setup func(f *fixture, forum, x int64) (domain.Viewer, domain.Permission)
		steps []step
	}{
		{
			name: "grant and revoke",
			setup: func(f *fixture, forum, x int64) (domain.Viewer, domain.Permission) {
				f.grant(0, f.members, domain.PermReply)
				return f.viewer(domain.RoleMember), domain.PermReply
			},
			steps: []step{
				{"revoke", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.Revoke(f.t.Context(), audit.By{}, 0, f.members, domain.PermReply)
				}, Deny},
				{"grant", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.Grant(f.t.Context(), audit.By{}, forum, f.members, domain.PermReply)
				}, Allow},
			},
		},
		{
			name: "membership",
			setup: func(f *fixture, forum, x int64) (domain.Viewer, domain.Permission) {
				f.grant(forum, x, domain.PermUpload)
				return f.viewer(domain.RoleMember), domain.PermUpload
			},
			steps: []step{
				{"add", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.AddMember(f.t.Context(), audit.By{}, x, v.UserID)
				}, Allow},
				{"remove", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.RemoveMember(f.t.Context(), audit.By{}, x, v.UserID)
				}, Deny},
				{"idp sync adds", func(f *fixture, forum, x int64, v domain.Viewer) error {
					_, err := f.ev.SyncIdPGroups(f.t.Context(), v.UserID, IdPSync{Slugs: []string{f.slug(x)}})
					return err
				}, Allow},
				{"idp sync removes", func(f *fixture, forum, x int64, v domain.Viewer) error {
					_, err := f.ev.SyncIdPGroups(f.t.Context(), v.UserID, IdPSync{})
					return err
				}, Deny},
			},
		},
		{
			name: "visibility",
			setup: func(f *fixture, forum, x int64) (domain.Viewer, domain.Permission) {
				f.grant(0, f.members, domain.PermRead)
				return f.viewer(domain.RoleMember), domain.PermRead
			},
			steps: []step{
				{"restrict", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.SetVisibility(f.t.Context(), audit.By{}, forum, domain.VisibilityRestricted)
				}, Hidden},
				{"open", func(f *fixture, forum, x int64, v domain.Viewer) error {
					return f.ev.SetVisibility(f.t.Context(), audit.By{}, forum, domain.VisibilityPublic)
				}, Allow},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			forum, x := f.forum(domain.VisibilityPublic), f.newGroup()
			v, p := c.setup(f, forum, x)
			before := f.check(v, forum, p) // fills the cache
			for _, s := range c.steps {
				if err := s.mutate(f, forum, x, v); err != nil {
					t.Fatal(err)
				}
				if d := f.check(v, forum, p); d != s.want {
					t.Fatalf("%s: got %v on the next check, want %v (was %v)", s.name, d, s.want, before)
				}
				before = s.want
			}
		})
	}
}

// TestMissingForumIsNotCached: a probe for a forum id that does not exist yet
// must not hide the forum once it is created, since creating a forum does not
// invalidate the cache.
func TestMissingForumIsNotCached(t *testing.T) {
	f := newFixture(t)
	f.grant(0, f.members, domain.PermRead)
	member := f.viewer(domain.RoleMember)
	first := f.forum(domain.VisibilityPublic)
	next := first + 1
	if d := f.check(member, next, domain.PermRead); d != Hidden {
		t.Fatalf("before creation: got %v, want hidden", d)
	}
	if got := f.forum(domain.VisibilityPublic); got != next {
		t.Fatalf("the next forum got id %d, not %d", got, next)
	}
	if d := f.check(member, next, domain.PermRead); d != Allow {
		t.Fatalf("after creation: got %v, want allow", d)
	}
}

// TestInvalidateDuringEvaluationIsNotCached: a decision computed from rows
// read before an invalidation must not be stored after it, or it would
// outlive the write that invalidated it by up to the TTL. The clock read, which
// access makes after taking the generation and before evaluating, is where the
// invalidation is slipped in.
func TestInvalidateDuringEvaluationIsNotCached(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	member := f.viewer(domain.RoleMember)

	f.ev.now = func() time.Time {
		f.ev.now = f.clock.now
		f.ev.Invalidate()
		return f.clock.now()
	}
	if d := f.check(member, forum, domain.PermRead); d != Allow {
		t.Fatalf("got %v, want allow", d)
	}
	if n := cached(f.ev); n != 0 {
		t.Fatalf("%d cache entries were stored from an evaluation that an invalidation overtook", n)
	}
	f.check(member, forum, domain.PermRead)
	if n := cached(f.ev); n != 1 {
		t.Fatalf("an undisturbed check left %d cache entries, want 1", n)
	}
}

// TestStaleGenerationIsNotServed: Invalidate bumps the generation before it
// clears the map, and a check that runs between the two must not be answered
// from an entry filled under the old generation. The window is reproduced by
// bumping the generation without clearing.
func TestStaleGenerationIsNotServed(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	member := f.viewer(domain.RoleMember)
	f.check(member, forum, domain.PermRead)

	f.exec("DELETE FROM permissions")
	f.ev.gen.Add(1) // Invalidate's first half
	if d := f.check(member, forum, domain.PermRead); d != Deny {
		t.Fatalf("got %v from an entry of the previous generation, want deny", d)
	}
}

// TestEveryMutationInvalidates: each mutation method clears the cache. The
// behavioural test above shows the effect for most of them; this one covers
// every method, including SetRole, whose effect a held Viewer cannot show.
func TestEveryMutationInvalidates(t *testing.T) {
	f := newFixture(t)
	forum, x, y := f.forum(domain.VisibilityPublic), f.newGroup(), f.newGroup()
	v := f.viewer(domain.RoleMember)
	ctx := t.Context()
	// In order: a Revoke is refused unless the Grant before it made the row,
	// and a RemoveMember is a no-op unless the AddMember before it ran.
	for _, m := range []struct {
		name   string
		mutate func() error
	}{
		{"Grant", func() error { return f.ev.Grant(ctx, audit.By{}, forum, x, domain.PermRead) }},
		{"Revoke", func() error { return f.ev.Revoke(ctx, audit.By{}, forum, x, domain.PermRead) }},
		{"AddMember", func() error { return f.ev.AddMember(ctx, audit.By{}, x, v.UserID) }},
		{"RemoveMember", func() error { return f.ev.RemoveMember(ctx, audit.By{}, x, v.UserID) }},
		{"SyncIdPGroups", func() error {
			// Another group than x, so AddMember never meets an IdP
			// membership of x.
			_, err := f.ev.SyncIdPGroups(ctx, v.UserID, IdPSync{Slugs: []string{f.slug(y)}})
			return err
		}},
		{"SetVisibility", func() error { return f.ev.SetVisibility(ctx, audit.By{}, forum, domain.VisibilityMembers) }},
		{"SetForumArchived", func() error { return f.ev.SetForumArchived(ctx, audit.By{}, forum, true) }},
		{"ChangeRole", func() error { return f.ev.ChangeRole(ctx, audit.By{}, v.UserID, domain.RoleModerator) }},
	} {
		name, mutate := m.name, m.mutate
		f.check(v, forum, domain.PermRead)
		if cached(f.ev) == 0 {
			t.Fatalf("%s: the check did not fill the cache", name)
		}
		if err := mutate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n := cached(f.ev); n != 0 {
			t.Errorf("%s left %d cache entries", name, n)
		}
	}
}

// TestFailedWriteKeepsCache: a write that fails changed nothing, so it has no
// reason to cost every session its cache.
func TestFailedWriteKeepsCache(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	v := f.viewer(domain.RoleMember)
	f.check(v, forum, domain.PermRead)
	// A duplicate grant violates the unique index.
	f.grant(forum, f.members, domain.PermRead)
	f.check(v, forum, domain.PermRead)
	if err := f.ev.Grant(t.Context(), audit.By{}, forum, f.members, domain.PermRead); err == nil {
		t.Fatal("a duplicate grant succeeded")
	}
	if cached(f.ev) != 1 {
		t.Fatal("a failed write cleared the cache")
	}
}

func cached(e *Evaluator) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.entries)
}

// BenchmarkCheckCached is the hot path: a member's check that the cache
// answers.
func BenchmarkCheckCached(b *testing.B) {
	f := newFixture(b)
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	v := f.viewer(domain.RoleMember)
	f.check(v, forum, domain.PermRead)
	ctx := b.Context()
	for b.Loop() {
		if d, err := f.ev.Check(ctx, v, forum, domain.PermRead); d != Allow || err != nil {
			b.Fatal(d, err)
		}
	}
}

// TestConcurrentChecksAndWrites is for the race detector: checks from many
// sessions while grants and revokes invalidate underneath them. After the
// writers stop, every session sees the final state.
func TestConcurrentChecksAndWrites(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	viewers := []domain.Viewer{domain.Anonymous()}
	for range 4 {
		viewers = append(viewers, f.viewer(domain.RoleMember))
	}
	ctx := t.Context()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for _, v := range viewers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := f.ev.Check(ctx, v, forum, domain.PermRead); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	for range 20 {
		if err := f.ev.Grant(ctx, audit.By{}, 0, f.members, domain.PermRead); err != nil {
			t.Fatal(err)
		}
		if err := f.ev.Revoke(ctx, audit.By{}, 0, f.members, domain.PermRead); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	for _, v := range viewers {
		if d := f.check(v, forum, domain.PermRead); d != Deny {
			t.Errorf("viewer %d after the last revoke: got %v, want deny", v.UserID, d)
		}
	}
}

// TestWriteInvalidatesEveryEvaluator: two Evaluators over one database share
// invalidation, so a revoke through one is seen by the other on its next
// check rather than after the TTL (review 383, second finding).
func TestWriteInvalidatesEveryEvaluator(t *testing.T) {
	f := newFixture(t)
	other := New(f.db)
	other.now = f.clock.now
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	member := f.viewer(domain.RoleMember)
	if ok, err := other.Can(t.Context(), member, forum, domain.PermRead); !ok || err != nil {
		t.Fatalf("before the revoke: got %v, %v; want allow", ok, err)
	}
	if err := f.ev.Revoke(t.Context(), audit.By{}, 0, f.members, domain.PermRead); err != nil {
		t.Fatal(err)
	}
	if d, err := other.Check(t.Context(), member, forum, domain.PermRead); d != Deny || err != nil {
		t.Fatalf("the other Evaluator after the revoke: got %v, %v; want deny", d, err)
	}

	// The other Evaluator's entries for sessions that have not come back are
	// swept on its next fill, not left until the TTL.
	idle := f.viewer(domain.RoleMember)
	if err := f.ev.Grant(t.Context(), audit.By{}, 0, f.members, domain.PermReply); err != nil {
		t.Fatal(err)
	}
	other.Can(t.Context(), idle, forum, domain.PermRead)
	if err := f.ev.Revoke(t.Context(), audit.By{}, 0, f.members, domain.PermReply); err != nil {
		t.Fatal(err)
	}
	other.Can(t.Context(), member, forum, domain.PermRead)
	if n := cached(other); n != 1 {
		t.Fatalf("the other Evaluator holds %d entries after a fill, want 1: stale ones were not swept", n)
	}

	// An Evaluator over a different database is not disturbed.
	g := newFixture(t)
	g.grant(0, g.members, domain.PermRead)
	gForum, gMember := g.forum(domain.VisibilityPublic), g.viewer(domain.RoleMember)
	g.check(gMember, gForum, domain.PermRead)
	f.ev.Invalidate()
	if cached(g.ev) != 1 {
		t.Fatal("invalidating one database cleared another database's cache")
	}
}

// TestRoleChangeAppliesToTheNextViewer states what SetRole can promise
// (review 383, first finding): a Viewer built from the users row after the
// change, as the session middleware builds one per request, has the new
// role. A Viewer held from before keeps the old one; the package comment
// says so and TKT-01M29C5Y3E carries the per-request requirement.
func TestRoleChangeAppliesToTheNextViewer(t *testing.T) {
	f := newFixture(t)
	restricted := f.forum(domain.VisibilityRestricted)
	admin := f.viewer(domain.RoleAdmin)
	if d := f.check(admin, restricted, domain.PermRead); d != Allow {
		t.Fatalf("admin before the change: got %v, want allow", d)
	}
	f.viewer(domain.RoleAdmin) // another admin, so this one may be demoted
	if err := f.ev.ChangeRole(t.Context(), audit.By{}, admin.UserID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	var next domain.Viewer
	err := f.db.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		u, err := q.GetUser(t.Context(), admin.UserID)
		next = domain.Viewer{UserID: u.ID, Role: u.Role, Active: u.Status == "active", SessionKey: admin.SessionKey}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if d := f.check(next, restricted, domain.PermRead); d != Hidden {
		t.Fatalf("the next request's Viewer after the demotion: got %v, want hidden", d)
	}
}
