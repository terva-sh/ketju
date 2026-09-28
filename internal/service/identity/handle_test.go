package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
)

// AC1 and AC3: a handle is normalized, then held to the rule, then to the
// reserved list, which is compared on the normalized form and so without
// regard to case.
func TestCheckHandle(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  error
	}{
		{in: "alice", want: "alice"},
		{in: "Alice", want: "alice"},
		{in: "  ALICE_99 ", want: "alice_99"},
		{in: "a-b", want: "a-b"},
		{in: "abc", want: "abc"},
		{in: strings.Repeat("a", 30), want: strings.Repeat("a", 30)},
		// The rule, after normalizing.
		{in: "", err: domain.ErrInvalidHandle},
		{in: "ab", err: domain.ErrInvalidHandle},
		{in: strings.Repeat("a", 31), err: domain.ErrInvalidHandle},
		{in: "_alice", err: domain.ErrInvalidHandle},
		{in: "al ice", err: domain.ErrInvalidHandle},
		{in: "al.ice", err: domain.ErrInvalidHandle},
		{in: "alicé", err: domain.ErrInvalidHandle},
		// The Kelvin sign folds to "k" under strings.ToLower; here it is
		// refused as the non-ASCII character it is.
		{in: "Kate", err: domain.ErrInvalidHandle},
		// Reserved: the top-level routes, and anything that could pass for
		// an anonymized account, in any case.
		{in: "admin", err: ErrHandleReserved},
		{in: "Admin", err: ErrHandleReserved},
		{in: "ADMIN", err: ErrHandleReserved},
		{in: "api", err: ErrHandleReserved},
		{in: "assets", err: ErrHandleReserved},
		{in: "auth", err: ErrHandleReserved},
		{in: "blobs", err: ErrHandleReserved},
		{in: "events", err: ErrHandleReserved},
		{in: "healthz", err: ErrHandleReserved},
		{in: "new", err: ErrHandleReserved},
		{in: "deleted-user", err: ErrHandleReserved},
		{in: "deleted-user-7", err: ErrHandleReserved},
		{in: "Deleted-User-7", err: ErrHandleReserved},
		{in: "deleted-users", err: ErrHandleReserved},
		// Near misses are not reserved.
		{in: "admins", want: "admins"},
		{in: "newt", want: "newt"},
		{in: "deleted", want: "deleted"},
		{in: "user-deleted", want: "user-deleted"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := CheckHandle(c.in)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("CheckHandle(%q) = %q, %v; want %v", c.in, got, err, c.err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("CheckHandle(%q) = %q, %v; want %q", c.in, got, err, c.want)
			}
		})
	}
}

// Every handle the shared table calls valid is one registration accepts
// as it is, unless reserved: CheckHandle adds to the rule and loosens none
// of it.
func TestCheckHandleKeepsTheRule(t *testing.T) {
	for _, c := range domain.HandleCases {
		got, err := CheckHandle(c.Handle)
		if c.Valid && (err != nil || got != c.Handle) {
			t.Errorf("%s (%q): CheckHandle = %q, %v", c.Why, c.Handle, got, err)
		}
		// An invalid handle stays invalid unless only its case was wrong.
		if !c.Valid && err == nil && NormalizeHandle(c.Handle) == c.Handle {
			t.Errorf("%s (%q): CheckHandle accepted it", c.Why, c.Handle)
		}
	}
}

func TestPrefill(t *testing.T) {
	cases := []struct {
		name, preferred, email, want string
	}{
		{"preferred_username first", "Drew", "someone@example.org", "drew"},
		{"the email's local part next", "", "drew.short@example.org", "drew_short"},
		{"a preferred_username that makes no handle", "?!", "sam@example.org", "sam"},
		{"a run of dropped characters is one _", "J. R. R.", "", "j_r_r"},
		{"a trailing - is trimmed, since it cannot be mentioned", "bob-", "", "bob"},
		{"a leading _ is trimmed", "__bob", "", "bob"},
		{"non-ASCII letters are dropped, not folded", "Jösé", "", "j_s"},
		{"too long is cut", strings.Repeat("ab", 20), "", strings.Repeat("ab", 15)},
		{"cut at 30, then trimmed", strings.Repeat("a", 29) + "-b", "", strings.Repeat("a", 29)},
		{"too short is nothing", "al", "", ""},
		{"reserved is nothing", "Admin", "admin@example.org", ""},
		{"nothing at all", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Prefill(c.preferred, c.email); got != c.want {
				t.Errorf("Prefill(%q, %q) = %q, want %q", c.preferred, c.email, got, c.want)
			}
		})
	}
}
