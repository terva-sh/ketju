package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Registration's refusals. Each is a fact about the forum rather than a
// fault, so the screen says it in words; none carries a database's text.
var (
	// ErrRegistrationClosed: registration is not open for the provider:
	// registration.mode is not "open", and the provider is not an
	// auto_approve one under "approval" (RegistrationOpenFor).
	ErrRegistrationClosed = errors.New("identity: registration is not open")
	// ErrHandleTaken: someone has the handle, in any case.
	ErrHandleTaken = errors.New("identity: the handle is taken")
	// ErrAlreadyRegistered: the identity has an account already, most often
	// because the same form was submitted twice. Signing in reaches it.
	ErrAlreadyRegistered = errors.New("identity: the identity is registered already")
)

// Claims are what the IdP vouched for at the callback, carried to the handle
// screen and back (docs/05 "Registration path"). They are the fields a
// registration writes or offers, not the whole token.
type Claims struct {
	Provider          string `json:"p"`
	Issuer            string `json:"i"`
	Subject           string `json:"s"`
	Email             string `json:"e,omitempty"`
	EmailVerified     bool   `json:"ev,omitempty"`
	Name              string `json:"n,omitempty"`
	PreferredUsername string `json:"u,omitempty"`
	// Groups are the values of the groups claim that the provider's
	// group_map names, so the new member's first sign-in maps them as any
	// later one would. Values the map does not name are dropped: they
	// could grant nothing, and an IdP that asserts hundreds of groups
	// would otherwise overflow the cookie that carries these claims.
	Groups []string `json:"g,omitempty"`
	// RawClaims is the claim set as the IdP sent it, which an approval-mode
	// application records on the identity it writes, so the form's
	// from_claim fields can be prefilled from it (TKT-01M29CP2KZ). It is
	// never sealed into the registration cookie, which has no room for it.
	RawClaims json.RawMessage `json:"-"`
}

// ClaimsOf is the part of id a registration keeps.
func (s *Service) ClaimsOf(id *oidc.Identity) Claims {
	return Claims{Provider: id.Provider, Issuer: id.Issuer, Subject: id.Subject, Email: id.Email,
		EmailVerified: id.EmailVerified, Name: id.Name, PreferredUsername: id.PreferredUsername,
		Groups: s.Policy.mapped(id.Provider, id.Groups), RawClaims: id.RawClaims}
}

func (c Claims) identity() *oidc.Identity {
	return &oidc.Identity{Provider: c.Provider, Issuer: c.Issuer, Subject: c.Subject, Email: c.Email,
		EmailVerified: c.EmailVerified, Name: c.Name, PreferredUsername: c.PreferredUsername, Groups: c.Groups}
}

// RegistrationOpenFor reports whether someone new from provider may register
// themselves: registration.mode is "open", or it is "approval" and the
// provider is configured with auto_approve (docs/05 "mode = approval"). The
// callback asks it to choose between the handle screen and the application
// form; Register asks the same question again in the transaction that
// writes the account.
func (s *Service) RegistrationOpenFor(ctx context.Context, provider string) (bool, error) {
	mode, err := settings.RegistrationMode.Get(ctx, s.DB)
	if err != nil {
		return false, fmt.Errorf("identity: reading registration.mode: %w", err)
	}
	return s.Policy.registrationOpen(mode, provider), nil
}

// AutoApproves reports whether provider is configured with auto_approve.
func (s *Service) AutoApproves(provider string) bool {
	return s.Policy.AutoApproves(provider)
}

// SkipReason is why an auto_approve provider's preferred_username could not
// be the new member's handle, so the handle screen is shown after all, with
// the reason (docs/05 "mode = approval").
type SkipReason int

const (
	// SkipNone: the handle was given out; nothing was skipped.
	SkipNone SkipReason = iota
	// SkipMissing: the IdP asserted no preferred_username.
	SkipMissing
	// SkipInvalid: it does not make a valid handle.
	SkipInvalid
	// SkipReserved: it makes a handle registration never gives out.
	SkipReserved
	// SkipTaken: someone has the handle, in any case, or a release still
	// reserves it.
	SkipTaken
)

// ErrNotAutoApprove: AutoRegister was asked to register through a provider
// that is not configured with auto_approve, which must choose a handle.
var ErrNotAutoApprove = errors.New("identity: the provider does not auto-approve")

// AutoHandle is the handle an auto_approve provider's preferred_username
// makes, or why it makes none. It is normalized as the prefill is, then
// checked as a typed handle is (CheckHandle). Whether the handle is free is
// not decided here, but in the transaction that would give it out.
func AutoHandle(preferred string) (string, SkipReason) {
	if strings.TrimSpace(preferred) == "" {
		return "", SkipMissing
	}
	h, err := CheckHandle(normalize(preferred))
	switch {
	case errors.Is(err, ErrHandleReserved):
		return "", SkipReserved
	case err != nil:
		return "", SkipInvalid
	}
	return h, SkipNone
}

// AutoRegister registers someone new from an auto_approve provider under
// the handle their preferred_username makes, with no handle screen (docs/05,
// decided 2026-09-28). When it makes none, or the handle is taken, it
// writes nothing and returns why, and the caller shows the handle screen.
//
// It goes to a session only through Register, and so through
// session.IssueUserWith and every check Register makes in the transaction
// that writes: that registration is open for this provider, that the
// identity is new, and that the handle is free (HandleFree). A handle an
// inactive account holds is taken like any other; the account is never
// the one signed in.
func (s *Service) AutoRegister(ctx context.Context, c Claims, meta session.Meta, replaces string) (Result, SkipReason, error) {
	if !s.AutoApproves(c.Provider) {
		return Result{}, SkipNone, ErrNotAutoApprove
	}
	h, why := AutoHandle(c.PreferredUsername)
	if why != SkipNone {
		return Result{}, why, nil
	}
	res, err := s.Register(ctx, c, h, meta, replaces)
	if errors.Is(err, ErrHandleTaken) {
		return Result{}, SkipTaken, nil
	}
	return res, SkipNone, err
}

// Register makes the identity c describes a member with handle, and signs
// them in (docs/05 "mode = open", and an auto_approve provider under
// "approval"). replaces is the session token the browser holds, if any,
// which a registration ends as a sign-in does.
//
// The handle is checked by CheckHandle first, which returns an error
// wrapping domain.ErrInvalidHandle or ErrHandleReserved. Then the user, the
// identity and the session are written in one transaction, or nothing is:
// session.IssueUserWith runs the writes in the transaction that inserts the
// session. Whether registration is open, and whether the handle is free,
// are decided there, not before, so of
// two people submitting "alice" and "Alice" in the same second one gets the
// account and the other ErrHandleTaken. The write connection serializes the
// two, and the users table's NOCASE unique index stands behind the read.
//
// A config-named admin is promoted right after, and the provider's
// group_map applied, as a sign-in would do both (settle). If either fails,
// the session just issued is ended and Register fails, as Login does; the
// account stands, and signing in again retries them.
func (s *Service) Register(ctx context.Context, c Claims, handle string, meta session.Meta, replaces string) (Result, error) {
	h, err := CheckHandle(handle)
	if err != nil {
		return Result{}, err
	}
	display := strings.TrimSpace(c.Name)
	if display == "" {
		display = h
	}
	now := s.now().UnixMilli()
	var userID int64
	token, sess, err := s.Sessions.IssueUserWith(ctx, meta, func(ctx context.Context, q *dbq.Queries) (int64, error) {
		// Read here, in the transaction that writes the account, so an
		// admin who closes registration while someone is choosing a handle
		// cannot be missed between a check and the insert (review 1145).
		// Under approval mode only an auto_approve provider registers, so a
		// sealed registration from any other provider is refused here
		// whatever the cookie says.
		switch mode, err := settings.RegistrationMode.In(ctx, q); {
		case err != nil:
			return 0, fmt.Errorf("reading registration.mode: %w", err)
		case !s.Policy.registrationOpen(mode, c.Provider):
			return 0, ErrRegistrationClosed
		}
		switch _, err := q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: c.Provider, Subject: c.Subject}); {
		case err == nil:
			return 0, ErrAlreadyRegistered
		case !errors.Is(err, sql.ErrNoRows):
			return 0, err
		}
		// Free of any user and of any reservation, such as an anonymized
		// account's old handle (TKT-01M29CKRFS), so a newcomer cannot take
		// it and the links old mentions of it would give them.
		if err := HandleFree(ctx, q, h, now); err != nil {
			return 0, err
		}
		var err error
		userID, err = q.CreateUser(ctx, dbq.CreateUserParams{Handle: h, DisplayName: display,
			Status: "active", Role: "member", Kind: "human", CreatedAt: now, UpdatedAt: now})
		if db.IsUniqueViolation(err) {
			return 0, ErrHandleTaken
		}
		if err != nil {
			return 0, err
		}
		verified := int64(0)
		if c.EmailVerified {
			verified = 1
		}
		email := sql.NullString{String: c.Email, Valid: c.Email != ""}
		identityID, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: userID, Valid: true},
			ProviderKey: c.Provider, Subject: c.Subject, Email: email, EmailVerified: verified, Issuer: c.Issuer,
			CreatedAt: now, UpdatedAt: now})
		if db.IsUniqueViolation(err) {
			return 0, ErrAlreadyRegistered
		}
		if err != nil {
			return 0, err
		}
		// Registering is the identity's first sign-in.
		return identityID, q.RecordIdentityLogin(ctx, dbq.RecordIdentityLoginParams{
			LastLoginAt: sql.NullInt64{Int64: now, Valid: true}, Email: email, EmailVerified: verified,
			UpdatedAt: now, ID: identityID})
	})
	switch {
	case errors.Is(err, ErrHandleTaken), errors.Is(err, ErrAlreadyRegistered), errors.Is(err, ErrRegistrationClosed):
		return Result{}, err
	case err != nil:
		return Result{}, fmt.Errorf("identity: registering: %w", err)
	}
	if err := s.settle(ctx, token, userID, c.identity(), meta.IP, replaces); err != nil {
		return Result{}, err
	}
	return Result{Outcome: SignedIn, Token: token, Session: sess}, nil
}
