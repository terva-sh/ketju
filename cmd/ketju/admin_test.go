package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// dataDir is a data directory as serve leaves it, with users of the given
// roles by handle. The database is closed again: nothing is running.
func dataDir(t *testing.T, users map[string]string) string {
	t.Helper()
	root := t.TempDir()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(root, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		for handle, role := range users {
			if _, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
				Status: "active", Role: role, Kind: "human", CreatedAt: 1, UpdatedAt: 1}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	return root
}

// roleIn reopens the database and reads handle's role and the promotions
// audit_log records for them, with the actor of the last.
func roleIn(t *testing.T, root, handle string) (role string, promotions int, actor sql.NullInt64, details string) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(root, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		if err := tx.QueryRowContext(t.Context(), "SELECT role FROM users WHERE handle = ?", handle).Scan(&role); err != nil {
			return err
		}
		return tx.QueryRowContext(t.Context(), `SELECT count(*), max(actor_id), coalesce(max(details_json), '')
			FROM audit_log a JOIN users u ON a.target_id = CAST(u.id AS TEXT)
			WHERE a.action = 'user.promoted' AND a.target_type = 'user' AND u.handle = ?`, handle).
			Scan(&promotions, &actor, &details)
	}); err != nil {
		t.Fatal(err)
	}
	return
}

func runPromote(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := admin(t.Context(), &out, &errOut, append([]string{"promote"}, args...))
	return out.String(), errOut.String(), err
}

// AC3: promote works from the data directory alone, against a stopped
// instance, finding the handle whatever its case, and records it.
func TestAdminPromote(t *testing.T) {
	root := dataDir(t, map[string]string{"alice": "member", "bob": "member"})
	out, _, err := runPromote(t, "--root", root, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if out != "alice is now an admin\n" {
		t.Errorf("said %q", out)
	}
	role, n, actor, details := roleIn(t, root, "alice")
	if role != "admin" || n != 1 || actor.Valid || !strings.Contains(details, `"via":"cli"`) {
		t.Errorf("alice: role %s, %d promotions, actor %v, details %s", role, n, actor, details)
	}
	if role, n, _, _ := roleIn(t, root, "bob"); role != "member" || n != 0 {
		t.Errorf("bob: role %s, %d promotions", role, n)
	}

	// Again: nothing changes and nothing more is recorded.
	out, _, err = runPromote(t, "--root", root, "alice")
	if err != nil || out != "alice is already an admin\n" {
		t.Errorf("second promote = %q, %v", out, err)
	}
	if _, n, _, _ := roleIn(t, root, "alice"); n != 1 {
		t.Errorf("%d promotions after promoting an admin", n)
	}
}

// Settled in grooming: promote may run while serve holds the database open,
// since WAL and busy_timeout serialise the two writers.
func TestAdminPromoteBesideAnOpenDatabase(t *testing.T) {
	root := dataDir(t, map[string]string{"alice": "member"})
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(root, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := runPromote(t, "--root", root, "alice"); err != nil {
		t.Fatal(err)
	}
	var role string
	if err := d.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		u, err := q.GetUserByHandle(t.Context(), "alice")
		role = u.Role
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if role != "admin" {
		t.Errorf("the open handle reads role %s", role)
	}
}

func TestAdminPromoteRefuses(t *testing.T) {
	root := dataDir(t, map[string]string{"alice": "member"})
	empty := t.TempDir()
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"unknown handle", []string{"--root", root, "carol"}, exitNotFound, `no member has the handle "carol"`},
		{"no database", []string{"--root", empty, "alice"}, exitInternal, "has no database"},
		{"no handle", []string{"--root", root}, exitUsage, "exactly one handle"},
		{"two handles", []string{"--root", root, "alice", "bob"}, exitUsage, "exactly one handle"},
		{"bad flag", []string{"--nope", "alice"}, exitUsage, "nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := runPromote(t, c.args...)
			if err == nil || exitCodeFor(err) != c.code || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err %v (exit %d), want exit %d mentioning %q", err, exitCodeFor(err), c.code, c.want)
			}
		})
	}
	// A mistyped --root leaves nothing behind.
	if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
		t.Errorf("the empty directory now holds %v (%v)", entries, err)
	}
}

func TestAdminSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := admin(t.Context(), &out, &errOut, nil); exitCodeFor(err) != exitUsage {
		t.Errorf("no subcommand: %v", err)
	}
	if err := admin(t.Context(), &out, &errOut, []string{"demote", "alice"}); exitCodeFor(err) != exitUsage {
		t.Errorf("unknown subcommand: %v", err)
	}
	out.Reset()
	if err := admin(t.Context(), &out, &errOut, []string{"help"}); err != nil || !strings.Contains(out.String(), "promote") {
		t.Errorf("help: %q, %v", out.String(), err)
	}
}
