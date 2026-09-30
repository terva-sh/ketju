package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ErrHandleReserved is a handle registration refuses although it is valid:
// one that names a top-level route, or one the software gives out itself
// (docs/04 "users").
var ErrHandleReserved = errors.New("identity: the handle is reserved")

// The codes a refused handle carries in a validation-failed body's errors,
// under the field "handle": application approval (docs/07 "Applications")
// and a handle change (docs/05 "Changing a handle") answer with the same
// ones, so a client says each in one set of words. Each has one meaning
// (TKT-01M3NRP3W9):
const (
	// CodeInvalidHandle: domain.ValidateHandle refuses it.
	CodeInvalidHandle = "invalid_handle"
	// CodeReserved: CheckHandle refuses it as a route name or the
	// deleted-user prefix (ErrHandleReserved). Never a released handle.
	CodeReserved = "reserved"
	// CodeTaken: HandleFree refuses it, because a user holds it in any case
	// or a release still reserves it. A form says it may have been given up
	// lately, and never which.
	CodeTaken = "taken"
	// CodeSame: it is the account's current handle, on a handle change.
	CodeSame = "same"
)

// reservedHandles are the handles that collide with top-level routes (settled
// in grooming for TKT-01M29C8005). "me" is shorter than any handle may be,
// and is listed so the list stays the whole of the rule if the minimum
// changes.
var reservedHandles = []string{"admin", "api", "assets", "auth", "blobs", "events", "healthz", "me", "new"}

// reservedPrefix is the handle an anonymized account is given,
// deleted-user-<id> (docs/05 "Account lifecycle"). Anything that starts with
// it could pass for one. What marks an anonymized account is the
// users.anonymized column (member.Anonymized); the prefix is only refused
// here, so that no member can look like one.
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

// HandleFree is the one check that h, as CheckHandle returned it, may be
// given to an account at now: no user holds it, in any case (the column is
// NOCASE, so "Alice" is found for "alice"), and no release still reserves it
// (an anonymized account's old handle, for domain.HandleReservation). Either
// answers ErrHandleTaken, never "reserved", so a form does not say which
// handles were given up lately (TKT-01M3NRP3W9).
//
// Every path that writes a handle calls it inside the transaction that
// writes, so the answer cannot go stale before the write; Terva review 1450
// asked that the reservation live here rather than in Register alone, and
// TestEveryHandleWriteChecksHandleFree holds every writer to it.
func HandleFree(ctx context.Context, q *dbq.Queries, h string, now int64) error {
	switch _, err := q.GetUserByHandle(ctx, h); {
	case err == nil:
		return ErrHandleTaken
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	switch reserved, err := q.HandleReserved(ctx, dbq.HandleReservedParams{Handle: h, Now: now}); {
	case err != nil:
		return err
	case reserved:
		return ErrHandleTaken
	}
	return nil
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

// suggest is s made into a handle registration would accept (normalize), or
// "" when it makes none.
func suggest(s string) string {
	h := normalize(s)
	if _, err := CheckHandle(h); err != nil {
		return ""
	}
	return h
}

// normalize makes s into the shape of a handle: ASCII letters lowercased,
// every run of characters a handle cannot hold turned into one '_', what a
// handle may not start with trimmed, and cut to the longest a handle may
// be. A trailing '_' or '-' is trimmed too: '-' because a new handle may not
// end in one (TKT-01M3ANX1NF), and '_' because it is only ever left over
// from a character that was dropped. The result may still not be a valid
// handle, too short say, or reserved; suggest and AutoHandle check it.
func normalize(s string) string {
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
	return strings.TrimRight(h, "_-")
}

// FallbackPrefill is what the handle screen offers after an auto_approve
// provider's preferred_username could not be used as the handle: the
// normalized name when there is one, so the screen shows what was tried
// beside the reason, and otherwise Prefill (docs/05 "mode = approval").
func FallbackPrefill(preferredUsername, email string) string {
	if h := normalize(preferredUsername); h != "" {
		return h
	}
	return Prefill(preferredUsername, email)
}
