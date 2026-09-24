package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// TestHandlePrefixSearchesTheIndex: mention autocomplete runs
// UsersByHandlePrefix on keystrokes, so it must be a range search on the
// handle index and never a scan of users. The range only uses the index when
// it compares under the index's own collation (NOCASE, the column's), which a
// rewrite adding COLLATE BINARY, or LIKE with case_sensitive_like, would
// quietly lose.
//
// The query is read from its source, as TestMeCountsScansNoTable does.
func TestHandlePrefixSearchesTheIndex(t *testing.T) {
	b, err := os.ReadFile("queries/identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "-- name: UsersByHandlePrefix :many")
	if start < 0 {
		t.Fatal("no UsersByHandlePrefix in queries/identity.sql")
	}
	src = src[start:]
	src = strings.NewReplacer("sqlc.arg(lo)", "?1", "sqlc.arg(hi)", "?2", "sqlc.arg(lim)", "?3").
		Replace(src[:strings.Index(src, ";")])

	d, _ := openTest(t)
	var plan []string
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+src, "ab", "ab{", 8)
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
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(plan, "\n")
	if len(plan) != 1 || !strings.HasPrefix(plan[0], "SEARCH users USING INDEX sqlite_autoindex_users_") ||
		!strings.Contains(plan[0], "(handle>? AND handle<?)") {
		t.Errorf("UsersByHandlePrefix is not one range search on the handle index:\n%s", got)
	}
}
