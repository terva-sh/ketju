package views

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func crossList(t testing.TB, d *db.DB, p CrossParams) []ThreadRow {
	t.Helper()
	var rows []ThreadRow
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = CrossList(t.Context(), tx, p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

// The site BenchmarkCrossLists10k and TestCrossListsScanNoTable measure:
// docs/04 "Sizing sanity check"'s ten thousand threads, in fifty forums, of
// five posts each, with the reader's thousand read positions, a hundred
// watches, a few mutes and two hundred threads they posted in.
const (
	siteThreads = 10_000
	siteForums  = 50
	sitePosts   = 5
	reader      = 1
)

// seedSite builds that site. Thread i is in forum i%50+1, started at
// 10 000 + i*10 and last posted at 10 000 + i*10 + 8, so the newest thread
// is the most recently active, ordinals run 1..5 at two-unit steps, and a
// mark at 50 000 falls in the middle of the site. The reader read the
// thousand threads i%10 == 0 to the end, watches i%100 == 1, mutes i%500 ==
// 2, and posted in i%50 == 3. Every other user writes the rest.
func seedSite(t testing.TB, d *db.DB, mark string) {
	t.Helper()
	gen := fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < %d) `, siteThreads)
	stmts := []string{
		`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at) VALUES
		 (1, 'reader', 'R', ` + mark + `, 1, 1), (2, 'writer', 'W', NULL, 1, 1)`,
		fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < %d)
		 INSERT INTO forums (id, slug, name, description, position, visibility, created_at, updated_at)
		 SELECT i, 'f' || i, 'F' || i, '', i, 'public', 1, 1 FROM n`, siteForums),
		gen + fmt.Sprintf(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count,
		 last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
		 SELECT i, i %% %d + 1, 's', 'T', 2, 10000 + i * 10, %d, 10000 + i * 10 + 8, 2, %d, 0 FROM n`,
			siteForums, sitePosts, sitePosts),
	}
	for o := 1; o <= sitePosts; o++ {
		stmts = append(stmts, gen+fmt.Sprintf(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset)
		 SELECT i, %d, CASE WHEN %d = 2 AND i %% 50 = 3 THEN 1 ELSE 2 END, 10000 + i * 10 + %d, 0 FROM n`,
			o, o, 2*(o-1)))
	}
	stmts = append(stmts,
		gen+fmt.Sprintf(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
		 SELECT 1, i, %d, 1 FROM n WHERE i %% 10 = 0`, sitePosts),
		gen+`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
		 SELECT 1, i, 'watch', 1, 1 FROM n WHERE i % 100 = 1`,
		gen+`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
		 SELECT 1, i, 'mute', 1, 1 FROM n WHERE i % 500 = 2`,
		`ANALYZE`,
	)
	exec(t, d, stmts...)
}

func allForums() []int64 {
	out := make([]int64, siteForums)
	for i := range out {
		out[i] = int64(i + 1)
	}
	return out
}

// TestCrossListsScanNoTable: no list reads a table that grows with the site
// by a scan. Unread and New search their site-wide index from the mark up,
// in order, so the walk ends at the mark and needs no sort step; the cursor
// bounds it from above. Participated and Watching search threads by id from
// the viewer's own rows, each found by an index on the viewer. Each with and
// without a mark and a cursor, so a plan that changes once a reader has used
// "mark all read", or pages, fails here rather than on a big site.
func TestCrossListsScanNoTable(t *testing.T) {
	after := &Key{At: 60000, ID: 5000}
	scan := regexp.MustCompile(`^SCAN (\S+)`)
	walk := map[List]string{Unread: "SEARCH t USING INDEX threads_activity (last_post_at>?",
		New: "SEARCH t USING INDEX threads_created (created_at>?"}
	for _, mark := range []string{"NULL", "50000"} {
		d := open(t)
		seedSite(t, d, mark)
		for _, l := range Lists {
			for _, a := range []*Key{nil, after} {
				t.Run(fmt.Sprintf("mark %s %s cursor %t", mark, l, a != nil), func(t *testing.T) {
					q, args := crossListSQL(CrossParams{List: l, UserID: reader, Forums: allForums(), After: a, Limit: 26})
					plan := explain(t, d, q, args...)
					for _, p := range plan {
						// json_each is the forum list: one parameter, a row
						// per forum.
						if m := scan.FindStringSubmatch(p); m != nil && m[1] != "json_each" {
							t.Errorf("scans %s", p)
						}
						if walk[l] != "" && strings.Contains(p, "TEMP B-TREE") {
							t.Errorf("sorts: %s", p)
						}
					}
					all := strings.Join(plan, "\n")
					switch l {
					case Unread, New:
						if !strings.Contains(all, walk[l]) {
							t.Errorf("does not walk from the mark: want %q", walk[l])
						}
					case Participated:
						if !strings.Contains(all, "SEARCH pm USING INDEX post_meta_author") {
							t.Errorf("does not find the viewer's posts by author")
						}
					case Watching:
						if !strings.Contains(all, "SEARCH w USING INDEX watches_thread (user_id=?") {
							t.Errorf("does not find the viewer's watches by user")
						}
					}
					if !strings.Contains(all, "SEARCH rp USING PRIMARY KEY") {
						t.Errorf("does not find the read position by its key")
					}
					if t.Failed() {
						t.Logf("plan:\n%s", all)
					}
				})
			}
		}
	}
}

func explain(t testing.TB, d *db.DB, q string, args ...any) []string {
	t.Helper()
	var plan []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+q, args...)
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
	return plan
}

// BenchmarkCrossLists10k is the cost of a first page of each list, and of
// /me's counts, on seedSite's site, for a reader with no mark, one whose
// mark is halfway through the site, and the walk's worst case: no mark,
// and every thread read but the thirty oldest, so Unread and New walk the
// whole site to fill a page. docs/04 "Sizing sanity check" records the
// numbers.
func BenchmarkCrossLists10k(b *testing.B) {
	for _, mark := range []string{"NULL", "50000", "sparse"} {
		d := open(b)
		if mark == "sparse" {
			seedSite(b, d, "NULL")
			exec(b, d, fmt.Sprintf(`INSERT OR REPLACE INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at)
				SELECT 1, id, %d, 1 FROM threads WHERE id > 30`, sitePosts), `ANALYZE`)
		} else {
			seedSite(b, d, mark)
		}
		for _, l := range Lists {
			b.Run(fmt.Sprintf("mark=%s/%s/first", mark, l), func(b *testing.B) {
				for b.Loop() {
					crossList(b, d, CrossParams{List: l, UserID: reader, Forums: allForums(), Limit: 26})
				}
			})
		}
		b.Run(fmt.Sprintf("mark=%s/MeCounts", mark), func(b *testing.B) {
			for b.Loop() {
				if err := d.Read(b.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
					_, err := q.MeCounts(b.Context(), reader)
					return err
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestCrossListRules: over random sites, each list is exactly the threads
// its rule in Go picks, in its order, and paging by the key of each page's
// last row reads the whole list once.
func TestCrossListRules(t *testing.T) {
	d := open(t)
	exec(t, d, `INSERT INTO forums (id, slug, name, description, position, visibility, created_at, updated_at) VALUES
		(1, 'a', 'A', '', 1, 'public', 1, 1), (2, 'b', 'B', '', 2, 'public', 1, 1),
		(3, 'c', 'C', '', 3, 'public', 1, 1), (4, 'd', 'D', '', 4, 'public', 1, 1)`)
	for seed := range uint64(40) {
		rng := rand.New(rand.NewPCG(seed, 23))
		mark := int64(-1)
		markSQL := "NULL"
		if rng.IntN(2) == 0 {
			mark = int64(1000 + rng.IntN(200))
			markSQL = fmt.Sprint(mark)
		}
		stmts := []string{`DELETE FROM read_positions`, `DELETE FROM post_meta`, `DELETE FROM threads`,
			`DELETE FROM watches`, `DELETE FROM users`,
			`INSERT INTO users (id, handle, display_name, mark_all_read_at, created_at, updated_at)
			 VALUES (1, 'reader', 'R', ` + markSQL + `, 1, 1), (2, 'writer', 'W', NULL, 1, 1)`}
		type th struct {
			id, forum, created, lastAt, last, position int64
			hasRow, mine, watched, muted, forumMuted   bool
		}
		forumMuted := map[int64]bool{3: rng.IntN(2) == 0}
		if forumMuted[3] {
			stmts = append(stmts, `INSERT INTO watches (user_id, forum_id, level, created_at, updated_at) VALUES (1, 3, 'mute', 1, 1)`)
		}
		var all []th
		n := 1 + rng.IntN(30)
		for i := 1; i <= n; i++ {
			x := th{id: int64(i), forum: int64(1 + rng.IntN(4)), created: int64(950 + rng.IntN(300))}
			posts := rng.IntN(5) // 0: a thread with no post yet
			at := x.created
			for o := 1; o <= posts; o++ {
				at += int64(rng.IntN(40))
				author, moved := 2, 0
				if rng.IntN(4) == 0 {
					author = 1
					if rng.IntN(3) == 0 {
						moved = 1
					}
				}
				x.mine = x.mine || (author == 1 && moved == 0)
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at,
					moved, log_offset) VALUES (%d, %d, %d, %d, %d, 0)`, i, o, author, at, moved))
			}
			x.last, x.lastAt = int64(posts), at
			stmts = append(stmts, fmt.Sprintf(`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at,
				post_count, last_post_at, last_post_author_id, last_post_ordinal, log_length_projected)
				VALUES (%d, %d, 's', 'T', 2, %d, %d, %d, 2, %d, 0)`, i, x.forum, x.created, posts, at, posts))
			if rng.IntN(2) == 0 {
				x.hasRow, x.position = true, int64(rng.IntN(posts+1))
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal,
					last_read_at) VALUES (1, %d, %d, 1)`, i, x.position))
			}
			switch rng.IntN(5) {
			case 0:
				x.watched = true
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
					VALUES (1, %d, 'watch', 1, 1)`, i))
			case 1:
				x.muted = true
				stmts = append(stmts, fmt.Sprintf(`INSERT INTO watches (user_id, thread_id, level, created_at, updated_at)
					VALUES (1, %d, 'mute', 1, 1)`, i))
			}
			x.forumMuted = forumMuted[x.forum]
			all = append(all, x)
		}
		exec(t, d, stmts...)

		readable := []int64{1, 2, 3} // forum 4 is hidden from the reader
		var everyID []int64
		for _, x := range all {
			everyID = append(everyID, x.id)
		}
		member := map[List]map[int64]bool{}
		for _, l := range Lists {
			member[l] = map[int64]bool{}
		}
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			ms, err := Memberships(t.Context(), tx, reader, everyID)
			for _, m := range ms {
				if !slices.Contains(readable, m.ForumID) {
					continue
				}
				member[Unread][m.ThreadID] = m.Unread
				member[New][m.ThreadID] = m.New
				member[Participated][m.ThreadID] = m.Participated
				member[Watching][m.ThreadID] = m.Watching
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, l := range Lists {
			var want []th
			for _, x := range all {
				muted := x.muted || (x.forumMuted && !x.watched)
				if x.last == 0 || !slices.Contains(readable, x.forum) || muted {
					continue
				}
				var in bool
				switch l {
				case Unread:
					in = x.last > x.position && x.lastAt > mark
				case New:
					in = !x.hasRow && x.created > mark
				case Participated:
					in = x.mine
				case Watching:
					in = x.watched
				}
				if in {
					want = append(want, x)
				}
			}
			at := func(x th) int64 {
				if l == New {
					return x.created
				}
				return x.lastAt
			}
			slices.SortFunc(want, func(a, b th) int {
				if at(a) != at(b) {
					return int(at(b) - at(a))
				}
				return int(b.id - a.id)
			})
			var wantIDs, got []int64
			for _, x := range want {
				wantIDs = append(wantIDs, x.id)
			}
			var after *Key
			for range 50 {
				rows := crossList(t, d, CrossParams{List: l, UserID: reader, Forums: readable, After: after, Limit: 3})
				for _, r := range rows {
					got = append(got, r.ID)
				}
				if len(rows) < 3 {
					break
				}
				k := l.Key(rows[len(rows)-1])
				after = &k
			}
			if !slices.Equal(got, wantIDs) {
				t.Fatalf("seed %d, %s (mark %d): got %v, want %v", seed, l, mark, got, wantIDs)
			}
			var byMembership []int64
			for _, x := range want {
				if member[l][x.id] {
					byMembership = append(byMembership, x.id)
				}
			}
			for id, in := range member[l] {
				if in && !slices.Contains(wantIDs, id) {
					byMembership = append(byMembership, id)
				}
			}
			if !slices.Equal(byMembership, wantIDs) {
				t.Fatalf("seed %d, %s (mark %d): Memberships says %v, the list %v", seed, l, mark, byMembership, wantIDs)
			}
		}
	}
}
