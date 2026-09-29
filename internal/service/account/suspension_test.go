package account

import (
	"context"
	"database/sql"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The expiry job reads the end again in the transaction that lifts a
// suspension (TKT-01M29CP2QK). A suspension lifted and replaced after the
// job listed the account, with a later end, is left alone, and so is one a
// moderator lifted in that gap. One whose end is still due is lifted.
func TestSuspensionExpiryRereadsTheEnd(t *testing.T) {
	w := newWorld(t)
	mod := w.user("mod", domain.RoleModerator)
	for _, h := range []string{"moved", "lifted", "due"} {
		w.user(h, domain.RoleMember)
	}
	end := w.now.Add(time.Hour)
	for _, h := range []string{"moved", "lifted", "due"} {
		if err := w.svc.Suspend(t.Context(), mod, "", h, "spam", &end, false); err != nil {
			t.Fatal(err)
		}
	}
	w.now = end
	w.svc.listed = func() {
		if err := w.svc.Unsuspend(t.Context(), mod, "", "moved", false); err != nil {
			t.Fatal(err)
		}
		later := w.now.Add(time.Hour)
		if err := w.svc.Suspend(t.Context(), mod, "", "moved", "again", &later, false); err != nil {
			t.Fatal(err)
		}
		if err := w.svc.Unsuspend(t.Context(), mod, "", "lifted", false); err != nil {
			t.Fatal(err)
		}
	}
	n, err := w.svc.ExpireSuspensions(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("lifted %d (%v), want only the one still due", n, err)
	}
	for h, want := range map[string]string{"moved": domain.StatusSuspended, "lifted": domain.StatusActive, "due": domain.StatusActive} {
		if s := w.status(h); s != want {
			t.Errorf("%s is %s, want %s", h, s, want)
		}
	}
	var expired int
	if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_log WHERE action = 'user.unsuspended'
			AND actor_id IS NULL AND json_extract(details_json, '$.via') = 'expiry'`).Scan(&expired)
	}); err != nil {
		t.Fatal(err)
	}
	if expired != 1 {
		t.Errorf("%d rows name the expiry, want 1", expired)
	}
}

// Hiding or restoring posts needs the thread service; a service built
// without it, as the expiry job's is, refuses before changing anything.
func TestHideNeedsTheThreadService(t *testing.T) {
	w := newWorld(t)
	mod := w.user("mod", domain.RoleModerator)
	w.user("mem", domain.RoleMember)
	if err := w.svc.Suspend(t.Context(), mod, "", "mem", "spam", nil, true); err == nil {
		t.Fatal("a suspension with hide_posts and no thread service was made")
	}
	if s := w.status("mem"); s != domain.StatusActive {
		t.Errorf("mem is %s: the refusal came after the suspension", s)
	}
}

// fakePosts records the suspensions a restore was asked to put back.
type fakePosts struct{ restored [][]int64 }

func (f *fakePosts) HidePostsOf(context.Context, domain.Viewer, int64, int64) (int, error) {
	return 0, nil
}

func (f *fakePosts) RestorePostsOf(_ context.Context, _ domain.Viewer, _ int64, ids []int64) (int, error) {
	f.restored = append(f.restored, slices.Clone(ids))
	return 0, nil
}

// suspensionIDs is the ids of handle's user.suspended rows, oldest first.
func (w *world) suspensionIDs(handle string) []int64 {
	w.t.Helper()
	var ids []int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(w.t.Context(), handle)
		if err != nil {
			return err
		}
		ids, err = q.AuditIDs(w.t.Context(), suspensionRows(u.ID))
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return ids
}

// Unsuspending with restore_posts restores the posts of the suspension it
// lifted and no other (Terva review 1499 on PR #172): not an earlier one,
// and not one another moderator makes in the moment after the unsuspension
// commits. The standalone restore takes every suspension there was when it
// read the account.
func TestUnsuspendRestoresOnlyTheSuspensionItLifted(t *testing.T) {
	w := newWorld(t)
	posts := &fakePosts{}
	w.svc.Threads = posts
	mod := w.user("mod", domain.RoleModerator)
	w.user("mem", domain.RoleMember)

	// An earlier suspension, lifted without a restore, then the current one.
	if err := w.svc.Suspend(t.Context(), mod, "", "mem", "spam", nil, false); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Unsuspend(t.Context(), mod, "", "mem", false); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Suspend(t.Context(), mod, "", "mem", "spam", nil, false); err != nil {
		t.Fatal(err)
	}
	// The account's lock is held here, so the new suspension is made as a
	// writer that does not take it would make it: straight through perm.
	w.svc.lifted = func() {
		if _, err := w.svc.Perm.ChangeStatusID(t.Context(), w.idOf("mem"), perm.StatusChange{
			By: audit.By{Actor: mod.UserID}, Action: perm.ActionSuspended, Reason: "again"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.svc.Unsuspend(t.Context(), mod, "", "mem", true); err != nil {
		t.Fatal(err)
	}
	ids := w.suspensionIDs("mem")
	if len(ids) != 3 {
		t.Fatalf("%d suspensions, want 3", len(ids))
	}
	if want := [][]int64{{ids[1]}}; !slices.EqualFunc(posts.restored, want, slices.Equal) {
		t.Errorf("the unsuspension restored %v, want only the lifted suspension %d", posts.restored, ids[1])
	}

	w.svc.lifted = nil
	posts.restored = nil
	if err := w.svc.RestorePosts(t.Context(), mod, "", "mem"); err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(posts.restored, [][]int64{ids}, slices.Equal) {
		t.Errorf("the standalone restore took %v, want %v", posts.restored, ids)
	}
}

// idOf is the id of the account handle names.
func (w *world) idOf(handle string) int64 {
	w.t.Helper()
	var id int64
	if err := w.db.Read(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(w.t.Context(), handle)
		id = u.ID
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// blockingPosts holds a hide open until released, and records the order
// in which hides and restores ran.
type blockingPosts struct {
	mu      sync.Mutex
	order   []string
	inHide  chan struct{}
	release chan struct{}
}

func (b *blockingPosts) note(s string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.order = append(b.order, s)
}

func (b *blockingPosts) seen() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.order)
}

func (b *blockingPosts) HidePostsOf(context.Context, domain.Viewer, int64, int64) (int, error) {
	b.note("hide started")
	close(b.inHide)
	<-b.release
	b.note("hide finished")
	return 1, nil
}

func (b *blockingPosts) RestorePostsOf(context.Context, domain.Viewer, int64, []int64) (int, error) {
	b.note("restore")
	return 1, nil
}

// An unsuspension with restore_posts waits for a hide of the same account
// that is still appending, so the restore runs after the last hide and
// nothing the hide wrote is left standing (Terva review 1503 on PR #172). A
// hide asked for once the unsuspension has committed is conflict.
func TestUnsuspendWaitsForARunningHide(t *testing.T) {
	w := newWorld(t)
	b := &blockingPosts{inHide: make(chan struct{}), release: make(chan struct{})}
	w.svc.Threads = b
	mod := w.user("mod", domain.RoleModerator)
	w.user("mem", domain.RoleMember)
	if err := w.svc.Suspend(t.Context(), mod, "", "mem", "spam", nil, false); err != nil {
		t.Fatal(err)
	}

	hid := make(chan error, 1)
	go func() { hid <- w.svc.HidePosts(t.Context(), mod, "", "mem") }()
	<-b.inHide
	lifted := make(chan error, 1)
	go func() { lifted <- w.svc.Unsuspend(t.Context(), mod, "", "mem", true) }()
	select {
	case err := <-lifted:
		t.Fatalf("the unsuspension finished while the hide was running: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if s := w.status("mem"); s != domain.StatusSuspended {
		t.Errorf("mem is %s while the hide runs", s)
	}
	close(b.release)
	if err := <-hid; err != nil {
		t.Fatal(err)
	}
	if err := <-lifted; err != nil {
		t.Fatal(err)
	}
	if got, want := b.seen(), []string{"hide started", "hide finished", "restore"}; !slices.Equal(got, want) {
		t.Errorf("ran %v, want %v", got, want)
	}
	if err := w.svc.HidePosts(t.Context(), mod, "", "mem"); code(err) != "conflict" {
		t.Errorf("a hide after the unsuspension: %v, want conflict", err)
	}
}
