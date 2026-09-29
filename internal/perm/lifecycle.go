package perm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The account lifecycle (docs/05 "Account lifecycle", TKT-01M29CKRFS). A
// status decides what a viewer may do: the session middleware builds every
// Viewer from the users row, and a user who is not active has no member
// rights. So a status change is a decision write, and it is made here, as a
// role change is: in one transaction with its audit row and the deletion of
// the account's sessions, or not at all. SetUserStatus, AnonymizeUser and
// DeleteUserIdentities are in decisionWrites, so no other package can change
// a status without the row.

// The audit_log actions a status change writes, against the user.
const (
	// ActionDeactivated is a member deactivating their own account.
	ActionDeactivated = "user.deactivated"
	// ActionSuspended is a suspension, with its reason and end in the
	// details.
	ActionSuspended = "user.suspended"
	// ActionUnsuspended ends a suspension.
	ActionUnsuspended = "user.unsuspended"
	// ActionReactivated is a deactivated member coming back, through the
	// Reactivate button of the "account unavailable" page.
	ActionReactivated = "user.reactivated"
	// ActionAnonymized is an anonymization. Its details name no handle.
	ActionAnonymized = "user.anonymized"
)

var (
	// ErrBadTransition is the answer for a status change the account's
	// status does not allow, such as unsuspending an active account.
	ErrBadTransition = errors.New("perm: the account's status does not allow this change")
	// ErrAnonymized is the answer for any change to an anonymized account,
	// which is final (docs/08 "Primitives").
	ErrAnonymized = errors.New("perm: the account is anonymized")
)

// transitions are the status changes, by the action that names them: the
// statuses each may start from, and the one it ends in. Anything else is
// ErrBadTransition. Reactivation starts only from deactivated, so a
// suspended, pending or anonymized account never comes back by it.
var transitions = map[string]struct {
	from []string
	to   string
}{
	ActionDeactivated: {[]string{domain.StatusActive}, domain.StatusDeactivated},
	ActionSuspended:   {[]string{domain.StatusActive, domain.StatusDeactivated}, domain.StatusSuspended},
	ActionUnsuspended: {[]string{domain.StatusSuspended}, domain.StatusActive},
	ActionReactivated: {[]string{domain.StatusDeactivated}, domain.StatusActive},
}

// StatusChange is one status change to an account.
type StatusChange struct {
	// By is who made it, for the audit row.
	By audit.By
	// Action names the change: ActionDeactivated, ActionSuspended,
	// ActionUnsuspended or ActionReactivated.
	Action string
	// Reason and Until are a suspension's, shown to the member at sign-in.
	// Until is Unix ms, and 0 means no end is set. Both are ignored for any
	// other change, which clears them.
	Reason string
	Until  int64
	// Allow is the caller's rule for who may change whose status, asked
	// about the account as read in the transaction that changes it, so a
	// promotion that lands between the caller's read and the change cannot
	// slip past it. An error from it is returned as it is, and nothing is
	// written. Nil allows.
	Allow func(target dbq.User) error
}

// ChangeStatus makes c to userID in one transaction, then clears the cache:
// the status, the audit row, and, when the account leaves active, the
// deletion of every session it holds and the revocation of an agent's
// credential, so its next request is anonymous, or for an agent refused.
//
// It refuses an anonymized account (ErrAnonymized), a change its status does
// not allow (ErrBadTransition), and one that would leave the forum with no
// active admin (ErrLastAdmin), counted inside the transaction so two admins
// suspending each other at once cannot both succeed.
func (e *Evaluator) ChangeStatus(ctx context.Context, userID int64, c StatusChange) error {
	_, err := e.ChangeStatusID(ctx, userID, c)
	return err
}

// ChangeStatusID is ChangeStatus, answering the id of the audit row it
// wrote. A suspension's hidden posts carry that id, so that a restore can
// put back exactly the posts it hid (TKT-01M29CP2QK).
func (e *Evaluator) ChangeStatusID(ctx context.Context, userID int64, c StatusChange) (int64, error) {
	var id int64
	err := e.write(ctx, func(q *dbq.Queries, now int64) error {
		var err error
		id, err = changeStatus(ctx, q, now, userID, c)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ChangeStatusIn is ChangeStatus inside the caller's transaction, for a
// change that commits with something else: reactivation commits with the
// session it issues (identity.Service.Reactivate). The caller must call
// Invalidate once the transaction has committed.
func (e *Evaluator) ChangeStatusIn(ctx context.Context, q *dbq.Queries, userID int64, c StatusChange) error {
	_, err := changeStatus(ctx, q, e.now().UnixMilli(), userID, c)
	return err
}

// changeStatus makes c and answers the id of its audit row.
func changeStatus(ctx context.Context, q *dbq.Queries, now, userID int64, c StatusChange) (int64, error) {
	t, ok := transitions[c.Action]
	if !ok {
		return 0, fmt.Errorf("%w: %q is not a status change", ErrBadTransition, c.Action)
	}
	u, err := target(ctx, q, userID, c.Allow)
	if err != nil {
		return 0, err
	}
	if !slices.Contains(t.from, u.Status) {
		return 0, fmt.Errorf("%w: %s from %s", ErrBadTransition, c.Action, u.Status)
	}
	if err := keepAnAdmin(ctx, q, u, t.to); err != nil {
		return 0, err
	}
	var reason string
	var until sql.NullInt64
	details := map[string]any{"from": u.Status}
	if t.to == domain.StatusSuspended {
		reason, until = c.Reason, sql.NullInt64{Int64: c.Until, Valid: c.Until != 0}
		details["reason"] = reason
		details["until"] = nil
		if until.Valid {
			details["until"] = until.Int64
		}
	}
	if err := q.SetUserStatus(ctx, dbq.SetUserStatusParams{Status: t.to, StatusReason: reason, StatusUntil: until,
		UpdatedAt: now, ID: userID}); err != nil {
		return 0, err
	}
	if t.to != domain.StatusActive {
		if err := deleteSessions(ctx, q, userID); err != nil {
			return 0, err
		}
		if _, err := RevokeAgentCredentialIn(ctx, q, time.UnixMilli(now), c.By, userID, c.Action); err != nil {
			return 0, err
		}
	}
	return audit.Append(ctx, q, time.UnixMilli(now), c.By, c.Action, audit.User(userID), details)
}

// ActionCredentialRevoked is an agent's credential ending, against the
// agent, with what ended it as "cause": an admin's revocation, a
// replacement, or the status change that took the agent out of active.
const ActionCredentialRevoked = "agent.credential_revoked"

// RevokeAgentCredentialIn ends userID's live agent credential, if it has
// one, inside the caller's transaction, and writes ActionCredentialRevoked
// with {"cause": cause} beside it. It reports whether a credential was
// live; when none was it writes nothing, so it is safe to call for any
// account. That is how a status change revokes an agent's credential
// without asking the account's kind: a human never has one (docs/05
// "Account lifecycle", TKT-01M29P9RSW).
//
// The cause is not written as "via", which audit.By.Via owns.
func RevokeAgentCredentialIn(ctx context.Context, q *dbq.Queries, at time.Time, by audit.By, userID int64, cause string) (bool, error) {
	n, err := q.RevokeAgentCredentials(ctx, dbq.RevokeAgentCredentialsParams{Now: at.UnixMilli(),
		RevokedBy: sql.NullInt64{Int64: by.Actor, Valid: by.Actor != 0}, UserID: userID})
	if err != nil || n == 0 {
		return false, err
	}
	_, err = audit.Append(ctx, q, at, by, ActionCredentialRevoked, audit.User(userID), map[string]any{"cause": cause})
	return err == nil, err
}

// Anonymize anonymizes userID on by's authority, in one transaction, then
// clears the cache (docs/05 "Account lifecycle"): the handle becomes
// deleted-user-<id>, the display name, avatar and bio are cleared, the
// account becomes deactivated, its sessions and identities are deleted, and
// an agent's credential is revoked.
// The old handle is reserved for domain.HandleReservation by a
// handle_releases row that names no user, so nobody can take it, and the
// links an old mention of it would give them, while nothing ties it to
// deleted-user-<id> (maintainer decision 2026-09-29, Terva review 1448).
// An application that named one of the identities keeps its row with a null
// identity_id, by the column's foreign key. The ActionAnonymized row names
// no handle. Post bodies, in the thread logs, are not touched.
//
// allow is the caller's authority rule, as StatusChange.Allow. It refuses an
// account already anonymized (ErrAnonymized) and the last active admin
// (ErrLastAdmin).
func (e *Evaluator) Anonymize(ctx context.Context, userID int64, by audit.By, allow func(target dbq.User) error) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		u, err := target(ctx, q, userID, allow)
		if err != nil {
			return err
		}
		if err := keepAnAdmin(ctx, q, u, domain.StatusDeactivated); err != nil {
			return err
		}
		if err := q.AnonymizeUser(ctx, dbq.AnonymizeUserParams{UpdatedAt: now, ID: userID}); err != nil {
			return err
		}
		if err := q.InsertHandleRelease(ctx, dbq.InsertHandleReleaseParams{Handle: u.Handle, ReleasedAt: now,
			ReservedUntil: now + domain.HandleReservation.Milliseconds()}); err != nil {
			return err
		}
		// Sessions first: sessions.identity_id references the identities.
		if err := deleteSessions(ctx, q, userID); err != nil {
			return err
		}
		if err := q.DeleteUserIdentities(ctx, sql.NullInt64{Int64: userID, Valid: true}); err != nil {
			return err
		}
		if _, err := RevokeAgentCredentialIn(ctx, q, time.UnixMilli(now), by, userID, ActionAnonymized); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, time.UnixMilli(now), by, ActionAnonymized, audit.User(userID),
			map[string]any{"from": u.Status})
		return err
	})
}

// deleteSessions ends every session the account holds: its member sessions,
// and any pre-user session still bound to one of its identities from before
// it had an account, which no longer resolves but is removed all the same.
func deleteSessions(ctx context.Context, q *dbq.Queries, userID int64) error {
	id := sql.NullInt64{Int64: userID, Valid: true}
	if err := q.DeleteUserSessions(ctx, id); err != nil {
		return err
	}
	return q.DeleteIdentitySessions(ctx, id)
}

// target reads the account a lifecycle change is about, in the change's
// transaction, and asks allow about it. An anonymized account is refused
// whatever allow says.
func target(ctx context.Context, q *dbq.Queries, userID int64, allow func(dbq.User) error) (dbq.User, error) {
	u, err := q.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNoUser
	}
	if err != nil {
		return u, err
	}
	if u.Anonymized != 0 {
		return u, ErrAnonymized
	}
	if allow != nil {
		if err := allow(u); err != nil {
			return u, err
		}
	}
	return u, nil
}

// keepAnAdmin refuses a change that would take the last active admin out of
// active (settled in grooming, 2026-09-29): the forum must not be able to
// lock itself out.
func keepAnAdmin(ctx context.Context, q *dbq.Queries, u dbq.User, to string) error {
	if u.Role != domain.RoleAdmin || u.Status != domain.StatusActive || to == domain.StatusActive {
		return nil
	}
	n, err := q.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}
