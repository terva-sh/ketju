// Package account is the account lifecycle as members and staff reach it
// (docs/05 "Account lifecycle", TKT-01M29CKRFS): a member deactivating their
// own account, staff suspending and unsuspending one, and an admin
// anonymizing one.
//
// It holds who may do what to whom, and the checks on what they send. The
// change itself is internal/perm's, which writes the status, its audit row
// and the deletion of the account's sessions in one transaction, and asks
// this package's authority rule about the account as read in that
// transaction.
package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ReasonMaxLen bounds a suspension's reason, in characters. The member reads
// it on the sign-in page, so it is a sentence or two, not an essay.
const ReasonMaxLen = 500

// Service is the account lifecycle.
type Service struct {
	DB   *db.DB
	Perm *perm.Evaluator
	// Threads writes a suspension's hide and restore of the account's
	// posts. Nil refuses both, for a service that only changes statuses,
	// such as the expiry job's.
	Threads *threads.Service
	// Now is the clock a suspension's end is checked against; nil is
	// time.Now.
	Now func() time.Time

	// listed, when set, is called by ExpireSuspensions between reading the
	// list and changing the first account. Tests use it to change an
	// account in that gap.
	listed func()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func invalid(format string, args ...any) error {
	return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("account: "+format, args...))
}

func forbidden(format string, args ...any) error {
	return problem.Coded(problem.CodeForbidden, fmt.Errorf("account: "+format, args...))
}

// Deactivate is POST /settings/deactivate: v deactivates their own account.
// Their sessions end with it, this one included, and they come back by
// signing in and pressing Reactivate. The last active admin cannot leave
// this way (conflict). ip is the request's address, for the audit row.
func (s *Service) Deactivate(ctx context.Context, v domain.Viewer, ip string) error {
	if err := member.Require(v, "deactivate an account"); err != nil {
		return err
	}
	return s.Perm.ChangeStatus(ctx, v.UserID, perm.StatusChange{
		By: audit.By{Actor: v.UserID, IP: ip}, Action: perm.ActionDeactivated,
		Allow: func(t dbq.User) error {
			if t.ID != v.UserID {
				return forbidden("a member deactivates only their own account")
			}
			return nil
		},
	})
}

// Suspend is POST /admin/users/{handle}/suspend: staff suspend the account
// handle names, with a reason the member is shown at sign-in and, when until
// is not nil, the time it ends. The account's sessions end with it.
//
// The reason is 1 to ReasonMaxLen characters once trimmed, with no control
// characters; until must be in the future. Who may suspend whom is mayActOn.
//
// With hidePosts, the account's visible posts are then hidden with the
// suspension (HidePosts). The suspension commits first, because a database
// transaction cannot span the thread logs. A hide that fails part-way
// leaves the account suspended and answers the error, and
// POST …/posts/hide finishes the job.
func (s *Service) Suspend(ctx context.Context, v domain.Viewer, ip, handle, reason string, until *time.Time, hidePosts bool) error {
	if err := member.RequireStaff(v, "suspend an account"); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	switch n := utf8.RuneCountInString(reason); {
	case !utf8.ValidString(reason):
		return invalid("the reason is not valid UTF-8")
	case n == 0:
		return invalid("a suspension needs a reason")
	case n > ReasonMaxLen:
		return invalid("the reason is %d characters, over %d", n, ReasonMaxLen)
	case strings.IndexFunc(reason, unicode.IsControl) >= 0:
		return invalid("the reason has a control character")
	}
	var ms int64
	if until != nil {
		if !until.After(s.now()) {
			return invalid("the suspension's end is not in the future")
		}
		ms = until.UnixMilli()
	}
	if hidePosts && s.Threads == nil {
		return errNoThreads
	}
	id, err := s.lookup(ctx, handle)
	if err != nil {
		return err
	}
	by := audit.By{Actor: v.UserID, IP: ip}
	suspension, err := s.Perm.ChangeStatusID(ctx, id, perm.StatusChange{
		By: by, Action: perm.ActionSuspended, Reason: reason, Until: ms,
		Allow: mayActOn(v, "suspend"),
	})
	if err != nil || !hidePosts {
		return err
	}
	return s.hide(ctx, v, by, id, suspension)
}

// Unsuspend is POST /admin/users/{handle}/unsuspend: staff end a
// suspension, on the same authority as Suspend. With restorePosts, the
// posts the account's suspensions hid are then restored (RestorePosts).
// Without it they stay hidden, as they do when a suspension expires.
func (s *Service) Unsuspend(ctx context.Context, v domain.Viewer, ip, handle string, restorePosts bool) error {
	if err := member.RequireStaff(v, "unsuspend an account"); err != nil {
		return err
	}
	if restorePosts && s.Threads == nil {
		return errNoThreads
	}
	id, err := s.lookup(ctx, handle)
	if err != nil {
		return err
	}
	by := audit.By{Actor: v.UserID, IP: ip}
	if err := s.Perm.ChangeStatus(ctx, id, perm.StatusChange{
		By: by, Action: perm.ActionUnsuspended, Allow: mayActOn(v, "unsuspend"),
	}); err != nil || !restorePosts {
		return err
	}
	return s.restore(ctx, v, by, id)
}

// Anonymize is POST /admin/users/{handle}/anonymize: an admin anonymizes
// the account handle names, which cannot be undone (docs/08 "Primitives").
// An admin cannot anonymize themselves, and nobody can anonymize the last
// active admin (conflict).
func (s *Service) Anonymize(ctx context.Context, v domain.Viewer, ip, handle string) error {
	if err := member.RequireAdmin(v, "anonymize an account"); err != nil {
		return err
	}
	id, err := s.lookup(ctx, handle)
	if err != nil {
		return err
	}
	return s.Perm.Anonymize(ctx, id, audit.By{Actor: v.UserID, IP: ip}, func(t dbq.User) error {
		if t.ID == v.UserID {
			return forbidden("an admin cannot anonymize their own account")
		}
		return nil
	})
}

// mayActOn is who may suspend or unsuspend whom (maintainer decision
// 2026-09-29, docs/05 "Account lifecycle"): a moderator acts on members
// only, and an admin on anyone except themselves. It is asked about the
// account as read in the transaction that changes it, so the target's role
// is the one it has when the change is made. v's role is the one its request
// began with, as for every other check (package perm).
func mayActOn(v domain.Viewer, what string) func(dbq.User) error {
	return func(t dbq.User) error {
		switch {
		case t.ID == v.UserID:
			return forbidden("staff cannot %s their own account", what)
		case v.Role == domain.RoleAdmin:
			return nil
		case v.Role == domain.RoleModerator && t.Role == domain.RoleMember:
			return nil
		default:
			return forbidden("a %s cannot %s a %s", v.Role, what, t.Role)
		}
	}
}

// lookup is the id of the account handle names, matched as the column's
// NOCASE collation matches it. Staff may act on any account, a pending one
// included, so there is no visibility rule here: a handle nobody has is
// not-found.
func (s *Service) lookup(ctx context.Context, handle string) (int64, error) {
	var id int64
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(ctx, handle)
		id = u.ID
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, problem.Coded(problem.CodeNotFound, fmt.Errorf("account: no account has the handle %q", handle))
	}
	return id, err
}
