package dbtest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// snapshot is everything a migration run leaves in a database: the header
// pragmas that persist in the file, the schema, the migration record, and
// every row of every table. Migrations seed rows (the system groups, the
// default permissions, the registration form), so a template that matched
// only in schema could still hand a fixture a database missing them.
//
// schema_migrations.created_at is the one column left out, because it is when
// the migration ran rather than what it did.
func snapshot(t *testing.T, d *db.DB) (header, schema, recorded, rows []string) {
	t.Helper()
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, p := range []string{"user_version", "application_id", "page_size", "auto_vacuum", "journal_mode", "encoding"} {
			var v string
			if err := tx.QueryRowContext(t.Context(), "PRAGMA "+p).Scan(&v); err != nil {
				return err
			}
			header = append(header, p+"="+v)
		}
		var err error
		// The same query as schemaSnapshot in internal/store/db's
		// migrate_test.go, which is unexported there.
		if schema, err = strings1(tx, t, `SELECT type || ' ' || name || ' ' || coalesce(sql, '') FROM sqlite_schema ORDER BY type, name`); err != nil {
			return err
		}
		ms, err := q.ListMigrations(t.Context())
		if err != nil {
			return err
		}
		for _, m := range ms {
			recorded = append(recorded, fmt.Sprintf("%04d_%s %s", m.Version, m.Name, m.Checksum))
		}
		tables, err := strings1(tx, t, `SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name`)
		if err != nil {
			return err
		}
		for _, table := range tables {
			r, err := tableRows(tx, t, table)
			if err != nil {
				return err
			}
			rows = append(rows, r...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return header, schema, recorded, rows
}

func strings1(tx *sql.Tx, t *testing.T, query string) ([]string, error) {
	rows, err := tx.QueryContext(t.Context(), query)
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

// tableRows is every row of table as canonical text, sorted.
func tableRows(tx *sql.Tx, t *testing.T, table string) ([]string, error) {
	rows, err := tx.QueryContext(t.Context(), fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var out []string
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		var b strings.Builder
		b.WriteString(table + ":")
		for i, c := range cols {
			if table == "schema_migrations" && c == "created_at" {
				continue
			}
			fmt.Fprintf(&b, " %s=%s", c, format(vals[i]))
		}
		out = append(out, b.String())
	}
	slices.Sort(out)
	return out, rows.Err()
}

func freshMigration(t *testing.T) *db.DB {
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

// TestTemplateIsAFreshMigration is the template's contract: what Open
// returns and what db.Open plus Migrate returns cannot be told apart.
func TestTemplateIsAFreshMigration(t *testing.T) {
	fh, fs, fr, frows := snapshot(t, freshMigration(t))
	th, ts, tr, trows := snapshot(t, Open(t, db.Options{}))

	for _, c := range []struct {
		what        string
		fresh, tmpl []string
	}{
		{"header pragmas", fh, th},
		{"sqlite_schema", fs, ts},
		{"schema_migrations", fr, tr},
		{"rows", frows, trows},
	} {
		if len(c.fresh) == 0 {
			t.Errorf("a fresh migration has no %s; the snapshot is not reading the database", c.what)
		}
		if !slices.Equal(c.fresh, c.tmpl) {
			t.Errorf("the template's %s differ from a fresh migration's:\nfresh:\n  %s\ntemplate:\n  %s",
				c.what, strings.Join(c.fresh, "\n  "), strings.Join(c.tmpl, "\n  "))
		}
	}
}

// TestTemplateHasEveryMigration checks the template against the migration
// files on disk rather than against another migration run: a migration added
// to the directory is in the template with nothing to regenerate, and a
// template cached somewhere that outlived its binary would fail here.
func TestTemplateHasEveryMigration(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	name := regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)
	var want []string
	for _, e := range entries {
		if m := name.FindStringSubmatch(e.Name()); m != nil {
			want = append(want, m[1]+"_"+m[2])
		}
	}
	if len(want) < 17 {
		t.Fatalf("found %d migration files; is ../migrations the migrations directory?", len(want))
	}
	d := Open(t, db.Options{})
	var got []string
	err = d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		ms, err := q.ListMigrations(t.Context())
		for _, m := range ms {
			got = append(got, fmt.Sprintf("%04d_%s", m.Version, m.Name))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("the template records migrations\n  %s\nand the directory holds\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestTemplateOpensAreIndependent checks that each Open is a copy, not a
// shared file: what one test writes, the next does not see.
func TestTemplateOpensAreIndependent(t *testing.T) {
	first := Open(t, db.Options{})
	exec(t, first, `INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (1, 'alice', 'Alice', 'active', 'member', 'human', '{}', 1, 1)`)
	second := Open(t, db.Options{})

	count := func(d *db.DB) int {
		t.Helper()
		var n int
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM users WHERE handle = 'alice'`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(first); got != 1 {
		t.Fatalf("the first database has %d rows for alice, want the 1 it wrote", got)
	}
	if got := count(second); got != 0 {
		t.Fatalf("the second database has %d rows for alice, written through the first", got)
	}
	for i, d := range []*db.DB{first, second} {
		if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			var ok string
			if err := tx.QueryRowContext(t.Context(), `PRAGMA integrity_check`).Scan(&ok); err != nil {
				return err
			}
			if ok != "ok" {
				return fmt.Errorf("integrity_check: %s", ok)
			}
			var table string
			var rowid sql.NullInt64
			var parent string
			var fkid int64
			switch err := tx.QueryRowContext(t.Context(), `PRAGMA foreign_key_check`).Scan(&table, &rowid, &parent, &fkid); err {
			case sql.ErrNoRows:
				return nil
			case nil:
				return fmt.Errorf("foreign_key_check: row %d of %s names a missing %s", rowid.Int64, table, parent)
			default:
				return err
			}
		}); err != nil {
			t.Errorf("database %d: %v", i+1, err)
		}
	}
}

// TestTemplateMigratesOnce checks that Open copies rather than migrates: the
// template is built once in this binary however many databases are opened,
// and an opened database runs no DDL.
func TestTemplateMigratesOnce(t *testing.T) {
	// A statement from a migration script carries the comment lines above
	// it, so the keyword is looked for at the start of any line.
	writes := regexp.MustCompile(`(?im)^\s*(CREATE|DROP|ALTER|INSERT)\b`)
	var ddl atomic.Int32
	trace := func(sql string) {
		if writes.MatchString(sql) {
			ddl.Add(1)
		}
	}
	for range 3 {
		Open(t, db.Options{Trace: trace})
	}
	if n := builds.Load(); n != 1 {
		t.Errorf("the template was built %d times in this binary, want 1", n)
	}
	if n := ddl.Load(); n != 0 {
		t.Errorf("opening three databases ran %d CREATE, DROP, ALTER or INSERT statements; Open migrated", n)
	}
}

// TestOpenRefusesAnExistingDatabase checks that Open never overwrites a file
// a test put there.
func TestOpenRefusesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ketju.db")
	if err := os.WriteFile(path, []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeImage(path, []byte("template")); err == nil {
		t.Fatal("writeImage overwrote an existing file")
	}
	if b, _ := os.ReadFile(path); string(b) != "not ours" {
		t.Fatalf("the existing file now holds %q", b)
	}
	if err := os.WriteFile(path+"2-wal", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeImage(path+"2", []byte("template")); err == nil {
		t.Fatal("writeImage created a database beside a stale WAL")
	}
}
