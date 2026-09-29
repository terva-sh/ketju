package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// A suspension's posts, and its end (docs/08 "Suspension", TKT-01M29CP2QK).
//
// Hiding a suspended account's posts is a moderator's Delete of each one,
// carrying the id of the suspension's user.suspended row
// (threads.Service.HidePostsOf). Restoring puts back exactly the posts one of
// the account's suspensions hid (threads.Service.RestorePostsOf), so a post
// a moderator hid for its own sake stays hidden. A suspension that expires
// leaves its hidden posts hidden until a moderator restores them
// (maintainer decision 2026-09-29).

// The audit_log actions of a suspension's posts, against the user. The
// per-post rows are the projection's post.hidden and post.restored; these
// say which suspension and how many.
const (
	// ActionPostsHidden is a suspension's hide, with the suspension's audit
	// id as "suspension" and the number hidden as "posts".
	ActionPostsHidden = "user.posts_hidden"
	// ActionPostsRestored is the restore of posts suspensions hid, with the
	// number restored as "posts".
	ActionPostsRestored = "user.posts_restored"
)

// ViaExpiry is what an expired suspension's user.unsuspended row names as
// having lifted it, with no actor (audit.System).
const ViaExpiry = "expiry"

var errNoThreads = errors.New("account: this service has no thread service to hide or restore posts with")

// errNotDue is the expiry job's refusal of an account whose suspension was
// replaced or lifted after the job listed it.
var errNotDue = errors.New("account: the suspension is not due to expire")

// HidePosts is POST /admin/users/{handle}/posts/hide: staff hide the
// visible posts of a suspended account with its current suspension, on the
// authority that suspends it (mayActOn). Posts already hidden or deleted are
// passed over, so it finishes a hide that stopped part-way and changes
// nothing the second time. An account that is not suspended is conflict.
func (s *Service) HidePosts(ctx context.Context, v domain.Viewer, ip, handle string) error {
	if err := member.RequireStaff(v, "hide an account's posts"); err != nil {
		return err
	}
	if s.Threads == nil {
		return errNoThreads
	}
	var (
		u   dbq.User
		ids []int64
	)
	if err := s.readAccount(ctx, handle, func(q *dbq.Queries, t dbq.User) error {
		u = t
		var err error
		ids, err = q.AuditIDs(ctx, suspensionRows(t.ID))
		return err
	}); err != nil {
		return err
	}
	if err := mayActOn(v, "hide the posts of")(u); err != nil {
		return err
	}
	if u.Status != domain.StatusSuspended || len(ids) == 0 {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("account: %s is not suspended", handle))
	}
	// A suspended account's newest user.suspended row is its current
	// suspension: only a suspension leads into suspended.
	return s.hide(ctx, v, audit.By{Actor: v.UserID, IP: ip}, u.ID, ids[len(ids)-1])
}

// RestorePosts is POST /admin/users/{handle}/posts/restore: staff restore
// the posts any of the account's suspensions hid, on the authority that
// suspends it (mayActOn), whatever its status now. It is how a moderator
// brings back the posts of a suspension that expired.
func (s *Service) RestorePosts(ctx context.Context, v domain.Viewer, ip, handle string) error {
	if err := member.RequireStaff(v, "restore an account's posts"); err != nil {
		return err
	}
	if s.Threads == nil {
		return errNoThreads
	}
	var (
		u   dbq.User
		ids []int64
	)
	if err := s.readAccount(ctx, handle, func(q *dbq.Queries, t dbq.User) error {
		u = t
		var err error
		ids, err = q.AuditIDs(ctx, suspensionRows(t.ID))
		return err
	}); err != nil {
		return err
	}
	if err := mayActOn(v, "restore the posts of")(u); err != nil {
		return err
	}
	// The suspensions are the ones there were when the account was read, so
	// one another moderator makes while this runs keeps its hidden posts.
	return s.restore(ctx, v, audit.By{Actor: v.UserID, IP: ip}, u.ID, ids)
}

// hide hides userID's visible posts with suspension and writes
// ActionPostsHidden for however many it hid, the error's part-way count
// included.
func (s *Service) hide(ctx context.Context, v domain.Viewer, by audit.By, userID, suspension int64) error {
	n, err := s.Threads.HidePostsOf(ctx, v, userID, suspension)
	if werr := s.postsRow(ctx, by, ActionPostsHidden, userID, n, map[string]any{"suspension": suspension}); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		return fmt.Errorf("account: hiding the account's posts stopped after %d: %w", n, err)
	}
	return nil
}

// restore restores the posts of userID's that the suspensions ids hid, and
// writes ActionPostsRestored for however many it restored.
func (s *Service) restore(ctx context.Context, v domain.Viewer, by audit.By, userID int64, ids []int64) error {
	n, err := s.Threads.RestorePostsOf(ctx, v, userID, ids)
	if werr := s.postsRow(ctx, by, ActionPostsRestored, userID, n, map[string]any{}); werr != nil && err == nil {
		err = werr
	}
	if err != nil {
		return fmt.Errorf("account: restoring the account's posts stopped after %d: %w", n, err)
	}
	return nil
}

// postsRow writes action against userID with "posts": n, when n is not
// zero. A run that changed no post writes nothing.
func (s *Service) postsRow(ctx context.Context, by audit.By, action string, userID int64, n int, details map[string]any) error {
	if n == 0 {
		return nil
	}
	details["posts"] = n
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := audit.Append(ctx, q, s.now(), by, action, audit.User(userID), details)
		return err
	})
}

// ExpireSuspensions lifts every suspension whose end has passed, and
// answers how many it lifted: the suspension expiry job's work
// (jobs.SuspensionExpiry). Each goes through perm.ChangeStatus as an
// unsuspension by no account, "via": "expiry", so its sessions stay ended
// and its row is written with the status. The end is checked again inside
// that transaction, so an account suspended afresh after the list was read
// keeps its new suspension. Posts a suspension hid stay hidden.
//
// An account that fails is reported with the others' errors, after the rest
// have been tried.
func (s *Service) ExpireSuspensions(ctx context.Context) (int64, error) {
	now := s.now().UnixMilli()
	var ids []int64
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		ids, err = q.ExpiredSuspensions(ctx, now)
		return err
	}); err != nil {
		return 0, err
	}
	if s.listed != nil {
		s.listed()
	}
	var (
		n    int64
		errs []error
	)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		err := s.Perm.ChangeStatus(ctx, id, perm.StatusChange{
			By: audit.System(ViaExpiry), Action: perm.ActionUnsuspended,
			Allow: func(t dbq.User) error {
				if t.Status != domain.StatusSuspended || !t.StatusUntil.Valid || t.StatusUntil.Int64 > now {
					return errNotDue
				}
				return nil
			},
		})
		switch {
		case err == nil:
			n++
		case errors.Is(err, errNotDue), errors.Is(err, perm.ErrBadTransition), errors.Is(err, perm.ErrNoUser),
			errors.Is(err, perm.ErrAnonymized):
			// Changed since it was listed: nothing is due.
		default:
			errs = append(errs, fmt.Errorf("account: expiring user %d's suspension: %w", id, err))
		}
	}
	return n, errors.Join(errs...)
}

// readAccount reads the account handle names and calls fn with it, in one
// read transaction. A handle nobody has is not-found.
func (s *Service) readAccount(ctx context.Context, handle string, fn func(q *dbq.Queries, u dbq.User) error) error {
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(ctx, handle)
		if err != nil {
			return err
		}
		return fn(q, u)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return problem.Coded(problem.CodeNotFound, fmt.Errorf("account: no account has the handle %q", handle))
	}
	return err
}

// suspensionRows names the user.suspended rows about userID.
func suspensionRows(userID int64) dbq.AuditIDsParams {
	t := audit.User(userID)
	return dbq.AuditIDsParams{TargetType: t.Type, TargetID: t.ID, Action: perm.ActionSuspended}
}
