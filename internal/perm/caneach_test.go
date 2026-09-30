package perm

import (
	"context"
	"database/sql"
	"errors"
	"github.com/terva-sh/ketju/internal/audit"
	"maps"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// referenceEvaluate is evaluate as it was before the batch statement
// (TKT-01M29CA99R): one forum at a time, the implicit groups by slug,
// GetForum, GroupsForUser and PermissionRows, with the archived rule
// (TKT-01M3N24RHD) added the same way. It is kept here, unused by the package, so the batch can be
// held to it over random worlds; a test comparing CanEach only with Check
// would compare evaluateMany with itself.
func referenceEvaluate(ctx context.Context, e *Evaluator, v domain.Viewer, forumID int64) (access, error) {
	a := access{perms: make(map[domain.Permission]bool)}
	err := e.db.Read(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		ev, err := q.GetGroupBySlug(ctx, domain.GroupEveryone)
		if err != nil {
			return err
		}
		mem, err := q.GetGroupBySlug(ctx, domain.GroupMembers)
		if err != nil {
			return err
		}
		everyoneID, membersID := ev.ID, mem.ID
		forum, err := q.GetForum(ctx, forumID)
		if errors.Is(err, sql.ErrNoRows) {
			a.hidden, a.missing = true, true
			return nil
		}
		if err != nil {
			return err
		}
		groups := []int64{everyoneID}
		if v.Member() {
			groups = append(groups, membersID)
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
			groups = without(groups, everyoneID)
		case domain.VisibilityRestricted:
			restricted = true
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
		if forum.Archived != 0 {
			delete(a.perms, domain.PermCreateThread)
			delete(a.perms, domain.PermReply)
			delete(a.perms, domain.PermUpload)
		}
		a.hidden = restricted && !a.perms[domain.PermRead]
		return nil
	})
	return a, err
}

// TestCanEachAgreesWithCheck: over random groups, forums (some archived) and
// grants, for every kind of viewer and permission, CanEach on a cold cache,
// CanEach on a warm one, and Check forum by forum give the same answers, and
// the batch evaluation matches the one-forum evaluation it replaced. A forum
// that does not exist is not allowed.
func TestCanEachAgreesWithCheck(t *testing.T) {
	perms := []domain.Permission{domain.PermRead, domain.PermReply, domain.PermCreateThread, domain.PermUpload, domain.PermModerate}
	for seed := range uint64(8) {
		rng := rand.New(rand.NewPCG(seed, 3))
		f := newFixture(t)
		groups := []int64{f.everyone, f.members, f.newGroup(), f.newGroup()}
		var forums []int64
		for range 3 + rng.IntN(6) {
			vis := []string{domain.VisibilityPublic, domain.VisibilityMembers, domain.VisibilityRestricted}[rng.IntN(3)]
			forums = append(forums, f.forum(vis))
			if rng.IntN(3) == 0 {
				if err := f.ev.SetForumArchived(t.Context(), forums[len(forums)-1], true); err != nil {
					t.Fatal(err)
				}
			}
		}
		for range rng.IntN(14) {
			g := groups[rng.IntN(len(groups))]
			p := perms[rng.IntN(len(perms))]
			if rng.IntN(3) == 0 {
				if err := f.ev.Grant(t.Context(), audit.By{}, 0, g, p); err != nil && !errors.Is(err, ErrGrantState) {
					t.Fatal(err)
				}
			} else if err := f.ev.Grant(t.Context(), audit.By{}, forums[rng.IntN(len(forums))], g, p); err != nil &&
				!errors.Is(err, ErrGrantState) {
				t.Fatal(err)
			}
		}
		inactive := f.viewer(domain.RoleMember, groups[2])
		inactive.Active = false
		viewers := []domain.Viewer{
			domain.Anonymous(),
			f.viewer(domain.RoleMember),
			f.viewer(domain.RoleMember, groups[2]),
			f.viewer(domain.RoleMember, groups[2], groups[3]),
			f.viewer(domain.RoleModerator, groups[3]),
			f.viewer(domain.RoleAdmin),
			inactive,
		}
		ids := append(append([]int64{}, forums...), 1_000_000) // and one that does not exist
		for vi, v := range viewers {
			for _, p := range perms {
				cold := New(f.db)
				got, err := cold.CanEach(t.Context(), v, ids, p)
				if err != nil {
					t.Fatal(err)
				}
				again, err := cold.CanEach(t.Context(), v, ids, p)
				if err != nil {
					t.Fatal(err)
				}
				ref := New(f.db)
				for _, id := range ids {
					want, err := ref.Can(t.Context(), v, id, p)
					if err != nil {
						t.Fatal(err)
					}
					if got[id] != want || again[id] != want {
						t.Fatalf("seed %d, viewer %d, %s in forum %d: CanEach %v (warm %v), Can %v",
							seed, vi, p, id, got[id], again[id], want)
					}
					// And the batch evaluation is the old one-forum evaluation.
					nv := v
					if !nv.Member() {
						nv = domain.Anonymous()
					}
					old, err := referenceEvaluate(t.Context(), ref, nv, id)
					if err != nil {
						t.Fatal(err)
					}
					cur, err := ref.evaluate(t.Context(), nv, id)
					if err != nil {
						t.Fatal(err)
					}
					if old.hidden != cur.hidden || old.missing != cur.missing || !maps.Equal(old.perms, cur.perms) {
						t.Fatalf("seed %d, viewer %d, forum %d: batch %+v, reference %+v", seed, vi, id, cur, old)
					}
				}
			}
		}
	}
}

// TestCanEachColdCostIsFixed: a cold CanEach runs one statement, over one
// forum or twenty, and a warm one runs none.
func TestCanEachColdCostIsFixed(t *testing.T) {
	var mu sync.Mutex
	counting := false
	var ran []string
	f := newFixtureWith(t, func(sql string) {
		mu.Lock()
		defer mu.Unlock()
		if !counting {
			return
		}
		switch strings.ToUpper(strings.Fields(sql + " x")[0]) {
		case "BEGIN", "COMMIT", "ROLLBACK", "END":
			return
		}
		ran = append(ran, sql)
	})
	g := f.newGroup()
	var forums []int64
	for i := range 20 {
		forums = append(forums, f.forum([]string{domain.VisibilityPublic, domain.VisibilityMembers, domain.VisibilityRestricted}[i%3]))
		f.grant(forums[i], g, domain.PermRead)
	}
	v := f.viewer(domain.RoleMember, g)
	count := func(ev *Evaluator, ids []int64) int {
		mu.Lock()
		counting, ran = true, nil
		mu.Unlock()
		if _, err := ev.CanEach(t.Context(), v, ids, domain.PermRead); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		counting = false
		return len(ran)
	}
	// Brand-new Evaluators: nothing is looked up before the one statement.
	one, many := New(f.db), New(f.db)
	if n1, n20 := count(one, forums[:1]), count(many, forums); n1 != 1 || n20 != 1 {
		t.Fatalf("cold CanEach ran %d statements for one forum and %d for twenty; want 1 each", n1, n20)
	}
	if n := count(many, forums); n != 0 {
		t.Fatalf("warm CanEach ran %d statements", n)
	}
}

// TestCanEachDoesNotCacheMissing: as with Check, a forum that does not exist
// yet is not remembered as missing. Creating a forum invalidates nothing, so
// a cached "missing" would hide the new forum from whoever asked first.
func TestCanEachDoesNotCacheMissing(t *testing.T) {
	f := newFixture(t)
	f.grant(0, f.everyone, domain.PermRead)
	first := f.forum(domain.VisibilityPublic)
	next := first + 1 // SQLite hands out the next rowid
	v := f.viewer(domain.RoleMember)
	got, err := f.ev.CanEach(t.Context(), v, []int64{first, next}, domain.PermRead)
	if err != nil || !got[first] || got[next] {
		t.Fatalf("before: %v, %v", got, err)
	}
	if id := f.forum(domain.VisibilityPublic); id != next {
		t.Fatalf("the new forum is %d, not %d", id, next)
	}
	if got, err := f.ev.CanEach(t.Context(), v, []int64{next}, domain.PermRead); err != nil || !got[next] {
		t.Fatalf("after creating it, the forum is still unreadable: %v, %v", got, err)
	}
}
