package identity

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
)

// ErrHandleReserved is a handle registration refuses although it is valid:
// one that names a top-level route, or one the software gives out itself
// (docs/04 "users").
var ErrHandleReserved = errors.New("identity: the handle is reserved")

// reservedHandles are the handles that collide with top-level routes (settled
// in grooming for TKT-01M29C8005). "me" is shorter than any handle may be,
// and is listed so the list stays the whole of the rule if the minimum
// changes.
var reservedHandles = []string{"admin", "api", "assets", "auth", "blobs", "events", "healthz", "me", "new"}

// reservedPrefix is the handle an anonymized account is given,
// deleted-user-<id> (docs/05 "Account lifecycle"). Anything that starts with
// it could pass for one, and a profile reads the prefix as the sign of an
// anonymized account (domain.Anonymized).
const reservedPrefix = domain.AnonymizedHandlePrefix

// NormalizeHandle is a handle as typed, made into the form it is stored in:
// surrounding space trimmed and ASCII letters lowercased. Nothing else is
// changed. strings.ToLower is not used on purpose: it folds the Kelvin sign
// (U+212A) to an ASCII "k", which would let a character the rule refuses
// slip past it as the letter it looks like.
func NormalizeHandle(h string) string {
	h = strings.TrimSpace(h)
	b := []byte(h)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// CheckHandle normalizes h and reports whether registration may give it out:
// valid by domain.ValidateHandle, and not reserved. It returns the handle to
// store. Whether someone already has it is the database's to say, in the
// transaction that would give it out (Register).
func CheckHandle(h string) (string, error) {
	h = NormalizeHandle(h)
	if err := domain.ValidateHandle(h); err != nil {
		return "", err
	}
	if slices.Contains(reservedHandles, h) || strings.HasPrefix(h, reservedPrefix) {
		return "", fmt.Errorf("%w: %q", ErrHandleReserved, h)
	}
	return h, nil
}

// Prefill is the handle the registration screen offers: the IdP's
// preferred_username, or failing that the local part of the email address,
// made into a handle (docs/05 "mode = open"). It is a suggestion, reserved
// for nobody, and "" when neither makes a handle registration would accept.
func Prefill(preferredUsername, email string) string {
	local, _, _ := strings.Cut(email, "@")
	for _, s := range []string{preferredUsername, local} {
		if h := suggest(s); h != "" {
			return h
		}
	}
	return ""
}

// suggest makes s into a handle: ASCII letters lowercased, every run of
// characters a handle cannot hold turned into one '_', and what a handle
// may not start with trimmed. A trailing '_' or '-' is trimmed too: '-'
// because a handle ending in one cannot be mentioned (TKT-01M3ANX1NF), and
// '_' because it is only ever left over from a character that was dropped.
func suggest(s string) string {
	var b strings.Builder
	dropped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			dropped = true
			continue
		}
		if dropped && b.Len() > 0 {
			b.WriteByte('_')
		}
		dropped = false
		b.WriteByte(c)
	}
	h := strings.TrimLeft(b.String(), "_-")
	if len(h) > domain.HandleMaxLen {
		h = h[:domain.HandleMaxLen]
	}
	h = strings.TrimRight(h, "_-")
	if _, err := CheckHandle(h); err != nil {
		return ""
	}
	return h
}
