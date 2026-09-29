package audit

import (
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (id, handle, display_name, status, role, kind,
			prefs_json, created_at, updated_at) VALUES (7, 'alice', 'Alice', 'active', 'admin', 'human', '{}', 1, 1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

type row struct {
	at      int64
	actor   sql.NullInt64
	action  string
	typ, id string
	details map[string]any
	ip      string
}

// appendOne appends one row in its own write transaction and reads it back.
func appendOne(t *testing.T, d *db.DB, at time.Time, by By, action string, target Target, details map[string]any) row {
	t.Helper()
	var id int64
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		id, err = Append(t.Context(), q, at, by, action, target, details)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var r row
	var dj string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT at, actor_id, action, target_type, target_id, details_json, ip
			FROM audit_log WHERE id = ?`, id).Scan(&r.at, &r.actor, &r.action, &r.typ, &r.id, &dj, &r.ip)
	}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(dj), &r.details); err != nil {
		t.Fatalf("details_json %s: %v", dj, err)
	}
	return r
}

var when = time.UnixMilli(1_757_500_000_000)

// TestSystemActorIsNull is AC6: work nobody's account did is written with
// actor_id NULL and what did it in the details, and no row is ever written
// with actor id 0, however the caller spells "nobody".
func TestSystemActorIsNull(t *testing.T) {
	d := openDB(t)
	r := appendOne(t, d, when, System("expiry"), "user.suspension_expired", User(7), nil)
	if r.actor.Valid || r.details["via"] != "expiry" || len(r.details) != 1 {
		t.Errorf("System(expiry) wrote actor %v, details %v", r.actor, r.details)
	}
	appendOne(t, d, when, By{}, "setting.changed", Setting("site_name"), map[string]any{"via": "cli"})
	appendOne(t, d, when, By{Actor: 7, IP: "192.0.2.1"}, "user.promoted", User(7), nil)

	var zero, null, named int
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT count(*) FILTER (WHERE actor_id = 0),
			count(*) FILTER (WHERE actor_id IS NULL), count(*) FILTER (WHERE actor_id = 7) FROM audit_log`).
			Scan(&zero, &null, &named)
	}); err != nil {
		t.Fatal(err)
	}
	if zero != 0 || null != 2 || named != 1 {
		t.Errorf("rows with actor 0: %d, NULL: %d, 7: %d; want 0, 2, 1", zero, null, named)
	}

	// A negative id names nobody either, and is refused rather than written.
	err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := Append(t.Context(), q, when, By{Actor: -1}, "user.promoted", User(7), nil)
		return err
	})
	if err == nil {
		t.Error("Append accepted actor -1")
	}
}

// TestAppendTakesTheCallersTime: the projection writer's row carries the
// frame's timestamp, the frame's actor 0 as NULL, and no address, and Append
// writes exactly that rather than reading the clock.
func TestAppendTakesTheCallersTime(t *testing.T) {
	d := openDB(t)
	frame := time.UnixMilli(1_600_000_000_123)
	id, err := threadid.Parse("01k4z3j0w8x2c")
	if err != nil {
		t.Fatal(err)
	}
	r := appendOne(t, d, frame, By{}, "post.hidden", Post(id, 3), map[string]any{"reason": "spam"})
	if r.at != frame.UnixMilli() || r.actor.Valid || r.ip != "" {
		t.Errorf("at %d actor %v ip %q; want %d, NULL, empty", r.at, r.actor, r.ip, frame.UnixMilli())
	}
	if r.action != "post.hidden" || r.typ != "post" || r.id != "01k4z3j0w8x2c/3" || r.details["reason"] != "spam" {
		t.Errorf("row %+v", r)
	}
}

// TestViaIsTheActors: a By's Via is written as "via" and takes the key over
// from the details; with no Via the details are written as given.
func TestViaIsTheActors(t *testing.T) {
	d := openDB(t)
	r := appendOne(t, d, when, System("auto-hide"), "post.hidden", Flag(1), map[string]any{"via": "forged", "n": 3})
	if r.details["via"] != "auto-hide" || r.details["n"] != float64(3) {
		t.Errorf("details %v", r.details)
	}
	r = appendOne(t, d, when, By{Actor: 7}, "user.promoted", User(7), map[string]any{"via": "bootstrap"})
	if r.details["via"] != "bootstrap" || r.actor.Int64 != 7 {
		t.Errorf("details %v, actor %v", r.details, r.actor)
	}
	if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := Append(t.Context(), q, when, By{Actor: 7}, "", User(7), nil)
		return err
	}); err == nil {
		t.Error("Append accepted an empty action")
	}
}

// TestTargetFormats: each constructor's target_id, and a post target reads
// back into its thread and ordinal.
func TestTargetFormats(t *testing.T) {
	id, err := threadid.Parse("01k4z3j0w8x2c")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		got  Target
		want Target
	}{
		{User(12), Target{"user", "12"}},
		{Forum(3), Target{"forum", "3"}},
		{Group(4), Target{"group", "4"}},
		{Flag(99), Target{"flag", "99"}},
		{Application(5), Target{"application", "5"}},
		{Thread(id), Target{"thread", "01k4z3j0w8x2c"}},
		{Post(id, 42), Target{"post", "01k4z3j0w8x2c/42"}},
		{Setting("registration.mode"), Target{"setting", "registration.mode"}},
		{Form("registration"), Target{"form", "registration"}},
	} {
		if c.got != c.want {
			t.Errorf("got %+v, want %+v", c.got, c.want)
		}
		if !slices.Contains(Types, c.got.Type) {
			t.Errorf("%s is not in Types", c.got.Type)
		}
	}
	thread, ordinal, err := ParsePost(Post(id, 42).ID)
	if err != nil || thread != id || ordinal != 42 {
		t.Errorf("ParsePost = %v, %d, %v", thread, ordinal, err)
	}
	for _, bad := range []string{"01k4z3j0w8x2c", "01k4z3j0w8x2c/0", "01k4z3j0w8x2c/x", "nope/3", "01k4z3j0w8x2c/3/4"} {
		if _, _, err := ParsePost(bad); err == nil {
			t.Errorf("ParsePost(%q) accepted it", bad)
		}
	}
}

// TestTargetTypesDocumented is AC3 held in place: docs/04 "audit_log" lists
// every target type, and no type that does not exist.
func TestTargetTypesDocumented(t *testing.T) {
	b, err := os.ReadFile("../../docs/04-data-model.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	start := strings.Index(doc, "<!-- audit-targets:begin -->")
	end := strings.Index(doc, "<!-- audit-targets:end -->")
	if start < 0 || end < start {
		t.Fatal("docs/04 has no audit-targets:begin/audit-targets:end block")
	}
	var listed []string
	for _, line := range strings.Split(doc[start:end], "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		listed = append(listed, strings.Trim(strings.TrimSpace(cells[1]), "`"))
	}
	for _, typ := range Types {
		if !slices.Contains(listed, typ) {
			t.Errorf("docs/04 does not list the target type %q", typ)
		}
	}
	for _, typ := range listed {
		if !slices.Contains(Types, typ) {
			t.Errorf("docs/04 lists %q, which is not a target type", typ)
		}
	}
}

// TestOneWayIntoAuditLog is AC2: AppendAudit is called only here and in the
// store's own package. Everything else, the projection writer included,
// goes through Append, which is where actor 0 becomes NULL and "via" is
// written. Test files are exempt, since a fixture may write a row directly.
func TestOneWayIntoAuditLog(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	allowed := []string{
		filepath.Join(root, "internal", "audit") + string(filepath.Separator),
		filepath.Join(root, "internal", "store", "db") + string(filepath.Separator),
	}
	fset := token.NewFileSet()
	checked := 0
	err = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			switch e.Name() {
			case ".git", "spikes", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, a := range allowed {
			if strings.HasPrefix(path, a) {
				return nil
			}
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && (sel.Sel.Name == "AppendAudit" || sel.Sel.Name == "AppendAuditParams") {
				t.Errorf("%s uses %s; audit rows are written through audit.Append, "+
					"which writes a missing actor as NULL and records what acted",
					fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 20 {
		t.Fatalf("checked only %d Go files; the walk is not seeing the module", checked)
	}
}
