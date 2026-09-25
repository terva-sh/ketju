package views

import (
	"database/sql"
	"fmt"
	"go/build"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func open(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return d
}

func exec(t *testing.T, d *db.DB, stmts ...string) {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(t.Context(), s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// thread is a threads row for insertThreads.
type thread struct {
	id, forum, created, lastAt, ordinal int64
	scope                               string // "", "forum" or "global"
	until                               int64  // 0 for no expiry
}

func insertThreads(t *testing.T, d *db.DB, ts ...thread) {
	t.Helper()
	var stmts []string
	for _, th := range ts {
		scope, until := "NULL", "NULL"
		if th.scope != "" {
			scope = "'" + th.scope + "'"
			if th.until != 0 {
				until = fmt.Sprint(th.until)
			}
		}
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at,
			post_count, last_post_at, last_post_author_id, last_post_ordinal, pinned_scope, pinned_until,
			log_length_projected) VALUES (%d, %d, 's', 'T', 1, %d, %d, %d, 1, %d, %s, %s, 0)`,
			th.id, th.forum, th.created, th.ordinal, th.lastAt, th.ordinal, scope, until))
	}
	exec(t, d, stmts...)
}

func list(t *testing.T, d *db.DB, p ListParams) []ThreadRow {
	t.Helper()
	var rows []ThreadRow
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = ThreadList(t.Context(), tx, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func ids(rows []ThreadRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// TestPlansSeek: the list reads its forum's threads through an index in the
// list's order, with no sort step, however far into the list the cursor is;
// the pins read only the pinned rows. A plan that scans threads, or sorts a
// forum's threads to find one page, fails here rather than on a big forum.
func TestPlansSeek(t *testing.T) {
	d := open(t)
	after := &Key{At: 5, ID: 5}
	type planCase struct {
		q    string
		args []any
		// sorts is whether a temp b-tree may order the result.
		sorts bool
	}
	cases := map[string]planCase{}
	for _, s := range []Sort{ByActivity, ByCreated} {
		for _, a := range []*Key{nil, after} {
			q, args := threadListSQL(ListParams{ForumID: 1, UserID: 1, Sort: s, After: a, Now: 1, Limit: 26})
			cases[fmt.Sprintf("list sort %d cursor %t", s, a != nil)] = planCase{q, args, false}
		}
	}
	q, args := pinnedSQL(1, 1, 1)
	cases["pinned"] = planCase{q, args, true}

	scan := regexp.MustCompile(`^SCAN (\S+)`)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var plan []string
			if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+c.q, c.args...)
				if err != nil {
					return err
				}
				defer rows.Close()
				for rows.Next() {
					var id, parent, unused int
					var detail string
					if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
						return err
					}
					plan = append(plan, detail)
				}
				return rows.Err()
			}); err != nil {
				t.Fatal(err)
			}
			for _, p := range plan {
				if m := scan.FindStringSubmatch(p); m != nil {
					t.Errorf("scans %s", m[1])
				}
				if !c.sorts && strings.Contains(p, "TEMP B-TREE") {
					t.Errorf("sorts: %s", p)
				}
			}
			if !strings.Contains(strings.Join(plan, "\n"), "SEARCH t USING") {
				t.Errorf("does not search threads by an index")
			}
			if t.Failed() {
				t.Logf("plan:\n%s", strings.Join(plan, "\n"))
			}
		})
	}
}

// TestUnreadMatchesUnreadCount is AC3's rule: over random threads, read
// positions and marks, each row's Unread is what dbq's UnreadCount (docs/04's
// per-post rule) says for that thread, and LastReadOrdinal is the position,
// 0 with no row.
func TestUnreadMatchesUnreadCount(t *testing.T) {
	d := open(t)
	for seed := range uint64(60) {
		rng := rand.New(rand.NewPCG(seed, 11))
		mark := "NULL"
		if rng.IntN(2) == 0 {
			mark = fmt.Sprint(1000 + rng.IntN(100))
		}
		stmts := []string{`DELETE FROM read_positions`, `DELETE FROM post_meta`, `DELETE FROM threads`, `DELETE FROM users`,
			`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at)
			 VALUES (1, 'reader', 'R', ` + mark + `, 1, 1)`}
		n := 1 + rng.IntN(12)
		position := map[int64]int64{}
		for th := 1; th <= n; th++ {
			posts := 1 + rng.IntN(6)
			at := int64(990 + rng.IntN(60))
			for o := 1; o <= posts; o++ {
				at += int64(rng.IntN(20)) // non-decreasing, ties included
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at,
					log_offset) VALUES (%d, %d, 1, %d, 0)`, th, o, at))
			}
			stmts = append(stmts, fmt.Sprintf(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at,
				post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
				VALUES (%d, 1, 's', 'T', 1, 1, %d, %d, 1, %d, 0)`, th, posts, at, posts))
			if rng.IntN(3) > 0 {
				position[int64(th)] = int64(rng.IntN(posts + 2))
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal,
					last_read_at) VALUES (1, %d, %d, 1)`, th, position[int64(th)]))
			}
		}
		exec(t, d, stmts...)

		rows := list(t, d, ListParams{ForumID: 1, UserID: 1, Now: 1, Limit: 100})
		if len(rows) != n {
			t.Fatalf("seed %d: %d rows, want %d", seed, len(rows), n)
		}
		if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			for _, r := range rows {
				want, err := q.UnreadCount(t.Context(), dbq.UnreadCountParams{UserID: 1, ThreadID: r.ID})
				if err != nil {
					return err
				}
				if r.Unread != want || r.LastReadOrdinal != position[r.ID] {
					t.Errorf("seed %d, thread %d (mark %s, position %d of %d): unread %d, last read %d; want %d, %d",
						seed, r.ID, mark, position[r.ID], r.LastPostOrdinal, r.Unread, r.LastReadOrdinal, want, position[r.ID])
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if t.Failed() {
			return
		}
	}
}

// TestKeysetSurvivesBumps is AC2: paging through a forum while threads are
// bumped repeats no thread, and every thread that stays below the cursor
// appears on a later page. Threads share last_post_at values, so ties are
// broken by id and pages end in the middle of a tie.
func TestKeysetSurvivesBumps(t *testing.T) {
	for _, s := range []Sort{ByActivity, ByCreated} {
		for seed := range uint64(40) {
			d := open(t)
			rng := rand.New(rand.NewPCG(seed, 3))
			var ts []thread
			for id := int64(1); id <= 40; id++ {
				at := int64(100 + rng.IntN(15))
				ts = append(ts, thread{id: id, forum: 1, created: at, lastAt: at, ordinal: 1})
			}
			insertThreads(t, d, ts...)
			clock := int64(1000)

			seen := map[int64]bool{}
			var after *Key
			for page := 0; ; page++ {
				rows := list(t, d, ListParams{ForumID: 1, Sort: s, After: after, Now: 1, Limit: 7})
				for _, r := range rows {
					if seen[r.ID] {
						t.Fatalf("sort %d seed %d: thread %d again on page %d", s, seed, r.ID, page)
					}
					seen[r.ID] = true
				}
				if len(rows) < 7 {
					break
				}
				after = &Key{}
				*after = rows[len(rows)-1].Key(s)
				// Bump a few threads, some above the cursor and some below.
				for range 3 {
					clock++
					exec(t, d, fmt.Sprintf(`UPDATE threads SET last_post_at = %d, last_post_ordinal = last_post_ordinal + 1,
						post_count = post_count + 1 WHERE id = %d`, clock, 1+rng.IntN(40)))
				}
			}
			// Every thread not seen must have moved above the cursor, which
			// only a bump does, and only in the activity order.
			var missing []int64
			for id := int64(1); id <= 40; id++ {
				if !seen[id] {
					missing = append(missing, id)
				}
			}
			if s == ByCreated && len(missing) > 0 {
				t.Fatalf("seed %d: sort by created skipped %v, and nothing changes created_at", seed, missing)
			}
			for _, id := range missing {
				var at int64
				if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
					return tx.QueryRowContext(t.Context(), `SELECT last_post_at FROM threads WHERE id = ?`, id).Scan(&at)
				}); err != nil {
					t.Fatal(err)
				}
				if at < 1000 {
					t.Fatalf("seed %d: thread %d was never bumped and was skipped", seed, id)
				}
			}
		}
	}
}

// TestPins is AC1 at the query: pins leave the paged list while they last
// and return to it when they expire; PinnedThreads has global pins from
// every forum first, then this forum's own, and no other forum's forum pin.
func TestPins(t *testing.T) {
	d := open(t)
	insertThreads(t, d,
		thread{id: 1, forum: 1, created: 1, lastAt: 10, ordinal: 1},
		thread{id: 2, forum: 1, created: 2, lastAt: 20, ordinal: 1, scope: "forum"},
		thread{id: 3, forum: 1, created: 3, lastAt: 30, ordinal: 1, scope: "forum", until: 500},
		thread{id: 4, forum: 2, created: 4, lastAt: 5, ordinal: 1, scope: "global"},
		thread{id: 5, forum: 2, created: 5, lastAt: 50, ordinal: 1, scope: "forum"},
		thread{id: 6, forum: 1, created: 6, lastAt: 1, ordinal: 1, scope: "global", until: 500},
		thread{id: 7, forum: 1, created: 7, lastAt: 70, ordinal: 0, scope: "forum"}, // no post yet
		thread{id: 8, forum: 1, created: 8, lastAt: 80, ordinal: 0},                 // no post yet
	)
	pinned := func(now int64) []int64 {
		var rows []ThreadRow
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			var err error
			rows, err = PinnedThreads(t.Context(), tx, 1, 0, now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ids(rows)
	}
	for _, c := range []struct {
		now          int64
		pins, others []int64
	}{
		{now: 499, pins: []int64{4, 6, 3, 2}, others: []int64{1}},
		// At pinned_until the pin has ended: until is exclusive.
		{now: 500, pins: []int64{4, 2}, others: []int64{3, 1, 6}},
	} {
		if got := pinned(c.now); !slices.Equal(got, c.pins) {
			t.Errorf("now %d: pinned %v, want %v", c.now, got, c.pins)
		}
		if got := ids(list(t, d, ListParams{ForumID: 1, Now: c.now, Limit: 25})); !slices.Equal(got, c.others) {
			t.Errorf("now %d: list %v, want %v", c.now, got, c.others)
		}
	}
}

// TestViewsReadNoLogs is AC4's structural half: this package cannot open a
// thread log, because it does not import the packages that can.
func TestViewsReadNoLogs(t *testing.T) {
	p, err := build.Default.Import("github.com/terva-sh/ketju/internal/store/db/views", ".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range p.Imports {
		if strings.HasSuffix(i, "/store/threadlog") || strings.HasSuffix(i, "/store/threadcache") {
			t.Errorf("views imports %s", i)
		}
	}
}

// TestKeysetUnderConcurrentBumps is AC2 with a real concurrent writer, for
// -race: a goroutine bumps threads while a reader pages, and no thread is
// seen twice.
func TestKeysetUnderConcurrentBumps(t *testing.T) {
	d := open(t)
	var ts []thread
	for id := int64(1); id <= 200; id++ {
		ts = append(ts, thread{id: id, forum: 1, created: id, lastAt: 100 + id%17, ordinal: 1})
	}
	insertThreads(t, d, ts...)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		rng := rand.New(rand.NewPCG(9, 9))
		for clock := int64(1000); ; clock++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(), `UPDATE threads SET last_post_at = ?,
					last_post_ordinal = last_post_ordinal + 1, post_count = post_count + 1 WHERE id = ?`,
					clock, 1+rng.IntN(200))
				return err
			}); err != nil {
				done <- err
				return
			}
		}
	}()
	for round := range 20 {
		seen := map[int64]bool{}
		var after *Key
		for {
			rows := list(t, d, ListParams{ForumID: 1, After: after, Now: 1, Limit: 9})
			for _, r := range rows {
				if seen[r.ID] {
					t.Fatalf("round %d: thread %d twice", round, r.ID)
				}
				seen[r.ID] = true
			}
			if len(rows) < 9 {
				break
			}
			k := rows[len(rows)-1].Key(ByActivity)
			after = &k
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
