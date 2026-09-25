package db

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestForumIndexQueriesScanNoTable: GET /forums is the front page, so its
// queries follow MeCounts's rule (TestMeCountsScansNoTable). Each may scan
// only its own CTEs and forum_thread_counts, which has a row per forum; every
// real table is searched by a key. ForumUnread also reads no per-post table:
// read state is a position per thread (docs/04 "read_positions"), and the
// forum's count is a count of threads (TKT-01M29CA9A5).
func TestForumIndexQueriesScanNoTable(t *testing.T) {
	cases := []struct {
		file, name string
		args       []any
		allowed    []string
	}{
		{"viewer_state.sql", "ForumUnread", []any{1}, []string{"CONSTANT", "touched", "a", "n", "c"}},
		{"forums_groups.sql", "ForumLastPosts", nil, []string{"CONSTANT"}},
		{"forums_groups.sql", "ForumThreadCounts", nil, []string{"forum_thread_counts"}},
	}
	scan := regexp.MustCompile(`^SCAN (\S+)`)
	perPost := regexp.MustCompile(`\b(post_meta|posts|post_revisions)\b`)
	d, _ := openTest(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := os.ReadFile("queries/" + c.file)
			if err != nil {
				t.Fatal(err)
			}
			src := string(b)
			start := strings.Index(src, "-- name: "+c.name+" :many")
			if start < 0 {
				t.Fatalf("no %s in queries/%s", c.name, c.file)
			}
			src = src[start+len("-- name: "+c.name+" :many"):]
			src = src[:strings.Index(src, ";")]
			if m := perPost.FindString(src); m != "" {
				t.Errorf("%s reads %s", c.name, m)
			}
			src = strings.ReplaceAll(src, "sqlc.arg(user_id)", "?1")
			src = strings.ReplaceAll(src, "sqlc.slice('forum_ids')", "1, 2, 3")
			allowed := map[string]bool{}
			for _, a := range c.allowed {
				allowed[a] = true
			}
			err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
				rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+src, c.args...)
				if err != nil {
					return err
				}
				defer rows.Close()
				var plan []string
				for rows.Next() {
					var id, parent, unused int
					var detail string
					if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
						return err
					}
					plan = append(plan, detail)
					if m := scan.FindStringSubmatch(detail); m != nil && !allowed[m[1]] {
						t.Errorf("%s scans %s", c.name, m[1])
					}
				}
				if t.Failed() || testing.Verbose() {
					t.Logf("plan:\n%s", strings.Join(plan, "\n"))
				}
				return rows.Err()
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
