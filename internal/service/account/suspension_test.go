package account

import (
	"database/sql"
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
