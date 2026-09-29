package notify

import (
	"database/sql"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

func open(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return d
}

func exec(t *testing.T, d *db.DB, query string, args ...any) {
	t.Helper()
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), query, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// row is one notification as (user, kind, application, actor, at, read).
type row struct {
	user          int64
	kind          string
	app, actor    sql.NullInt64
	at            int64
	read          sql.NullInt64
	thread, ordin sql.NullInt64
}

func rows(t *testing.T, d *db.DB) []row {
	t.Helper()
	var out []row
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rs, err := tx.QueryContext(t.Context(), `SELECT user_id, kind, application_id, actor_id, at, read_at, thread_id,
			ordinal FROM notifications ORDER BY user_id, id`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.user, &r.kind, &r.app, &r.actor, &r.at, &r.read, &r.thread, &r.ordin); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rs.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// application writes a pending application with no identity and returns
// its id.
func application(t *testing.T, d *db.DB) int64 {
	t.Helper()
	var id int64
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `INSERT INTO applications (form_id, form_version, answers_json, status,
			created_at) VALUES (1, 1, '{}', 'pending', 1) RETURNING id`).Scan(&id)
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// AC1: one new_application row naming the application for each active user
// with role moderator or admin, agents included, and none for a member, a
// suspended or deactivated moderator or admin, or a member of the
// moderators or admins group without the role.
func TestEveryStaffMemberIsNotified(t *testing.T) {
	d := open(t)
	exec(t, d, `INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at) VALUES
		(1, 'admin', 'Admin', 'active', 'admin', 'human', '{}', 1, 1),
		(2, 'mod', 'Moderator', 'active', 'moderator', 'human', '{}', 1, 1),
		(3, 'plain', 'Member', 'active', 'member', 'human', '{}', 1, 1),
		(4, 'suspmod', 'Suspended moderator', 'suspended', 'moderator', 'human', '{}', 1, 1),
		(5, 'bot', 'Agent moderator', 'active', 'moderator', 'agent', '{}', 1, 1),
		(6, 'groupmod', 'In the moderators group', 'active', 'member', 'human', '{}', 1, 1),
		(7, 'gone', 'Deactivated admin', 'deactivated', 'admin', 'human', '{}', 1, 1),
		(8, 'groupadmin', 'In the admins group', 'active', 'member', 'human', '{}', 1, 1),
		(9, 'waiting', 'Pending admin', 'pending', 'admin', 'human', '{}', 1, 1)`)
	exec(t, d, `INSERT INTO group_members (group_id, user_id, source, added_at)
		SELECT id, 6, 'idp', 1 FROM groups WHERE slug = 'moderators'
		UNION ALL SELECT id, 8, 'idp', 1 FROM groups WHERE slug = 'admins'
		UNION ALL SELECT id, 4, 'idp', 1 FROM groups WHERE slug = 'moderators'`)
	app := application(t, d)
	now := time.UnixMilli(1_757_600_000_000)

	var n int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = NewApplication(t.Context(), q, app, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("wrote %d rows, want 3", n)
	}
	got := rows(t, d)
	var users []int64
	for _, r := range got {
		users = append(users, r.user)
		if r.kind != "new_application" || r.app.Int64 != app || r.actor.Valid || r.at != now.UnixMilli() ||
			r.read.Valid || r.thread.Valid || r.ordin.Valid {
			t.Errorf("row %+v, want an unread new_application naming %d at %d with no actor or post", r, app,
				now.UnixMilli())
		}
	}
	if !slices.Equal(users, []int64{1, 2, 5}) {
		t.Errorf("notified users %v, want the admin, the moderator and the agent moderator [1 2 5]", users)
	}
}

// With no staff at all, a submission still commits and writes nothing.
func TestNoStaffNoRows(t *testing.T) {
	d := open(t)
	app := application(t, d)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		n, err := NewApplication(t.Context(), q, app, time.UnixMilli(1))
		if n != 0 {
			t.Errorf("wrote %d rows with no staff", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// AC2's row: one application_decided row for the new member, naming the
// application and the deciding moderator, unread.
func TestApplicationDecidedNamesTheMember(t *testing.T) {
	d := open(t)
	exec(t, d, `INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at) VALUES
		(1, 'mod', 'Moderator', 'active', 'moderator', 'human', '{}', 1, 1),
		(2, 'new', 'Newcomer', 'active', 'member', 'human', '{}', 1, 1)`)
	app := application(t, d)
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		return ApplicationDecided(t.Context(), q, 2, app, 1, time.UnixMilli(42))
	}); err != nil {
		t.Fatal(err)
	}
	got := rows(t, d)
	want := []row{{user: 2, kind: "application_decided", app: sql.NullInt64{Int64: app, Valid: true},
		actor: sql.NullInt64{Int64: 1, Valid: true}, at: 42}}
	if !slices.Equal(got, want) {
		t.Errorf("rows %+v, want %+v", got, want)
	}
}

// AC4: notifications are in-app only. No Go file under internal/ or cmd/
// outside a test imports net/smtp, so nothing can send mail, and no mail
// setting is needed (docs/10 item 12).
func TestNoMailIsSent(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				switch e.Name() {
				case "node_modules", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			checked++
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); p == "net/smtp" || strings.HasPrefix(p, "net/smtp/") {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s imports %s. Notifications are in-app only (docs/10 item 12).", rel, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 50 {
		t.Fatalf("checked only %d Go files; the walk is not seeing the module", checked)
	}
}
