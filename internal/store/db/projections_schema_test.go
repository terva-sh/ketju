package db

import (
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

const projectionMarker = "-- PROJECTION: derived from thread logs; rebuild with ketju reindex"

// TestProjectionTablesAreMarked is AC1. The marker is read back from
// sqlite_schema, which keeps a CREATE statement's text, so it is checked in
// the schema as built rather than in the migration file. The marked set must
// equal ProjectionTables, the list reindex empties, in both directions: a
// projection added without the marker, or marked but missing from the list,
// fails.
func TestProjectionTablesAreMarked(t *testing.T) {
	d, _ := openTest(t)
	var marked []string
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(),
			`SELECT name, sql FROM sqlite_schema WHERE type = 'table' ORDER BY name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name, ddl string
			if err := rows.Scan(&name, &ddl); err != nil {
				return err
			}
			if strings.Contains(ddl, projectionMarker) {
				marked = append(marked, name)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Sorted(slices.Values(ProjectionTables))
	if !slices.Equal(marked, want) {
		t.Fatalf("tables carrying the PROJECTION marker are %v; ProjectionTables is %v", marked, want)
	}
}

// TestPostQuotesMigrationRewindsTheProjection (Terva review 1998): 0025
// moves every thread's projection_state back to the end of its file header,
// so the next catch-up replays each log and fills post_quotes for posts
// projected before the table existed. The rows stay, so a projection whose
// log is gone still classifies as corrupt; nothing else in them moves, and
// no authoritative table is touched (notification_progress included, so the
// generator waits for the replay rather than repeating itself).
func TestPostQuotesMigrationRewindsTheProjection(t *testing.T) {
	d, _ := openUnmigrated(t)
	if err := d.migrate(t.Context(), migrationsBefore(t, 25), "m"); err != nil {
		t.Fatal(err)
	}
	execAll(t, d,
		`INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (1, 4096, 11), (2, 64, 12), (3, 200, 13)`,
		`INSERT INTO notification_progress (thread_id, log_length, updated_at) VALUES (1, 4096, 21), (3, 150, 23)`,
	)
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var state, progress []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		state = rows(t, tx, `SELECT thread_id, log_length, updated_at FROM projection_state ORDER BY thread_id`)
		progress = rows(t, tx, `SELECT thread_id, log_length, updated_at FROM notification_progress ORDER BY thread_id`)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"[1 64 11]", "[2 64 12]", "[3 64 13]"}; !slices.Equal(state, want) {
		t.Errorf("after 0025 projection_state is %v, want %v", state, want)
	}
	if want := []string{"[1 4096 21]", "[3 150 23]"}; !slices.Equal(progress, want) {
		t.Errorf("after 0025 notification_progress is %v, want %v", progress, want)
	}
}

// TestPostMetaStaysTiny is AC2: exactly docs/04's columns, and besides the
// primary key exactly the two documented indexes. A column or index added for
// a within-thread question fails here, because that question belongs to the
// .idx file.
func TestPostMetaStaysTiny(t *testing.T) {
	d, _ := openTest(t)
	wantCols := []string{"thread_id", "ordinal", "author_id", "created_at", "current_revision",
		"edited_at", "reply_to", "deleted", "moved", "log_offset"}
	wantIndexes := map[string][]string{
		"post_meta_author":   {"author_id", "created_at"},
		"post_meta_reply_to": {"thread_id", "reply_to"},
	}
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		cols, err := strings1(tx, t, `SELECT name FROM pragma_table_info('post_meta') ORDER BY cid`)
		if err != nil {
			return err
		}
		if !slices.Equal(cols, wantCols) {
			t.Errorf("post_meta columns are %v, want %v", cols, wantCols)
		}
		names, err := strings1(tx, t, `SELECT name FROM pragma_index_list('post_meta') WHERE origin = 'c' ORDER BY name`)
		if err != nil {
			return err
		}
		if len(names) != len(wantIndexes) {
			t.Errorf("post_meta has secondary indexes %v, want exactly %d", names, len(wantIndexes))
		}
		for _, n := range names {
			got, err := strings1(tx, t, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, n)
			if err != nil {
				return err
			}
			if want, ok := wantIndexes[n]; !ok || !slices.Equal(got, want) {
				t.Errorf("index %s is on %v; want %v", n, got, wantIndexes[n])
			}
		}
		pk, err := strings1(tx, t, `SELECT name FROM pragma_table_info('post_meta') WHERE pk > 0 ORDER BY pk`)
		if err != nil {
			return err
		}
		if !slices.Equal(pk, []string{"thread_id", "ordinal"}) {
			t.Errorf("post_meta primary key is %v, want (thread_id, ordinal)", pk)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestProjectionsHaveNoForeignKeys: reindex rebuilds from the logs alone, into
// a database whose users may be older than the logs (settled in grooming), so
// no projection row may be refused for naming a user or forum.
func TestProjectionsHaveNoForeignKeys(t *testing.T) {
	d, _ := openTest(t)
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, table := range ProjectionTables {
			fks, err := strings1(tx, t, `SELECT "table" FROM pragma_foreign_key_list(?)`, table)
			if err != nil {
				return err
			}
			if len(fks) > 0 {
				t.Errorf("%s has foreign keys to %v", table, fks)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// And a row naming a user that does not exist is accepted.
	insertProjectionRows(t, d, 999_999)
}

// TestClearProjectionsEmptiesEveryTable: ClearProjections empties every table
// in ProjectionTables and touches nothing authoritative. forum_thread_counts
// also gets a row no thread accounts for, as a drift would leave, which
// ClearThreads' triggers do not remove and reindex must.
func TestClearProjectionsEmptiesEveryTable(t *testing.T) {
	d, _ := openTest(t)
	insertProjectionRows(t, d, 1)
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO forum_thread_counts (forum_id, threads) VALUES (99, 3)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := countRows(t, d, "groups")
	for _, table := range ProjectionTables {
		if countRows(t, d, table) == 0 {
			t.Fatalf("the fixture left %s empty, so clearing it proves nothing", table)
		}
	}
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return ClearProjections(t.Context(), q)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range ProjectionTables {
		if n := countRows(t, d, table); n != 0 {
			t.Errorf("%s still has %d rows after ClearProjections", table, n)
		}
	}
	if after := countRows(t, d, "groups"); after != before {
		t.Errorf("ClearProjections changed an authoritative table: groups went from %d to %d rows", before, after)
	}
}

// insertProjectionRows puts one row in every projection table for thread 42,
// written by author.
func insertProjectionRows(t *testing.T, d *DB, author int64) {
	t.Helper()
	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, s := range []struct {
			query string
			args  []any
		}{
			{`INSERT INTO threads (id, forum_id, slug, title, author_id, created_at, post_count, last_post_at,
			  last_post_author_id, last_post_ordinal, log_length_projected) VALUES (42, 7, 's', 'T', ?, 1, 1, 1, ?, 1, 100)`,
				[]any{author, author}},
			{`INSERT INTO thread_tags (thread_id, tag) VALUES (42, 'meta')`, nil},
			{`INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, log_offset) VALUES (42, 1, ?, 1, 64)`,
				[]any{author}},
			{`INSERT INTO mentions (thread_id, ordinal, mentioned_user_id) VALUES (42, 1, ?)`, []any{author}},
			{`INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (42, 100, 1)`, nil},
			{`INSERT INTO post_attachments (thread_id, ordinal, hash, position) VALUES (42, 1, ?, 0)`,
				[]any{strings.Repeat("ab", 32)}},
			{`INSERT INTO post_quotes (thread_id, ordinal, quoted_ordinal) VALUES (42, 2, 1)`, nil},
		} {
			if _, err := tx.ExecContext(t.Context(), s.query, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func countRows(t *testing.T, d *DB, table string) int {
	t.Helper()
	var n int
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM "`+table+`"`).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// strings1 runs a query returning one text column.
func strings1(tx *sql.Tx, t *testing.T, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
