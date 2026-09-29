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
// invalidate the cache when they commit: the cache of every Evaluator over the
// same database, not only the one that wrote.
//
// A Viewer is a snapshot taken when the request began, so a role or status
// change applies from the viewer's next request, not to a request already
// holding the old Viewer. That holds only while the session middleware reads
// role and status from the users row on every request (TKT-01M29C5Y3E); a
// Viewer cached across requests would carry an admin role past its removal.
package perm

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"

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

	// gen is bumped by Invalidate, and shared by every Evaluator over the
	// same database (generationFor). An entry filled under an older
	// generation is stale, whatever its expiry.
	gen *atomic.Uint64

	mu      sync.Mutex
	entries map[cacheKey]*entry
}

// New returns an Evaluator over d.
func New(d *db.DB) *Evaluator {
	return &Evaluator{db: d, now: time.Now, gen: generationFor(d), entries: make(map[cacheKey]*entry)}
}

// generations maps each open database to the generation counter its
// Evaluators share, so a write through one Evaluator makes every other one's
// entries stale. The key is weak and the entry is removed when the database
// is collected, so tests that open many databases do not accumulate them.
var generations sync.Map // weak.Pointer[db.DB] -> *atomic.Uint64

func generationFor(d *db.DB) *atomic.Uint64 {
	k := weak.Make(d)
	if g, ok := generations.Load(k); ok {
		return g.(*atomic.Uint64)
	}
	g, loaded := generations.LoadOrStore(k, new(atomic.Uint64))
	if !loaded {
		runtime.AddCleanup(d, func(k weak.Pointer[db.DB]) { generations.Delete(k) }, k)
	}
	return g.(*atomic.Uint64)
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

	// moderates is ModeratesAny's answer, once known: set is false until it
	// has been asked.
	moderates struct{ set, any bool }
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

// CanEach reports, for each forum, whether Check would Allow v to do p there.
// Forums already in v's cache cost nothing; the rest are evaluated together,
// in one statement however many there are, and cached as Check would cache
// them. GET /me asks about every forum with unread threads on
// every page load, and one evaluation per forum on a cold cache would make
// its cost grow with the number of forums.
func (e *Evaluator) CanEach(ctx context.Context, v domain.Viewer, forumIDs []int64, p domain.Permission) (map[int64]bool, error) {
	if !v.Member() {
		v = domain.Anonymous()
	}
	out := make(map[int64]bool, len(forumIDs))
	if v.Role == domain.RoleAdmin {
		for _, id := range forumIDs {
			out[id] = true
		}
		return out, nil
	}
	key := cacheKey{session: v.SessionKey, user: v.UserID, role: v.Role, active: v.Active}
	gen := e.gen.Load()
	now := e.now()
	var misses []int64
	e.mu.Lock()
	ent := e.entries[key]
	if ent != nil && (ent.gen != gen || !now.Before(ent.expires)) {
		delete(e.entries, key)
		ent = nil
	}
	for _, id := range forumIDs {
		if a, ok := ent.lookup(id); ok {
			out[id] = !a.hidden && a.perms[p]
		} else {
			misses = append(misses, id)
		}
	}
	e.mu.Unlock()
	if len(misses) == 0 {
		return out, nil
	}

	evaluated, err := e.evaluateMany(ctx, v, misses)
	if err != nil {
		return nil, err
	}
	for id, a := range evaluated {
		out[id] = !a.hidden && a.perms[p]
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen.Load() {
		return out, nil // invalidated while evaluating: answer, but do not cache
	}
	ent = e.entries[key]
	if ent == nil || ent.gen != gen || !now.Before(ent.expires) {
		e.sweepLocked(now)
		ent = &entry{gen: gen, expires: now.Add(TTL), forums: make(map[int64]access)}
		e.entries[key] = ent
	}
	for id, a := range evaluated {
		if !a.missing {
			ent.forums[id] = a
		}
	}
	return out, nil
}

func (ent *entry) lookup(forumID int64) (access, bool) {
	if ent == nil {
		return access{}, false
	}
	a, ok := ent.forums[forumID]
	return a, ok
}

// ReadableForums lists the forums v may read, in ListForums order. It asks
// about them all at once (CanEach), so a profile or post history, which
// needs the whole set, costs one evaluation however many forums there are.
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
	ids := make([]int64, len(forums))
	for i, f := range forums {
		ids[i] = f.ID
	}
	readable, err := e.CanEach(ctx, v, ids, domain.PermRead)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, id := range ids {
		if readable[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// ModeratesAny reports whether v may moderate somewhere: an active moderator
// or admin by role, or a member whose groups grant moderate in at least one
// forum they can see (docs/04 "permissions"). The admin API's moderates gate
// asks it, so a member who moderates one forum through a group reaches the
// queue, whose handlers then filter by forum.
//
// The answer is cached with v's other decisions and cleared by the same
// writes, so revoking the grant applies on the next request. A forum created
// since is taken into account within TTL, since creating a forum clears
// nothing: that matters only for a global grant with no forum to apply to
// yet.
func (e *Evaluator) ModeratesAny(ctx context.Context, v domain.Viewer) (bool, error) {
	if !v.Member() {
		return false, nil
	}
	if v.Role == domain.RoleAdmin || v.Role == domain.RoleModerator {
		return true, nil
	}
	key := cacheKey{session: v.SessionKey, user: v.UserID, role: v.Role, active: v.Active}
	gen := e.gen.Load()
	now := e.now()
	e.mu.Lock()
	if ent := e.entries[key]; ent != nil && ent.gen == gen && now.Before(ent.expires) && ent.moderates.set {
		e.mu.Unlock()
		return ent.moderates.any, nil
	}
	e.mu.Unlock()

	var ids []int64
	err := e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		forums, err := q.ListForums(ctx)
		for _, f := range forums {
			ids = append(ids, f.ID)
		}
		return err
	})
	if err != nil {
		return false, err
	}
	may, err := e.CanEach(ctx, v, ids, domain.PermModerate)
	if err != nil {
		return false, err
	}
	answer := false
	for _, ok := range may {
		answer = answer || ok
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if gen != e.gen.Load() {
		return answer, nil // invalidated while evaluating: answer, but do not cache
	}
	ent := e.entries[key]
	if ent == nil || ent.gen != gen || !now.Before(ent.expires) {
		e.sweepLocked(now)
		ent = &entry{gen: gen, expires: now.Add(TTL), forums: make(map[int64]access)}
		e.entries[key] = ent
	}
	ent.moderates.set, ent.moderates.any = true, answer
	return answer, nil
}

// Invalidate drops every cached decision, in this Evaluator and in every other
// over the same database. The mutation methods call it after they commit;
// call it directly after any other write that changes groups, memberships,
// permissions, forum visibility or roles.
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

// sweepLocked drops expired entries and those another Evaluator's write made
// stale, so the cache holds only live sessions.
func (e *Evaluator) sweepLocked(now time.Time) {
	gen := e.gen.Load()
	for k, ent := range e.entries {
		if ent.gen != gen || !now.Before(ent.expires) {
			delete(e.entries, k)
		}
	}
}

// evaluate computes access from the database, with no cache.
func (e *Evaluator) evaluate(ctx context.Context, v domain.Viewer, forumID int64) (access, error) {
	m, err := e.evaluateMany(ctx, v, []int64{forumID})
	return m[forumID], err
}

// evaluateMany evaluates several forums for v at once, in one statement
// however many forums are asked about, and with nothing to look up first:
// ForumAccessRows finds the implicit groups by slug itself and returns each
// forum's visibility with every permission row a group of v's holds there or
// globally, and decide applies the rules to those rows. A forum that does
// not exist comes back hidden and missing.
//
// evaluate is this for one forum, and Preview decides the same rows with
// staged changes laid over them, so the rules exist once, in decide.
func (e *Evaluator) evaluateMany(ctx context.Context, v domain.Viewer, forumIDs []int64) (map[int64]access, error) {
	var rows []dbq.ForumAccessRowsRow
	if err := e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = q.ForumAccessRows(ctx, accessParams(v, forumIDs))
		return err
	}); err != nil {
		return nil, err
	}
	out := make(map[int64]access, len(forumIDs))
	for _, id := range forumIDs {
		out[id] = access{hidden: true, missing: true}
	}
	forums, order := collect(rows)
	for _, id := range order {
		a, err := decide(id, forums[id], v)
		if err != nil {
			return nil, err
		}
		out[id] = a
	}
	return out, nil
}

// accessParams asks ForumAccessRows about forumIDs for v: the everyone
// group always, and the members group and v's stored memberships only for a
// member.
func accessParams(v domain.Viewer, forumIDs []int64) dbq.ForumAccessRowsParams {
	params := dbq.ForumAccessRowsParams{EveryoneSlug: domain.GroupEveryone, ForumIds: forumIDs}
	if v.Member() {
		params.UserID, params.MembersSlug = v.UserID, domain.GroupMembers
	}
	return params
}

// forumRows is one forum as decide reads it: its visibility, and what each
// of the viewer's groups is granted in it and site-wide.
type forumRows struct {
	visibility string
	// local[group] and global[group]: what each group of v's is granted
	// in this forum, and site-wide. A group with a key in local is governed
	// by those rows alone, so a key is present only while it has a row.
	local, global map[int64][]domain.Permission
	// everyone is the everyone group's id, when it is known.
	everyone int64
}

// collect groups ForumAccessRows' rows by forum, in the order they came.
func collect(rows []dbq.ForumAccessRowsRow) (map[int64]*forumRows, []int64) {
	forums := map[int64]*forumRows{}
	var order []int64
	for _, r := range rows {
		f := forums[r.ForumID]
		if f == nil {
			f = &forumRows{visibility: r.Visibility, local: map[int64][]domain.Permission{}, global: map[int64][]domain.Permission{}}
			forums[r.ForumID] = f
			order = append(order, r.ForumID)
		}
		if !r.GroupID.Valid {
			continue
		}
		if r.GroupSlug.String == domain.GroupEveryone {
			f.everyone = r.GroupID.Int64
		}
		if r.GrantForumID.Valid {
			f.local[r.GroupID.Int64] = append(f.local[r.GroupID.Int64], domain.Permission(r.Permission.String))
		} else {
			f.global[r.GroupID.Int64] = append(f.global[r.GroupID.Int64], domain.Permission(r.Permission.String))
		}
	}
	return forums, order
}

// decide applies the rules in the package comment to one forum's rows for v.
// It reads nothing and caches nothing.
func decide(id int64, f *forumRows, v domain.Viewer) (access, error) {
	a := access{perms: make(map[domain.Permission]bool)}
	restricted, noEveryone := false, false
	switch f.visibility {
	case domain.VisibilityPublic:
	case domain.VisibilityMembers:
		// The everyone grant does not reach a members forum, and an
		// anonymous viewer has no other group.
		noEveryone = true
	case domain.VisibilityRestricted:
		restricted = true
	default:
		return access{}, fmt.Errorf("perm: forum %d has unknown visibility %q", id, f.visibility)
	}
	// Every group v is in that holds a row here or globally; a group with
	// no rows grants nothing either way.
	groups := map[int64]bool{}
	for g := range f.local {
		groups[g] = true
	}
	for g := range f.global {
		groups[g] = true
	}
	for g := range groups {
		if noEveryone && f.everyone != 0 && g == f.everyone {
			continue
		}
		granted, ok := f.local[g]
		if !ok && !restricted {
			granted = f.global[g]
		}
		for _, p := range granted {
			a.perms[p] = true
		}
	}
	if v.Member() && v.Role == domain.RoleModerator {
		a.perms[domain.PermModerate] = true
	}
	a.hidden = restricted && !a.perms[domain.PermRead]
	return a, nil
}

// decision is what Check answers for p, given v's access to the forum.
func decision(v domain.Viewer, a access, p domain.Permission) Decision {
	switch {
	case v.Member() && v.Role == domain.RoleAdmin:
		return Allow
	case a.hidden:
		return Hidden
	case a.perms[p]:
		return Allow
	default:
		return Deny
	}
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
