package views

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestEffectiveWatchSQLAgrees is AC1's SQL half (TKT-01M29CRJ29): every
// combination of a thread's own level and its forum's, written as real
// watches rows, comes back from the list and the pins as the level
// domain.EffectiveWatch gives. Three forums carry no row, a watch and a
// mute; each holds a thread with no row, a watch and a mute. Every thread is
// a global pin, so the pins of forum 1 carry the other forums' threads, each
// of which must take its own forum's row and not forum 1's. Another member's
// rows, which disagree with the viewer's everywhere, must change nothing.
func TestEffectiveWatchSQLAgrees(t *testing.T) {
	d := open(t)
	levels := []string{domain.WatchNone, domain.WatchWatch, domain.WatchMute}
	stmts := []string{`INSERT INTO users (id, handle, display_name, created_at, updated_at)
		VALUES (1, 'viewer', 'V', 1, 1), (2, 'other', 'O', 1, 1)`}
	want := map[int64]string{}
	var threads []thread
	for f := range 3 {
		forum := int64(f + 1)
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO forums (id, slug, name, visibility, created_at, updated_at)
			VALUES (%d, 'f%d', 'F', 'public', 1, 1)`, forum, forum))
		if levels[f] != "" {
			stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at)
				VALUES (1, %d, '%s', 1, 1)`, forum, levels[f]))
		}
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, forum_id, level, created_at, updated_at)
			VALUES (2, %d, 'mute', 1, 1)`, forum))
		for th := range 3 {
			id := forum*10 + int64(th)
			threads = append(threads, thread{id: id, forum: forum, created: id, lastAt: id, ordinal: 1, scope: "global"})
			if levels[th] != "" {
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
					VALUES (1, %d, '%s', 1, 1)`, id, levels[th]))
			}
			stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
				VALUES (2, %d, 'watch', 1, 1)`, id))
			want[id] = domain.EffectiveWatch(levels[th], levels[f])
		}
	}
	exec(t, d, stmts...)
	insertThreads(t, d, threads...)

	check := func(what string, rows []ThreadRow, n int) {
		t.Helper()
		if len(rows) != n {
			t.Fatalf("%s: %d rows, want %d", what, len(rows), n)
		}
		for _, r := range rows {
			if r.Watch != want[r.ID] {
				t.Errorf("%s: thread %d in forum %d has level %q, want %q", what, r.ID, r.ForumID, r.Watch, want[r.ID])
			}
		}
	}
	for f := int64(1); f <= 3; f++ {
		check(fmt.Sprintf("forum %d's list", f), list(t, d, ListParams{ForumID: f, UserID: 1, Limit: 25}), 3)
	}
	var pins []ThreadRow
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		pins, err = PinnedThreads(t.Context(), tx, 1, 1, 1, "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check("forum 1's pins", pins, 9)
	// An anonymous viewer, user 0, has no rows at all.
	for _, r := range list(t, d, ListParams{ForumID: 3, UserID: 0, Limit: 25}) {
		if r.Watch != "" {
			t.Errorf("an anonymous viewer has level %q on thread %d", r.Watch, r.ID)
		}
	}
}
