package domain

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

// The tag limits (docs/04 `thread_tags`, maintainer, 2026-10-02,
// TKT-01M3M4KVNW).
const (
	// MaxTagLen is the longest tag, in characters. A tag that passes is
	// ASCII, so it is bytes too.
	MaxTagLen = 24
	// MaxTags is the most tags a thread carries.
	MaxTags = 8
)

// ReservedTags are the tags only a moderator of the forum or an agent
// account may set on create (docs/08 "Thread tags", which lists them between
// its reserved-tags markers; TestReservedTagsDocumented holds the two to each
// other). An agent's working thread is marked by one, so a member cannot make
// a human thread look like an agent's.
var ReservedTags = []string{"agent"}

// The ways a tag is refused, each a field code on validation-failed
// (docs/07 `POST /forums/{slug}/threads`).
var (
	// ErrInvalidTag is a tag that is empty once trimmed, or does not match
	// [a-z0-9]+(-[a-z0-9]+)* once folded.
	ErrInvalidTag = errors.New("invalid tag")
	// ErrTagTooLong is a tag over MaxTagLen characters.
	ErrTagTooLong = errors.New("tag too long")
)

// NormalizeTag returns s as a thread stores it, or why it cannot be a tag
// (docs/04 `thread_tags`). Surrounding whitespace is trimmed, and only ASCII
// A-Z is folded: a character that folds to ASCII only through Unicode case
// rules, such as the Kelvin sign, is refused rather than becoming "k".
//
// The length is counted in characters before anything is copied, so a long
// input costs one scan; the result is copied only when it holds an
// uppercase letter.
func NormalizeTag(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrInvalidTag
	}
	if len(s) > MaxTagLen && utf8.RuneCountInString(s) > MaxTagLen {
		return "", ErrTagTooLong
	}
	upper := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
			upper = true
		case c == '-' && i > 0 && i < len(s)-1 && s[i-1] != '-':
		default:
			return "", ErrInvalidTag
		}
	}
	if upper {
		b := []byte(s)
		for i, c := range b {
			if c >= 'A' && c <= 'Z' {
				b[i] = c + 'a' - 'A'
			}
		}
		s = string(b)
	}
	return s, nil
}

// IsReservedTag reports whether t, a tag NormalizeTag returned, is one of
// ReservedTags.
func IsReservedTag(t string) bool {
	return slices.Contains(ReservedTags, t)
}
