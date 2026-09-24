// Package domain holds ketju's pure types and the rules that belong to them.
// It imports nothing of ours, so every layer can depend on it (docs/02).
package domain

import (
	"errors"
	"fmt"
)

// Handle length bounds, from docs/04 "users".
const (
	HandleMinLen = 3
	HandleMaxLen = 30
)

// ErrInvalidHandle is returned by ValidateHandle for a handle that breaks the
// rule. The wrapped message says which part.
var ErrInvalidHandle = errors.New("invalid handle")

// ValidateHandle reports whether h is a valid stored handle: 3 to 30
// characters from a-z, 0-9, '_' and '-', starting with a letter or digit
// (docs/04, decided 2026-09-24). It does not lowercase for the caller:
// registration normalises first and then validates what it will store.
//
// The users table enforces the same rule with a CHECK constraint, and
// TestHandleRuleMatchesTheSchema in internal/store/db holds the two together.
// ASCII only, on purpose: a handle appears in URLs and @mentions, where a
// homoglyph must never pass for someone else.
func ValidateHandle(h string) error {
	if n := len(h); n < HandleMinLen || n > HandleMaxLen {
		return fmt.Errorf("%w: %d characters, want %d to %d", ErrInvalidHandle, n, HandleMinLen, HandleMaxLen)
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '_' || c == '-') && i > 0:
		case c == '_' || c == '-':
			return fmt.Errorf("%w: must start with a letter or digit", ErrInvalidHandle)
		default:
			return fmt.Errorf("%w: %q is not allowed; use a-z, 0-9, '_' or '-'", ErrInvalidHandle, rune(c))
		}
	}
	return nil
}
