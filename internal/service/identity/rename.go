package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	// Aliased: the package's tests declare a member helper.
	members "github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Handle changes (docs/05 "Changing a handle", decided 2026-09-28;
// TKT-01M3NRP3W9). A member may change their own handle once every
// RenameCooldown; an admin may rename any account, without the cooldown and
// without starting it. Either way the new handle passes registration's
// checks (CheckHandle, HandleFree), the old one is reserved for
// domain.HandleReservation by a handle_releases row naming the account, so
// members asking for it are redirected, and one ActionRenamed audit row
// records the old and the new handle. An anonymized account is never
// renamed.
//
// The rename lives here, beside the handle rules, rather than in perm: it
// changes no permission decision. No session is touched and no cache is
// cleared: a session reads the users row on every request, the response
// cache's authors digest sees the new handle, and the render and response
// caches check every mention lookup on a hit (docs/07 "The response cache").

// RenameCooldown is how often a member may change their own handle.
const RenameCooldown = 30 * 24 * time.Hour

// ActionRenamed is a handle change, against the user, with {"from", "to"}.
// Anonymizing leaves these rows as they are (docs/05 "Account lifecycle").
const ActionRenamed = "user.renamed"

// ChangeHandle is PUT /settings/handle: v changes their own handle to to.
// ip is the request's address, for the audit row.
//
// A second change inside RenameCooldown of the last is conflict, carrying
// when the next may be made (problem.AvailableAt).
func (s *Service) ChangeHandle(ctx context.Context, v domain.Viewer, ip, to string) (dto.HandleChange, error) {
	if err := members.Require(v, "change a handle"); err != nil {
		return dto.HandleChange{}, err
	}
	return s.rename(ctx, v, ip, "", to)
}

// HandleState is GET /settings/handle: v's handle, and when they may next
// change it themselves. NextChangeAt comes from nextChange, the rule
// ChangeHandle refuses by, so the date a screen shows and the available_at
// of a refusal cannot disagree.
func (s *Service) HandleState(ctx context.Context, v domain.Viewer) (dto.HandleState, error) {
	if err := members.Require(v, "read a handle's cooldown"); err != nil {
		return dto.HandleState{}, err
	}
	ms := s.now().UnixMilli()
	var out dto.HandleState
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(ctx, v.UserID)
		if err != nil {
			return err
		}
		out = dto.HandleState{Handle: u.Handle, NextChangeAt: nextChange(u, ms)}
		return nil
	})
	if err != nil {
		return dto.HandleState{}, err
	}
	return out, nil
}

// RenameMember is POST /admin/users/{handle}/rename: an admin renames the
// account target names to to, without the cooldown, and the account's own
// cooldown does not move.
func (s *Service) RenameMember(ctx context.Context, v domain.Viewer, ip, target, to string) (dto.HandleChange, error) {
	if err := members.RequireAdmin(v, "rename an account"); err != nil {
		return dto.HandleChange{}, err
	}
	return s.rename(ctx, v, ip, target, to)
}

// handleError is a validation-failed refusal of the handle, with its code.
func handleError(code string) error {
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: []forms.FieldError{{Field: "handle", Code: code}}}
}

// rename renames v's own account when target is empty, and otherwise, as an
// admin, the account target names. Everything it reads to decide is read in
// the transaction that writes, so neither a rename nor a registration can
// land between the check and the write.
func (s *Service) rename(ctx context.Context, v domain.Viewer, ip, target, to string) (dto.HandleChange, error) {
	h, err := CheckHandle(to)
	switch {
	case errors.Is(err, ErrHandleReserved):
		return dto.HandleChange{}, handleError(CodeReserved)
	case err != nil:
		return dto.HandleChange{}, handleError(CodeInvalidHandle)
	}
	own := target == ""
	now := s.now()
	ms := now.UnixMilli()
	var out dto.HandleChange
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var (
			u   dbq.User
			err error
		)
		if own {
			u, err = q.GetUser(ctx, v.UserID)
		} else {
			u, err = q.GetUserByHandle(ctx, target)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("identity: no account has the handle %q", target))
		}
		if err != nil {
			return err
		}
		if members.Anonymized(u) {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("identity: account %d is anonymized", u.ID))
		}
		if strings.EqualFold(u.Handle, h) {
			return handleError(CodeSame)
		}
		next := nextChange(u, ms)
		if own && next != nil {
			return problem.AvailableAt(problem.CodeConflict, int64(*next),
				fmt.Errorf("identity: account %d changed its handle within %v", u.ID, RenameCooldown))
		}
		if err := HandleFree(ctx, q, h, ms); errors.Is(err, ErrHandleTaken) {
			return handleError(CodeTaken)
		} else if err != nil {
			return err
		}
		changed := sql.NullInt64{Int64: ms, Valid: own}
		n, err := q.RenameUser(ctx, dbq.RenameUserParams{Handle: h, ChangedAt: changed, UpdatedAt: ms, ID: u.ID,
			OldHandle: u.Handle})
		if db.IsUniqueViolation(err) {
			return handleError(CodeTaken)
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("identity: account %d changed while it was renamed", u.ID))
		}
		if err := q.InsertHandleRelease(ctx, dbq.InsertHandleReleaseParams{Handle: u.Handle,
			UserID: sql.NullInt64{Int64: u.ID, Valid: true}, ReleasedAt: ms,
			ReservedUntil: ms + domain.HandleReservation.Milliseconds()}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionRenamed, audit.User(u.ID),
			map[string]any{"from": u.Handle, "to": h}); err != nil {
			return err
		}
		if own {
			u.HandleChangedAt = changed
		}
		out = dto.HandleChange{Handle: h, Previous: u.Handle, NextChangeAt: nextChange(u, ms)}
		return nil
	})
	if err != nil {
		return dto.HandleChange{}, err
	}
	return out, nil
}

// nextChange is when u may next change their own handle, or nil when they
// may at now.
func nextChange(u dbq.User, now int64) *dto.Millis {
	if !u.HandleChangedAt.Valid {
		return nil
	}
	at := u.HandleChangedAt.Int64 + RenameCooldown.Milliseconds()
	if at <= now {
		return nil
	}
	m := dto.Millis(at)
	return &m
}
