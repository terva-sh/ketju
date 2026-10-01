// Package lens is the thread page's lens framework (docs/06 "Lenses",
// TKT-01M29CRJ1P): the ?lens= grammar, the filter a lens is, and the
// selection of posts a filter makes from a thread's in-memory index.
//
// A lens filters and never reorders (docs/06 "The stance"). The types hold
// that rule rather than a comment: a Filter answers keep or skip for one post
// at a time, Apply walks the index once in ascending order, and a Selection
// keeps its ordinals where nothing outside this package can build or reorder
// them.
//
// A lens chooses ordinals and nothing else. What each post shows, a removed
// post's body included, is the canonical page's rule (revealRemoved in
// internal/service/threads), applied to whatever ordinals the lens chose.
package lens

import (
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
)

// Kind names a lens. Canonical, the empty kind, is no lens: the full thread.
type Kind string

// The kinds docs/07 "What the thread endpoint does" decides the grammar of.
// A kind is served once internal/service/threads has a resolver for it; until
// then a well-formed lens of that kind serves the canonical page.
const (
	Canonical Kind = ""
	Unread    Kind = "unread"
	Author    Kind = "author"
	Chain     Kind = "chain"
	Since     Kind = "since"
	Mentions  Kind = "mentions"
	Mod       Kind = "mod"
)

// Spec is a parsed ?lens= value. Only the fields its kind uses are set.
type Spec struct {
	Kind Kind
	// Ordinal is unread's boundary B (the posts after it) or chain's post.
	Ordinal uint32
	// Handle is author's handle, ASCII letters lowercased. It is the handle
	// as asked for; a resolver decides whom it names.
	Handle string
	// SinceMS is since's moment as Unix ms, never negative.
	SinceMS int64
	// Bare is unread with no boundary: the viewer's own read position
	// decides it (docs/06 "the unread lens holds its boundary").
	Bare bool
}

// Parse reads a ?lens= value. It is total: anything that is not one of the
// decided forms, the empty string included, is Spec{Kind: Canonical}, so an
// old or mistyped link opens the thread rather than an error (docs/07,
// decided 2026-10-01).
//
// The forms are unread, unread:<ordinal>, author:<handle>, chain:<ordinal>,
// since:<when>, mentions and mod. <when> is a date YYYY-MM-DD read as
// midnight UTC, an RFC 3339 time, or Unix ms. A moment before 1970 reads as
// 0, which selects the same posts. Parse(s.String()) is s for every Spec
// Parse returns.
func Parse(raw string) Spec {
	kind, value, hasValue := strings.Cut(raw, ":")
	switch Kind(kind) {
	case Unread:
		if !hasValue {
			return Spec{Kind: Unread, Bare: true}
		}
		if n, ok := ordinal(value); ok {
			return Spec{Kind: Unread, Ordinal: n}
		}
	case Chain:
		if n, ok := ordinal(value); ok && n > 0 {
			return Spec{Kind: Chain, Ordinal: n}
		}
	case Author:
		if h, ok := handle(value); ok {
			return Spec{Kind: Author, Handle: h}
		}
	case Since:
		if ms, ok := moment(value); ok {
			return Spec{Kind: Since, SinceMS: ms}
		}
	case Mentions, Mod:
		if !hasValue {
			return Spec{Kind: Kind(kind)}
		}
	}
	return Spec{}
}

// String is the lens in the form Parse reads, with since's moment as Unix ms:
// kind alone when Value is empty, kind:value otherwise. Canonical is "".
func (s Spec) String() string {
	if v := s.Value(); v != "" {
		return string(s.Kind) + ":" + v
	}
	return string(s.Kind)
}

// Value is the part of String after the colon: unread's boundary, author's
// handle, chain's ordinal or since's moment in Unix ms, and "" for a kind
// that takes none and for bare unread.
func (s Spec) Value() string {
	switch s.Kind {
	case Unread:
		if s.Bare {
			return ""
		}
		return strconv.FormatUint(uint64(s.Ordinal), 10)
	case Chain:
		return strconv.FormatUint(uint64(s.Ordinal), 10)
	case Author:
		return s.Handle
	case Since:
		return strconv.FormatInt(s.SinceMS, 10)
	}
	return ""
}

// ordinal is a decimal number that fits an ordinal: digits only, no sign.
func ordinal(s string) (uint32, bool) {
	if !digits(s) {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 32)
	return uint32(n), err == nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// handle is an author lens's handle, lowercased: one domain.ValidateHandle
// accepts, which deleted-user-<id> is (docs/07: the placeholder for an
// author the database lacks). Whom it names is the resolver's question.
func handle(s string) (string, bool) {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	h := string(b)
	return h, domain.ValidateHandle(h) == nil
}

// moment is since's <when> as Unix ms.
func moment(s string) (int64, bool) {
	var ms int64
	switch {
	case digits(s):
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		ms = n
	case len(s) == len("2006-01-02"):
		t, err := time.Parse(time.DateOnly, s)
		if err != nil {
			return 0, false
		}
		ms = t.UnixMilli()
	default:
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return 0, false
		}
		ms = t.UnixMilli()
	}
	return max(ms, 0), true
}
