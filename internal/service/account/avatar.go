package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// ActionAvatarCleared is the audit row staff removing a member's avatar
// writes (docs/04 "audit_log", TKT-01M3YSQ4CR). A member's own change of
// their avatar writes none, as no edit of their own profile does.
const ActionAvatarCleared = "user.avatar_cleared"

// ClearAvatar is DELETE /admin/users/{handle}/avatar: staff remove the
// avatar of the account handle names (docs/05 "Display name and bio":
// a moderator may clear one). Who may act on whom is mayActOn's rule, as
// for a suspension: a moderator acts on members, and an admin on anyone
// but themselves, who clear their own on /settings/profile. The account is
// read, checked, cleared and audited in one write. An account with no
// avatar is conflict, and an anonymized one not-found, as it has none.
func (s *Service) ClearAvatar(ctx context.Context, v domain.Viewer, ip, handle string) error {
	if err := member.RequireStaff(v, "remove an account's avatar"); err != nil {
		return err
	}
	id, err := s.lookup(ctx, handle)
	if err != nil {
		return err
	}
	allow := mayActOn(v, "remove the avatar of")
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUser(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("account: no account %d", id))
		}
		if err != nil {
			return err
		}
		if err := allow(u); err != nil {
			return err
		}
		if member.Anonymized(u) {
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("account: %d is anonymized", id))
		}
		if !u.AvatarBlob.Valid {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("account: %q has no avatar", u.Handle))
		}
		now := s.now()
		if _, err := q.SetUserAvatar(ctx, dbq.SetUserAvatarParams{UpdatedAt: now.UnixMilli(), ID: id}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip},
			ActionAvatarCleared, audit.User(id), nil)
		return err
	})
}
