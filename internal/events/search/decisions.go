package search

import "github.com/terva-sh/ketju/internal/store/threadlog"

// effect is what a record can do to the index.
type effect int

const (
	// none: the record changes nothing the index holds. Its decision says
	// why.
	none effect = iota

	// touchesOrdinal: the record can change its post's text or whether the
	// post is visible, so after the scan the post's row is put or dropped
	// from the thread's index entry.
	touchesOrdinal

	// setsTitle: the record sets the thread's title.
	setsTitle

	// touchesOrdinals: a thread-level record that can change whether the
	// posts its body names are visible, so each is put or dropped as
	// touchesOrdinal's one is. A PostsSplitOut marks its posts moved.
	touchesOrdinals
)

// decision is what the indexer does with one record type: an effect, and a
// reason when the effect is none. TestEveryRecordTypeHasAnIndexDecision holds
// every catalogued type to having one, and every type that can change a
// post's index entry to touching its ordinal or ordinals, so a record added
// to the catalogue cannot be skipped by the index without somebody writing
// down why (Terva review 1695).
type decision struct {
	effect effect
	reason string
}

var decisions = map[threadlog.RecordType]decision{
	threadlog.TypeThreadCreated: {effect: setsTitle},
	threadlog.TypePost:          {effect: touchesOrdinal},
	threadlog.TypeEdit:          {effect: touchesOrdinal},
	// Every form: an author's delete, a moderator's hide (by role or by
	// group), the auto-hide, and a suspension's hide. Each sets a flag that
	// makes the entry not Visible (threadlog postFlagEffect).
	threadlog.TypeDelete:  {effect: touchesOrdinal},
	threadlog.TypeRestore: {effect: touchesOrdinal},
	// A post marker touches its post; a title marker blanks the title, which
	// the ThreadRenamed after it replaces. index.go reads the body.
	threadlog.TypeRedacted:      {effect: touchesOrdinal},
	threadlog.TypeThreadRenamed: {effect: setsTitle},

	threadlog.TypeThreadMoved: {reason: "the forum is the thread's, joined from threads when a search is served, " +
		"so a move changes no row"},
	threadlog.TypeThreadLocked:   {reason: "a lock changes who may post, not what may be read"},
	threadlog.TypeThreadUnlocked: {reason: "an unlock changes who may post, not what may be read"},
	threadlog.TypeThreadPinned:   {reason: "a pin changes the thread list's order, which search does not use"},
	threadlog.TypeThreadUnpinned: {reason: "an unpin changes the thread list's order, which search does not use"},
	threadlog.TypeThreadTagged:   {reason: "tags are not indexed"},
	threadlog.TypeThreadArchived: {reason: "an archived thread stays readable, and so searchable"},

	// A move (TKT-01M29CXJZZ). The split marks every post it names moved,
	// which is not Visible, so their rows go. The posts land in the target
	// as Posts with an origin, which the Post decision indexes there.
	threadlog.TypePostsSplitOut: {effect: touchesOrdinals},
	threadlog.TypePostsMergedIn: {reason: "the copies that follow it are Posts, indexed as any Post is"},
}
