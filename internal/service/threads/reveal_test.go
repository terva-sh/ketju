package threads

import (
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestRevealRemoved is the rule for who gets a removed post's content
// (TKT-01M29CKRJ9): a moderator every removed post but a moved one, the
// author only their own post while a moderator's removal holds it, and
// nobody anything through it for a visible post.
func TestRevealRemoved(t *testing.T) {
	const author, other = 7, 8
	member := func(id int64) access {
		return access{viewer: domain.Viewer{UserID: id, Role: domain.RoleMember, Active: true}, class: ClassMember}
	}
	moderator := access{viewer: domain.Viewer{UserID: 9, Role: domain.RoleModerator, Active: true},
		class: ClassModerator, canModerate: true}
	inactive := access{viewer: domain.Viewer{UserID: author, Role: domain.RoleMember}, class: ClassAnon}
	anon := access{viewer: domain.Anonymous(), class: ClassAnon}

	const (
		visible = threadlog.EntryFlags(0)
		deleted = threadlog.EntryDeleted
		hidden  = threadlog.EntryHiddenByMod
		moved   = threadlog.EntryMoved
		both    = threadlog.EntryDeleted | threadlog.EntryHiddenByMod
	)
	for _, c := range []struct {
		name    string
		a       access
		flags   threadlog.EntryFlags
		removal string
		want    bool
	}{
		{"a visible post, to its author", member(author), visible, "", false},
		{"a visible post, to a moderator", moderator, visible, "", false},
		{"a deleted post, to its author", member(author), deleted, dto.RemovalAuthor, false},
		{"a deleted post, to another member", member(other), deleted, dto.RemovalAuthor, false},
		{"a deleted post, to a moderator", moderator, deleted, dto.RemovalAuthor, true},
		{"a hidden post, to its author", member(author), hidden, dto.RemovalModerator, true},
		{"a hidden post, to another member", member(other), hidden, dto.RemovalModerator, false},
		{"a hidden post, to anonymous", anon, hidden, dto.RemovalModerator, false},
		{"a hidden post, to its author's inactive session", inactive, hidden, dto.RemovalModerator, false},
		{"a hidden post, to a moderator", moderator, hidden, dto.RemovalModerator, true},
		{"a moved post, to its author", member(author), moved, "", false},
		{"a moved post, to a moderator", moderator, moved, "", false},
		{"a hidden and moved post, to its author", member(author), moved | hidden, "", false},
		{"a deleted and hidden post, to its author", member(author), both, dto.RemovalAuthor, false},
		{"a deleted and hidden post, to a moderator", moderator, both, dto.RemovalAuthor, true},
	} {
		e := threadlog.IndexEntry{Author: author, Flags: c.flags}
		if got := removal(c.flags); got != c.removal {
			t.Errorf("%s: removal %q, want %q", c.name, got, c.removal)
		}
		if got := revealRemoved(c.a, e); got != c.want {
			t.Errorf("%s: revealRemoved %v, want %v", c.name, got, c.want)
		}
	}
}

// TestPageClass: a page is drawn for the author's own class only when it
// holds a removed post revealRemoved shows them, and a moderator's and
// everyone else's class is the access check's.
func TestPageClass(t *testing.T) {
	alice := access{viewer: domain.Viewer{UserID: 7, Role: domain.RoleMember, Active: true}, class: ClassMember}
	carol := access{viewer: domain.Viewer{UserID: 8, Role: domain.RoleMember, Active: true}, class: ClassMember}
	bob := access{viewer: domain.Viewer{UserID: 9, Role: domain.RoleModerator, Active: true}, class: ClassModerator, canModerate: true}
	anon := access{viewer: domain.Anonymous(), class: ClassAnon}
	snap := func(flags ...threadlog.EntryFlags) snapshot {
		var s snapshot
		for _, f := range flags {
			s.entries = append(s.entries, threadlog.IndexEntry{Author: 7, Flags: f})
		}
		return s
	}
	for _, c := range []struct {
		name string
		a    access
		snap snapshot
		want string
	}{
		{"alice, her hidden post", alice, snap(0, threadlog.EntryHiddenByMod), "author:7"},
		{"alice, her deleted post", alice, snap(0, threadlog.EntryDeleted), ClassMember},
		{"alice, no removed post", alice, snap(0, 0), ClassMember},
		{"carol, alice's hidden post", carol, snap(threadlog.EntryHiddenByMod), ClassMember},
		{"anonymous, alice's hidden post", anon, snap(threadlog.EntryHiddenByMod), ClassAnon},
		{"bob, alice's hidden post", bob, snap(threadlog.EntryHiddenByMod), ClassModerator},
	} {
		if got := pageClass(c.a, c.snap); got != c.want {
			t.Errorf("%s: class %q, want %q", c.name, got, c.want)
		}
	}
}
