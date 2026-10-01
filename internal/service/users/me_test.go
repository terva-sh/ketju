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
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
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
// newest post, and counts by subtracting from the site-wide count what the
// reader's own rows rule out; UnreadCount (docs/04) counts unread posts one by
// one. Over random threads, read positions, marks, watches, mutes and
// authorship, MeCounts' three counts are what UnreadCount and those rows say,
// summed per forum. Threads share a few forums, so a thread wrongly
// subtracted from, or added to, another's count changes a total.
//
// ForumUnread, the forum index's column (TKT-01M29CA9A5), is held to the same
// threads count here, so the index and the /me badge cannot disagree.
func TestThreadRuleMatchesPostRule(t *testing.T) {
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	for seed := range uint64(60) {
		rng := rand.New(rand.NewPCG(seed, 7))
		var mark sql.NullInt64
		if rng.IntN(2) == 0 {
			mark = sql.NullInt64{Int64: 1000 + rng.Int64N(100), Valid: true}
		}
		threads := 1 + rng.IntN(12)
		// What the reader did to each thread, for the reference below.
		muted, watched, wrote := map[int64]bool{}, map[int64]bool{}, map[int64]bool{}
		// Threads share forums, so a rule that subtracts one thread from
		// another's count shows up in the forum's total.
		forums := 1 + rng.IntN(3)
		forumOf := map[int64]int64{}
		if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			ex := func(s string, args ...any) error {
				_, err := tx.ExecContext(t.Context(), s, args...)
				return err
			}
			for _, s := range []string{`DELETE FROM read_positions`, `DELETE FROM watches`, `DELETE FROM post_meta`,
				`DELETE FROM threads`, `DELETE FROM forums`, `DELETE FROM users`} {
				if err := ex(s); err != nil {
					return err
				}
			}
			if err := ex(`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at)
				VALUES (1, 'reader', 'R', ?, 1, 1), (2, 'writer', 'W', NULL, 1, 1)`, mark); err != nil {
				return err
			}
			for f := 1; f <= forums; f++ {
				if err := ex(`INSERT INTO forums (id, slug, name, visibility, created_at, updated_at)
					VALUES (?, ?, 'F', 'public', 1, 1)`, f, fmt.Sprintf("f%d", f)); err != nil {
					return err
				}
			}
			for th := 1; th <= threads; th++ {
				forumOf[int64(th)] = int64(1 + rng.IntN(forums))
				posts := rng.IntN(6)
				at := int64(990 + rng.IntN(60))
				for o := 1; o <= posts; o++ {
					at += int64(rng.IntN(20)) // non-decreasing, ties included
					author := 2
					if rng.IntN(5) == 0 {
						author = 1
						wrote[int64(th)] = true
					}
					if err := ex(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
						VALUES (?, ?, ?, ?, 0)`, th, o, author, at); err != nil {
						return err
					}
				}
				if err := ex(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count,
					last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
					VALUES (?, ?, 't', 'T', 2, 1, ?, ?, 2, ?, 0)`, th, forumOf[int64(th)], posts, at, posts); err != nil {
					return err
				}
				if rng.IntN(3) > 0 {
					if err := ex(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
						VALUES (1, ?, ?, 1)`, th, rng.IntN(posts+2)); err != nil {
						return err
					}
				}
				if level := []string{"", "", "watch", "mute"}[rng.IntN(4)]; level != "" {
					if err := ex(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
						VALUES (1, ?, ?, 1, 1)`, th, level); err != nil {
						return err
					}
					muted[int64(th)], watched[int64(th)] = level == "mute", level == "watch"
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		var rows []dbq.MeCountsRow
		var index []dbq.ForumUnreadRow
		want := map[int64]bool{}
		if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			if rows, err = q.MeCounts(t.Context(), 1); err != nil {
				return err
			}
			if index, err = q.ForumUnread(t.Context(), 1); err != nil {
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
		type counts struct{ threads, participated, watching int64 }
		got := map[int64]counts{}
		for _, r := range rows {
			if r.ForumID.Valid {
				got[r.ForumID.Int64] = counts{r.Threads.Int64, r.Participated.Int64, r.Watching.Int64}
			}
		}
		b := func(x bool) int64 {
			if x {
				return 1
			}
			return 0
		}
		wantBy := map[int64]counts{}
		for th := int64(1); th <= int64(threads); th++ {
			unread := want[th] && !muted[th]
			c := wantBy[forumOf[th]]
			c.threads += b(unread)
			c.participated += b(unread && wrote[th])
			c.watching += b(unread && watched[th])
			wantBy[forumOf[th]] = c
		}
		gotIndex := map[int64]int64{}
		for _, r := range index {
			if r.Threads <= 0 {
				t.Fatalf("seed %d: ForumUnread has a row for forum %d with %d threads", seed, r.ForumID, r.Threads)
			}
			gotIndex[r.ForumID] = r.Threads
		}
		for f := int64(1); f <= int64(forums); f++ {
			if got[f] != wantBy[f] {
				t.Fatalf("seed %d, forum %d (mark %v): MeCounts %+v, want %+v", seed, f, mark, got[f], wantBy[f])
			}
			if gotIndex[f] != wantBy[f].threads {
				t.Fatalf("seed %d, forum %d (mark %v): ForumUnread %d, want %d", seed, f, mark, gotIndex[f], wantBy[f].threads)
			}
		}
	}
}

func TestPrefs(t *testing.T) {
	type want struct {
		size  int
		theme string
	}
	for prefs, w := range map[string]want{
		`{}`: {25, "system"}, `{"page_size": 10}`: {10, "system"}, `{"page_size": 50}`: {50, "system"},
		`{"page_size": 9}`: {25, "system"}, `{"page_size": 51}`: {25, "system"}, `{"page_size": 30.5}`: {25, "system"},
		`{"page_size": "30"}`: {25, "system"}, `{"page_size": null}`: {25, "system"}, `not json`: {25, "system"},
		``: {25, "system"}, `[1]`: {25, "system"}, `null`: {25, "system"},
		`{"page_size": 30, "other": [1]}`: {30, "system"},
		`{"theme": "dark"}`:               {25, "dark"}, `{"theme": "light", "page_size": 40}`: {40, "light"},
		`{"theme": "sepia"}`: {25, "system"}, `{"theme": 1}`: {25, "system"}, `{"theme": "Dark"}`: {25, "system"},
		// One bad key does not cost the other its value.
		`{"theme": "dark", "page_size": "30"}`: {25, "dark"},
	} {
		if size, theme := Prefs(prefs); size != w.size || theme != w.theme {
			t.Errorf("Prefs(%s) = %d, %q; want %d, %q", prefs, size, theme, w.size, w.theme)
		}
	}
}

// BenchmarkMeCounts measures MeCounts at the size the ticket names before a
// per-user counter table is worth its cost: 10,000 threads over 20 forums,
// about 100,000 posts, a heavy reader with 2,000 read positions, some 300
// posts of their own, 50 watches and 20 mutes, and a light reader who has
// opened 20 threads. Run it with:
//
//	go test ./internal/service/users -run '^$' -bench MeCounts -benchtime 200x
func BenchmarkMeCounts(b *testing.B) {
	d := seedBench(b)
	for _, c := range []struct {
		name string
		user int64
	}{{"heavy reader", 1}, {"light reader", 3}} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				if err := d.Read(b.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
					_, err := q.MeCounts(b.Context(), c.user)
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func seedBench(b *testing.B) *db.DB {
	d := dbtest.Open(b, db.Options{Path: filepath.Join(b.TempDir(), "ketju.db")})
	rng := rand.New(rand.NewPCG(1, 1))
	const threads, forums = 10_000, 20
	if err := d.Write(b.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		ex := func(s string, args ...any) error {
			_, err := tx.ExecContext(b.Context(), s, args...)
			return err
		}
		if err := ex(`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at)
			VALUES (1, 'reader', 'R', 500000, 1, 1), (2, 'writer', 'W', NULL, 1, 1),
			       (3, 'lurker', 'L', NULL, 1, 1)`); err != nil {
			return err
		}
		for f := 1; f <= forums; f++ {
			if err := ex(`INSERT INTO forums (id, slug, name, visibility, created_at, updated_at)
				VALUES (?, ?, 'F', 'public', 1, 1)`, f, fmt.Sprintf("f%d", f)); err != nil {
				return err
			}
		}
		for th := 1; th <= threads; th++ {
			posts := 1 + rng.IntN(19)
			at := int64(th * 100)
			for o := 1; o <= posts; o++ {
				author := 2
				if rng.IntN(333) == 0 {
					author = 1
				}
				if err := ex(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
					VALUES (?, ?, ?, ?, 0)`, th, o, author, at+int64(o)); err != nil {
					return err
				}
			}
			if err := ex(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count,
				last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
				VALUES (?, ?, 't', 'T', 2, 1, ?, ?, 2, ?, 0)`, th, 1+th%forums, posts, at+int64(posts), posts); err != nil {
				return err
			}
			if th%500 == 7 { // the light reader has opened 20 threads
				if err := ex(`INSERT INTO read_positions VALUES (3, ?, 1, 1)`, th); err != nil {
					return err
				}
			}
			switch {
			case th%5 == 0:
				if err := ex(`INSERT INTO read_positions VALUES (1, ?, ?, 1)`, th, rng.IntN(posts+1)); err != nil {
					return err
				}
			case th%200 == 1:
				if err := ex(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
					VALUES (1, ?, 'watch', 1, 1)`, th); err != nil {
					return err
				}
			case th%500 == 3:
				if err := ex(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
					VALUES (1, ?, 'mute', 1, 1)`, th); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	return d
}
