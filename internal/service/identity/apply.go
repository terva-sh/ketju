package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// BeginApplication is approval mode's first step for someone new (docs/05
// "mode = approval" step 1, TKT-01M29CKRG9). It writes the identity c
// describes with no user, and places the visitor in a pre-user session bound
// to it, which reaches the application form, the status page and sign-out,
// and nothing else (web.PreUserRoutes). replaces is the session token the
// browser holds, if any, which it ends before writing anything.
//
// The identity and the session are written in one transaction, or neither
// is: session.IssuePreUserWith runs the write in the transaction that
// inserts the session, so a failed insert leaves no identity behind. Whether
// the mode is approval, and whether the identity is new, are decided there
// too, not before, so an admin who opens registration between the callback
// and the write is not missed (review 1145). It refuses with
// ErrRegistrationClosed unless registration.mode is "approval", and with
// ErrAlreadyRegistered an identity that exists, which another tab wrote in
// the meantime.
//
// There is no user yet, so neither the bootstrap configuration nor group
// mapping applies: approval creates the user, and the applicant's next
// sign-in, through session.IssueUser, is where both run.
func (s *Service) BeginApplication(ctx context.Context, c Claims, meta session.Meta, replaces string) (Result, error) {
	// The session the browser holds is ended first, not after the new one
	// is issued as Login and Register do (replace). The identity written
	// below outlives any later failure: if ending the old session failed
	// after it, the application would have begun without a session, and
	// the reader's retry would find an applicant coming back rather than
	// someone new (review 1397). Ended first, a failure writes nothing, and
	// no session issued here ever sits beside a live old one.
	if replaces != "" {
		if err := s.Sessions.Revoke(ctx, replaces); err != nil {
			return Result{}, fmt.Errorf("identity: ending the session an application replaces: %w", err)
		}
	}
	now := s.now().UnixMilli()
	token, sess, err := s.Sessions.IssuePreUserWith(ctx, meta, func(ctx context.Context, q *dbq.Queries) (int64, error) {
		switch mode, err := settings.RegistrationMode.In(ctx, q); {
		case err != nil:
			return 0, fmt.Errorf("reading registration.mode: %w", err)
		case mode != "approval":
			return 0, ErrRegistrationClosed
		}
		switch _, err := q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: c.Provider, Subject: c.Subject}); {
		case err == nil:
			return 0, ErrAlreadyRegistered
		case !errors.Is(err, sql.ErrNoRows):
			return 0, err
		}
		verified := int64(0)
		if c.EmailVerified {
			verified = 1
		}
		email := sql.NullString{String: c.Email, Valid: c.Email != ""}
		identityID, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{ProviderKey: c.Provider, Subject: c.Subject,
			Email: email, EmailVerified: verified, Issuer: c.Issuer, CreatedAt: now, UpdatedAt: now})
		if db.IsUniqueViolation(err) {
			return 0, ErrAlreadyRegistered
		}
		if err != nil {
			return 0, err
		}
		// Applying is the identity's first sign-in, and its claims are
		// recorded with it: the application form prefills its from_claim
		// fields from them (TKT-01M29CP2KZ).
		return identityID, q.RecordIdentityLogin(ctx, dbq.RecordIdentityLoginParams{
			LastLoginAt:   sql.NullInt64{Int64: now, Valid: true},
			RawClaimsJson: sql.NullString{String: string(c.RawClaims), Valid: len(c.RawClaims) > 0},
			Email:         email, EmailVerified: verified, UpdatedAt: now, ID: identityID})
	})
	switch {
	case errors.Is(err, ErrRegistrationClosed), errors.Is(err, ErrAlreadyRegistered):
		return Result{}, err
	case err != nil:
		return Result{}, fmt.Errorf("identity: beginning an application: %w", err)
	}
	return Result{Outcome: Applying, Token: token, Session: sess}, nil
}
