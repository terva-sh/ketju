package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Account linking: one user, many identities (docs/05 "Login flow" step 3,
// TKT-01M29CKRES). A member adds an identity only through the explicit
// "link a sign-in" action, which seals their user id into the sign-in's
// flow cookie; Login is handed that id as linkUser. A link is not a
// sign-in: it writes the identity and its audit row, and no session. The
// member keeps the session they have, and the identity is later used to
// sign in through session.IssueUser, which checks the account's status as
// for any other identity.

// ActionLinked is the audit action for an identity attached to an account,
// against the user, with "provider" and "identity", the identity's id.
const ActionLinked = "identity.linked"

// LinkRefusal is why a sign-in may not be a link.
type LinkRefusal int

const (
	// LinkRefusalNone: nothing was refused.
	LinkRefusalNone LinkRefusal = iota
	// LinkNoIntent: nobody has the identity and a member is signed in, but
	// the member did not start this sign-in as a link.
	LinkNoIntent
	// LinkNotYou: the link was started by a member this browser is no
	// longer signed in as, as an active member: signed out since, signed
	// in as someone else, or no longer active.
	LinkNotYou
	// LinkOtherUser: the identity belongs to another account.
	LinkOtherUser
	// LinkApplicant: the identity belongs to an application to join, with
	// no account yet.
	LinkApplicant
	// LinkAlready: the identity is already this member's.
	LinkAlready
)

func (r LinkRefusal) String() string {
	switch r {
	case LinkRefusalNone:
		return "none"
	case LinkNoIntent:
		return "no link was started"
	case LinkNotYou:
		return "the link was started by another viewer"
	case LinkOtherUser:
		return "the identity belongs to another account"
	case LinkApplicant:
		return "the identity belongs to an applicant"
	case LinkAlready:
		return "the identity is already linked"
	}
	return fmt.Sprintf("LinkRefusal(%d)", int(r))
}

// ErrCannotLink is Link's refusal of the account itself: it is not an
// active human member. An agent never uses OIDC (docs/05), so it is never
// given an identity to sign in with.
var ErrCannotLink = errors.New("identity: this account cannot link a sign-in")

// ErrLinkTaken is Link's answer when the identity was written by someone
// else in the moment between its read and its insert.
var ErrLinkTaken = errors.New("identity: the identity was taken while it was being linked")

// linking is Login's answer for a sign-in started as a link by linkUser. It
// never signs in and never registers. A browser no longer signed in as that
// member, active, is refused first; then an identity that exists is refused
// with what it belongs to, since a found identity is never re-attached; and
// only an identity nobody has is a link.
func linking(found bool, ident dbq.Identity, viewer domain.Viewer, linkUser int64) Result {
	switch {
	case !viewer.Member() || viewer.UserID != linkUser:
		return Result{Outcome: LinkRefused, Refusal: LinkNotYou}
	case found:
		return Result{Outcome: LinkRefused, Refusal: refusalFor(ident, linkUser)}
	}
	return Result{Outcome: Link}
}

// refusalFor is why ident, which exists, cannot be linked to userID.
func refusalFor(ident dbq.Identity, userID int64) LinkRefusal {
	switch {
	case !ident.UserID.Valid:
		return LinkApplicant
	case ident.UserID.Int64 == userID:
		return LinkAlready
	}
	return LinkOtherUser
}

// Link attaches the identity c describes to userID, the member who started
// the link, and records it (TKT-01M29CKRES). token is the session cookie
// the callback's browser holds, and ip the request's address, for the audit
// row.
//
// One transaction reads the account, checks the browser is still signed in
// as it, checks the identity is still nobody's, writes it with the email
// and email_verified the IdP asserted, and appends identity.linked, so the
// identity and its record commit together or not at all. An account that is
// not an active human member is ErrCannotLink, so a member suspended
// between the start and the callback cannot link. A token that is no
// longer a live session of userID is LinkNotYou: the callback's viewer was
// read when the request began, and a sign-out or revocation since then
// must stop the link rather than race it (Terva review 1505). An identity
// someone has by now is refused with the LinkRefusal it would have had at
// the callback. Each refusal writes nothing.
//
// A link is not a sign-in (review 1342, finding 2): last_login_at and
// raw_claims_json stay empty until the identity is first used to sign in.
// It issues no session, and the bootstrap configuration and the provider's
// group_map are not applied here: they run at the identity's first sign-in,
// as at any sign-in.
func (s *Service) Link(ctx context.Context, userID int64, token string, c Claims, ip string) (LinkRefusal, error) {
	now := s.now()
	refusal := LinkRefusalNone
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(ctx, userID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: no such user", ErrCannotLink)
		case err != nil:
			return err
		case u.Status != "active":
			return fmt.Errorf("%w: the account is %s", ErrCannotLink, u.Status)
		case u.Kind != domain.KindHuman:
			return fmt.Errorf("%w: the account is an %s", ErrCannotLink, u.Kind)
		}
		switch err := session.HeldBy(ctx, q, token, userID, now); {
		case errors.Is(err, session.ErrNoSession):
			refusal = LinkNotYou
			return nil
		case err != nil:
			return err
		}
		ident, err := q.GetIdentityBySubject(ctx, dbq.GetIdentityBySubjectParams{ProviderKey: c.Provider, Subject: c.Subject})
		switch {
		case err == nil:
			refusal = refusalFor(ident, userID)
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		verified := int64(0)
		if c.EmailVerified {
			verified = 1
		}
		identityID, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: userID, Valid: true},
			ProviderKey: c.Provider, Subject: c.Subject, Email: sql.NullString{String: c.Email, Valid: c.Email != ""},
			EmailVerified: verified, Issuer: c.Issuer, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()})
		if db.IsUniqueViolation(err) {
			return ErrLinkTaken
		}
		if err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, now, audit.By{Actor: userID, IP: ip}, ActionLinked, audit.User(userID),
			map[string]any{"provider": c.Provider, "identity": identityID})
		return err
	})
	switch {
	case errors.Is(err, ErrCannotLink), errors.Is(err, ErrLinkTaken):
		return LinkRefusalNone, err
	case err != nil:
		return LinkRefusalNone, fmt.Errorf("identity: linking: %w", err)
	}
	return refusal, nil
}
