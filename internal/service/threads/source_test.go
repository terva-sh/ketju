package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestSourceRefusal is the rule for who reads a post's source
// (TKT-01M3QJYV4Z): the author of a post they may edit now or later, and a
// moderator of any post that is not moved; another member never, whatever
// the post's state; the author of a hidden post forbidden, and of a deleted
// or moved one conflict. A moved post is only reachable here, since no
// record moves a post yet.
func TestSourceRefusal(t *testing.T) {
	const author, other = 7, 8
	member := func(id int64) access {
		return access{viewer: domain.Viewer{UserID: id, Role: domain.RoleMember, Active: true}, class: ClassMember}
	}
	moderator := func(id int64) access {
		return access{viewer: domain.Viewer{UserID: id, Role: domain.RoleModerator, Active: true},
			class: ClassModerator, canModerate: true}
	}

	const (
		visible  = threadlog.EntryFlags(0)
		deleted  = threadlog.EntryDeleted
		hidden   = threadlog.EntryHiddenByMod
		review   = threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden
		moved    = threadlog.EntryMoved
		both     = threadlog.EntryDeleted | threadlog.EntryHiddenByMod
		edited   = threadlog.EntryEdited | threadlog.EntryMachineAuthored | threadlog.EntryHasAttachments
		redacted = threadlog.EntryRedacted
	)
	const ok, forbidden, conflict = problem.Code(""), problem.CodeForbidden, problem.CodeConflict
	for _, c := range []struct {
		name  string
		a     access
		flags threadlog.EntryFlags
		want  problem.Code
	}{
		{"a visible post, to its author", member(author), visible, ok},
		{"an edited agent's post with attachments, to its author", member(author), edited, ok},
		{"a visible post, to another member", member(other), visible, forbidden},
		{"a visible post, to a moderator", moderator(other), visible, ok},
		{"a deleted post, to its author", member(author), deleted, conflict},
		{"a deleted post, to another member", member(other), deleted, forbidden},
		{"a deleted post, to a moderator", moderator(other), deleted, ok},
		{"a hidden post, to its author", member(author), hidden, forbidden},
		{"a hidden post, to another member", member(other), hidden, forbidden},
		{"a hidden post, to a moderator", moderator(other), hidden, ok},
		{"a post hidden pending review, to its author", member(author), review, forbidden},
		{"a post hidden pending review, to a moderator", moderator(other), review, ok},
		{"a moved post, to its author", member(author), moved, conflict},
		{"a moved post, to another member", member(other), moved, forbidden},
		{"a moved post, to a moderator", moderator(other), moved, conflict},
		{"a hidden and moved post, to its author", member(author), moved | hidden, forbidden},
		{"a hidden and moved post, to a moderator", moderator(other), moved | hidden, conflict},
		{"a deleted and hidden post, to its author", member(author), both, forbidden},
		{"a deleted and hidden post, to a moderator", moderator(other), both, ok},
		{"a moderator's own deleted post", moderator(author), deleted, ok},
		{"a moderator's own moved post", moderator(author), moved, conflict},
		{"a post to an anonymous viewer", access{viewer: domain.Anonymous(), class: ClassAnon}, visible, forbidden},
		// A hard redaction left nothing to edit, and nobody reads it
		// (TKT-01M29CXJZ9).
		{"a redacted post, to its author", member(author), redacted, conflict},
		{"a redacted post, to another member", member(other), redacted, forbidden},
		{"a redacted post, to a moderator", moderator(other), redacted, conflict},
		{"a moderator's own redacted post", moderator(author), redacted, conflict},
		{"a redacted post a moderator had hidden, to a moderator", moderator(other), redacted | hidden, conflict},
		{"a redacted post a moderator had hidden, to its author", member(author), redacted | hidden, forbidden},
		{"a redacted post its author had deleted, to a moderator", moderator(other), redacted | deleted, conflict},
	} {
		got := codeOf(sourceRefusal(c.a, threadlog.IndexEntry{Author: author, Flags: c.flags}))
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// TestSourceNeverOutrunsRevealRemoved: over every combination of the bits
// that remove a post, and every kind of reader, a removed post's source is
// served only to a viewer revealRemoved gives its content to, and to none of
// them but a moderator. So a rule that narrows revealRemoved, as redaction
// does (TKT-01M29CXJZ9), narrows this with it. A redacted post, whatever
// else it carries, goes to nobody.
func TestSourceNeverOutrunsRevealRemoved(t *testing.T) {
	const author = 7
	readers := map[string]access{
		"author":           {viewer: domain.Viewer{UserID: author, Role: domain.RoleMember, Active: true}, class: ClassMember},
		"member":           {viewer: domain.Viewer{UserID: 8, Role: domain.RoleMember, Active: true}, class: ClassMember},
		"moderator":        {viewer: domain.Viewer{UserID: 9, Role: domain.RoleModerator, Active: true}, class: ClassModerator, canModerate: true},
		"moderator author": {viewer: domain.Viewer{UserID: author, Role: domain.RoleModerator, Active: true}, class: ClassModerator, canModerate: true},
		"inactive author":  {viewer: domain.Viewer{UserID: author, Role: domain.RoleMember}, class: ClassAnon},
	}
	bits := []threadlog.EntryFlags{threadlog.EntryDeleted, threadlog.EntryMoved, threadlog.EntryHiddenByMod,
		threadlog.EntryAutoHidden, threadlog.EntryRedacted}
	for mask := 0; mask < 1<<len(bits); mask++ {
		var f threadlog.EntryFlags
		for i, b := range bits {
			if mask&(1<<i) != 0 {
				f |= b
			}
		}
		e := threadlog.IndexEntry{Author: author, Flags: f}
		for name, a := range readers {
			served := sourceRefusal(a, e) == nil
			switch {
			case f.Has(threadlog.EntryRedacted):
				if served {
					t.Errorf("redacted %#x to %s: served", f, name)
				}
			case f.Visible():
				if want := a.canModerate || name == "author"; served != want {
					t.Errorf("visible %#x to %s: served %v, want %v", f, name, served, want)
				}
			case served && (!revealRemoved(a, e) || !a.canModerate):
				t.Errorf("removed %#x to %s: served, and revealRemoved %v, moderator %v",
					f, name, revealRemoved(a, e), a.canModerate)
			}
		}
	}
}
