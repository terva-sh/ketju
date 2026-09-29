package domain

import (
	"errors"
	"fmt"
)

// GroupSlugMaxLen is the longest slug a created group may have.
const GroupSlugMaxLen = 40

// ErrInvalidGroupSlug is returned by ValidateGroupSlug. The wrapped message
// says which part of the rule failed.
var ErrInvalidGroupSlug = errors.New("invalid group slug")

// ValidateGroupSlug reports whether s is a valid slug for a new group: 1 to
// 40 characters from a-z, 0-9 and '-', starting with a letter or a number
// (docs/04 "groups, group_members", settled in TKT-01M29CXK1X). The slug is
// what an IdP group_map names, so it is kept to characters a config file
// and a URL carry without quoting.
func ValidateGroupSlug(s string) error {
	if n := len(s); n < 1 || n > GroupSlugMaxLen {
		return fmt.Errorf("%w: %d characters, want 1 to %d", ErrInvalidGroupSlug, n, GroupSlugMaxLen)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		case c == '-':
			return fmt.Errorf("%w: must start with a letter or a number", ErrInvalidGroupSlug)
		default:
			return fmt.Errorf("%w: %q is not allowed; use a-z, 0-9 or '-'", ErrInvalidGroupSlug, rune(c))
		}
	}
	return nil
}

// ValidRole reports whether r is one of the three roles.
func ValidRole(r string) bool {
	return r == RoleMember || r == RoleModerator || r == RoleAdmin
}

// ValidPermission reports whether p is one of AllPermissions.
func ValidPermission(p Permission) bool {
	for _, q := range AllPermissions {
		if p == q {
			return true
		}
	}
	return false
}
