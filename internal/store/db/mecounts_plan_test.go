package db

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestMeCountsScansNoTable: GET /me runs MeCounts on every page load, so its
// plan may scan only its own CTEs and forum_thread_counts, which has a row per
// forum. Every real table is searched by a key: the viewer's rows by user_id,
// threads by id or by threads_forum_active past the mark. A rewrite that
// scans threads, or any other table that grows with the site, fails here
// rather than in production.
//
// The query is read from its source rather than from dbq, which does not
// export it; sqlc's named argument becomes ?1, as sqlc writes it.
func TestMeCountsScansNoTable(t *testing.T) {
	b, err := os.ReadFile("queries/viewer_state.sql")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "-- name: MeCounts :many")
	if start < 0 {
		t.Fatal("no MeCounts in queries/viewer_state.sql")
	}
	src = src[start:]
	src = strings.ReplaceAll(src[:strings.Index(src, ";")], "sqlc.arg(user_id)", "?1")

	allowed := map[string]bool{
		"CONSTANT": true, "one": true, "touched": true, "mine": true, "f": true,
		// The viewer's unread post notifications, read by notifications_user
		// (user_id, read_at), grouped by thread (TKT-01M29CRJ2T).
		"noted_threads": true,
		"c":             true, // forum_thread_counts: a row per forum
	}
	scan := regexp.MustCompile(`^SCAN (\S+)`)
	d, _ := openTest(t)
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+src, 1)
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
				t.Errorf("MeCounts scans %s", m[1])
			}
		}
		if t.Failed() {
			t.Logf("plan:\n%s", strings.Join(plan, "\n"))
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
