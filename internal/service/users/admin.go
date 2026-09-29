package users

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
)

// The admin users screen (TKT-01M29CXK1X, docs/07 "Moderation and admin").
// Moderators and admins may list accounts and read one, since suspension is
// a moderator action reached from here (docs/08 "Primitives"). An account's
// identities and sessions, and its role, are an admin's alone.

// AdminPageSize is how many accounts one page of the list holds.
const AdminPageSize = 50

// AdminFilter is the list's filters, as the query string gives them. An
// empty one does not filter.
type AdminFilter struct {
	Prefix, Role, Status, Kind string
}

// AdminList is GET /admin/users: a page of accounts in handle order,
// narrowed by f, after cursor. Staff only.
//
// Every status is listed, pending and suspended included: that is what the
// screen is for. A filter value that is not one of its column's values is
// validation-failed rather than an empty page, so a typo does not read as
// "nobody".
func (s *Service) AdminList(ctx context.Context, v domain.Viewer, f AdminFilter, cursor string) (dto.AdminUserList, error) {
	out := dto.AdminUserList{Users: []dto.AdminUser{}}
	if err := member.RequireStaff(v, "list accounts"); err != nil {
		return out, err
	}
	if f.Role != "" && !domain.ValidRole(f.Role) {
		return out, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("users: %q is not a role", f.Role))
	}
	switch f.Status {
	case "", domain.StatusActive, domain.StatusPending, domain.StatusSuspended, domain.StatusDeactivated:
	default:
		return out, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("users: %q is not a status", f.Status))
	}
	if f.Kind != "" && !domain.ValidKind(f.Kind) {
		return out, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("users: %q is not a kind", f.Kind))
	}
	after := ""
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(b) == 0 {
			return out, problem.Coded(problem.CodeBadRequest, fmt.Errorf("users: cursor %q is malformed", cursor))
		}
		after = string(b)
	}
	var rows []views.AdminUserRow
	err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = views.AdminUsers(ctx, tx, views.AdminUserFilter{Prefix: f.Prefix, Role: f.Role, Status: f.Status,
			Kind: f.Kind}, after, AdminPageSize+1)
		return err
	})
	if err != nil {
		return out, err
	}
	if len(rows) > AdminPageSize {
		rows = rows[:AdminPageSize]
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(rows[len(rows)-1].Handle))
	}
	for _, r := range rows {
		out.Users = append(out.Users, member.AdminUser(dbq.User{ID: r.ID, Handle: r.Handle, DisplayName: r.DisplayName,
			AvatarBlob: r.Avatar, Role: r.Role, Kind: r.Kind, Status: r.Status, CreatedAt: r.CreatedAt,
			LastSeenAt: r.LastSeenAt, Anonymized: r.Anonymized}))
	}
	return out, nil
}

// AdminDetail is GET /admin/users/{handle}: the account, its groups, and,
// for an admin only, its identities and live sessions. Staff only. A handle
// nobody has is not-found.
func (s *Service) AdminDetail(ctx context.Context, v domain.Viewer, handle string) (dto.AdminUserDetail, error) {
	var out dto.AdminUserDetail
	if err := member.RequireStaff(v, "read an account"); err != nil {
		return out, err
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		return s.adminDetail(ctx, q, handle, v.Role == domain.RoleAdmin, &out)
	})
	return out, err
}

// adminDetail reads AdminDetail's answer into out, in q's transaction; the
// identities and sessions only when admin.
func (s *Service) adminDetail(ctx context.Context, q *dbq.Queries, handle string, admin bool, out *dto.AdminUserDetail) error {
	u, err := q.GetUserByHandle(ctx, handle)
	if errors.Is(err, sql.ErrNoRows) {
		return problem.Coded(problem.CodeNotFound, fmt.Errorf("users: no account has the handle %q", handle))
	}
	if err != nil {
		return err
	}
	*out = dto.AdminUserDetail{User: member.AdminUser(u), Groups: []dto.Membership{}}
	ms, err := q.MembershipsForUser(ctx, u.ID)
	if err != nil {
		return err
	}
	var adders []int64
	for _, m := range ms {
		if m.AddedBy.Valid {
			adders = append(adders, m.AddedBy.Int64)
		}
	}
	authors, err := member.AuthorsByID(ctx, q, adders)
	if err != nil {
		return err
	}
	for _, m := range ms {
		mb := dto.Membership{Group: dto.GroupRef{ID: m.GroupID, Slug: m.Slug, Name: m.Name}, Source: m.Source,
			AddedAt: dto.Millis(m.AddedAt)}
		if m.AddedBy.Valid {
			a := authors[m.AddedBy.Int64]
			mb.AddedBy = &a
		}
		out.Groups = append(out.Groups, mb)
	}
	if !admin {
		return nil
	}
	ids, err := q.IdentitiesForUser(ctx, sql.NullInt64{Int64: u.ID, Valid: true})
	if err != nil {
		return err
	}
	out.Identities = []dto.AdminIdentity{}
	for _, i := range ids {
		ai := dto.AdminIdentity{Provider: i.ProviderKey, EmailVerified: i.EmailVerified != 0}
		if i.Email.Valid {
			ai.Email = new(i.Email.String)
		}
		if i.LastLoginAt.Valid {
			ai.LastLoginAt = new(dto.Millis(i.LastLoginAt.Int64))
		}
		out.Identities = append(out.Identities, ai)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	cut := session.ExpiredBefore(now())
	n, err := q.CountUserSessions(ctx, dbq.CountUserSessionsParams{UserID: sql.NullInt64{Int64: u.ID, Valid: true},
		ExpiresAfter: cut.ExpiresAt, LastUsedAfter: cut.LastUsedAt})
	if err != nil {
		return err
	}
	out.Sessions = new(int(n))
	out.Agent, err = agentInfo(ctx, q, u)
	return err
}

// SetRole is PUT /admin/users/{handle}/role: an admin sets the account's
// role, through perm.ChangeRole, which audits it in the same transaction
// and refuses to demote the last active admin (conflict). ip is the
// request's address, for the audit row. It answers with the account as it
// now stands, read in the same transaction, so an error means the role was
// not changed (Terva review 1423).
func (s *Service) SetRole(ctx context.Context, v domain.Viewer, ip, handle, role string) (dto.AdminUserDetail, error) {
	if err := member.RequireAdmin(v, "change a role"); err != nil {
		return dto.AdminUserDetail{}, err
	}
	var id int64
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(ctx, handle)
		id = u.ID
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return dto.AdminUserDetail{}, problem.Coded(problem.CodeNotFound, fmt.Errorf("users: no account has the handle %q", handle))
	}
	if err != nil {
		return dto.AdminUserDetail{}, err
	}
	var out dto.AdminUserDetail
	if err := s.Perm.ChangeRoleThen(ctx, audit.By{Actor: v.UserID, IP: ip}, id, role, func(q *dbq.Queries) error {
		return s.adminDetail(ctx, q, handle, true, &out)
	}); err != nil {
		return dto.AdminUserDetail{}, err
	}
	return out, nil
}
