// Package perm decides what a viewer may do in a forum. Every read and every
// write in ketju passes through it, which makes it both the hottest path and
// the one where a bug is a security bug (TKT-01M29C5Y2E).
//
// The rules, from docs/04 "permissions" and docs/05 "Roles and groups":
//
//   - An active admin passes every check, without rows and without touching
//     the database. This is "an admin is implicitly a member of every group",
//     implemented once, here.
//   - A viewer's groups are everyone (always), members (when signed in and
//     active), and their stored group_members rows. A signed-in viewer who is
//     not active counts as anonymous.
//   - Visibility gates first. On a public forum every group counts. On a
//     members forum the everyone group does not count, so an anonymous viewer
//     gets nothing. On a restricted forum only per-forum rows count; global
//     rows are ignored.
//   - Fallback is per group: a group with any row for the forum uses those
//     rows, and otherwise its global rows. The viewer's permissions are the
//     union over their groups.
//   - An active moderator (the role) has moderate everywhere they can see.
//     The moderators group is separate and grants per forum. Neither implies
//     the other.
//   - A restricted forum the viewer cannot read is Hidden, for every
//     permission, and so is a forum that does not exist: the caller answers
//     404 and the forum's existence does not leak. Anything else the viewer
//     lacks is Deny, a 403.
//
// Decisions are cached per session for 60 seconds (docs/02). Every write that
// could change a decision goes through this package's mutation methods, which
// clear the cache when they commit.
package perm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Decision is the outcome of a check.
type Decision int

const (
	// Deny means the viewer can see the forum but may not do this: 403.
	Deny Decision = iota
	// Allow means the viewer may do this.
	Allow
	// Hidden means the viewer may not know the forum exists: 404.
	Hidden
)

func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Hidden:
		return "hidden"
	default:
		return "deny"
	}
}

// TTL is how long a cached decision lives when nothing invalidates it.
// docs/02 sets it at 60 seconds, and the admin screens must say that a change
// made outside this package can take that long to apply.
const TTL = 60 * time.Second

// Evaluator answers permission checks. It is safe for concurrent use.
type Evaluator struct {
	db  *db.DB
	now func() time.Time

	// gen is bumped by Invalidate. An entry filled under an older
	// generation is stale, whatever its expiry.
	gen atomic.Uint64

	mu      sync.Mutex
	entries map[cacheKey]*entry

	// The ids of the implicit groups, found by slug on first use. A failed
	// lookup is retried on the next check rather than remembered.
	implicitMu sync.Mutex
	implicitOK bool
	everyoneID int64
	membersID  int64
}

// New returns an Evaluator over d.
func New(d *db.DB) *Evaluator {
	return &Evaluator{db: d, now: time.Now, entries: make(map[cacheKey]*entry)}
}

// cacheKey carries everything about the viewer that evaluation reads, so a
// role or status change produces a new key rather than a stale hit.
type cacheKey struct {
	session string
	user    int64
	role    string
	active  bool
}

type entry struct {
	gen     uint64
	expires time.Time
	forums  map[int64]access
}

// access is the evaluated state of one forum for one viewer.
type access struct {
	hidden bool
	perms  map[domain.Permission]bool
	// missing marks a forum that does not exist. It is Hidden, and it is
	// never cached: creating a forum invalidates nothing, so a cached
	// "missing" would hide a new forum from whoever probed for it first.
	missing bool
}

// Check decides whether v may do p in the forum forumID.
func (e *Evaluator) Check(ctx context.Context, v domain.Viewer, forumID int64, p domain.Permission) (Decision, error) {
	if !v.Member() {
		v = domain.Anonymous()
	}
	if v.Role == domain.RoleAdmin {
		return Allow, nil
	}
	a, err := e.access(ctx, v, forumID)
	if err != nil {
		return Deny, err
	}
	switch {
	case a.hidden:
		return Hidden, nil
	case a.perms[p]:
		return Allow, nil
	default:
		return Deny, nil
	}
}

// Can reports whether Check would Allow.
func (e *Evaluator) Can(ctx context.Context, v domain.Viewer, forumID int64, p domain.Permission) (bool, error) {
	d, err := e.Check(ctx, v, forumID, p)
	return d == Allow, err
}

// ReadableForums lists the forums v may read, in ListForums order.
func (e *Evaluator) ReadableForums(ctx context.Context, v domain.Viewer) ([]int64, error) {
	var forums []dbq.Forum
	err := e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		forums, err = q.ListForums(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	var out []int64
	for _, f := range forums {
		ok, err := e.Can(ctx, v, f.ID, domain.PermRead)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, f.ID)
		}
	}
	return out, nil
}

// Invalidate drops every cached decision. The mutation methods call it after
// they commit; call it directly after any other write that changes groups,
// memberships, permissions, forum visibility or roles.
func (e *Evaluator) Invalidate() {
	e.gen.Add(1)
	e.mu.Lock()
	clear(e.entries)
	e.mu.Unlock()
}

// access returns the cached or freshly evaluated access to one forum.
func (e *Evaluator) access(ctx context.Context, v domain.Viewer, forumID int64) (access, error) {
	key := cacheKey{session: v.SessionKey, user: v.UserID, role: v.Role, active: v.Active}
	gen := e.gen.Load()
	now := e.now()

	e.mu.Lock()
	ent := e.entries[key]
	if ent != nil && (ent.gen != gen || !now.Before(ent.expires)) {
		delete(e.entries, key)
		ent = nil
	}
	if ent != nil {
		if a, ok := ent.forums[forumID]; ok {
			e.mu.Unlock()
			return a, nil
		}
	}
	e.mu.Unlock()

	// Evaluate outside the lock. If Invalidate runs meanwhile, the result is
	// stamped with the generation read above and is stale on the next call.
	a, err := e.evaluate(ctx, v, forumID)
	if err != nil {
		return access{}, err
	}
	if a.missing {
		return a, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen.Load() {
		return a, nil // invalidated while evaluating: answer, but do not cache
	}
	ent = e.entries[key]
	if ent == nil || ent.gen != gen || !now.Before(ent.expires) {
		e.sweepLocked(now)
		ent = &entry{gen: gen, expires: now.Add(TTL), forums: make(map[int64]access)}
		e.entries[key] = ent
	}
	ent.forums[forumID] = a
	return a, nil
}

// sweepLocked drops expired entries, so the cache holds only live sessions.
func (e *Evaluator) sweepLocked(now time.Time) {
	for k, ent := range e.entries {
		if !now.Before(ent.expires) {
			delete(e.entries, k)
		}
	}
}

// evaluate computes access from the database, with no cache.
func (e *Evaluator) evaluate(ctx context.Context, v domain.Viewer, forumID int64) (access, error) {
	if err := e.loadImplicit(ctx); err != nil {
		return access{}, err
	}
	a := access{perms: make(map[domain.Permission]bool)}
	err := e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		forum, err := q.GetForum(ctx, forumID)
		if errors.Is(err, sql.ErrNoRows) {
			a.hidden, a.missing = true, true
			return nil
		}
		if err != nil {
			return err
		}

		groups := []int64{e.everyoneID}
		if v.Member() {
			groups = append(groups, e.membersID)
			stored, err := q.GroupsForUser(ctx, v.UserID)
			if err != nil {
				return err
			}
			for _, g := range stored {
				groups = append(groups, g.GroupID)
			}
		}

		restricted := false
		switch forum.Visibility {
		case domain.VisibilityPublic:
		case domain.VisibilityMembers:
			// The everyone grant does not reach a members forum, and an
			// anonymous viewer has no other group.
			groups = without(groups, e.everyoneID)
		case domain.VisibilityRestricted:
			restricted = true
		default:
			return fmt.Errorf("perm: forum %d has unknown visibility %q", forumID, forum.Visibility)
		}

		if len(groups) > 0 {
			rows, err := q.PermissionRows(ctx, dbq.PermissionRowsParams{ForumID: forumID, GroupIds: groups})
			if err != nil {
				return err
			}
			local := map[int64][]domain.Permission{}
			global := map[int64][]domain.Permission{}
			for _, r := range rows {
				if r.ForumID.Valid {
					local[r.GroupID] = append(local[r.GroupID], domain.Permission(r.Permission))
				} else {
					global[r.GroupID] = append(global[r.GroupID], domain.Permission(r.Permission))
				}
			}
			for _, g := range groups {
				granted, ok := local[g]
				if !ok && !restricted {
					granted = global[g]
				}
				for _, p := range granted {
					a.perms[p] = true
				}
			}
		}

		if v.Member() && v.Role == domain.RoleModerator {
			a.perms[domain.PermModerate] = true
		}
		a.hidden = restricted && !a.perms[domain.PermRead]
		return nil
	})
	return a, err
}

// loadImplicit finds the ids of the implicit groups by slug. The seed gives
// them fixed ids, but looking them up keeps that a fact about the migration
// rather than a constant repeated here.
func (e *Evaluator) loadImplicit(ctx context.Context) error {
	e.implicitMu.Lock()
	defer e.implicitMu.Unlock()
	if e.implicitOK {
		return nil
	}
	return e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		ev, err := q.GetGroupBySlug(ctx, domain.GroupEveryone)
		if err != nil {
			return fmt.Errorf("perm: finding the %s group: %w", domain.GroupEveryone, err)
		}
		mem, err := q.GetGroupBySlug(ctx, domain.GroupMembers)
		if err != nil {
			return fmt.Errorf("perm: finding the %s group: %w", domain.GroupMembers, err)
		}
		e.everyoneID, e.membersID, e.implicitOK = ev.ID, mem.ID, true
		return nil
	})
}

func without(s []int64, x int64) []int64 {
	out := s[:0:0]
	for _, v := range s {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}
