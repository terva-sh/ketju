// Package identity is who a sign-in makes someone: the decision the OIDC
// callback hands over once the IdP has vouched for an identity (docs/05
// "Login flow", step 3).
package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Outcome is where a verified identity goes.
type Outcome int

const (
	// SignedIn: the identity's user is active and has a session.
	SignedIn Outcome = iota + 1
	// Unavailable: the identity's user exists but is not active. They are
	// told so and get no session. Result.Status says which status.
	Unavailable
	// Register: nobody has this identity and nobody is signed in, so this is
	// someone new (docs/05 "Registration path", TKT-01M29C8005).
	Register
	// Link: nobody has this identity and a member is signed in, so this is
	// an account link (TKT-01M29CKRES).
	Link
	// Applicant: the identity exists with no user, an approval-mode
	// applicant (docs/05 "mode = approval", TKT-01M29CKRG9).
	Applicant
)

// Result is a sign-in's outcome, with the session when there is one.
type Result struct {
	Outcome Outcome
	// Token and Session are set for SignedIn.
	Token   string
	Session session.Session
	// Status is the user's status for Unavailable.
	Status string
}

// cleanupTimeout bounds ending a session a failed sign-in issued.
const cleanupTimeout = 5 * time.Second

// Sessions is the part of *session.Store a sign-in uses.
type Sessions interface {
	IssueUser(ctx context.Context, identityID int64, meta session.Meta) (string, session.Session, error)
	IssueUserWith(ctx context.Context, meta session.Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error)
	Revoke(ctx context.Context, token string) error
}

// Service decides sign-ins.
type Service struct {
	DB       *db.DB
	Sessions Sessions
	// Logger takes what goes wrong after a sign-in has succeeded; nil means
	// slog.Default.
	Logger *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Bootstrap names the identities made admin at sign-in (docs/05
	// "Principles"). Perm makes the change; it may be nil only when
	// Bootstrap names nobody.
	Bootstrap config.Bootstrap
	Perm      *perm.Evaluator
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Login decides what a verified identity gets. viewer is whoever the request
// is already signed in as, which only matters for an identity nobody has.
// replaces is the session token the browser already holds, if any.
//
// A sign-in that succeeds ends the session it replaces: it never keeps a
// session id it did not issue (fixation). If that fails, the session just
// issued is ended too and Login fails, so the browser never comes out
// holding a new session while its old one still works (review 511). Only
// then is the sign-in recorded on the identity, so the record is of
// sign-ins that happened (review 512).
//
// Only session.IssueUser creates the session, and it checks the user's
// status in the transaction that writes the row (TKT-01M29C7ZZR): Login does
// not read the status itself, so there is no second check here to fall out
// of step with the first.
func (s *Service) Login(ctx context.Context, id *oidc.Identity, viewer domain.Viewer, meta session.Meta, replaces string) (Result, error) {
	var ident dbq.Identity
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		ident, err = q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: id.Provider, Subject: id.Subject})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		// Email is never used to find an identity: two providers asserting
		// one address are two identities until their owner links them
		// (docs/05).
		if viewer.Member() {
			return Result{Outcome: Link}, nil
		}
		return Result{Outcome: Register}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("identity: looking up %s: %w", id.Provider, err)
	}

	token, sess, err := s.Sessions.IssueUser(ctx, ident.ID, meta)
	var inactive *session.NotActiveError
	switch {
	case errors.As(err, &inactive):
		return Result{Outcome: Unavailable, Status: inactive.Status}, nil
	case errors.Is(err, session.ErrNoUser):
		return Result{Outcome: Applicant}, nil
	case err != nil:
		return Result{}, fmt.Errorf("identity: signing in: %w", err)
	}
	if err := s.settle(ctx, token, ident.UserID.Int64, id, replaces); err != nil {
		return Result{}, err
	}

	now := s.now().UnixMilli()
	verified := int64(0)
	if id.EmailVerified {
		verified = 1
	}
	if err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.RecordIdentityLogin(ctx, dbq.RecordIdentityLoginParams{
			LastLoginAt:   sql.NullInt64{Int64: now, Valid: true},
			RawClaimsJson: sql.NullString{String: string(id.RawClaims), Valid: len(id.RawClaims) > 0},
			Email:         sql.NullString{String: id.Email, Valid: id.Email != ""},
			EmailVerified: verified, UpdatedAt: now, ID: ident.ID,
		})
	}); err != nil {
		// The session exists and is valid; failing the sign-in now would
		// leave a row nobody holds the cookie for. The record is
		// bookkeeping, so it is logged and the sign-in stands.
		s.logger().WarnContext(ctx, "recording a sign-in failed", "provider", id.Provider, "identity", ident.ID, "error", err)
	}
	return Result{Outcome: SignedIn, Token: token, Session: sess}, nil
}

// settle is what a sign-in or a registration does once it has issued token
// to userID: end the session it replaces, then apply the bootstrap
// configuration. If either fails, the session just issued is ended too and
// settle fails, so the browser never comes out holding a new session while
// its old one still works (review 511).
func (s *Service) settle(ctx context.Context, token string, userID int64, id *oidc.Identity, replaces string) error {
	// abandon ends the session just issued. The request may be what failed:
	// cancelled mid-sign-in. So the session is ended on a context of its
	// own, bounded, and a cancelled request cannot leave it behind
	// (review 513).
	abandon := func(err error) error {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if err2 := s.Sessions.Revoke(cctx, token); err2 != nil {
			err = errors.Join(err, fmt.Errorf("and ending the session it issued: %w", err2))
		}
		return err
	}
	if replaces != "" {
		if err := s.Sessions.Revoke(ctx, replaces); err != nil {
			return fmt.Errorf("identity: ending the session a sign-in replaces: %w", abandon(err))
		}
	}
	// Bootstrap runs once the user is known to be active and signed in, so
	// a suspended account the configuration names is not made admin. A
	// failure fails the sign-in: signed in as a member, the operator would
	// have only a log line to say why they are not an admin, and signing in
	// again retries it.
	if _, err := s.ApplyBootstrap(ctx, userID, id); err != nil {
		return abandon(err)
	}
	return nil
}
