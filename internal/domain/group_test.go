package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateGroupSlug(t *testing.T) {
	for _, c := range []struct {
		slug string
		ok   bool
	}{
		{"a", true},
		{"staff", true},
		{"forum-mods", true},
		{"2026-crew", true},
		{strings.Repeat("a", GroupSlugMaxLen), true},
		{"", false},
		{strings.Repeat("a", GroupSlugMaxLen+1), false},
		{"-staff", false},
		{"Staff", false},
		{"forum_mods", false},
		{"forum mods", false},
		{"é", false},
		{"staff/x", false},
	} {
		err := ValidateGroupSlug(c.slug)
		if (err == nil) != c.ok {
			t.Errorf("ValidateGroupSlug(%q) = %v, want ok=%t", c.slug, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrInvalidGroupSlug) {
			t.Errorf("ValidateGroupSlug(%q) = %v, not ErrInvalidGroupSlug", c.slug, err)
		}
	}
}

func TestValidRoleAndPermission(t *testing.T) {
	for _, r := range []string{RoleMember, RoleModerator, RoleAdmin} {
		if !ValidRole(r) {
			t.Errorf("%q is a role", r)
		}
	}
	for _, r := range []string{"", "Admin", "owner"} {
		if ValidRole(r) {
			t.Errorf("%q is not a role", r)
		}
	}
	for _, p := range AllPermissions {
		if !ValidPermission(p) {
			t.Errorf("%q is a permission", p)
		}
	}
	if ValidPermission("admin") || ValidPermission("") {
		t.Error("an unknown permission passed")
	}
}
