package users

import (
	"database/sql"
	"fmt"
	"go/build"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestMeReadsNoLogs is AC2's structural half: this package does not import
// the thread log or the thread cache, so its counts can only be projections.
// The behavioural half is internal/web/api's TestMeShapes, which answers
// from a database with no thread log on disk at all.
//
// It checks this package's own imports rather than everything reachable:
// internal/config names threadcache's types, so every package that logs
// reaches it, and that is not reading a log.
func TestMeReadsNoLogs(t *testing.T) {
	p, err := build.Default.Import("github.com/terva-sh/ketju/internal/service/users", ".", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Imports) < 5 {
		t.Fatalf("read %d imports; the check is not seeing the package", len(p.Imports))
	}
	for _, i := range p.Imports {
		if strings.HasSuffix(i, "/store/threadlog") || strings.HasSuffix(i, "/store/threadcache") {
			t.Errorf("users imports %s", i)
		}
	}
}

// TestThreadRuleMatchesPostRule: MeCounts decides a thread is unread from its
// newest post; UnreadCount (docs/04) counts unread posts one by one. Over
// random threads, read positions and marks they agree on every thread. Each
// thread is alone in its own forum, so MeCounts' rows are per thread.
func TestThreadRuleMatchesPostRule(t *testing.T) {
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for seed := range uint64(20) {
		rng := rand.New(rand.NewPCG(seed, 7))
		var mark sql.NullInt64
		if rng.IntN(2) == 0 {
			mark = sql.NullInt64{Int64: 1000 + rng.Int64N(100), Valid: true}
		}
		threads := 1 + rng.IntN(12)
		if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			ex := func(s string, args ...any) error {
				_, err := tx.ExecContext(t.Context(), s, args...)
				return err
			}
			for _, s := range []string{`DELETE FROM read_positions`, `DELETE FROM post_meta`, `DELETE FROM threads`,
				`DELETE FROM forums`, `DELETE FROM users`} {
				if err := ex(s); err != nil {
					return err
				}
			}
			if err := ex(`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at)
				VALUES (1, 'reader', 'R', ?, 1, 1)`, mark); err != nil {
				return err
			}
			for th := 1; th <= threads; th++ {
				if err := ex(`INSERT INTO forums (id, slug, name, visibility, created_at, updated_at)
					VALUES (?, ?, 'F', 'public', 1, 1)`, th, fmt.Sprintf("f%d", th)); err != nil {
					return err
				}
				posts := rng.IntN(6)
				at := int64(990 + rng.IntN(60))
				for o := 1; o <= posts; o++ {
					at += int64(rng.IntN(20)) // non-decreasing, ties included
					if err := ex(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
						VALUES (?, ?, 2, ?, 0)`, th, o, at); err != nil {
						return err
					}
				}
				if err := ex(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count,
					last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
					VALUES (?, ?, 't', 'T', 2, 1, ?, ?, 2, ?, 0)`, th, th, posts, at, posts); err != nil {
					return err
				}
				if rng.IntN(3) > 0 {
					if err := ex(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
						VALUES (1, ?, ?, 1)`, th, rng.IntN(posts+2)); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		var rows []dbq.MeCountsRow
		want := map[int64]bool{}
		if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			if rows, err = q.MeCounts(t.Context(), 1); err != nil {
				return err
			}
			for th := 1; th <= threads; th++ {
				n, err := q.UnreadCount(t.Context(), dbq.UnreadCountParams{UserID: 1, ThreadID: int64(th)})
				if err != nil {
					return err
				}
				want[int64(th)] = n > 0
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		got := map[int64]bool{}
		for _, r := range rows {
			if r.ForumID.Valid {
				got[r.ForumID.Int64] = r.Threads.Int64 == 1
			}
		}
		for th := int64(1); th <= int64(threads); th++ {
			if got[th] != want[th] {
				t.Fatalf("seed %d, thread %d (mark %v): MeCounts says unread=%v, UnreadCount %v",
					seed, th, mark, got[th], want[th])
			}
		}
	}
}

func TestPageSize(t *testing.T) {
	for prefs, want := range map[string]int{
		`{}`: 25, `{"page_size": 10}`: 10, `{"page_size": 50}`: 50, `{"page_size": 9}`: 25,
		`{"page_size": 51}`: 25, `{"page_size": 30.5}`: 25, `{"page_size": "30"}`: 25,
		`{"page_size": null}`: 25, `not json`: 25, ``: 25, `{"page_size": 30, "other": [1]}`: 30,
	} {
		if got := pageSize(prefs); got != want {
			t.Errorf("pageSize(%s) = %d, want %d", prefs, got, want)
		}
	}
}
