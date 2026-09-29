package identity

import (
	"context"
	"database/sql"
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
	// ErrRegistrationClosed: registration.mode is not "open".
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
}

// ClaimsOf is the part of id a registration keeps.
func (s *Service) ClaimsOf(id *oidc.Identity) Claims {
	return Claims{Provider: id.Provider, Issuer: id.Issuer, Subject: id.Subject, Email: id.Email,
		EmailVerified: id.EmailVerified, Name: id.Name, PreferredUsername: id.PreferredUsername,
		Groups: s.Policy.mapped(id.Provider, id.Groups)}
}

func (c Claims) identity() *oidc.Identity {
	return &oidc.Identity{Provider: c.Provider, Issuer: c.Issuer, Subject: c.Subject, Email: c.Email,
		EmailVerified: c.EmailVerified, Name: c.Name, PreferredUsername: c.PreferredUsername, Groups: c.Groups}
}

// RegistrationOpen reports whether someone new may register themselves:
// registration.mode is "open" (docs/05). An auto_approve provider under
// approval mode is TKT-01M29CKRE9's, and is not open here.
func (s *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	mode, err := settings.RegistrationMode.Get(ctx, s.DB)
	if err != nil {
		return false, fmt.Errorf("identity: reading registration.mode: %w", err)
	}
	return mode == "open", nil
}

// Register makes the identity c describes a member with handle, and signs
// them in (docs/05 "mode = open"). replaces is the session token the browser
// holds, if any, which a registration ends as a sign-in does.
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
		switch mode, err := settings.RegistrationMode.In(ctx, q); {
		case err != nil:
			return 0, fmt.Errorf("reading registration.mode: %w", err)
		case mode != "open":
			return 0, ErrRegistrationClosed
		}
		switch _, err := q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: c.Provider, Subject: c.Subject}); {
		case err == nil:
			return 0, ErrAlreadyRegistered
		case !errors.Is(err, sql.ErrNoRows):
			return 0, err
		}
		// The column is NOCASE, so this finds "Alice" for "alice".
		switch _, err := q.GetUserByHandle(ctx, h); {
		case err == nil:
			return 0, ErrHandleTaken
		case !errors.Is(err, sql.ErrNoRows):
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
