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
	// applicant signing in again, who has a pre-user session (docs/05
	// "mode = approval" step 4, TKT-01M29CKRG9).
	Applicant
	// Applying: someone new under approval mode, whose identity was just
	// written with no user and who has a pre-user session (docs/05
	// "mode = approval" step 1). BeginApplication is the one way to it.
	Applying
)

// Result is a sign-in's outcome, with the session when there is one.
type Result struct {
	Outcome Outcome
	// Token and Session are set for SignedIn, and for Applicant and
	// Applying, whose session is a pre-user one.
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
	IssuePreUser(ctx context.Context, identityID int64, meta session.Meta) (string, session.Session, error)
	IssuePreUserWith(ctx context.Context, meta session.Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, session.Session, error)
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
	// "Principles"). Perm makes the change.
	Bootstrap config.Bootstrap
	// Perm makes every change a sign-in makes to what a user may do: the
	// bootstrap promotion, and the IdP group mapping, which runs at every
	// sign-in. A sign-in fails without it.
	Perm *perm.Evaluator
	// Policy is each provider's configuration of a sign-in through it
	// (PolicyFrom). Nil means no provider maps groups.
	Policy Policy
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
// Only session.IssueUser creates a member session, and it checks the user's
// status in the transaction that writes the row (TKT-01M29C7ZZR): Login does
// not read the status itself, so there is no second check here to fall out
// of step with the first. An identity with no user is an approval-mode
// applicant, who gets a pre-user session from session.IssuePreUser, which
// refuses any identity that has a user (TKT-01M29CKRG9).
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
		return s.applicant(ctx, ident.ID, id, meta, replaces)
	case err != nil:
		return Result{}, fmt.Errorf("identity: signing in: %w", err)
	}
	if err := s.settle(ctx, token, ident.UserID.Int64, id, meta.IP, replaces); err != nil {
		return Result{}, err
	}
	s.recordLogin(ctx, ident.ID, id)
	return Result{Outcome: SignedIn, Token: token, Session: sess}, nil
}

// applicant signs in an identity with no user: an approval-mode applicant
// coming back, who sees the status page (docs/05 "mode = approval" step 4).
// An identity approved between IssueUser's refusal and this is refused with
// session.ErrHasUser, and the reader signs in again, as a member.
func (s *Service) applicant(ctx context.Context, identityID int64, id *oidc.Identity, meta session.Meta, replaces string) (Result, error) {
	token, sess, err := s.Sessions.IssuePreUser(ctx, identityID, meta)
	if err != nil {
		return Result{}, fmt.Errorf("identity: signing in an applicant: %w", err)
	}
	if err := s.replace(ctx, token, replaces); err != nil {
		return Result{}, err
	}
	s.recordLogin(ctx, identityID, id)
	return Result{Outcome: Applicant, Token: token, Session: sess}, nil
}

// recordLogin notes on the identity a sign-in that happened: when, and the
// claims the IdP asserted this time.
func (s *Service) recordLogin(ctx context.Context, identityID int64, id *oidc.Identity) {
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
			EmailVerified: verified, UpdatedAt: now, ID: identityID,
		})
	}); err != nil {
		// The session exists and is valid; failing the sign-in now would
		// leave a row nobody holds the cookie for. The record is
		// bookkeeping, so it is logged and the sign-in stands.
		s.logger().WarnContext(ctx, "recording a sign-in failed", "provider", id.Provider, "identity", identityID, "error", err)
	}
}

// settle is what a sign-in or a registration does once it has issued token
// to userID: end the session it replaces, apply the bootstrap
// configuration, then the provider's group_map. If any of them fails, the
// session just issued is ended too and settle fails, so the browser never
// comes out holding a new session while its old one still works
// (review 511), nor signed in with groups its directory has revoked. ip is
// the request's address, for the audit rows.
func (s *Service) settle(ctx context.Context, token string, userID int64, id *oidc.Identity, ip, replaces string) error {
	if err := s.replace(ctx, token, replaces); err != nil {
		return err
	}
	// Bootstrap runs once the user is known to be active and signed in, so
	// a suspended account the configuration names is not made admin. A
	// failure fails the sign-in: signed in as a member, the operator would
	// have only a log line to say why they are not an admin, and signing in
	// again retries it.
	if _, err := s.ApplyBootstrap(ctx, userID, id); err != nil {
		return s.abandon(ctx, token, err)
	}
	if err := s.applyGroupMap(ctx, userID, id, ip); err != nil {
		return s.abandon(ctx, token, err)
	}
	return nil
}

// applyGroupMap makes userID's IdP-mapped memberships exactly what this
// sign-in's provider maps from this token (docs/05 "Roles and groups"). It
// runs at every sign-in, through a provider with a group_map or not: one
// without asserts no groups, so a directory's revocation cannot be dodged
// by signing in through another provider (review 1342). A slug the map
// names and no group has is logged, for the operator to fix.
func (s *Service) applyGroupMap(ctx context.Context, userID int64, id *oidc.Identity, ip string) error {
	if s.Perm == nil {
		return errors.New("identity: no permission evaluator is set, so the sign-in's group mapping cannot run")
	}
	ch, err := s.Perm.SyncIdPGroups(ctx, userID, perm.IdPSync{Provider: id.Provider, IP: ip,
		Slugs: s.Policy.LocalGroups(id.Provider, id.Groups)})
	if err != nil {
		return fmt.Errorf("identity: applying group_map: %w", err)
	}
	for _, slug := range ch.Unknown {
		s.logger().WarnContext(ctx, "a sign-in's group_map names a group that does not exist, so it granted nothing",
			"provider", id.Provider, "group", slug)
	}
	if len(ch.Added)+len(ch.Removed) > 0 {
		s.logger().InfoContext(ctx, "group_map changed a member's groups", "provider", id.Provider, "user", userID,
			"added", ch.Added, "removed", ch.Removed)
	}
	return nil
}

// replace ends the session a sign-in replaces, once token has been issued
// in its place: a browser never keeps a session id it did not issue
// (fixation). If that fails, token is ended too and replace fails, so the
// browser never comes out holding a new session while its old one still
// works (review 511). Every way to a session runs it: a sign-in, a
// registration, and both applicant paths.
func (s *Service) replace(ctx context.Context, token, replaces string) error {
	if replaces == "" {
		return nil
	}
	if err := s.Sessions.Revoke(ctx, replaces); err != nil {
		return fmt.Errorf("identity: ending the session a sign-in replaces: %w", s.abandon(ctx, token, err))
	}
	return nil
}

// abandon ends the session just issued and returns err, with anything that
// went wrong doing so. The request may be what failed: cancelled
// mid-sign-in. So the session is ended on a context of its own, bounded,
// and a cancelled request cannot leave it behind (review 513).
func (s *Service) abandon(ctx context.Context, token string, err error) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err2 := s.Sessions.Revoke(cctx, token); err2 != nil {
		err = errors.Join(err, fmt.Errorf("and ending the session it issued: %w", err2))
	}
	return err
}
