package db

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestQueriesDoNotMixParameterStyles guards against a sqlc behaviour that
// neither the regen-diff step nor the type checker catches. In a query that
// mixes sqlc.arg/sqlc.narg/sqlc.slice with a bare '?', sqlc numbers the named
// parameter ('?3') and leaves the bare ones bare, and SQLite numbers a bare
// '?' after '?3' as 4. The arguments are then bound to the wrong placeholders
// and the query silently matches nothing. It happened to RevokePermission in
// TKT-01M29BYYHG: a query is all bare '?' or all named, never both.
func TestQueriesDoNotMixParameterStyles(t *testing.T) {
	files, err := filepath.Glob("queries/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no query files found: %v", err)
	}
	nameLine := regexp.MustCompile(`(?m)^-- name: (\S+)`)
	comment := regexp.MustCompile(`(?m)--.*$`)
	checked := 0
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(body)
		idx := nameLine.FindAllStringSubmatchIndex(src, -1)
		for i, m := range idx {
			end := len(src)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			name := src[m[2]:m[3]]
			sqlText := comment.ReplaceAllString(src[m[1]:end], "")
			checked++
			if strings.Contains(sqlText, "sqlc.") && strings.Contains(sqlText, "?") {
				t.Errorf("%s: %s mixes sqlc.arg-style and bare '?' parameters; name them all", filepath.Base(f), name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("parsed no queries")
	}
}
