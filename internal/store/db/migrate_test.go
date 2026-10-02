package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// schemaSnapshot is everything a migration run could change: the schema and
// the migration record.
func schemaSnapshot(t *testing.T, d *DB) (schema []string, recorded []dbq.SchemaMigration) {
	t.Helper()
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(),
			`SELECT type || ' ' || name || ' ' || coalesce(sql, '') FROM sqlite_schema ORDER BY type, name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			schema = append(schema, s)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		recorded, err = q.ListMigrations(t.Context())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return schema, recorded
}

func mapFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys["m/"+name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

var twoMigrations = map[string]string{
	"0001_init.sql":  mustReadMigration("0001_init.sql"),
	"0002_thing.sql": "CREATE TABLE thing (id INTEGER PRIMARY KEY, created_at INTEGER NOT NULL);",
}

func mustReadMigration(name string) string {
	b, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestMigrateTwiceIsNoop is AC3: migrations run in order, are recorded, and a
// second run changes nothing.
func TestMigrateTwiceIsNoop(t *testing.T) {
	d, path := openUnmigrated(t)
	if err := d.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	schema1, rec1 := schemaSnapshot(t, d)
	if len(rec1) != 2 || rec1[0].Version != 1 || rec1[1].Version != 2 || rec1[1].Name != "thing" {
		t.Fatalf("recorded migrations = %+v, want versions 1 and 2 with names", rec1)
	}
	if !strings.Contains(strings.Join(schema1, "\n"), "table thing") {
		t.Fatal("migration 0002 did not create its table")
	}

	// Second run in the same process, then a second run after a restart.
	if err := d.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d2, err := Open(t.Context(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	if err := d2.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	schema2, rec2 := schemaSnapshot(t, d2)
	if !reflect.DeepEqual(schema1, schema2) || !reflect.DeepEqual(rec1, rec2) {
		t.Fatalf("a second migration run changed the database:\nbefore %v %+v\nafter  %v %+v", schema1, rec1, schema2, rec2)
	}
}

// TestMigrateAppliesInOrder checks that a later migration can depend on an
// earlier one, which only works if they run in numeric order and not in
// directory order.
func TestMigrateAppliesInOrder(t *testing.T) {
	d, _ := openUnmigrated(t)
	files := map[string]string{
		"0001_init.sql": mustReadMigration("0001_init.sql"),
		"0010_c.sql":    "ALTER TABLE a ADD COLUMN c INTEGER;",
		"0002_a.sql":    "CREATE TABLE a (id INTEGER PRIMARY KEY);",
	}
	for i := 3; i <= 9; i++ {
		files[fmt.Sprintf("%04d_pad.sql", i)] = "SELECT 1;"
	}
	if err := d.migrate(t.Context(), mapFS(files), "m"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateRefusesBadSets(t *testing.T) {
	init := mustReadMigration("0001_init.sql")
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"gap", map[string]string{"0001_init.sql": init, "0003_x.sql": "SELECT 1;"}, "expected migration 0002"},
		{"duplicate", map[string]string{"0001_init.sql": init, "0002_a.sql": "SELECT 1;", "0002_b.sql": "SELECT 1;"}, "two migrations are numbered 0002"},
		{"misnamed", map[string]string{"0001_init.sql": init, "2_x.sql": "SELECT 1;"}, "not named NNNN_name.sql"},
		{"empty", map[string]string{}, "no migrations found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, _ := openUnmigrated(t)
			fsys := mapFS(c.files)
			if len(c.files) == 0 {
				fsys = fstest.MapFS{"m": &fstest.MapFile{Mode: fs.ModeDir}}
			}
			err := d.migrate(t.Context(), fsys, "m")
			if !errors.Is(err, ErrMigrations) {
				t.Fatalf("err = %v, want ErrMigrations", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestMigrateRefusesChecksumDrift: a migration edited after it ran is refused
// rather than skipped, because the database no longer matches the file.
func TestMigrateRefusesChecksumDrift(t *testing.T) {
	d, _ := openUnmigrated(t)
	if err := d.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	edited := map[string]string{
		"0001_init.sql":  twoMigrations["0001_init.sql"],
		"0002_thing.sql": twoMigrations["0002_thing.sql"] + "\n-- an innocent comment",
	}
	err := d.migrate(t.Context(), mapFS(edited), "m")
	if !errors.Is(err, ErrMigrations) || !strings.Contains(err.Error(), "0002_thing") {
		t.Fatalf("err = %v, want ErrMigrations naming 0002_thing", err)
	}

	renamed := map[string]string{
		"0001_init.sql":  twoMigrations["0001_init.sql"],
		"0002_other.sql": twoMigrations["0002_thing.sql"],
	}
	if err := d.migrate(t.Context(), mapFS(renamed), "m"); !errors.Is(err, ErrMigrations) {
		t.Fatalf("renamed migration: err = %v, want ErrMigrations", err)
	}
}

// TestNewerSchemaRefused is AC4: a binary that knows fewer migrations than the
// database has recorded refuses, and says which way round the mismatch is.
func TestNewerSchemaRefused(t *testing.T) {
	d, _ := openUnmigrated(t)
	if err := d.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	older := map[string]string{"0001_init.sql": twoMigrations["0001_init.sql"]}
	err := d.migrate(t.Context(), mapFS(older), "m")
	if !errors.Is(err, ErrSchemaNewer) {
		t.Fatalf("err = %v, want ErrSchemaNewer", err)
	}
	for _, want := range []string{"schema version 2", "knows up to 1", "this binary is older than the database"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not say %q", err, want)
		}
	}
}

// TestPendingCountsWithoutWriting: Pending counts the migrations a database
// lacks, writes nothing, and answers ErrSchemaNewer for a database a newer
// binary migrated.
func TestPendingCountsWithoutWriting(t *testing.T) {
	d, _ := openUnmigrated(t)
	older := map[string]string{"0001_init.sql": twoMigrations["0001_init.sql"]}
	if n, err := d.pending(t.Context(), mapFS(older), "m"); err != nil || n != 1 {
		t.Fatalf("an unmigrated database: pending = %d, %v; want 1", n, err)
	}
	if err := d.migrate(t.Context(), mapFS(older), "m"); err != nil {
		t.Fatal(err)
	}
	before, recorded := schemaSnapshot(t, d)
	if n, err := d.pending(t.Context(), mapFS(twoMigrations), "m"); err != nil || n != 1 {
		t.Fatalf("one behind: pending = %d, %v; want 1", n, err)
	}
	if after, again := schemaSnapshot(t, d); !slices.Equal(before, after) || len(again) != len(recorded) {
		t.Errorf("Pending changed the schema or the record")
	}
	if n, err := d.pending(t.Context(), mapFS(older), "m"); err != nil || n != 0 {
		t.Errorf("current: pending = %d, %v; want 0", n, err)
	}
	if err := d.migrate(t.Context(), mapFS(twoMigrations), "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.pending(t.Context(), mapFS(older), "m"); !errors.Is(err, ErrSchemaNewer) {
		t.Errorf("a newer database: err = %v, want ErrSchemaNewer", err)
	}
}

// TestFailedMigrationLeavesNothing: a migration that fails partway is rolled
// back whole and not recorded, so fixing it and rerunning starts clean.
func TestFailedMigrationLeavesNothing(t *testing.T) {
	d, _ := openUnmigrated(t)
	init := map[string]string{"0001_init.sql": twoMigrations["0001_init.sql"]}
	if err := d.migrate(t.Context(), mapFS(init), "m"); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"0001_init.sql": twoMigrations["0001_init.sql"],
		"0002_bad.sql":  "CREATE TABLE half (id INTEGER); CREATE TABLE half (id INTEGER);",
	}
	if err := d.migrate(t.Context(), mapFS(bad), "m"); err == nil {
		t.Fatal("a failing migration reported success")
	}
	schema, rec := schemaSnapshot(t, d)
	if strings.Contains(strings.Join(schema, "\n"), "table half") {
		t.Fatal("the failed migration's first statement was left behind")
	}
	if len(rec) != 1 {
		t.Fatalf("recorded = %+v, want only 0001", rec)
	}
}

// TestEmbeddedMigrationsLoad keeps the shipped set well-formed: a misnamed
// file under migrations/ fails here, not at a user's first start.
func TestEmbeddedMigrationsLoad(t *testing.T) {
	ms, err := loadMigrations(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].name != "init" {
		t.Fatalf("embedded migrations = %+v", ms)
	}
}

// TestEveryMigrationFileIsEmbedded: whatever sits in migrations/ on disk is in
// the binary, so loadMigrations sees a misnamed file and refuses it rather
// than the embed pattern hiding it. No name is exempt: a plain directory embed
// skips names starting with '.' or '_', which is why the directive says all:.
func TestEveryMigrationFileIsEmbedded(t *testing.T) {
	onDisk, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	for _, e := range embedded {
		in[e.Name()] = true
	}
	for _, e := range onDisk {
		if n := e.Name(); !in[n] {
			t.Errorf("migrations/%s is on disk but not embedded, so the runner would never see it", n)
		}
	}
}

// TestStrayExtensionRefused: the case the whole-directory embed exists for.
func TestStrayExtensionRefused(t *testing.T) {
	d, _ := openUnmigrated(t)
	for _, stray := range []string{"0002_thing.sq", "_0002_thing.sql", ".0002_thing.sql.swp"} {
		files := map[string]string{"0001_init.sql": twoMigrations["0001_init.sql"], stray: "SELECT 1;"}
		if err := d.migrate(t.Context(), mapFS(files), "m"); !errors.Is(err, ErrMigrations) {
			t.Fatalf("err = %v, want ErrMigrations for %s", err, stray)
		}
	}
}

// TestMigrateRefusesABadRecord: a schema_migrations table that is not exactly
// 1..n is refused with ErrMigrations, never a panic, whatever the embedded
// set. A clean 1..n longer than the embedded set is a newer schema instead.
func TestMigrateRefusesABadRecord(t *testing.T) {
	one := map[string]string{"0001_init.sql": twoMigrations["0001_init.sql"]}
	for _, c := range []struct {
		name    string
		records []int64
		want    error
	}{
		// The review's example: rows 0 and 1 against one embedded migration.
		{"a zero before the real row", []int64{0}, ErrMigrations},
		{"a gap", []int64{3}, ErrMigrations},
		{"a clean run past the embedded set", []int64{2, 3}, ErrSchemaNewer},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, _ := openUnmigrated(t)
			if err := d.migrate(t.Context(), mapFS(one), "m"); err != nil { // records version 1
				t.Fatal(err)
			}
			err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
				for _, v := range c.records {
					if err := q.InsertMigration(t.Context(), dbq.InsertMigrationParams{Version: v, Name: "x", Checksum: "x"}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = d.migrate(t.Context(), mapFS(one), "m")
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}
