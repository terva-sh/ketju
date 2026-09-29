package perm

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The writes below are every write that can change a decision. Each commits,
// then clears the cache, so the change applies on the next check rather than
// within the TTL. TestMutationsGoThroughPerm keeps the underlying dbq calls
// out of every other package: a write that bypassed these would be invisible
// for up to a minute, and nothing would say so.
//
// Invalidation follows the commit, never precedes it. The other order leaves
// a window where a check refills the cache from the old rows.

// write runs fn in a write transaction and invalidates the cache once it has
// committed. A failed write leaves the cache alone: nothing changed.
func (e *Evaluator) write(ctx context.Context, fn func(q *dbq.Queries, now int64) error) error {
	err := e.db.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		return fn(q, e.now().UnixMilli())
	})
	if err != nil {
		return err
	}
	e.Invalidate()
	return nil
}

func nullForum(forumID int64) sql.NullInt64 {
	return sql.NullInt64{Int64: forumID, Valid: forumID != 0}
}

// Grant gives group p in forumID, or globally when forumID is 0. Forum id 0
// is reserved by the schema, so it can mean "global" here without ambiguity.
func (e *Evaluator) Grant(ctx context.Context, forumID, groupID int64, p domain.Permission) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.GrantPermission(ctx, dbq.GrantPermissionParams{
			ForumID: nullForum(forumID), GroupID: groupID, Permission: string(p), CreatedAt: now,
		})
	})
}

// Revoke removes a grant made by Grant with the same arguments.
func (e *Evaluator) Revoke(ctx context.Context, forumID, groupID int64, p domain.Permission) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.RevokePermission(ctx, dbq.RevokePermissionParams{
			ForumID: nullForum(forumID), GroupID: groupID, Permission: string(p),
		})
	})
}

// AddMember adds userID to groupID on an admin's authority, taking over any
// membership an IdP mapping made. addedBy is 0 when no user is to be named.
func (e *Evaluator) AddMember(ctx context.Context, groupID, userID, addedBy int64) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.AddAdminMembership(ctx, dbq.AddAdminMembershipParams{
			GroupID: groupID, UserID: userID,
			AddedBy: sql.NullInt64{Int64: addedBy, Valid: addedBy != 0}, AddedAt: now,
		})
	})
}

// RemoveMember removes userID from groupID, whoever added them.
func (e *Evaluator) RemoveMember(ctx context.Context, groupID, userID int64) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.RemoveMembership(ctx, dbq.RemoveMembershipParams{GroupID: groupID, UserID: userID})
	})
}

// SyncIdPGroups replaces userID's IdP-sourced memberships with groupIDs, in
// one transaction, as login re-evaluation does (TKT-01M29CKRF9). Memberships
// an admin added are kept.
func (e *Evaluator) SyncIdPGroups(ctx context.Context, userID int64, groupIDs []int64) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		if err := q.DeleteIdPMemberships(ctx, userID); err != nil {
			return err
		}
		for _, g := range groupIDs {
			if err := q.AddIdPMembership(ctx, dbq.AddIdPMembershipParams{GroupID: g, UserID: userID, AddedAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetVisibility changes a forum's visibility.
func (e *Evaluator) SetVisibility(ctx context.Context, forumID int64, visibility string) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.SetForumVisibility(ctx, dbq.SetForumVisibilityParams{Visibility: visibility, UpdatedAt: now, ID: forumID})
	})
}

// SetRole changes a user's role. It applies from the user's next request,
// whose Viewer the session middleware builds from the users row; a request
// already holding the old Viewer finishes under the old role (see the package
// comment). The role is part of the cache key, so the new Viewer never meets
// a decision cached for the old one; the invalidation is for a Viewer built
// by other means.
func (e *Evaluator) SetRole(ctx context.Context, userID int64, role string) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.SetUserRole(ctx, dbq.SetUserRoleParams{Role: role, UpdatedAt: now, ID: userID})
	})
}

// ActionPromoted is the audit_log action Promote writes. Bootstrap reads it
// back as the mark that a user has been made admin before.
const ActionPromoted = "user.promoted"

// ErrNoUser is Promote's answer for a user id that names nobody.
var ErrNoUser = errors.New("perm: no such user")

// Promotion is how a user comes to be made admin, for the audit row.
type Promotion struct {
	// Actor is who promoted them: 0 for the configuration or the command
	// line, which are nobody's account.
	Actor int64
	// Details go into the audit row's details_json, beside "role".
	Details map[string]string
	// Once leaves alone a user audit_log already records as promoted,
	// whatever their role is now. Bootstrap sets it, so that an admin who
	// was demoted since is not made admin again at their next sign-in
	// (TKT-01M29C800G). A user already an admin with no promotion on
	// record gets one, with "already": "admin" in its details, and keeps
	// their role, so the same holds for them.
	Once bool
}

// Promote makes userID an admin and records it in audit_log, in one
// transaction, then clears the cache. It reports whether it changed the
// role: a user who is already an admin, or with Once one promoted before,
// keeps their role, and without Once nothing is recorded for them.
//
// Clearing the cache reaches only evaluators in this process. That is
// enough for a promotion made elsewhere, such as by ketju admin promote
// beside a running server: a Viewer is built from the users row on every
// request, the role is part of the cache key, and an admin passes every
// check before the cache is read, so the promoted user's next request
// meets no stale entry (TestPromoteFromAnotherProcess).
func (e *Evaluator) Promote(ctx context.Context, userID int64, p Promotion) (bool, error) {
	details := map[string]any{}
	for k, v := range p.Details {
		details[k] = v
	}
	details["role"] = domain.RoleAdmin
	target := audit.User(userID)
	promoted := false
	err := e.write(ctx, func(q *dbq.Queries, now int64) error {
		u, err := q.GetUser(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		record := func(details map[string]any) error {
			_, err := audit.Append(ctx, q, time.UnixMilli(now), audit.By{Actor: p.Actor}, ActionPromoted, target, details)
			return err
		}
		if p.Once {
			seen, err := q.AuditExists(ctx, dbq.AuditExistsParams{TargetType: target.Type, TargetID: target.ID, Action: ActionPromoted})
			if err != nil || seen {
				return err
			}
			// Already an admin with no promotion on record (created as
			// one): the role is left alone, but the promotion this would
			// have been is recorded, so that once demoted they are not
			// made admin again (review 1128).
			if u.Role == domain.RoleAdmin {
				already := maps.Clone(details)
				already["already"] = domain.RoleAdmin
				return record(already)
			}
		}
		if u.Role == domain.RoleAdmin {
			return nil
		}
		if err := q.SetUserRole(ctx, dbq.SetUserRoleParams{Role: domain.RoleAdmin, UpdatedAt: now, ID: userID}); err != nil {
			return err
		}
		if err := record(details); err != nil {
			return err
		}
		promoted = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return promoted, nil
}
