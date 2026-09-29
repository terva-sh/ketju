package perm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The writes the admin users, groups and permissions screens make
// (TKT-01M29CXK1X). Each writes its audit_log row through internal/audit in
// the transaction that makes the change, so a failed audit insert leaves the
// change unmade, and each clears the cache once it has committed (write).

// The audit_log actions a role change writes, against the user. A change to
// admin writes ActionPromoted, through promote.
const (
	// ActionDemoted is a change from admin to another role. It is its own
	// action, and leaves the user's ActionPromoted rows alone, because
	// bootstrap reads those as "made admin once" and a demoted bootstrap
	// admin must stay demoted (docs/05 "Principles").
	ActionDemoted = "user.demoted"
	// ActionRoleChanged is a change between member and moderator.
	ActionRoleChanged = "user.role_changed"
)

// The audit_log actions ApplyGrants writes, one per change, against the
// permission (audit.Permission).
const (
	ActionPermissionGranted = "permission.granted"
	ActionPermissionRevoked = "permission.revoked"
)

var (
	// ErrInvalidRole is ChangeRole's answer for a role that is not one of
	// the three.
	ErrInvalidRole = errors.New("perm: not a role")
	// ErrLastAdmin is ChangeRole's answer for a change that would leave no
	// active admin. The screen must not be how a forum loses its only admin
	// (settled in grooming for TKT-01M29CXK1X).
	ErrLastAdmin = errors.New("perm: the last active admin cannot be demoted")
	// ErrNoGroup is the answer for a group id that names no group.
	ErrNoGroup = errors.New("perm: no such group")
	// ErrImplicitGroup is the answer for a membership of everyone or
	// members, which are implicit and never stored (docs/04).
	ErrImplicitGroup = errors.New("perm: membership of this group is implicit")
	// ErrIdPMembership is the answer for adding over, or removing, a
	// membership an IdP mapping made. The next sign-in would undo the edit,
	// so the screen refuses it rather than pretend (docs/05 "Roles and
	// groups").
	ErrIdPMembership = errors.New("perm: the membership comes from the identity provider")
	// ErrInvalidChange is the answer for a batch of grant changes with one
	// that names nothing, or names the same row twice.
	ErrInvalidChange = errors.New("perm: invalid permission change")
	// ErrGrantState is the answer for a batch with a grant of a row that
	// exists or a revoke of one that does not: the batch was staged against
	// rows that have changed since.
	ErrGrantState = errors.New("perm: the permission rows changed since the batch was staged")
)

// ChangeRole sets userID's role to role on by's authority, and records it
// in audit_log in the same transaction: ActionPromoted to admin (through
// promote, as bootstrap and the command line do), ActionDemoted from admin,
// ActionRoleChanged otherwise. A change to the role the user already has
// writes nothing.
//
// It refuses to demote the last active admin (ErrLastAdmin), counted inside
// the transaction, so two admins demoting each other at once cannot both
// succeed. A demotion leaves the user's ActionPromoted rows alone.
//
// The change applies from the user's next request (see the package
// comment): the Viewer is built from the users row on every request, and
// the role is part of the cache key, so the new Viewer never meets a
// decision cached for the old one. A request already holding the old Viewer
// finishes under the old role.
func (e *Evaluator) ChangeRole(ctx context.Context, by audit.By, userID int64, role string) error {
	return e.ChangeRoleThen(ctx, by, userID, role, nil)
}

// writeThen is write, then read in the same transaction, with the change
// made. A caller that answers with what it changed reads it here, so a
// failed read rolls the change back rather than reporting a failure for a
// change that committed (Terva reviews 1416 and 1423): the answer describes
// the committed rows, or nothing was changed. read must only read, and may
// be nil.
func (e *Evaluator) writeThen(ctx context.Context, fn func(q *dbq.Queries, now int64) error,
	read func(q *dbq.Queries) error) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		if err := fn(q, now); err != nil {
			return err
		}
		if read != nil {
			return read(q)
		}
		return nil
	})
}

// ChangeRoleThen is ChangeRole, then read in the same transaction
// (writeThen). read runs after a change that was a no-op too.
func (e *Evaluator) ChangeRoleThen(ctx context.Context, by audit.By, userID int64, role string,
	read func(q *dbq.Queries) error) error {
	if !domain.ValidRole(role) {
		return fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	return e.writeThen(ctx, func(q *dbq.Queries, now int64) error {
		u, err := q.GetUser(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		if u.Role == role {
			return nil
		}
		if role == domain.RoleAdmin {
			_, err := promote(ctx, q, now, u, Promotion{Actor: by.Actor, IP: by.IP,
				Details: map[string]string{"from": u.Role}})
			return err
		}
		action := ActionRoleChanged
		if u.Role == domain.RoleAdmin {
			action = ActionDemoted
			if u.Status == domain.StatusActive {
				n, err := q.CountActiveAdmins(ctx)
				if err != nil {
					return err
				}
				if n <= 1 {
					return ErrLastAdmin
				}
			}
		}
		if err := q.SetUserRole(ctx, dbq.SetUserRoleParams{Role: role, UpdatedAt: now, ID: userID}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, time.UnixMilli(now), by, action, audit.User(userID),
			map[string]any{"from": u.Role, "to": role})
		return err
	}, read)
}

// GrantChange is one staged change to the permission grid: grant or revoke
// Permission to GroupID in ForumID, or globally when ForumID is 0. Forum id
// 0 is reserved by the schema, so it can mean "global" without ambiguity.
type GrantChange struct {
	ForumID    int64
	GroupID    int64
	Permission domain.Permission
	Grant      bool
}

// Grant gives group p in forumID, or globally when forumID is 0: a batch of
// one (ApplyGrants).
func (e *Evaluator) Grant(ctx context.Context, by audit.By, forumID, groupID int64, p domain.Permission) error {
	return e.ApplyGrants(ctx, by, []GrantChange{{ForumID: forumID, GroupID: groupID, Permission: p, Grant: true}})
}

// Revoke removes a grant made by Grant with the same arguments: a batch of
// one (ApplyGrants).
func (e *Evaluator) Revoke(ctx context.Context, by audit.By, forumID, groupID int64, p domain.Permission) error {
	return e.ApplyGrants(ctx, by, []GrantChange{{ForumID: forumID, GroupID: groupID, Permission: p}})
}

// ApplyGrants applies a batch of changes in one transaction, with one audit
// row per change, or none of them. A half-applied grid is how an admin
// locks everyone out of a forum (settled in grooming).
//
// Every change must name an existing forum (or 0) and group and one of the
// five permissions, and no two may name the same row (ErrInvalidChange).
// Each must also agree with the rows as they are: a grant of a row that is
// there, or a revoke of one that is not, is ErrGrantState, because the
// batch was staged against a grid someone has changed since.
func (e *Evaluator) ApplyGrants(ctx context.Context, by audit.By, changes []GrantChange) error {
	return e.ApplyGrantsThen(ctx, by, changes, nil)
}

// ApplyGrantsThen is ApplyGrants, then read in the same transaction, with
// the batch applied (writeThen). A caller that answers with the grid as it
// now stands reads it here.
func (e *Evaluator) ApplyGrantsThen(ctx context.Context, by audit.By, changes []GrantChange,
	read func(q *dbq.Queries) error) error {
	if len(changes) == 0 {
		return nil
	}
	return e.writeThen(ctx, func(q *dbq.Queries, now int64) error {
		return e.applyGrants(ctx, q, now, by, changes)
	}, read)
}

func (e *Evaluator) applyGrants(ctx context.Context, q *dbq.Queries, now int64, by audit.By, changes []GrantChange) error {
	g, err := readGrid(ctx, q)
	if err != nil {
		return err
	}
	if err := g.check(changes); err != nil {
		return err
	}
	for _, c := range changes {
		forum := sql.NullInt64{Int64: c.ForumID, Valid: c.ForumID != 0}
		action := ActionPermissionRevoked
		if c.Grant {
			action = ActionPermissionGranted
			err = q.GrantPermission(ctx, dbq.GrantPermissionParams{
				ForumID: forum, GroupID: c.GroupID, Permission: string(c.Permission), CreatedAt: now,
			})
		} else {
			err = q.RevokePermission(ctx, dbq.RevokePermissionParams{
				ForumID: forum, GroupID: c.GroupID, Permission: string(c.Permission),
			})
		}
		if err != nil {
			return err
		}
		// A site-wide row has no forum: its slug and id are null in the
		// details, as forum_id is in the row (Terva review 1428). 0 is only
		// this package's key for it.
		var forumSlug, forumID any
		if c.ForumID != 0 {
			forumSlug, forumID = g.forums[c.ForumID], c.ForumID
		}
		if _, err := audit.Append(ctx, q, time.UnixMilli(now), by, action,
			audit.Permission(c.ForumID, c.GroupID, string(c.Permission)),
			map[string]any{"group": g.groups[c.GroupID].Slug, "group_id": c.GroupID, "forum": forumSlug,
				"forum_id": forumID, "permission": string(c.Permission)}); err != nil {
			return err
		}
	}
	return nil
}

// grid is every forum, group and permission row, read in one transaction,
// for checking a batch against.
type grid struct {
	forums map[int64]string // id -> slug
	groups map[int64]dbq.Group
	rows   map[rowKey]bool
}

type rowKey struct {
	forum, group int64
	p            domain.Permission
}

func readGrid(ctx context.Context, q *dbq.Queries) (grid, error) {
	g := grid{forums: map[int64]string{}, groups: map[int64]dbq.Group{}, rows: map[rowKey]bool{}}
	forums, err := q.ListForums(ctx)
	if err != nil {
		return g, err
	}
	for _, f := range forums {
		g.forums[f.ID] = f.Slug
	}
	groups, err := q.ListGroups(ctx)
	if err != nil {
		return g, err
	}
	for _, gr := range groups {
		g.groups[gr.ID] = gr
	}
	rows, err := q.ListPermissions(ctx)
	if err != nil {
		return g, err
	}
	for _, r := range rows {
		g.rows[rowKey{r.ForumID.Int64, r.GroupID, domain.Permission(r.Permission)}] = true
	}
	return g, nil
}

// check is ApplyGrants' rule for a batch, which Preview applies too, so a
// preview refuses exactly what applying would.
func (g grid) check(changes []GrantChange) error {
	seen := map[rowKey]bool{}
	for i, c := range changes {
		if !domain.ValidPermission(c.Permission) {
			return fmt.Errorf("%w: change %d: %q is not a permission", ErrInvalidChange, i+1, c.Permission)
		}
		if _, ok := g.forums[c.ForumID]; c.ForumID != 0 && !ok {
			return fmt.Errorf("%w: change %d: no forum %d", ErrInvalidChange, i+1, c.ForumID)
		}
		if _, ok := g.groups[c.GroupID]; !ok {
			return fmt.Errorf("%w: change %d: no group %d", ErrInvalidChange, i+1, c.GroupID)
		}
		k := rowKey{c.ForumID, c.GroupID, c.Permission}
		if seen[k] {
			return fmt.Errorf("%w: change %d names a row an earlier change names", ErrInvalidChange, i+1)
		}
		seen[k] = true
		if g.rows[k] == c.Grant {
			return fmt.Errorf("%w: change %d", ErrGrantState, i+1)
		}
	}
	return nil
}

// AddMember adds userID to groupID on by's authority, and records
// ActionMemberAdded against the user in the same transaction. A membership
// the user already holds on an admin's authority is left as it is, and
// nothing is recorded. One an IdP mapping made is refused with
// ErrIdPMembership and left as it is: taking it over would outlive the
// directory's decision, and the screen does not edit what the next sign-in
// would undo.
func (e *Evaluator) AddMember(ctx context.Context, by audit.By, groupID, userID int64) error {
	return e.membership(ctx, by, groupID, userID, true, nil)
}

// AddMemberThen is AddMember, then read in the same transaction
// (writeThen). read runs after a change that was a no-op too.
func (e *Evaluator) AddMemberThen(ctx context.Context, by audit.By, groupID, userID int64,
	read func(q *dbq.Queries) error) error {
	return e.membership(ctx, by, groupID, userID, true, read)
}

// RemoveMemberThen is RemoveMember, as AddMemberThen.
func (e *Evaluator) RemoveMemberThen(ctx context.Context, by audit.By, groupID, userID int64,
	read func(q *dbq.Queries) error) error {
	return e.membership(ctx, by, groupID, userID, false, read)
}

// RemoveMember removes userID from groupID on by's authority, and records
// ActionMemberRemoved against the user in the same transaction. A user who
// is not in the group is left alone, and nothing is recorded. An IdP
// membership is refused with ErrIdPMembership and left as it is.
func (e *Evaluator) RemoveMember(ctx context.Context, by audit.By, groupID, userID int64) error {
	return e.membership(ctx, by, groupID, userID, false, nil)
}

func (e *Evaluator) membership(ctx context.Context, by audit.By, groupID, userID int64, add bool,
	read func(q *dbq.Queries) error) error {
	return e.writeThen(ctx, func(q *dbq.Queries, now int64) error {
		g, err := q.GetGroup(ctx, groupID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoGroup
		}
		if err != nil {
			return err
		}
		if g.Slug == domain.GroupEveryone || g.Slug == domain.GroupMembers {
			return fmt.Errorf("%w: %s", ErrImplicitGroup, g.Slug)
		}
		if _, err := q.GetUser(ctx, userID); errors.Is(err, sql.ErrNoRows) {
			return ErrNoUser
		} else if err != nil {
			return err
		}
		source, err := q.GetMembership(ctx, dbq.GetMembershipParams{GroupID: groupID, UserID: userID})
		held := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if held && source == domain.MembershipIdP {
			return ErrIdPMembership
		}
		if held == add {
			return nil
		}
		action := ActionMemberRemoved
		if add {
			action = ActionMemberAdded
			err = q.AddAdminMembership(ctx, dbq.AddAdminMembershipParams{GroupID: groupID, UserID: userID,
				AddedBy: sql.NullInt64{Int64: by.Actor, Valid: by.Actor != 0}, AddedAt: now})
		} else {
			err = q.RemoveMembership(ctx, dbq.RemoveMembershipParams{GroupID: groupID, UserID: userID})
		}
		if err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, time.UnixMilli(now), by, action, audit.User(userID),
			map[string]any{"group": g.Slug, "group_id": groupID, "source": domain.MembershipAdmin})
		return err
	}, read)
}

// ForumEffect is one forum's decisions for a member, before and after a
// batch of staged changes.
type ForumEffect struct {
	ForumID       int64
	Before, After map[domain.Permission]Decision
}

// Preview is what userID could do in every forum, for each of the five
// permissions, now and with changes applied (settled in grooming: the
// effect of a change is shown before it is made). It lays changes over the
// rows in memory and decides both with the rules Check uses (decide), read
// in one transaction and without the cache, and writes nothing. A batch
// ApplyGrants would refuse, Preview refuses with the same error.
//
// The user is evaluated as Check would evaluate their next request: by
// their role and status as stored, so a user who is not active is previewed
// as anonymous. The forums come in ListForums order.
func (e *Evaluator) Preview(ctx context.Context, userID int64, changes []GrantChange) ([]ForumEffect, error) {
	return e.PreviewThen(ctx, userID, changes, nil)
}

// PreviewThen is Preview, then read in the same transaction, so what a
// caller reads to describe the preview, the user's role and status and the
// forums, is what the decisions were made from (Terva review 1424). read
// may be nil.
func (e *Evaluator) PreviewThen(ctx context.Context, userID int64, changes []GrantChange,
	read func(q *dbq.Queries) error) ([]ForumEffect, error) {
	var out []ForumEffect
	err := e.db.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := e.preview(ctx, q, userID, changes, &out); err != nil {
			return err
		}
		if read != nil {
			return read(q)
		}
		return nil
	})
	return out, err
}

// preview is Preview's body, in q's transaction, into out.
func (e *Evaluator) preview(ctx context.Context, q *dbq.Queries, userID int64, changes []GrantChange,
	out *[]ForumEffect) error {
	*out = nil
	u, err := q.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoUser
	}
	if err != nil {
		return err
	}
	v := domain.Viewer{UserID: u.ID, Role: u.Role, Active: u.Status == domain.StatusActive}
	if !v.Member() {
		v = domain.Anonymous()
	}
	g, err := readGrid(ctx, q)
	if err != nil {
		return err
	}
	if err := g.check(changes); err != nil {
		return err
	}
	forums, err := q.ListForums(ctx)
	if err != nil {
		return err
	}
	ids := make([]int64, len(forums))
	for i, f := range forums {
		ids[i] = f.ID
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.ForumAccessRows(ctx, accessParams(v, ids))
	if err != nil {
		return err
	}
	// The groups v is in, as ForumAccessRows counts them.
	in := map[int64]bool{}
	var everyone int64
	for id, gr := range g.groups {
		switch {
		case gr.Slug == domain.GroupEveryone:
			in[id], everyone = true, id
		case gr.Slug == domain.GroupMembers && v.Member():
			in[id] = true
		}
	}
	if v.Member() {
		mine, err := q.GroupsForUser(ctx, v.UserID)
		if err != nil {
			return err
		}
		for _, m := range mine {
			in[m.GroupID] = true
		}
	}
	before, _ := collect(rows)
	after, _ := collect(rows)
	for _, f := range after {
		f.everyone = everyone
	}
	for _, c := range changes {
		if !in[c.GroupID] {
			continue
		}
		for id, f := range after {
			switch {
			case c.ForumID == 0:
				f.global = overlay(f.global, c.GroupID, c.Permission, c.Grant)
			case c.ForumID == id:
				f.local = overlay(f.local, c.GroupID, c.Permission, c.Grant)
			}
		}
	}
	for _, id := range ids {
		b, err := decide(id, before[id], v)
		if err != nil {
			return err
		}
		a, err := decide(id, after[id], v)
		if err != nil {
			return err
		}
		fe := ForumEffect{ForumID: id, Before: map[domain.Permission]Decision{}, After: map[domain.Permission]Decision{}}
		for _, p := range domain.AllPermissions {
			fe.Before[p], fe.After[p] = decision(v, b, p), decision(v, a, p)
		}
		*out = append(*out, fe)
	}
	return nil
}

// overlay is m with group's grant of p added or removed. A group left with
// no permission loses its key, as a group with no rows has none in collect,
// so the per-group fallback reads it the same way.
func overlay(m map[int64][]domain.Permission, group int64, p domain.Permission, grant bool) map[int64][]domain.Permission {
	var kept []domain.Permission
	for _, q := range m[group] {
		if q != p {
			kept = append(kept, q)
		}
	}
	if grant {
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		delete(m, group)
	} else {
		m[group] = kept
	}
	return m
}
