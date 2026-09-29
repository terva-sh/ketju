// Package groups is the admin screen's groups and permissions
// (TKT-01M29CXK1X, docs/07 "Moderation and admin"): creating, renaming and
// deleting groups, their members, and the permission grid. Every method is
// an admin's alone, and asks again whatever gate the caller passed.
//
// Writes that change a permission decision, memberships and grants, go
// through internal/perm, which audits them and clears its cache. Creating,
// renaming and deleting a group change no decision, since a group is created
// empty and deleted only when empty, so they are written here, each with its
// audit row in the same transaction.
package groups

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
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Service is the groups service.
type Service struct {
	DB   *db.DB
	Perm *perm.Evaluator
	// Now is the clock for the audit rows; nil is time.Now.
	Now func() time.Time
}

// The audit_log actions this package writes, against the group.
const (
	ActionCreated = "group.created"
	ActionUpdated = "group.updated"
	ActionDeleted = "group.deleted"
)

// Limits with no product meaning beyond keeping a request bounded.
const (
	NameMaxLen        = 80
	DescriptionMaxLen = 500
	// MaxChanges is the most grant changes one batch may carry: more than
	// every cell of a grid of 20 groups by 20 forums for one permission.
	MaxChanges = 500
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func invalid(format string, args ...any) error {
	return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("groups: "+format, args...))
}

// List is GET /admin/groups: every group, with its counts, in id order.
func (s *Service) List(ctx context.Context, v domain.Viewer) (dto.GroupList, error) {
	out := dto.GroupList{Groups: []dto.Group{}}
	if err := member.RequireAdmin(v, "list groups"); err != nil {
		return out, err
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out.Groups, err = summaries(ctx, q)
		return err
	})
	return out, err
}

func summaries(ctx context.Context, q *dbq.Queries) ([]dto.Group, error) {
	rows, err := q.GroupSummaries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]dto.Group, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.Group{ID: r.ID, Slug: r.Slug, Name: r.Name, Description: r.Description,
			System: r.IsSystem != 0, Members: int(r.Members), Grants: int(r.Grants)})
	}
	return out, nil
}

// Detail is GET /admin/groups/{id}: the group and its stored members. The
// implicit groups have none.
func (s *Service) Detail(ctx context.Context, v domain.Viewer, id int64) (dto.GroupDetail, error) {
	if err := member.RequireAdmin(v, "read a group"); err != nil {
		return dto.GroupDetail{}, err
	}
	var out dto.GroupDetail
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out, err = detail(ctx, q, id)
		return err
	})
	return out, err
}

func detail(ctx context.Context, q *dbq.Queries, id int64) (dto.GroupDetail, error) {
	out := dto.GroupDetail{Members: []dto.GroupMember{}}
	all, err := summaries(ctx, q)
	if err != nil {
		return out, err
	}
	found := false
	for _, g := range all {
		if g.ID == id {
			out.Group, found = g, true
		}
	}
	if !found {
		return out, perm.ErrNoGroup
	}
	ms, err := q.GroupMembers(ctx, id)
	if err != nil {
		return out, err
	}
	ids := make([]int64, 0, 2*len(ms))
	for _, m := range ms {
		ids = append(ids, m.UserID)
		if m.AddedBy.Valid {
			ids = append(ids, m.AddedBy.Int64)
		}
	}
	users := map[int64]dbq.User{}
	if len(ids) > 0 {
		us, err := q.UsersByIDs(ctx, ids)
		if err != nil {
			return out, err
		}
		for _, u := range us {
			users[u.ID] = u
		}
	}
	for _, m := range ms {
		gm := dto.GroupMember{User: member.AdminUser(users[m.UserID]), Source: m.Source, AddedAt: dto.Millis(m.AddedAt)}
		if m.AddedBy.Valid {
			a := member.Missing(m.AddedBy.Int64)
			if u, ok := users[m.AddedBy.Int64]; ok {
				a = member.Author(u)
			}
			gm.AddedBy = &a
		}
		out.Members = append(out.Members, gm)
	}
	return out, nil
}

// checkName trims and checks a group's name: 1 to NameMaxLen characters,
// no control characters.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > NameMaxLen {
		return "", invalid("a name has 1 to %d characters, not %d", NameMaxLen, n)
	}
	if !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", invalid("a name has no control characters")
	}
	return name, nil
}

func checkDescription(d string) (string, error) {
	d = strings.TrimSpace(d)
	if n := utf8.RuneCountInString(d); n > DescriptionMaxLen {
		return "", invalid("a description has at most %d characters, not %d", DescriptionMaxLen, n)
	}
	if !utf8.ValidString(d) {
		return "", invalid("a description is UTF-8")
	}
	return d, nil
}

// Create is POST /admin/groups: a new, empty group. The slug is
// domain.ValidateGroupSlug's, and one another group has is conflict.
func (s *Service) Create(ctx context.Context, v domain.Viewer, ip, slug, name, description string) (dto.Group, error) {
	if err := member.RequireAdmin(v, "create a group"); err != nil {
		return dto.Group{}, err
	}
	if err := domain.ValidateGroupSlug(slug); err != nil {
		return dto.Group{}, problem.Coded(problem.CodeValidationFailed, err)
	}
	name, err := checkName(name)
	if err != nil {
		return dto.Group{}, err
	}
	description, err = checkDescription(description)
	if err != nil {
		return dto.Group{}, err
	}
	var out dto.Group
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		if _, err := q.GetGroupBySlug(ctx, slug); err == nil {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("groups: the slug %q is taken", slug))
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		id, err := q.CreateGroup(ctx, dbq.CreateGroupParams{Name: name, Slug: slug, Description: description,
			CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()})
		if err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionCreated, audit.Group(id),
			map[string]any{"slug": slug, "name": name}); err != nil {
			return err
		}
		out = dto.Group{ID: id, Slug: slug, Name: name, Description: description}
		return nil
	})
	return out, err
}

// Update is PATCH /admin/groups/{id}: a new name, description, or both.
// A nil field is left as it is. A system group may be renamed; its slug is
// fixed by trigger, and no request here can change it.
func (s *Service) Update(ctx context.Context, v domain.Viewer, ip string, id int64, name, description *string) (dto.Group, error) {
	if err := member.RequireAdmin(v, "change a group"); err != nil {
		return dto.Group{}, err
	}
	if name == nil && description == nil {
		return dto.Group{}, invalid("the change names neither a name nor a description")
	}
	var err error
	if name != nil {
		n, err := checkName(*name)
		if err != nil {
			return dto.Group{}, err
		}
		name = &n
	}
	if description != nil {
		d, err := checkDescription(*description)
		if err != nil {
			return dto.Group{}, err
		}
		description = &d
	}
	var out dto.Group
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		g, err := q.GetGroup(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return perm.ErrNoGroup
		}
		if err != nil {
			return err
		}
		newName, newDesc := g.Name, g.Description
		if name != nil {
			newName = *name
		}
		if description != nil {
			newDesc = *description
		}
		if _, err := q.UpdateGroup(ctx, dbq.UpdateGroupParams{Name: newName, Description: newDesc,
			UpdatedAt: now.UnixMilli(), ID: id}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionUpdated, audit.Group(id),
			map[string]any{"slug": g.Slug, "name": newName, "from_name": g.Name,
				"description_changed": newDesc != g.Description}); err != nil {
			return err
		}
		all, err := summaries(ctx, q)
		for _, x := range all {
			if x.ID == id {
				out = x
			}
		}
		return err
	})
	return out, err
}

// Delete is DELETE /admin/groups/{id}. Only a group with no stored member
// and no permission row, and not a system group, is deleted (conflict
// otherwise), in one statement, so nothing is deleted by cascade (docs/04).
func (s *Service) Delete(ctx context.Context, v domain.Viewer, ip string, id int64) error {
	if err := member.RequireAdmin(v, "delete a group"); err != nil {
		return err
	}
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		now := s.now()
		g, err := q.GetGroup(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return perm.ErrNoGroup
		}
		if err != nil {
			return err
		}
		n, err := q.DeleteEmptyGroup(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			if g.IsSystem != 0 {
				return problem.Coded(problem.CodeConflict, fmt.Errorf("groups: %s is a system group", g.Slug))
			}
			return problem.Coded(problem.CodeConflict, fmt.Errorf("groups: %s still has members or permission rows", g.Slug))
		}
		_, err = audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionDeleted, audit.Group(id),
			map[string]any{"slug": g.Slug, "name": g.Name})
		return err
	})
}

// userID is the id of the account with handle, or not-found.
func (s *Service) userID(ctx context.Context, handle string) (dbq.User, error) {
	var u dbq.User
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		u, err = q.GetUserByHandle(ctx, handle)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return u, problem.Coded(problem.CodeNotFound, fmt.Errorf("groups: no account has the handle %q", handle))
	}
	return u, err
}

// AddMember is POST /admin/groups/{id}/members: the account joins the
// group on the admin's authority, through perm.AddMember, which audits it.
// A membership the identity provider's mapping made is conflict and left as
// it is. It answers with the group as it now stands.
func (s *Service) AddMember(ctx context.Context, v domain.Viewer, ip string, id int64, handle string) (dto.GroupDetail, error) {
	return s.membership(ctx, v, ip, id, handle, s.Perm.AddMember)
}

// RemoveMember is DELETE /admin/groups/{id}/members/{handle}, as AddMember.
func (s *Service) RemoveMember(ctx context.Context, v domain.Viewer, ip string, id int64, handle string) (dto.GroupDetail, error) {
	return s.membership(ctx, v, ip, id, handle, s.Perm.RemoveMember)
}

func (s *Service) membership(ctx context.Context, v domain.Viewer, ip string, id int64, handle string,
	do func(context.Context, audit.By, int64, int64) error) (dto.GroupDetail, error) {
	if err := member.RequireAdmin(v, "change a group's members"); err != nil {
		return dto.GroupDetail{}, err
	}
	u, err := s.userID(ctx, handle)
	if err != nil {
		return dto.GroupDetail{}, err
	}
	if err := do(ctx, audit.By{Actor: v.UserID, IP: ip}, id, u.ID); err != nil {
		return dto.GroupDetail{}, err
	}
	return s.Detail(ctx, v, id)
}

// Change is one staged grid change as the API takes it: grant or revoke
// Permission to GroupID in ForumID, or site-wide when ForumID is nil.
type Change struct {
	ForumID    *int64 `json:"forum_id"`
	GroupID    int64  `json:"group_id"`
	Permission string `json:"permission"`
	Grant      bool   `json:"grant"`
}

func toPerm(changes []Change) ([]perm.GrantChange, error) {
	if len(changes) > MaxChanges {
		return nil, invalid("a batch has at most %d changes, not %d", MaxChanges, len(changes))
	}
	out := make([]perm.GrantChange, len(changes))
	for i, c := range changes {
		gc := perm.GrantChange{GroupID: c.GroupID, Permission: domain.Permission(c.Permission), Grant: c.Grant}
		if c.ForumID != nil {
			if *c.ForumID <= 0 {
				return nil, invalid("change %d: forum_id %d names no forum; null is site-wide", i+1, *c.ForumID)
			}
			gc.ForumID = *c.ForumID
		}
		out[i] = gc
	}
	return out, nil
}

// Grid is GET /admin/permissions: the groups, the forums, and every row.
func (s *Service) Grid(ctx context.Context, v domain.Viewer) (dto.PermissionGrid, error) {
	if err := member.RequireAdmin(v, "read the permission grid"); err != nil {
		return dto.PermissionGrid{}, err
	}
	var out dto.PermissionGrid
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		out, err = grid(ctx, q)
		return err
	})
	return out, err
}

func grid(ctx context.Context, q *dbq.Queries) (dto.PermissionGrid, error) {
	out := dto.PermissionGrid{Forums: []dto.AdminForum{}, Grants: []dto.Grant{}}
	var err error
	if out.Groups, err = summaries(ctx, q); err != nil {
		return out, err
	}
	forums, err := q.ListForums(ctx)
	if err != nil {
		return out, err
	}
	for _, f := range forums {
		out.Forums = append(out.Forums, adminForum(f))
	}
	rows, err := q.ListPermissions(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		g := dto.Grant{GroupID: r.GroupID, Permission: r.Permission}
		if r.ForumID.Valid {
			g.ForumID = new(r.ForumID.Int64)
		}
		out.Grants = append(out.Grants, g)
	}
	return out, nil
}

func adminForum(f dbq.Forum) dto.AdminForum {
	return dto.AdminForum{ID: f.ID, Slug: f.Slug, Name: f.Name, Visibility: f.Visibility}
}

// Apply is POST /admin/permissions: a batch of changes in one transaction,
// through perm.ApplyGrants, which audits each. It answers with the grid as
// it now stands, read in the same transaction, so an error means the batch
// was not made: a batch that committed is never reported as failed, where
// retrying it would be refused as conflict (Terva review 1416).
func (s *Service) Apply(ctx context.Context, v domain.Viewer, ip string, changes []Change) (dto.PermissionGrid, error) {
	if err := member.RequireAdmin(v, "change permissions"); err != nil {
		return dto.PermissionGrid{}, err
	}
	pc, err := toPerm(changes)
	if err != nil {
		return dto.PermissionGrid{}, err
	}
	if len(pc) == 0 {
		return dto.PermissionGrid{}, invalid("the batch has no changes")
	}
	var out dto.PermissionGrid
	err = s.Perm.ApplyGrantsThen(ctx, audit.By{Actor: v.UserID, IP: ip}, pc, func(q *dbq.Queries) error {
		var err error
		out, err = grid(ctx, q)
		return err
	})
	if err != nil {
		return dto.PermissionGrid{}, err
	}
	return out, nil
}

// Preview is POST /admin/permissions/preview: what the account with handle
// could do in every forum now, and with changes applied (perm.Preview). It
// writes nothing.
func (s *Service) Preview(ctx context.Context, v domain.Viewer, handle string, changes []Change) (dto.PermissionPreview, error) {
	if err := member.RequireAdmin(v, "preview permissions"); err != nil {
		return dto.PermissionPreview{}, err
	}
	pc, err := toPerm(changes)
	if err != nil {
		return dto.PermissionPreview{}, err
	}
	u, err := s.userID(ctx, handle)
	if err != nil {
		return dto.PermissionPreview{}, err
	}
	effects, err := s.Perm.Preview(ctx, u.ID, pc)
	if err != nil {
		return dto.PermissionPreview{}, err
	}
	forums := map[int64]dto.AdminForum{}
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		fs, err := q.ListForums(ctx)
		for _, f := range fs {
			forums[f.ID] = adminForum(f)
		}
		return err
	})
	if err != nil {
		return dto.PermissionPreview{}, err
	}
	out := dto.PermissionPreview{User: member.AdminUser(u), Forums: []dto.ForumEffect{}}
	for _, fe := range effects {
		f, ok := forums[fe.ForumID]
		if !ok {
			continue // created and deleted between the two reads: nothing to show
		}
		out.Forums = append(out.Forums, dto.ForumEffect{Forum: f, Before: decisions(fe.Before), After: decisions(fe.After)})
	}
	return out, nil
}

func decisions(m map[domain.Permission]perm.Decision) dto.Decisions {
	return dto.Decisions{
		Read:         m[domain.PermRead].String(),
		CreateThread: m[domain.PermCreateThread].String(),
		Reply:        m[domain.PermReply].String(),
		Upload:       m[domain.PermUpload].String(),
		Moderate:     m[domain.PermModerate].String(),
	}
}
