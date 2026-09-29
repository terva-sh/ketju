package account

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
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
	w.svc.lifted = func() {
		if err := w.svc.Suspend(t.Context(), mod, "", "mem", "again", nil, false); err != nil {
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
