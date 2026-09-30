package api

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Tests for the viewer block's author_windows (TKT-01M3PTT97X), which tells
// the client how long it may offer Edit, Delete and Restore on the viewer's
// own posts.

// windowsOf is the author_windows v is served on id's first page.
func (w *world) windowsOf(v domain.Viewer, id threadid.ID) *dto.AuthorWindows {
	w.t.Helper()
	return page(w.t, w.get(v, threadPath(id))).Viewer.AuthorWindows
}

func wantWindows(t *testing.T, name string, got *dto.AuthorWindows, edit, del int) {
	t.Helper()
	if got == nil || got.EditSeconds != edit || got.DeleteSeconds != del {
		t.Errorf("%s: author_windows %+v, want {%d %d}", name, got, edit, del)
	}
}

func wantNoWindows(t *testing.T, name string, got *dto.AuthorWindows) {
	t.Helper()
	if got != nil {
		t.Errorf("%s: author_windows %+v, want null", name, *got)
	}
}

// TestThreadViewerCarriesAuthorWindows is the first criterion: a member's
// page carries the two settings, and the next page after a change carries
// the new value. A viewer who may moderate the forum, by role or through a
// group, is bound by neither window and gets null, as does a viewer who is
// not a member. A member who moderates a different forum is still bound
// here, and gets the windows.
func TestThreadViewerCarriesAuthorWindows(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	wantWindows(t, "a member at the defaults", w.windowsOf(w.viewer("carol"), id), 86400, 900)

	w.setting("posts.edit_window_seconds", 600)
	w.setting("posts.delete_window_seconds", 0)
	wantWindows(t, "a member after a change", w.windowsOf(w.viewer("carol"), id), 600, 0)

	wantNoWindows(t, "an anonymous viewer", w.windowsOf(w.viewer(""), id))
	wantNoWindows(t, "a moderator by role", w.windowsOf(w.viewer("bob"), id))
	inactive := w.viewer("carol")
	inactive.Active = false
	wantNoWindows(t, "an inactive account", w.windowsOf(inactive, id))

	elsewhere := w.forumAt("elsewhere", "public", false)
	gina := w.groupModerator(elsewhere)
	wantWindows(t, "a member who moderates another forum", w.windowsOf(gina, id), 600, 0)
	// The same group, granted moderate here too.
	var mods int64
	if err := w.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		g, err := q.GetGroupBySlug(t.Context(), domain.GroupModerators)
		mods = g.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := perm.New(w.db).Grant(t.Context(), audit.By{}, w.forum, mods, domain.PermModerate); err != nil {
		t.Fatal(err)
	}
	wantNoWindows(t, "a member who moderates this forum through a group", w.windowsOf(gina, id))
}

// TestAuthorWindowsFollowTheClamp is the sixth criterion: author_windows
// carries the seconds the server enforces, int(secondsDuration(n) /
// time.Second), not the raw setting. With both settings past what a Duration
// holds, the page carries 9223372036 for each, and the author's delete 30
// days after posting is allowed, so the client and the server agree.
func TestAuthorWindowsFollowTheClamp(t *testing.T) {
	const tooLarge = 10_000_000_000
	w := newWorld(t)
	id, _ := w.seed()
	w.setting("posts.edit_window_seconds", tooLarge)
	w.setting("posts.delete_window_seconds", tooLarge)
	wantWindows(t, "both settings past a Duration", w.windowsOf(w.viewer("carol"), id), 9223372036, 9223372036)

	n, created := w.fresh(id, "carol")
	w.clock = created + (30 * 24 * time.Hour).Milliseconds()
	if rec := send(w.chain(), "carol", http.MethodDelete, postPathN(id, n), ""); rec.Code != http.StatusOK {
		t.Fatalf("the delete 30 days on: %d %s, want 200", rec.Code, rec.Body)
	}
}

// TestAuthorWindowsAreNotCached is the response cache invariant for this
// ticket: two members share one cached entry for the posts, the second a
// hit, and each is served the windows built for its own request. A change to
// a setting moves the ETag, though the log length did not change, so a
// client holding the old page is not told it is current.
func TestAuthorWindowsAreNotCached(t *testing.T) {
	w := newWorld(t)
	id, _ := w.seed()
	w.addUsers("active", "dana")
	path := threadPath(id)

	before := w.svc.Responses.Stats()
	carol := w.get(w.viewer("carol"), path)
	dana := w.get(w.viewer("dana"), path)
	after := w.svc.Responses.Stats()
	if after.Inserts-before.Inserts != 1 || after.Hits-before.Hits != 1 {
		t.Fatalf("stats %+v then %+v: want one entry, filled by carol and hit by dana", before, after)
	}
	wantWindows(t, "carol", page(t, carol).Viewer.AuthorWindows, 86400, 900)
	wantWindows(t, "dana", page(t, dana).Viewer.AuthorWindows, 86400, 900)
	// And a moderator, whose class has its own entry, is still sent none.
	wantNoWindows(t, "bob", w.windowsOf(w.viewer("bob"), id))

	etag := carol.Header().Get("ETag")
	if etag == "" {
		t.Fatal("the page has no ETag")
	}
	length := page(t, carol).Thread.LogLength
	w.setting("posts.delete_window_seconds", 300)
	hits := w.svc.Responses.Stats().Hits
	again := w.get(w.viewer("carol"), path, "If-None-Match", etag)
	if again.Code != http.StatusOK {
		t.Fatalf("after the setting changed, the old ETag answered %d, want 200 with the new page", again.Code)
	}
	p := page(t, again)
	if p.Thread.LogLength != length {
		t.Fatalf("log length moved from %d to %d; the test needs it still", length, p.Thread.LogLength)
	}
	if w.svc.Responses.Stats().Hits == hits {
		t.Error("the page after the setting changed did not take its posts from the response cache")
	}
	if got := again.Header().Get("ETag"); got == etag {
		t.Errorf("the ETag %s did not move with the setting", got)
	}
	wantWindows(t, "carol after the change", p.Viewer.AuthorWindows, 86400, 300)
}
