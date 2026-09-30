package perm

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The admin forum screen's decision writes (TKT-01M29CXK19): visibility and
// archiving, each audited against the forum in the transaction that makes
// it.

// forumState is a forum's visibility and archived flag as stored.
func (f *fixture) forumState(id int64) (string, int64) {
	f.t.Helper()
	var r dbq.Forum
	if err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		r, err = q.GetForum(f.t.Context(), id)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return r.Visibility, r.Archived
}

// TestForumDecisionWritesAudited: a visibility change writes
// forum.visibility_changed with from and to, an archive forum.archived and
// an unarchive forum.unarchived, each against the forum with the actor and
// address. A change to what the forum already has writes no row. With the audit insert failing, the forum is unchanged and the
// cache kept, since nothing changed.
func TestForumDecisionWritesAudited(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	target := strconv.FormatInt(forum, 10)
	for _, c := range []struct {
		name    string
		write   func() error
		action  string
		details map[string]any
	}{
		{"restrict", func() error { return f.ev.SetVisibility(t.Context(), f.by(), forum, domain.VisibilityRestricted) },
			ActionForumVisibilityChanged, map[string]any{"from": "public", "to": "restricted", "slug": "forum-1"}},
		{"archive", func() error { return f.ev.SetForumArchived(t.Context(), f.by(), forum, true) },
			ActionForumArchived, map[string]any{"slug": "forum-1"}},
		{"unarchive", func() error { return f.ev.SetForumArchived(t.Context(), f.by(), forum, false) },
			ActionForumUnarchived, map[string]any{"slug": "forum-1"}},
	} {
		mark := f.lastAudit()
		if err := c.write(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		rows := f.rowsSince(mark)
		if len(rows) != 1 {
			t.Fatalf("%s: %d audit rows, want 1: %+v", c.name, len(rows), rows)
		}
		r := rows[0]
		if r.action != c.action || r.targetType != "forum" || r.targetID != target || r.actor.Int64 != f.by().Actor ||
			r.ip != f.by().IP {
			t.Errorf("%s: audit row %+v, want %s against forum %s by %+v", c.name, r, c.action, target, f.by())
		}
		for k, want := range c.details {
			if r.details[k] != want {
				t.Errorf("%s: details[%s] = %v, want %v", c.name, k, r.details[k], want)
			}
		}

		// Again: the forum already has it, so no row is written. (The
		// cache is cleared all the same, as after any write that commits;
		// clearing it more often than needed is safe.)
		mark = f.lastAudit()
		if err := c.write(); err != nil {
			t.Fatalf("%s again: %v", c.name, err)
		}
		if rows := f.rowsSince(mark); len(rows) != 0 {
			t.Errorf("%s again wrote %+v", c.name, rows)
		}
	}

	f.failAudit()
	v := f.viewer(domain.RoleMember)
	f.check(v, forum, domain.PermRead)
	gen := f.ev.gen.Load()
	for name, write := range map[string]func() error{
		"visibility": func() error { return f.ev.SetVisibility(t.Context(), f.by(), forum, domain.VisibilityMembers) },
		"archive":    func() error { return f.ev.SetForumArchived(t.Context(), f.by(), forum, true) },
	} {
		if err := write(); err == nil || !strings.Contains(err.Error(), "injected audit failure") {
			t.Fatalf("%s answered %v, want the injected failure", name, err)
		}
	}
	if vis, archived := f.forumState(forum); vis != domain.VisibilityRestricted || archived != 0 {
		t.Errorf("after failed audit inserts the forum is %s, archived %d; want restricted, 0", vis, archived)
	}
	if f.ev.gen.Load() != gen || cached(f.ev) == 0 {
		t.Error("a failed forum write cleared the cache")
	}
}

// TestForumDecisionWritesRefuse: a forum that does not exist is ErrNoForum
// and a visibility that is not one of the three ErrInvalidVisibility, each
// writing nothing.
func TestForumDecisionWritesRefuse(t *testing.T) {
	f := newFixture(t)
	forum := f.forum(domain.VisibilityPublic)
	mark := f.lastAudit()
	if err := f.ev.SetVisibility(t.Context(), f.by(), forum+1, domain.VisibilityMembers); !errors.Is(err, ErrNoForum) {
		t.Errorf("SetVisibility of no forum: %v, want ErrNoForum", err)
	}
	if err := f.ev.SetForumArchived(t.Context(), f.by(), forum+1, true); !errors.Is(err, ErrNoForum) {
		t.Errorf("SetForumArchived of no forum: %v, want ErrNoForum", err)
	}
	for _, bad := range []string{"", "private", "Public"} {
		if err := f.ev.SetVisibility(t.Context(), f.by(), forum, bad); !errors.Is(err, ErrInvalidVisibility) {
			t.Errorf("SetVisibility %q: %v, want ErrInvalidVisibility", bad, err)
		}
	}
	if rows := f.rowsSince(mark); len(rows) != 0 {
		t.Errorf("refusals wrote %+v", rows)
	}
	if vis, _ := f.forumState(forum); vis != domain.VisibilityPublic {
		t.Errorf("the forum is %s after refusals, want public", vis)
	}
}
