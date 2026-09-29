package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Issuing a session is the one door into the forum, so there is exactly one
// of it (docs/09, the approval-bypass row; TKT-01M29C7ZZR). Gitea shipped the
// bug this prevents: an approval-gated instance where one code path created
// a session without checking approval, and a pending applicant walked in.
// The defense is structural rather than careful review:
//
//   - IssueUser and IssuePreUser, each with a With twin that runs the same
//     check after writing the identity, are the only exported ways to
//     create a session, and all four go through insert, the only caller of
//     the generated CreateSession. chokepoint_test.go fails the build on a
//     second caller, on a hand-written INSERT INTO sessions, on a query that
//     could turn a pre-user session into a user one, and on a caller of an
//     issuer outside this package that its list does not name.
//   - Each decides from the database, in the transaction that inserts the
//     row, not from a status a caller read earlier: a suspension that lands
//     between a caller's read and the insert cannot be missed.
//   - They take an identity, not a user. A sign-in is always by an identity
//     (docs/05 "Login flow"), and the user is whoever that identity belongs
//     to, so a caller cannot name one user and be let in as another.

// Meta is what a sign-in records about where it came from.
type Meta struct {
	IP        string
	UserAgent string
	// Provider is the identity provider's key in config.toml.
	Provider string
}

// ErrNotActive is a refusal to sign in a user whose status is not active.
// errors.As to *NotActiveError gives the status, which the sign-in page
// shows the reason for (docs/05 step 3).
var ErrNotActive = errors.New("session: the user is not active")

// NotActiveError is ErrNotActive with the user's status, and for a
// suspension its reason and end, which the "account unavailable" page shows
// (docs/05 "Login flow"). UserID is whose account it is, for reactivation.
type NotActiveError struct {
	Status string
	UserID int64
	Reason string
	// Until is when a suspension ends, in Unix ms; 0 means no end is set.
	Until int64
	// StatusVersion counts the account's status changes, as the refusal
	// read it. A reactivation is bound to it, so it is refused once the
	// status has changed since.
	StatusVersion int64
}

func (e *NotActiveError) Error() string {
	return fmt.Sprintf("session: the user is %s, not active", e.Status)
}

func (e *NotActiveError) Is(target error) bool { return target == ErrNotActive }

var (
	// ErrNoIdentity means the identity named does not exist.
	ErrNoIdentity = errors.New("session: no such identity")
	// ErrNoUser means the identity has no user yet: an approval-mode
	// applicant, who gets a pre-user session instead.
	ErrNoUser = errors.New("session: the identity has no user")
	// ErrHasUser means the identity already belongs to a user, who signs in
	// as that user or not at all, never as an applicant.
	ErrHasUser = errors.New("session: the identity already has a user")
)

// IssueUser signs in the user identityID belongs to, and returns the
// session's cookie value and the session. It refuses with ErrNoIdentity,
// ErrNoUser, or a *NotActiveError for any status but active — pending,
// suspended and deactivated alike — and then writes nothing.
func (s *Store) IssueUser(ctx context.Context, identityID int64, meta Meta) (string, Session, error) {
	return s.IssueUserWith(ctx, meta, func(context.Context, *dbq.Queries) (int64, error) { return identityID, nil })
}

// IssueUserWith is IssueUser for an identity that create writes, in the
// transaction that inserts the session. Registration uses it, so the user,
// the identity and the session commit together or not at all
// (TKT-01M29C8005). create returns the identity's id; an error from it is
// returned as it is and writes nothing. The identity then goes through
// exactly IssueUser's checks, which read what create wrote: a caller can
// write rows, but cannot decide who is let in.
func (s *Store) IssueUserWith(ctx context.Context, meta Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, Session, error) {
	return s.insert(ctx, func(q *dbq.Queries) (newSession, error) {
		identityID, err := create(ctx, q)
		if err != nil {
			return newSession{}, err
		}
		h, err := holder(ctx, q, identityID)
		if err != nil {
			return newSession{}, err
		}
		if !h.UserID.Valid {
			return newSession{}, ErrNoUser
		}
		if h.Status.String != "active" {
			return newSession{}, &NotActiveError{Status: h.Status.String, UserID: h.UserID.Int64,
				Reason: h.StatusReason.String, Until: h.StatusUntil.Int64, StatusVersion: h.StatusVersion.Int64}
		}
		return newSession{Kind: KindUser, UserID: h.UserID.Int64, IdentityID: identityID,
			IP: meta.IP, UserAgent: meta.UserAgent, Provider: meta.Provider}, nil
	})
}

// IssuePreUser places an approval-mode applicant in a pre-user session bound
// to identityID (docs/05 "mode = approval" step 1). It takes no user and can
// write only a pre-user row, and it refuses with ErrHasUser an identity that
// already belongs to a user: "no user, by design" is the only case it serves.
func (s *Store) IssuePreUser(ctx context.Context, identityID int64, meta Meta) (string, Session, error) {
	return s.IssuePreUserWith(ctx, meta, func(context.Context, *dbq.Queries) (int64, error) { return identityID, nil })
}

// IssuePreUserWith is IssuePreUser for an identity that create writes, in
// the transaction that inserts the session. A new applicant's identity row
// and their pre-user session commit together or not at all, so a failed
// insert leaves no orphaned identity, and create can read the registration
// mode in the same transaction as the write (TKT-01M29CKRG9, review 1145).
// create returns the identity's id; an error from it is returned as it is
// and writes nothing. The identity then goes through exactly IssuePreUser's
// check, which reads what create wrote.
func (s *Store) IssuePreUserWith(ctx context.Context, meta Meta, create func(context.Context, *dbq.Queries) (int64, error)) (string, Session, error) {
	return s.insert(ctx, func(q *dbq.Queries) (newSession, error) {
		identityID, err := create(ctx, q)
		if err != nil {
			return newSession{}, err
		}
		h, err := holder(ctx, q, identityID)
		if err != nil {
			return newSession{}, err
		}
		if h.UserID.Valid {
			return newSession{}, ErrHasUser
		}
		return newSession{Kind: KindPreUser, IdentityID: identityID,
			IP: meta.IP, UserAgent: meta.UserAgent, Provider: meta.Provider}, nil
	})
}

func holder(ctx context.Context, q *dbq.Queries, identityID int64) (dbq.IdentityHolderRow, error) {
	h, err := q.IdentityHolder(ctx, identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNoIdentity
	}
	return h, err
}
