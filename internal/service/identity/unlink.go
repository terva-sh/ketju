package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	members "github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// A member's linked sign-ins, listed and removed (TKT-01M29CCC0X, docs/05
// "Sessions and CSRF", decided by the run 2026-10-01, pending maintainer
// review). Linking is link.go's.
//
// A removal is refused unless the account keeps another identity on a
// provider configured now, so nobody can lock themselves out of an account
// that still owns posts. An identity of a provider the operator removed
// cannot sign anyone in, and does not count. Every session signed in
// through the removed identity ends with it, the one asking included, and
// the subject is then nobody's: a later sign-in with it, signed out, takes
// the registration path as a subject never seen does.

// ActionUnlinked is the audit action for an identity removed from an
// account, against the user, with "provider" and "identity", the removed
// identity's id.
const ActionUnlinked = "identity.unlinked"

var (
	// ErrCannotUnlink is Unlink's refusal of the account itself: it is not
	// an active human member. An agent has no identity to remove.
	ErrCannotUnlink = errors.New("identity: this account cannot remove a sign-in")
	// ErrNoIdentity is Unlink's answer for an identity that is not the
	// member's: another account's, or one that does not exist. The two are
	// the same answer, so the id space tells a member nothing.
	ErrNoIdentity = errors.New("identity: the account has no such identity")
	// ErrLastIdentity is Unlink's refusal to remove the account's last
	// identity on a configured provider.
	ErrLastIdentity = errors.New("identity: the account would keep no sign-in on a configured provider")
	// ErrNotSignedIn is Unlink's refusal when the browser asking is no
	// longer signed in as the member: a sign-out or revocation landed
	// while the request ran (as for Link, Terva review 1505).
	ErrNotSignedIn = errors.New("identity: the session asking is no longer the member's")
)

// ProviderName is a configured provider: its key, which identities store,
// and the name its sign-in button shows.
type ProviderName struct {
	Key, Name string
}

// ProviderNames is the providers config.toml configures, in its order.
func ProviderNames(providers []config.Provider) []ProviderName {
	out := make([]ProviderName, 0, len(providers))
	for _, p := range providers {
		out = append(out, ProviderName{Key: p.Key, Name: p.Name})
	}
	return out
}

// providerName is the configured name of key, and whether key is
// configured.
func (s *Service) providerName(key string) (string, bool) {
	for _, p := range s.Providers {
		if p.Key == key {
			return p.Name, true
		}
	}
	return "", false
}

// removable is whether the identity with id may be removed from an account
// whose identities are all: another of them is on a configured provider.
func (s *Service) removable(all []dbq.Identity, id int64) bool {
	for _, i := range all {
		if _, ok := s.providerName(i.ProviderKey); ok && i.ID != id {
			return true
		}
	}
	return false
}

// ListIdentities is GET /settings/identities for v, oldest first. current
// is the identity the request's session signed in through, 0 for none.
func (s *Service) ListIdentities(ctx context.Context, v domain.Viewer, current int64) (dto.IdentityList, error) {
	if err := members.Require(v, "list sign-ins"); err != nil {
		return dto.IdentityList{}, err
	}
	var all []dbq.Identity
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		all, err = q.UserIdentities(ctx, sql.NullInt64{Int64: v.UserID, Valid: true})
		return err
	}); err != nil {
		return dto.IdentityList{}, fmt.Errorf("identity: listing the identities of %d: %w", v.UserID, err)
	}
	out := dto.IdentityList{Identities: make([]dto.IdentityInfo, 0, len(all))}
	for _, i := range all {
		name, configured := s.providerName(i.ProviderKey)
		info := dto.IdentityInfo{ID: i.ID, Provider: i.ProviderKey, ProviderName: name, Configured: configured,
			CreatedAt: dto.Millis(i.CreatedAt), Current: i.ID == current, Removable: s.removable(all, i.ID)}
		if i.Email.Valid && i.Email.String != "" {
			info.Email = &i.Email.String
		}
		if i.LastLoginAt.Valid {
			at := dto.Millis(i.LastLoginAt.Int64)
			info.LastLoginAt = &at
		}
		out.Identities = append(out.Identities, info)
	}
	return out, nil
}

// Unlink is DELETE /settings/identities/{id}: v removes one of their own
// identities. token is the session cookie the request carries, and ip the
// request's address, for the audit row. It reports whether the session
// token names was signed in through the identity, and so has ended.
//
// One write transaction, in this order, so a removal racing another, a
// sign-out, or a link sees the other whole or not at all:
//  1. the account is an active human member, or ErrCannotUnlink;
//  2. token is still a live session of the member's (session.HeldThrough),
//     or ErrNotSignedIn;
//  3. the identity is the member's, or ErrNoIdentity;
//  4. another of the member's identities is on a configured provider, or
//     ErrLastIdentity;
//  5. every session through the identity ends (session.EndThrough);
//  6. the identity is deleted; an application naming it keeps its row,
//     its identity_id set to null by the foreign key;
//  7. identity.unlinked is appended against the member.
//
// Each refusal writes nothing, and is coded for the API: forbidden,
// unauthenticated, not-found and conflict.
func (s *Service) Unlink(ctx context.Context, v domain.Viewer, token string, identityID int64, ip string) (bool, error) {
	if err := members.Require(v, "remove a sign-in"); err != nil {
		return false, err
	}
	now := s.now()
	userID := v.UserID
	var endedCurrent bool
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(ctx, userID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return problem.Coded(problem.CodeForbidden, fmt.Errorf("%w: no such user", ErrCannotUnlink))
		case err != nil:
			return err
		case u.Status != "active":
			return problem.Coded(problem.CodeForbidden, fmt.Errorf("%w: the account is %s", ErrCannotUnlink, u.Status))
		case u.Kind != domain.KindHuman:
			return problem.Coded(problem.CodeForbidden, fmt.Errorf("%w: the account is an %s", ErrCannotUnlink, u.Kind))
		}
		through, err := session.HeldThrough(ctx, q, token, userID, now)
		switch {
		case errors.Is(err, session.ErrNoSession):
			return problem.Coded(problem.CodeUnauthenticated, ErrNotSignedIn)
		case err != nil:
			return err
		}
		all, err := q.UserIdentities(ctx, sql.NullInt64{Int64: userID, Valid: true})
		if err != nil {
			return err
		}
		var target *dbq.Identity
		for i := range all {
			if all[i].ID == identityID {
				target = &all[i]
			}
		}
		if target == nil {
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("%w: %d", ErrNoIdentity, identityID))
		}
		if !s.removable(all, identityID) {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("%w: removing %d", ErrLastIdentity, identityID))
		}
		if _, err := session.EndThrough(ctx, q, identityID); err != nil {
			return err
		}
		n, err := q.DeleteIdentity(ctx, dbq.DeleteIdentityParams{ID: identityID, UserID: sql.NullInt64{Int64: userID, Valid: true}})
		switch {
		case err != nil:
			return err
		case n != 1:
			return fmt.Errorf("identity: deleting identity %d removed %d rows", identityID, n)
		}
		endedCurrent = through == identityID
		_, err = audit.Append(ctx, q, now, audit.By{Actor: userID, IP: ip}, ActionUnlinked, audit.User(userID),
			map[string]any{"provider": target.ProviderKey, "identity": identityID})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("identity: removing identity %d: %w", identityID, err)
	}
	return endedCurrent, nil
}
