package api

import (
	"database/sql"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the viewer block's rename_until (TKT-01M3QXV399), which tells
// the client whether to offer the author Rename, and until when.

// renameUntil is the rename_until v is served on id's first page.
func (w *world) renameUntil(v domain.Viewer, id threadid.ID) *dto.Millis {
	w.t.Helper()
	return page(w.t, w.get(v, threadPath(id))).Viewer.RenameUntil
}

// TestRenameUntil is the fifth criterion: rename_until is the end of the
// author's window while every check Rename makes of the author passes, and
// null for anyone else, a moderator who wrote the thread included, and
// once any of those checks fails. Where it is set, the author's rename is
// accepted; where the author's checks fail, it is refused, so the offer and
// the refusal agree.
func TestRenameUntil(t *testing.T) {
	const day = int64(86_400_000)
	want := func(t *testing.T, name string, got *dto.Millis, at int64) {
		t.Helper()
		switch {
		case at == 0 && got != nil:
			t.Errorf("%s: rename_until %d, want null", name, *got)
		case at != 0 && got == nil:
			t.Errorf("%s: rename_until null, want %d", name, at)
		case at != 0 && int64(*got) != at:
			t.Errorf("%s: rename_until %d, want %d", name, *got, at)
		}
	}

	t.Run("who", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		created := w.threadRow(id).CreatedAt
		want(t, "the author inside the window", w.renameUntil(w.viewer("alice"), id), created+day)
		for _, who := range []string{"", "carol", "bob"} {
			want(t, "viewer "+who, w.renameUntil(w.viewer(who), id), 0)
		}
		gina := w.groupModerator(w.forum)
		want(t, "a group moderator", w.renameUntil(gina, id), 0)
		// Moderators who wrote the thread: they rename at any time, so no
		// window is theirs.
		want(t, "a moderator by role who is the author",
			w.renameUntil(w.viewer("bob"), w.threadIn(2, w.forum, "Bob's thread", "bob")), 0)
		want(t, "a group moderator who is the author",
			w.renameUntil(gina, w.threadIn(3, w.forum, "Gina's thread", "gina")), 0)
		// An inactive account is served as anonymous.
		suspended := w.viewer("alice")
		suspended.Active = false
		want(t, "the author, inactive", w.renameUntil(suspended, id), 0)
	})

	t.Run("window", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		alice := w.viewer("alice")
		created := w.threadRow(id).CreatedAt
		w.clock = created + day - 1
		want(t, "a millisecond before the end", w.renameUntil(alice, id), created+day)
		w.clock = created + day
		want(t, "at the end", w.renameUntil(alice, id), 0)
		if got := problemCode(t, w.sendAs(alice, http.MethodPatch, threadPath(id), rename("Too late"))); got != "rename-window-closed" {
			t.Errorf("the author's rename at the end: %s, want rename-window-closed", got)
		}

		w2 := newWorld(t)
		id2, _ := w2.seed()
		w2.setting("threads.rename_window_seconds", 0)
		want(t, "a window of 0", w2.renameUntil(w2.viewer("alice"), id2), 0)

		// A count too large for a Duration is the longest window there is,
		// and the end is that far after the start, not before it.
		w3 := newWorld(t)
		id3, _ := w3.seed()
		w3.setting("threads.rename_window_seconds", 10_000_000_000)
		longest := time.Duration(math.MaxInt64).Milliseconds()
		want(t, "a window of 10000000000 s", w3.renameUntil(w3.viewer("alice"), id3), w3.threadRow(id3).CreatedAt+longest)
	})

	t.Run("checks", func(t *testing.T) {
		w := newWorld(t)
		id, _ := w.seed()
		alice, bob := w.viewer("alice"), w.viewer("bob")
		header(t, w.sendAs(bob, http.MethodPost, modPath(id, "lock"), ``))
		want(t, "a locked thread", w.renameUntil(alice, id), 0)
		header(t, w.sendAs(bob, http.MethodPost, modPath(id, "unlock"), ``))
		if w.renameUntil(alice, id) == nil {
			t.Fatal("unlocked, the author has no rename_until")
		}
		w.archive()
		want(t, "an archived forum", w.renameUntil(alice, id), 0)

		w2 := newWorld(t)
		id2, _ := w2.seed()
		w2.cache.MarkArchived(id2)
		want(t, "an archived thread", w2.renameUntil(w2.viewer("alice"), id2), 0)

		// A per-forum read row for members replaces the global rows there,
		// so the author may read and not reply.
		w3 := newWorld(t)
		id3, _ := w3.seed()
		w3.write(func(q *dbq.Queries) error {
			return q.GrantPermission(t.Context(), dbq.GrantPermissionParams{
				ForumID: sql.NullInt64{Int64: w3.forum, Valid: true}, GroupID: 2, Permission: string(domain.PermRead), CreatedAt: 1})
		})
		w3 = newHandlerFor(w3)
		want(t, "without reply", w3.renameUntil(w3.viewer("alice"), id3), 0)
		if got := problemCode(t, w3.sendAs(w3.viewer("alice"), http.MethodPatch, threadPath(id3), rename("No reply"))); got != "forbidden" {
			t.Errorf("the author's rename without reply: %s, want forbidden", got)
		}
	})
}

// TestRenameUntilIsInTheViewerBlock is the sixth criterion and the response
// cache invariant for the new field: the author and another member share one
// cached posts entry, and only the author's viewer block carries
// rename_until. The window passing with no append turns it null, from the
// same cache entry, and changes the author's ETag.
func TestRenameUntilIsInTheViewerBlock(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	alice, carol := w.viewer("alice"), w.viewer("carol")
	before := w.svc.Responses.Stats()
	first := w.get(alice, threadPath(id))
	p := page(t, first)
	if p.Viewer.RenameUntil == nil {
		t.Fatal("the author's page has no rename_until")
	}
	if c := page(t, w.get(carol, threadPath(id))); c.Viewer.RenameUntil != nil {
		t.Errorf("another member's page carries rename_until %d", *c.Viewer.RenameUntil)
	}
	if s := w.svc.Responses.Stats(); s.Inserts-before.Inserts != 1 || s.Hits-before.Hits != 1 {
		t.Fatalf("stats went from %+v to %+v; want one draw shared by both members", before, s)
	}

	w.clock = int64(*p.Viewer.RenameUntil)
	mid := w.svc.Responses.Stats()
	again := w.get(alice, threadPath(id), "If-None-Match", first.Header().Get("ETag"))
	if again.Code != http.StatusOK {
		t.Fatalf("the author's page once the window closed: %d, want 200 with a new ETag", again.Code)
	}
	if got := page(t, again).Viewer.RenameUntil; got != nil {
		t.Errorf("once the window closed the author is served rename_until %d", *got)
	}
	if again.Header().Get("ETag") == first.Header().Get("ETag") {
		t.Error("the ETag did not change when rename_until did")
	}
	if s := w.svc.Responses.Stats(); s.Inserts != mid.Inserts || s.Hits-mid.Hits != 1 {
		t.Errorf("stats went from %+v to %+v; want the posts from the same entry", mid, s)
	}
}
