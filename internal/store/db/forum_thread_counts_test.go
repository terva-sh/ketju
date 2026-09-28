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

// counts is a forum's threads with a post, and their posts that are neither
// deleted nor moved.
type counts struct{ threads, posts int64 }

// forumThreadCounts reads forum_thread_counts, and what it should hold,
// counted from threads and post_meta directly.
func forumThreadCounts(t *testing.T, tx *sql.Tx) (got, want map[int64]counts) {
	t.Helper()
	read := func(query string) map[int64]counts {
		rows, err := tx.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		m := map[int64]counts{}
		for rows.Next() {
			var f int64
			var c counts
			if err := rows.Scan(&f, &c.threads, &c.posts); err != nil {
				t.Fatal(err)
			}
			m[f] = c
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return m
	}
	return read(`SELECT forum_id, threads, live_posts FROM forum_thread_counts`),
		read(`SELECT t.forum_id, count(*),
			sum((SELECT count(*) FROM post_meta AS p WHERE p.thread_id = t.id AND p.deleted = 0 AND p.moved = 0))
			FROM threads AS t WHERE t.last_post_ordinal > 0 GROUP BY t.forum_id`)
}

// liveDrift lists the threads whose live_post_count is not a count of their
// post_meta rows that are neither deleted nor moved.
func liveDrift(t *testing.T, tx *sql.Tx) []string {
	t.Helper()
	rows, err := tx.QueryContext(t.Context(), `SELECT id, live_post_count,
		(SELECT count(*) FROM post_meta AS p WHERE p.thread_id = t.id AND p.deleted = 0 AND p.moved = 0)
		FROM threads AS t`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, got, want int64
		if err := rows.Scan(&id, &got, &want); err != nil {
			t.Fatal(err)
		}
		if got != want {
			out = append(out, fmt.Sprintf("thread %d: live_post_count %d, post_meta says %d", id, got, want))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestForumThreadCountsFollowThreads: over random inserts, deletes, moves
// between forums, posts arriving and a thread emptied back to no posts,
// post_meta rows written, overwritten, deleted, restored and moved, and
// ClearThreads and ClearPostMeta as reindex runs them, in either order, the
// triggers keep threads.live_post_count equal to a count of post_meta and
// forum_thread_counts equal to a count of threads and their posts, with no
// row for a forum that has none. GET /me reads its unread badges from this
// table and GET /forums its totals, so a drift is a wrong number on every
// page.
func TestForumThreadCountsFollowThreads(t *testing.T) {
	d, _ := openTest(t)
	most := 0          // the most forums with a counted thread at once, over every seed
	var maxPosts int64 // the most posts one forum counted at once
	for seed := range uint64(40) {
		rng := rand.New(rand.NewPCG(seed, 7))
		err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			ex := func(query string, args ...any) {
				if _, err := tx.ExecContext(t.Context(), query, args...); err != nil {
					t.Fatalf("seed %d: %s: %v", seed, query, err)
				}
			}
			ex(`DELETE FROM threads`)
			ex(`DELETE FROM post_meta`)
			for step := range 300 {
				id, forum, ordinal := 1+rng.Int64N(12), 1+rng.Int64N(4), rng.Int64N(3)
				// A post_meta row may name a thread that has no row, as it
				// does for a moment inside reindex's clears.
				post, flag := 1+rng.Int64N(4), rng.Int64N(2)
				var op string
				switch r := rng.IntN(32); {
				case r >= 20 && r < 24:
					op = "write a post"
					// The upsert UpsertPostMeta is: a conflict is an update
					// that resets the flags, as a replay from the start does.
					ex(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
						VALUES (?, ?, 1, 1, 0) ON CONFLICT (thread_id, ordinal) DO UPDATE SET deleted = 0, moved = 0`,
						id, post)
				case r >= 24 && r < 27:
					op = "delete or restore a post"
					ex(`UPDATE post_meta SET deleted = ? WHERE thread_id = ? AND ordinal = ?`, flag, id, post)
				case r == 27:
					op = "move a post"
					ex(`UPDATE post_meta SET moved = ? WHERE thread_id = ? AND ordinal = ?`, flag, id, post)
				case r == 28:
					op = "move a post to another thread"
					ex(`UPDATE OR IGNORE post_meta SET thread_id = ? WHERE thread_id = ? AND ordinal = ?`,
						1+rng.Int64N(12), id, post)
				case r == 29 || r == 30:
					op = "remove a post row"
					ex(`DELETE FROM post_meta WHERE thread_id = ? AND ordinal = ?`, id, post)
				case r == 31:
					op = "clear post_meta"
					if err := q.ClearPostMeta(t.Context()); err != nil {
						t.Fatal(err)
					}
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
				if drift := liveDrift(t, tx); drift != nil {
					t.Fatalf("seed %d, step %d (%s thread %d, forum %d, ordinal %d, post %d): %v",
						seed, step, op, id, forum, ordinal, post, drift)
				}
				got, want := forumThreadCounts(t, tx)
				most = max(most, len(want))
				for _, c := range want {
					maxPosts = max(maxPosts, c.posts)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("seed %d, step %d (%s thread %d, forum %d, ordinal %d, post %d): forum_thread_counts %v, threads say %v",
						seed, step, op, id, forum, ordinal, post, got, want)
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
	if maxPosts < 4 {
		t.Fatalf("no forum ever counted more than %d posts; the walk barely exercised posts", maxPosts)
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
		if want := map[int64]counts{1: {2, 0}, 2: {2, 0}}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("after the backfill forum_thread_counts is %v, want %v", got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestForumPostCountsBackfill: a database that already has threads and posts
// when migration 0009 runs starts with each thread's live_post_count and each
// forum's posts right, deleted and moved posts left out, and the triggers
// carry on from there.
func TestForumPostCountsBackfill(t *testing.T) {
	before, all := map[string]string{}, map[string]string{}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body := mustReadMigration(e.Name())
		all[e.Name()] = body
		if e.Name() < "0009" {
			before[e.Name()] = body
		}
	}
	if len(before) != 8 {
		t.Fatalf("%d migrations before 0009, want 8", len(before))
	}

	d, err := Open(t.Context(), Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.migrate(t.Context(), mapFS(before), "m"); err != nil {
		t.Fatal(err)
	}
	// Thread 1 in forum 1: four posts, one deleted and one moved. Thread 2
	// in forum 1: two posts. Thread 3 in forum 2: one post, deleted. Thread 4
	// in forum 3 has no post yet, and forum 3 gets no row.
	err = d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, th := range []struct{ id, forum, posts int64 }{{1, 1, 4}, {2, 1, 2}, {3, 2, 1}, {4, 3, 0}} {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO threads (id, forum_id, slug, title, author_id,
				created_at, post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
				VALUES (?, ?, 's', 'T', 1, 1, ?, 1, 1, ?, 0)`, th.id, th.forum, th.posts, th.posts); err != nil {
				return err
			}
			for o := int64(1); o <= th.posts; o++ {
				if _, err := tx.ExecContext(t.Context(), `INSERT INTO post_meta (thread_id, ordinal, author_id,
					created_at, log_offset) VALUES (?, ?, 1, 1, 0)`, th.id, o); err != nil {
					return err
				}
			}
		}
		_, err := tx.ExecContext(t.Context(), `UPDATE post_meta SET deleted = 1
			WHERE (thread_id = 1 AND ordinal = 2) OR thread_id = 3`)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(t.Context(), `UPDATE post_meta SET moved = 1 WHERE thread_id = 1 AND ordinal = 3`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.migrate(t.Context(), mapFS(all), "m"); err != nil {
		t.Fatal(err)
	}
	check := func(when string, want map[int64]counts) {
		t.Helper()
		err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			if drift := liveDrift(t, tx); drift != nil {
				return fmt.Errorf("%s: %v", when, drift)
			}
			got, _ := forumThreadCounts(t, tx)
			if !reflect.DeepEqual(got, want) {
				return fmt.Errorf("%s: forum_thread_counts is %v, want %v", when, got, want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	check("after the backfill", map[int64]counts{1: {2, 4}, 2: {1, 0}})

	// The triggers take over from the backfilled numbers: restoring the
	// deleted post in thread 3 counts it, and moving thread 2 to forum 2
	// takes its two posts with it.
	err = d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		if _, err := tx.ExecContext(t.Context(), `UPDATE post_meta SET deleted = 0 WHERE thread_id = 3`); err != nil {
			return err
		}
		_, err := tx.ExecContext(t.Context(), `UPDATE threads SET forum_id = 2 WHERE id = 2`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	check("after a restore and a move", map[int64]counts{1: {1, 2}, 2: {2, 3}})
}
