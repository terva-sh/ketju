package dbtest

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func open(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
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
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(t.Context(), s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

var fixture = []string{
	`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
	  last_post_author_id, last_post_ordinal, log_length_projected) VALUES (42, 7, 's', 'T', 1, 1, 2, 5, 2, 2, 300)`,
	`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
	  last_post_author_id, last_post_ordinal, log_length_projected) VALUES (41, 7, 'r', 'S', 1, 1, 0, 1, 1, 0, 90)`,
	`INSERT INTO thread_tags (thread_id, tag) VALUES (42, 'tools'), (42, 'meta')`,
	`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, reply_to, log_offset) VALUES (42, 2, 2, 5, 1, 200)`,
	`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset) VALUES (42, 1, 1, 1, 100)`,
	`INSERT INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (42, 2, 1)`,
	`INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (42, 300, 1), (41, 90, 1)`,
}

// TestDumpIsCanonical is the part of TKT-01M29BYYJ8's AC3 this ticket can
// land without a writer: the same rows inserted in a different order, at a
// different wall-clock time, dump identically, and a changed row does not.
// The rebuild-from-logs equality itself is TKT-01M29C2WW5's and
// TKT-01M29C5Y25's, through this helper.
func TestDumpIsCanonical(t *testing.T) {
	a, b := open(t), open(t)
	exec(t, a, fixture...)
	reversed := make([]string, len(fixture))
	for i, s := range fixture {
		reversed[len(fixture)-1-i] = s
	}
	exec(t, b, reversed...)
	exec(t, b, `UPDATE projection_state SET updated_at = 999`)

	da, db2 := DumpProjections(t, a), DumpProjections(t, b)
	if da != db2 {
		t.Fatalf("the same rows dumped differently:\n%s\n---\n%s", da, db2)
	}
	for _, want := range []string{
		"threads: 2 rows", "thread_tags: 2 rows", "post_meta: 2 rows", "mentions: 1 rows",
		"projection_state: 2 rows", `title="T"`, "reply_to=NULL", "reply_to=1",
	} {
		if !strings.Contains(da, want) {
			t.Errorf("the dump lacks %q:\n%s", want, da)
		}
	}
	if strings.Contains(da, "updated_at") {
		t.Errorf("the dump includes a wall-clock column:\n%s", da)
	}
	if i, j := strings.Index(da, "post_meta: thread_id=42 ordinal=1 "),
		strings.Index(da, "post_meta: thread_id=42 ordinal=2 "); i < 0 || j < i {
		t.Errorf("post_meta rows are not in primary-key order:\n%s", da)
	}

	exec(t, b, `UPDATE post_meta SET deleted = 1 WHERE ordinal = 2`)
	if DumpProjections(t, b) == da {
		t.Fatal("a changed row dumped the same as before")
	}
}

// TestFormatTellsTypesApart: a projection that wrote a number where text
// belongs is not the same projection, though the two print alike.
func TestFormatTellsTypesApart(t *testing.T) {
	if format("7") == format(int64(7)) {
		t.Fatal(`format spells "7" and 7 alike`)
	}
	if format(nil) == format("NULL") {
		t.Fatal("format spells a null and the text NULL alike")
	}
}
