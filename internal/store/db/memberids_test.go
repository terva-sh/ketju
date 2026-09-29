package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// migrationsBefore is the embedded migrations numbered below v, as a
// directory "m" for DB.migrate.
func migrationsBefore(t *testing.T, v int) fstest.MapFS {
	t.Helper()
	all, err := loadMigrations(migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{}
	for _, m := range all[:v-1] {
		fsys[fmt.Sprintf("m/%04d_%s.sql", m.version, m.name)] = &fstest.MapFile{Data: []byte(m.sql)}
	}
	return fsys
}

// rows is every row query returns, one line each.
func rows(t *testing.T, tx *sql.Tx, query string, args ...any) []string {
	t.Helper()
	rs, err := tx.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rs.Close()
	cols, err := rs.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%v", vals))
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// execAll runs statements in one write transaction and fails the test on any
// error.
func execAll(t *testing.T, d *DB, stmts ...string) {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, s := range stmts {
			if _, err := tx.ExecContext(t.Context(), s); err != nil {
				return fmt.Errorf("%s: %w", s, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func createUser(t *testing.T, d *DB, handle string) int64 {
	t.Helper()
	var id int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestUsersAutoincrementMigration is TKT-01M3KB7FXC's migration: 0010
// rebuilds a populated users table with AUTOINCREMENT, keeps every row and
// id, and keeps every foreign key that names users (id) naming it and
// holding. The next member is given an id above the highest one, and
// foreign keys are enforced again afterwards.
func TestUsersAutoincrementMigration(t *testing.T) {
	d, _ := openUnmigrated(t)
	if err := d.migrate(t.Context(), migrationsBefore(t, 10), "m"); err != nil {
		t.Fatal(err)
	}
	// Ids with a gap, so max(id) is not the row count, and a row with every
	// column set, so a column the copy drops or reorders shows.
	execAll(t, d,
		`INSERT INTO users (id, handle, display_name, avatar_blob, status, role, kind, bio_source, timezone,
			locale, prefs_json, mark_all_read_at, created_at, updated_at, last_seen_at)
		 VALUES (1, 'alice', 'Alice', 'abc123', 'active', 'admin', 'human', 'hi', 'Europe/Helsinki',
			'fi', '{"x":1}', 7, 10, 11, 12)`,
		`INSERT INTO users (id, handle, display_name, created_at, updated_at) VALUES (2, 'bot', 'Bot', 1, 1)`,
		`UPDATE users SET kind = 'agent', status = 'suspended' WHERE id = 2`,
		`INSERT INTO users (id, handle, display_name, created_at, updated_at) VALUES (5, 'carol', 'Carol', 1, 1)`,
		`INSERT INTO identities (id, user_id, provider_key, subject, issuer, created_at, updated_at)
		 VALUES (1, 1, 'idp', 'sub-1', 'https://idp', 1, 1)`,
		`INSERT INTO local_credentials (user_id, password_hash, created_at, updated_at) VALUES (2, 'h', 1, 1)`,
		`INSERT INTO sessions (id_hash, user_id, kind, created_at, expires_at, last_used_at)
		 VALUES (zeroblob(32), 5, 'user', 1, 2, 1)`,
		`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES (5, 42, 3, 1)`,
		`INSERT INTO groups (id, name, slug, created_at, updated_at) VALUES (100, 'Editors', 'editors', 1, 1)`,
		`INSERT INTO group_members (group_id, user_id, source, added_by, added_at) VALUES (100, 5, 'admin', 1, 1)`,
	)

	// Every table's foreign keys to users, and the rows of the tables above.
	const fks = `SELECT m.name, f."from" FROM sqlite_schema m, pragma_foreign_key_list(m.name) f
		WHERE m.type = 'table' AND f."table" = 'users' ORDER BY 1, 2`
	snapshot := func() (users, refs, children []string) {
		err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			users = rows(t, tx, `SELECT * FROM users ORDER BY id`)
			refs = rows(t, tx, fks)
			for _, q := range []string{
				`SELECT * FROM identities`, `SELECT * FROM local_credentials`, `SELECT * FROM sessions`,
				`SELECT * FROM read_positions`, `SELECT * FROM group_members`,
			} {
				children = append(children, strings.Join(rows(t, tx, q), ";"))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return users, refs, children
	}
	usersBefore, refsBefore, childrenBefore := snapshot()
	if len(refsBefore) < 10 {
		t.Fatalf("only %d foreign keys name users before 0010: %v", len(refsBefore), refsBefore)
	}

	// Through 0010 only: later migrations add tables and columns of their
	// own, which the before-and-after comparison would read as changes.
	if err := d.migrate(t.Context(), migrationsBefore(t, 11), "m"); err != nil {
		t.Fatal(err)
	}

	usersAfter, refsAfter, childrenAfter := snapshot()
	if strings.Join(usersAfter, "\n") != strings.Join(usersBefore, "\n") {
		t.Errorf("users changed:\n%s\n---\n%s", strings.Join(usersBefore, "\n"), strings.Join(usersAfter, "\n"))
	}
	if strings.Join(refsAfter, "\n") != strings.Join(refsBefore, "\n") {
		t.Errorf("foreign keys to users changed:\n%v\n---\n%v", refsBefore, refsAfter)
	}
	if strings.Join(childrenAfter, "\n") != strings.Join(childrenBefore, "\n") {
		t.Errorf("rows referencing users changed:\n%v\n---\n%v", childrenBefore, childrenAfter)
	}
	err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if v := rows(t, tx, `PRAGMA foreign_key_check`); len(v) != 0 {
			t.Errorf("foreign_key_check after 0010 = %v", v)
		}
		if ddl := rows(t, tx, `SELECT sql FROM sqlite_schema WHERE name = 'users'`); len(ddl) != 1 ||
			!strings.Contains(ddl[0], "INTEGER PRIMARY KEY AUTOINCREMENT") {
			t.Errorf("users is %v, want AUTOINCREMENT", ddl)
		}
		if left := rows(t, tx, `SELECT name FROM sqlite_schema WHERE name LIKE 'users_%'`); len(left) != 0 {
			t.Errorf("the rebuild left %v behind", left)
		}
		floor, err := MemberIDFloor(t.Context(), tx)
		if err != nil || floor != 5 {
			t.Errorf("the floor after 0010 = %d, %v; want 5, the highest id", floor, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if id := createUser(t, d, "dave"); id != 6 {
		t.Errorf("the next member got id %d, want 6", id)
	}

	// Foreign keys hold again once the migration is done: the runner put
	// the pragma back on the write connection.
	for _, bad := range []string{
		`INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES (99, 1, 1, 1)`,
		`DELETE FROM users WHERE id = 1`,
	} {
		err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), bad)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("%s after migrating: %v, want a foreign key failure", bad, err)
		}
	}
}

// TestMigrateRefusesAForeignKeyViolation: foreign keys are off while
// migrations run, so the runner checks them itself before it commits. A
// migration that leaves a row naming a parent that does not exist is
// refused and changes nothing, and the connection enforces foreign keys
// again afterwards.
func TestMigrateRefusesAForeignKeyViolation(t *testing.T) {
	d, _ := openUnmigrated(t)
	parentChild := "CREATE TABLE p (id INTEGER PRIMARY KEY);\n" +
		"CREATE TABLE c (id INTEGER PRIMARY KEY, p INTEGER REFERENCES p (id));\n"
	bad := mapFS(map[string]string{
		"0001_init.sql":   mustReadMigration("0001_init.sql"),
		"0002_orphan.sql": parentChild + "INSERT INTO c VALUES (1, 9);",
	})
	err := d.migrate(t.Context(), bad, "m")
	if !errors.Is(err, ErrMigrations) || !strings.Contains(err.Error(), "row 1 of c names a p row") {
		t.Fatalf("migrating an orphan: %v, want ErrMigrations naming the row", err)
	}
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if left := rows(t, tx, `SELECT name FROM sqlite_schema`); len(left) != 0 {
			t.Errorf("a refused migration left %v", left)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	good := mapFS(map[string]string{
		"0001_init.sql":   mustReadMigration("0001_init.sql"),
		"0002_orphan.sql": parentChild,
	})
	if err := d.migrate(t.Context(), good, "m"); err != nil {
		t.Fatal(err)
	}
	err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO c VALUES (1, 9)`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("an orphan after migrating: %v, want a foreign key failure", err)
	}
}

// TestRaiseMemberIDFloor: the floor only goes up, a new member is given an
// id above it, and raising it to where it is, or below, writes nothing.
func TestRaiseMemberIDFloor(t *testing.T) {
	for _, c := range []struct {
		name    string
		members []string // created first, from id 1
		raise   int64
		before  int64
		after   int64
		next    int64
	}{
		{"an empty database", nil, 40, 0, 40, 41},
		{"behind the logs", []string{"alice", "bob"}, 40, 2, 40, 41},
		{"level with the logs", []string{"alice", "bob"}, 2, 2, 2, 3},
		{"ahead of the logs", []string{"alice", "bob", "carol"}, 2, 3, 3, 4},
		{"no ids in the logs", []string{"alice"}, 0, 1, 1, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, _ := openTest(t)
			for _, h := range c.members {
				createUser(t, d, h)
			}
			var before, after int64
			if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
				var err error
				before, after, err = RaiseMemberIDFloor(t.Context(), tx, c.raise)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if before != c.before || after != c.after {
				t.Errorf("RaiseMemberIDFloor(%d) = %d, %d; want %d, %d", c.raise, before, after, c.before, c.after)
			}
			if id := createUser(t, d, "newcomer"); id != c.next {
				t.Errorf("the next member got id %d, want %d", id, c.next)
			}
		})
	}
}
