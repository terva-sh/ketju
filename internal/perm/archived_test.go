package perm

import (
	"database/sql"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestArchivedForumIsReadOnly is the first criterion of TKT-01M3N24RHD: in an
// archived forum Check denies create_thread, reply and upload to an
// anonymous viewer, a member, a moderator by role and a member of the
// moderators group, all of whom had them granted; read and moderate are what
// they were before the archive; an admin is allowed all five. CanEach, over
// the archived forum and an open one beside it, agrees with Check.
func TestArchivedForumIsReadOnly(t *testing.T) {
	f := newFixture(t)
	archived, open := f.forum(domain.VisibilityPublic), f.forum(domain.VisibilityPublic)
	mods := f.group(domain.GroupModerators)
	for _, p := range domain.AllPermissions {
		if p != domain.PermModerate {
			// everyone too, so the anonymous viewer has something to lose.
			f.grant(0, f.everyone, p)
			f.grant(0, f.members, p)
		}
	}
	f.grant(archived, mods, domain.PermModerate)
	f.grant(open, mods, domain.PermModerate)

	viewers := []struct {
		name string
		v    domain.Viewer
	}{
		{"anonymous", domain.Anonymous()},
		{"member", f.viewer(domain.RoleMember)},
		{"moderator by role", f.viewer(domain.RoleModerator)},
		{"moderators group", f.viewer(domain.RoleMember, mods)},
		{"admin", f.viewer(domain.RoleAdmin)},
	}
	before := map[string]map[domain.Permission]Decision{}
	for _, c := range viewers {
		before[c.name] = map[domain.Permission]Decision{}
		for _, p := range domain.AllPermissions {
			before[c.name][p] = f.check(c.v, archived, p)
		}
		for _, p := range postingPermissions {
			if before[c.name][p] != Allow {
				t.Fatalf("%s: %s is %v before the archive; the test needs it granted", c.name, p, before[c.name][p])
			}
		}
	}

	if err := f.ev.SetForumArchived(t.Context(), archived, true); err != nil {
		t.Fatal(err)
	}
	for _, c := range viewers {
		for _, p := range domain.AllPermissions {
			want := before[c.name][p]
			switch {
			case c.v.Role == domain.RoleAdmin:
				want = Allow
			case p == domain.PermCreateThread || p == domain.PermReply || p == domain.PermUpload:
				want = Deny
			}
			if got := f.check(c.v, archived, p); got != want {
				t.Errorf("%s, %s in the archived forum: %v, want %v", c.name, p, got, want)
			}
			if got := f.check(c.v, open, p); got != before[c.name][p] {
				t.Errorf("%s, %s in the open forum: %v, want %v; archiving one forum changed another",
					c.name, p, got, before[c.name][p])
			}
			for _, ev := range []*Evaluator{f.ev, New(f.db)} { // warm and cold
				each, err := ev.CanEach(t.Context(), c.v, []int64{archived, open}, p)
				if err != nil {
					t.Fatal(err)
				}
				for _, id := range []int64{archived, open} {
					if want := f.check(c.v, id, p) == Allow; each[id] != want {
						t.Errorf("%s, %s in forum %d: CanEach %v, Check %v", c.name, p, id, each[id], want)
					}
				}
			}
		}
	}

	// Unarchiving gives it all back.
	if err := f.ev.SetForumArchived(t.Context(), archived, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range viewers {
		for _, p := range domain.AllPermissions {
			if got := f.check(c.v, archived, p); got != before[c.name][p] {
				t.Errorf("%s, %s after unarchiving: %v, want %v", c.name, p, got, before[c.name][p])
			}
		}
	}
}

// TestSetForumArchivedInvalidates is the third criterion: SetForumArchived
// writes forums.archived, and two Evaluators over the same database, each
// holding a cached reply Allow, both answer Deny on their next check, not
// only the one that wrote. The archive is read back from the row, so a
// write that only cleared the cache would not pass.
func TestSetForumArchivedInvalidates(t *testing.T) {
	f := newFixture(t)
	other := New(f.db)
	other.now = f.clock.now
	forum := f.forum(domain.VisibilityPublic)
	f.grant(0, f.members, domain.PermRead)
	f.grant(0, f.members, domain.PermReply)
	member := f.viewer(domain.RoleMember)
	for _, ev := range []*Evaluator{f.ev, other} {
		if d, err := ev.Check(t.Context(), member, forum, domain.PermReply); d != Allow || err != nil {
			t.Fatalf("before the archive: %v, %v; want allow", d, err)
		}
	}

	if err := f.ev.SetForumArchived(t.Context(), forum, true); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := f.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.GetForum(t.Context(), forum)
		stored = row.Archived
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("forums.archived is %d after SetForumArchived, want 1", stored)
	}
	for name, ev := range map[string]*Evaluator{"the writer": f.ev, "the other Evaluator": other} {
		if d, err := ev.Check(t.Context(), member, forum, domain.PermReply); d != Deny || err != nil {
			t.Errorf("%s after the archive: %v, %v; want deny on the next check", name, d, err)
		}
		if d, err := ev.Check(t.Context(), member, forum, domain.PermRead); d != Allow || err != nil {
			t.Errorf("%s: read after the archive is %v, %v; want allow", name, d, err)
		}
	}
}
