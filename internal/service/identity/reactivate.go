package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ErrCannotReactivate is Reactivate's refusal: the identity is gone, has no
// account, or its account is not deactivated. A suspended, pending or
// anonymized account never comes back this way (docs/05 "Account
// lifecycle").
var ErrCannotReactivate = errors.New("identity: the account cannot be reactivated")

// Reactivate brings back the deactivated account of the identity c names
// and signs it in (docs/05 "Account lifecycle", decided 2026-09-29 in
// TKT-01M29CKRFS). c is what the IdP vouched for at a sign-in that found the
// account deactivated, carried to the Reactivate button in a sealed cookie,
// so only someone who has just signed in as the account can press it.
// Signing in alone never reactivates: a stale IdP session would otherwise
// undo a deliberate deactivation unseen.
//
// stamp is the account row's updated_at as that sign-in found it
// (Result.Stamp). A row changed since is refused, so the cookie serves one
// press: reactivating changes the row, and so does deactivating again, which
// a cookie from before it cannot undo.
//
// The account moves from deactivated to active through
// perm.ChangeStatusIn, which writes user.reactivated, inside the
// transaction session.IssueUserWith inserts the session in. So the status,
// its audit row and the session commit together or not at all, and the
// session is issued by the same status check as every other sign-in, which
// reads the status the change has just written. Any other status is
// ErrCannotReactivate, and nothing is written.
//
// Then the sign-in finishes as any other does (settle): the session it
// replaces is ended, and the bootstrap and the provider's group_map are
// applied from c.
func (s *Service) Reactivate(ctx context.Context, c Claims, stamp int64, meta session.Meta, replaces string) (Result, error) {
	if s.Perm == nil {
		return Result{}, errors.New("identity: no permission evaluator is set, so an account cannot be reactivated")
	}
	var userID int64
	token, sess, err := s.Sessions.IssueUserWith(ctx, meta, func(ctx context.Context, q *dbq.Queries) (int64, error) {
		ident, err := q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: c.Provider, Subject: c.Subject})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return 0, fmt.Errorf("%w: the identity no longer exists", ErrCannotReactivate)
		case err != nil:
			return 0, err
		case !ident.UserID.Valid:
			return 0, fmt.Errorf("%w: the identity has no account", ErrCannotReactivate)
		}
		userID = ident.UserID.Int64
		err = s.Perm.ChangeStatusIn(ctx, q, userID, perm.StatusChange{
			By: audit.By{Actor: userID, IP: meta.IP}, Action: perm.ActionReactivated,
			Allow: func(u dbq.User) error {
				if u.UpdatedAt != stamp {
					return fmt.Errorf("%w: the account has changed since the sign-in", ErrCannotReactivate)
				}
				return nil
			}})
		if errors.Is(err, perm.ErrBadTransition) || errors.Is(err, perm.ErrAnonymized) {
			return 0, fmt.Errorf("%w: %w", ErrCannotReactivate, err)
		}
		return ident.ID, err
	})
	switch {
	case errors.Is(err, ErrCannotReactivate):
		return Result{}, err
	case err != nil:
		return Result{}, fmt.Errorf("identity: reactivating: %w", err)
	}
	s.Perm.Invalidate()
	if err := s.settle(ctx, token, userID, c.identity(), meta.IP, replaces); err != nil {
		return Result{}, err
	}
	return Result{Outcome: SignedIn, Token: token, Session: sess}, nil
}
