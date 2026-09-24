package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// forumThreadCounts reads forum_thread_counts, and what it should hold,
// counted from threads directly.
func forumThreadCounts(t *testing.T, tx *sql.Tx) (got, want map[int64]int64) {
	t.Helper()
	read := func(query string) map[int64]int64 {
		rows, err := tx.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		m := map[int64]int64{}
		for rows.Next() {
			var f, n int64
			if err := rows.Scan(&f, &n); err != nil {
				t.Fatal(err)
			}
			m[f] = n
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return m
	}
	return read(`SELECT forum_id, threads FROM forum_thread_counts`),
		read(`SELECT forum_id, count(*) FROM threads WHERE last_post_ordinal > 0 GROUP BY forum_id`)
}

// TestForumThreadCountsFollowThreads: over random inserts, deletes, moves
// between forums, posts arriving and a thread emptied back to no posts, and
// ClearThreads as reindex runs it, the triggers keep forum_thread_counts equal
// to a count of threads, with no row for a forum that has none. GET /me reads
// its unread badges from this table, so a drift is a wrong badge on every
// page.
func TestForumThreadCountsFollowThreads(t *testing.T) {
	d, _ := openTest(t)
	most := 0 // the most forums with a counted thread at once, over every seed
	for seed := range uint64(40) {
		rng := rand.New(rand.NewPCG(seed, 7))
		err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			ex := func(query string, args ...any) {
				if _, err := tx.ExecContext(t.Context(), query, args...); err != nil {
					t.Fatalf("seed %d: %s: %v", seed, query, err)
				}
			}
			ex(`DELETE FROM threads`)
			for step := range 200 {
				id, forum, ordinal := 1+rng.Int64N(12), 1+rng.Int64N(4), rng.Int64N(3)
				var op string
				switch r := rng.IntN(20); {
				case r < 7:
					op = "insert"
					// ON CONFLICT DO NOTHING rather than OR IGNORE, which would
					// also swallow a NOT NULL failure and insert nothing ever.
					ex(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count,
						last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
						VALUES (?, ?, 's', 'T', 1, 1, ?, 1, 1, ?, 0) ON CONFLICT (id) DO NOTHING`,
						id, forum, ordinal, ordinal)
				case r < 10:
					op = "delete"
					ex(`DELETE FROM threads WHERE id = ?`, id)
				case r < 13:
					op = "move"
					ex(`UPDATE threads SET forum_id = ? WHERE id = ?`, forum, id)
				case r < 16:
					op = "post"
					ex(`UPDATE threads SET last_post_ordinal = ?, post_count = ? WHERE id = ?`, ordinal, ordinal, id)
				case r < 19:
					op = "move and post"
					ex(`UPDATE threads SET forum_id = ?, last_post_ordinal = ? WHERE id = ?`, forum, ordinal, id)
				default:
					op = "clear"
					if err := q.ClearThreads(t.Context()); err != nil {
						t.Fatal(err)
					}
				}
				got, want := forumThreadCounts(t, tx)
				most = max(most, len(want))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("seed %d, step %d (%s thread %d, forum %d, ordinal %d): forum_thread_counts %v, threads say %v",
						seed, step, op, id, forum, ordinal, got, want)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if most < 4 {
		t.Fatalf("at most %d forums ever had a counted thread; the walk never exercised all four", most)
	}
}

// TestForumThreadCountsBackfill: a database that already has threads when
// migration 0007 runs starts with the right counts.
func TestForumThreadCountsBackfill(t *testing.T) {
	before, all := map[string]string{}, map[string]string{}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body := mustReadMigration(e.Name())
		all[e.Name()] = body
		if e.Name() < "0007" {
			before[e.Name()] = body
		}
	}
	if len(before) != 6 {
		t.Fatalf("%d migrations before 0007, want 6", len(before))
	}

	d, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.migrate(t.Context(), mapFS(before), "m"); err != nil {
		t.Fatal(err)
	}
	err = d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for i, f := range []int64{1, 1, 1, 2, 2, 3} {
			ordinal := int64(1)
			if i == 2 || i == 5 {
				ordinal = 0 // no post yet: not counted, and forum 3 gets no row
			}
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO threads (id, forum_id, slug, title, author_id,
				created_at, post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
				VALUES (?, ?, 's', 'T', 1, 1, ?, 1, 1, ?, 0)`, i+1, f, ordinal, ordinal); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.migrate(t.Context(), mapFS(all), "m"); err != nil {
		t.Fatal(err)
	}
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		got, _ := forumThreadCounts(t, tx)
		if want := map[int64]int64{1: 2, 2: 2}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("after the backfill forum_thread_counts is %v, want %v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
