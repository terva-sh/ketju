package perm

import (
	"context"
	"database/sql"

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

// SetRole changes a user's role. The session middleware reads the role into
// each request's Viewer, so a changed role also changes the cache key; the
// invalidation is for the requests already holding the old Viewer.
func (e *Evaluator) SetRole(ctx context.Context, userID int64, role string) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.SetUserRole(ctx, dbq.SetUserRoleParams{Role: role, UpdatedAt: now, ID: userID})
	})
}
