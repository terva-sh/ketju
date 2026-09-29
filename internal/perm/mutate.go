package perm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
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

// The audit_log actions SyncIdPGroups writes, one row per membership it
// adds or removes (docs/05 "Roles and groups").
const (
	ActionMemberAdded   = "group.member_added"
	ActionMemberRemoved = "group.member_removed"
)

// ViaGroupMap is the "via" of SyncIdPGroups' audit rows: no account made
// the change, the provider's group_map did.
const ViaGroupMap = "group_map"

// ErrNotActive is SyncIdPGroups' answer for a user who is not active. An
// IdP mapping applies only to an active user who has just signed in.
var ErrNotActive = errors.New("perm: the user is not active")

// IdPSync is one sign-in's view of a user's IdP-mapped groups.
type IdPSync struct {
	// Provider is the config key of the provider the user signed in with.
	Provider string
	// IP is the sign-in request's address, for the audit rows.
	IP string
	// Slugs are the local groups the provider's group_map yields from this
	// sign-in's token. Empty means none, and removes every idp membership.
	Slugs []string
}

// Changes is what SyncIdPGroups did, by group slug.
type Changes struct {
	Added, Removed []string
	// Unknown are slugs no group has. They were skipped.
	Unknown []string
}

// SyncIdPGroups makes userID's IdP-sourced memberships exactly the groups
// s.Slugs names, as every sign-in does (TKT-01M29CKRF9, docs/05 "Roles and
// groups"). A membership an admin added is never changed or removed, and a
// slug naming the implicit everyone or members group, or no group at all,
// is skipped: a claim never creates a group.
//
// Each membership it adds or removes writes one audit row, in the same
// transaction, so the change and its record commit together or not at all.
// A sync that changes nothing writes nothing. A user who is not active gets
// ErrNotActive and no change.
func (e *Evaluator) SyncIdPGroups(ctx context.Context, userID int64, s IdPSync) (Changes, error) {
	var ch Changes
	err := e.write(ctx, func(q *dbq.Queries, now int64) error {
		ch = Changes{}
		u, err := q.GetUser(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		if u.Status != domain.StatusActive {
			return fmt.Errorf("%w (%s)", ErrNotActive, u.Status)
		}

		want := map[int64]string{}
		seen := map[string]bool{}
		for _, slug := range s.Slugs {
			if seen[slug] || slug == domain.GroupEveryone || slug == domain.GroupMembers {
				continue
			}
			seen[slug] = true
			g, err := q.GetGroupBySlug(ctx, slug)
			if errors.Is(err, sql.ErrNoRows) {
				ch.Unknown = append(ch.Unknown, slug)
				continue
			}
			if err != nil {
				return err
			}
			want[g.ID] = g.Slug
		}
		have, err := q.IdPMembershipsForUser(ctx, userID)
		if err != nil {
			return err
		}

		record := func(action string, groupID int64, slug string) error {
			_, err := audit.Append(ctx, q, time.UnixMilli(now), audit.By{IP: s.IP, Via: ViaGroupMap}, action,
				audit.User(userID), map[string]any{"group": slug, "group_id": groupID, "source": domain.MembershipIdP,
					"provider": s.Provider})
			return err
		}
		held := make(map[int64]bool, len(have))
		for _, m := range have {
			held[m.GroupID] = true
			if _, ok := want[m.GroupID]; ok {
				continue
			}
			n, err := q.RemoveIdPMembership(ctx, dbq.RemoveIdPMembershipParams{GroupID: m.GroupID, UserID: userID})
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			if err := record(ActionMemberRemoved, m.GroupID, m.Slug); err != nil {
				return err
			}
			ch.Removed = append(ch.Removed, m.Slug)
		}
		for _, id := range slices.Sorted(maps.Keys(want)) {
			if held[id] {
				continue
			}
			n, err := q.AddIdPMembership(ctx, dbq.AddIdPMembershipParams{GroupID: id, UserID: userID, AddedAt: now})
			if err != nil {
				return err
			}
			// 0: the user is in the group already on an admin's
			// authority, which the mapping neither changes nor records.
			if n == 0 {
				continue
			}
			if err := record(ActionMemberAdded, id, want[id]); err != nil {
				return err
			}
			ch.Added = append(ch.Added, want[id])
		}
		return nil
	})
	if err != nil {
		return Changes{}, err
	}
	return ch, nil
}

// SetVisibility changes a forum's visibility.
func (e *Evaluator) SetVisibility(ctx context.Context, forumID int64, visibility string) error {
	return e.write(ctx, func(q *dbq.Queries, now int64) error {
		return q.SetForumVisibility(ctx, dbq.SetForumVisibilityParams{Visibility: visibility, UpdatedAt: now, ID: forumID})
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
	// IP is the request's address when an admin promoted them from the
	// screen, for the audit row; empty otherwise.
	IP string
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
	promoted := false
	err := e.write(ctx, func(q *dbq.Queries, now int64) error {
		u, err := q.GetUser(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		promoted, err = promote(ctx, q, now, u, p)
		return err
	})
	if err != nil {
		return false, err
	}
	return promoted, nil
}

// promote is Promote inside the caller's transaction, for u as read in it.
// ChangeRole calls it too, so a promotion from the admin screen is the same
// write with the same audit row as one from bootstrap or the command line.
// Only promote and ChangeRole call SetUserRole (TestOnlyRoleWritesSetTheRole).
func promote(ctx context.Context, q *dbq.Queries, now int64, u dbq.User, p Promotion) (bool, error) {
	details := map[string]any{}
	for k, v := range p.Details {
		details[k] = v
	}
	details["role"] = domain.RoleAdmin
	target := audit.User(u.ID)
	record := func(details map[string]any) error {
		_, err := audit.Append(ctx, q, time.UnixMilli(now), audit.By{Actor: p.Actor, IP: p.IP}, ActionPromoted, target, details)
		return err
	}
	if p.Once {
		seen, err := q.AuditExists(ctx, dbq.AuditExistsParams{TargetType: target.Type, TargetID: target.ID, Action: ActionPromoted})
		if err != nil || seen {
			return false, err
		}
		// Already an admin with no promotion on record (created as one):
		// the role is left alone, but the promotion this would have been is
		// recorded, so that once demoted they are not made admin again
		// (review 1128).
		if u.Role == domain.RoleAdmin {
			already := maps.Clone(details)
			already["already"] = domain.RoleAdmin
			return false, record(already)
		}
	}
	if u.Role == domain.RoleAdmin {
		return false, nil
	}
	if err := q.SetUserRole(ctx, dbq.SetUserRoleParams{Role: domain.RoleAdmin, UpdatedAt: now, ID: u.ID}); err != nil {
		return false, err
	}
	if err := record(details); err != nil {
		return false, err
	}
	return true, nil
}
