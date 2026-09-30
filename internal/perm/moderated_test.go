package perm

import (
	"database/sql"
	"reflect"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ModeratedForums' scope (TKT-01M29CRJ4E): the moderator queue shows a
// forum's flags only to someone it lists.
func TestModeratedForums(t *testing.T) {
	var mu sync.Mutex
	counting, ran := false, 0
	f := newFixtureWith(t, func(sql string) {
		mu.Lock()
		defer mu.Unlock()
		if counting {
			ran++
		}
	})
	public := f.forum(domain.VisibilityPublic)
	other := f.forum(domain.VisibilityPublic)
	restricted := f.forum(domain.VisibilityRestricted)
	f.grant(0, f.members, domain.PermRead)
	mods := f.newGroup()
	blind := f.newGroup()
	f.grant(public, mods, domain.PermModerate)
	f.grant(restricted, blind, domain.PermModerate)

	suspended := f.viewer(domain.RoleModerator)
	suspended.Active = false
	for _, c := range []struct {
		name string
		v    domain.Viewer
		want []int64
	}{
		{"anonymous", domain.Anonymous(), nil},
		{"inactive moderator", suspended, nil},
		{"member", f.viewer(domain.RoleMember), []int64{}},
		{"granted on one forum", f.viewer(domain.RoleMember, mods), []int64{public}},
		{"granted where hidden", f.viewer(domain.RoleMember, blind), []int64{}},
		{"moderator by role, not in the restricted forum", f.viewer(domain.RoleModerator), []int64{public, other}},
		{"admin, everywhere", f.viewer(domain.RoleAdmin), []int64{public, other, restricted}},
	} {
		got, err := f.ev.ModeratedForums(t.Context(), c.v)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ModeratedForums = %v, want %v", c.name, got, c.want)
		}
	}

	// One statement cold, which also answers the read checks, and none warm.
	v := f.viewer(domain.RoleMember, mods)
	count := func(fn func()) int {
		mu.Lock()
		counting, ran = true, 0
		mu.Unlock()
		fn()
		mu.Lock()
		defer mu.Unlock()
		counting = false
		return ran
	}
	if n := count(func() { f.ev.ModeratedForums(t.Context(), v) }); n != 1 {
		t.Errorf("a cold ModeratedForums ran %d statements, want 1", n)
	}
	if n := count(func() {
		f.ev.ModeratedForums(t.Context(), v)
		f.moderatesAny(v)
		f.ev.CanEach(t.Context(), v, []int64{public, other, restricted}, domain.PermRead)
	}); n != 0 {
		t.Errorf("warm, ModeratedForums, ModeratesAny and the read checks ran %d statements", n)
	}

	// A revoked grant applies on the next call.
	if err := f.ev.Revoke(t.Context(), audit.By{}, public, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ev.ModeratedForums(t.Context(), v); len(got) != 0 {
		t.Errorf("after the revoke: %v", got)
	}
}

// ForumAccessRowsAll is ForumAccessRows over every forum: the same rows, so
// decide reads them the same way.
func TestForumAccessRowsAllMatches(t *testing.T) {
	f := newFixture(t)
	a := f.forum(domain.VisibilityPublic)
	b := f.forum(domain.VisibilityMembers)
	c := f.forum(domain.VisibilityRestricted)
	g := f.newGroup()
	f.grant(0, f.members, domain.PermRead)
	f.grant(a, g, domain.PermModerate)
	f.grant(c, g, domain.PermRead)
	if err := f.ev.SetForumArchived(t.Context(), b, true); err != nil {
		t.Fatal(err)
	}
	v := f.viewer(domain.RoleMember, g)
	p := accessParams(v, []int64{a, b, c})
	var some []dbq.ForumAccessRowsRow
	var all []dbq.ForumAccessRowsAllRow
	if err := f.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if some, err = q.ForumAccessRows(t.Context(), p); err != nil {
			return err
		}
		all, err = q.ForumAccessRowsAll(t.Context(), dbq.ForumAccessRowsAllParams{
			UserID: p.UserID, EveryoneSlug: p.EveryoneSlug, MembersSlug: p.MembersSlug})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	conv := make([]dbq.ForumAccessRowsRow, len(all))
	for i, r := range all {
		conv[i] = dbq.ForumAccessRowsRow(r)
	}
	if len(some) == 0 || !reflect.DeepEqual(some, conv) {
		t.Fatalf("ForumAccessRows:\n%v\nForumAccessRowsAll:\n%v", some, conv)
	}
}

// The decisions ModeratedForums caches are the decisions Check makes: an
// archived forum stays read-only when the cache was warmed through it, so
// /me's use of it cannot open posting there (TKT-01M3N24RHD).
func TestModeratedForumsCachesWhatCheckDecides(t *testing.T) {
	f := newFixture(t)
	open := f.forum(domain.VisibilityPublic)
	archived := f.forum(domain.VisibilityPublic)
	g := f.newGroup()
	f.grant(0, f.members, domain.PermRead)
	f.grant(0, f.members, domain.PermReply)
	f.grant(0, g, domain.PermModerate)
	if err := f.ev.SetForumArchived(t.Context(), archived, true); err != nil {
		t.Fatal(err)
	}
	v := f.viewer(domain.RoleMember, g)
	got, err := f.ev.ModeratedForums(t.Context(), v)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int64{open, archived}) {
		t.Errorf("ModeratedForums = %v: moderating an archived forum is not posting", got)
	}
	reply, err := f.ev.CanEach(t.Context(), v, []int64{open, archived}, domain.PermReply)
	if err != nil {
		t.Fatal(err)
	}
	if !reply[open] || reply[archived] {
		t.Errorf("reply from the warmed cache: %v, want the open forum only", reply)
	}
}
