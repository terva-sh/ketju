package db

import (
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// newFlag flags post 3 of thread 42 as reporter.
func newFlag(t *testing.T, d *DB, reporter int64, reason, note string) (int64, error) {
	t.Helper()
	return flagPost(t, d, 3, reporter, reason, note)
}

// flagPost flags post ordinal of thread 42 as reporter. A reporter's repeat
// of their own open flag answers sql.ErrNoRows.
func flagPost(t *testing.T, d *DB, ordinal, reporter int64, reason, note string) (int64, error) {
	t.Helper()
	var id int64
	err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateFlag(t.Context(), dbq.CreateFlagParams{ThreadID: 42, Ordinal: ordinal,
			ReporterID: reporter, Reason: reason, Note: note, CreatedAt: 10})
		return err
	})
	return id, err
}

// TestFlagStatusAndNoDelete is AC1: the documented statuses and reasons are
// the only ones, a closed flag carries its resolution, only an open flag can
// be closed, and no flag is ever deleted.
func TestFlagStatusAndNoDelete(t *testing.T) {
	d, _ := openTest(t)
	reporter, mod := newUser(t, d, "reporter"), newUser(t, d, "mod")

	// Each on a post of its own: one reporter has one open flag per post.
	for i, reason := range []string{"spam", "abuse", "off-topic"} {
		if _, err := flagPost(t, d, int64(10+i), reporter, reason, ""); err != nil {
			t.Errorf("reason %s: %v", reason, err)
		}
	}
	if _, err := newFlag(t, d, reporter, "rude", ""); err == nil {
		t.Error("an undocumented reason was accepted")
	}
	if _, err := newFlag(t, d, reporter, "other", ""); err == nil {
		t.Error(`"other" with no note was accepted`)
	}
	id, err := newFlag(t, d, reporter, "other", "a link to a scam")
	if err != nil {
		t.Fatal(err)
	}

	closeFlag := func(status string, by int64) (int64, error) {
		var n int64
		err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			var err error
			n, err = q.CloseFlag(t.Context(), dbq.CloseFlagParams{Status: status,
				ResolvedBy: sql.NullInt64{Int64: by, Valid: true}, ResolvedAt: sql.NullInt64{Int64: 20, Valid: true},
				ResolutionNote: "removed the link", ID: id})
			return err
		})
		return n, err
	}
	if _, err := closeFlag("closed", mod); err == nil {
		t.Error(`status "closed" was accepted`)
	}
	if n, err := closeFlag("resolved", mod); err != nil || n != 1 {
		t.Fatalf("resolving an open flag: %d rows, %v", n, err)
	}
	if n, err := closeFlag("dismissed", mod); err != nil || n != 0 {
		t.Errorf("closing an already resolved flag changed %d rows, %v", n, err)
	}
	var f dbq.Flag
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		f, err = q.GetFlag(t.Context(), id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if f.Status != "resolved" || f.ResolutionNote != "removed the link" || f.ResolvedBy.Int64 != mod || f.ResolvedAt.Int64 != 20 {
		t.Errorf("the resolved flag reads back as %+v", f)
	}

	// The resolution and the status move together.
	if err := exec(t, d, `UPDATE flags SET status = 'dismissed', resolved_by = NULL, resolved_at = NULL WHERE id = ?`, id); err == nil {
		t.Error("a closed flag with no resolver was accepted")
	}
	if err := exec(t, d, `INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at, resolved_by, resolved_at)
		VALUES (1, 1, ?, 'spam', 1, ?, 2)`, reporter, mod); err == nil {
		t.Error("an open flag with a resolver was accepted")
	}

	if err := exec(t, d, `DELETE FROM flags WHERE id = ?`, id); err == nil {
		t.Error("a flag was deleted")
	}
	// REPLACE deletes the old row without firing DELETE triggers
	// (recursive_triggers is off by default), so it is refused at the insert.
	for _, stmt := range []string{
		`INSERT OR REPLACE INTO flags (id, thread_id, ordinal, reporter_id, reason, created_at) VALUES (?, 1, 1, ?, 'spam', 1)`,
		`REPLACE INTO flags (id, thread_id, ordinal, reporter_id, reason, created_at) VALUES (?, 1, 1, ?, 'spam', 1)`,
	} {
		if err := exec(t, d, stmt, id, reporter); err == nil {
			t.Errorf("%s replaced a flag", stmt)
		}
	}
	if err := exec(t, d, `DELETE FROM flags`); err == nil {
		t.Error("the flags table was emptied")
	}
}

// TestOpenFlagQueries: the queue lists open flags only, newest first, and a
// post's open-flag count is what the auto-hide threshold reads.
func TestOpenFlagQueries(t *testing.T) {
	d, _ := openTest(t)
	reporter, second := newUser(t, d, "reporter"), newUser(t, d, "second")
	for i, at := range []int64{10, 30, 20} {
		mustExec(t, d, `INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at) VALUES (42, ?, ?, 'spam', ?)`,
			i+1, reporter, at)
	}
	mustExec(t, d, `INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at, status, resolved_by, resolved_at)
		VALUES (42, 1, ?, 'spam', 40, 'dismissed', ?, 41)`, reporter, reporter)
	mustExec(t, d, `INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at) VALUES (42, 1, ?, 'abuse', 50)`, second)

	var open []dbq.Flag
	var count int64
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		if open, err = q.OpenFlags(t.Context(), 10); err != nil {
			return err
		}
		count, err = q.OpenFlagCount(t.Context(), dbq.OpenFlagCountParams{ThreadID: 42, Ordinal: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var at []int64
	for _, f := range open {
		at = append(at, f.CreatedAt)
	}
	if fmt.Sprint(at) != "[50 30 20 10]" {
		t.Errorf("the queue is %v, want the four open flags newest first", at)
	}
	if count != 2 {
		t.Errorf("post 1 has %d open flags, want 2 (one of three is dismissed)", count)
	}
}

// TestOneOpenFlagPerReporter (TKT-01M29CP2PJ, migration 0014): a
// reporter's second open flag on a post adds no row, through CreateFlag and
// through a raw insert alike, so the open count counts reporters. Another
// reporter, another post, and the same reporter after their flag was closed
// each add one. The CHECK on other still refuses a note-less flag, which a
// blanket OR IGNORE would have swallowed.
func TestOneOpenFlagPerReporter(t *testing.T) {
	d, _ := openTest(t)
	alice, bob, mod := newUser(t, d, "alice"), newUser(t, d, "bob"), newUser(t, d, "mod")
	first, err := flagPost(t, d, 1, alice, "spam", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flagPost(t, d, 1, alice, "abuse", "again"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a repeat flag: %v, want sql.ErrNoRows (no row added)", err)
	}
	if err := exec(t, d, `INSERT INTO flags (thread_id, ordinal, reporter_id, reason, created_at) VALUES (42, 1, ?, 'spam', 1)`, alice); err == nil {
		t.Error("a raw insert of a second open flag by one reporter was accepted")
	}
	if _, err := flagPost(t, d, 1, alice, "other", ""); err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Errorf(`a repeat "other" with no note: %v, want the CHECK's refusal`, err)
	}
	if _, err := flagPost(t, d, 1, bob, "spam", ""); err != nil {
		t.Errorf("another reporter: %v", err)
	}
	if _, err := flagPost(t, d, 2, alice, "spam", ""); err != nil {
		t.Errorf("another post: %v", err)
	}
	if got := openCount(t, d, 1); got != 2 {
		t.Errorf("post 1 has %d open flags, want 2 (alice and bob)", got)
	}

	// Dismissed, alice may flag again, and a reopen of the old flag then
	// leaves it dismissed rather than breaking the index.
	var dismissed []int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		dismissed, err = q.DismissOpenFlagsForPost(t.Context(), dbq.DismissOpenFlagsForPostParams{
			ResolvedBy: sql.NullInt64{Int64: mod, Valid: true}, ResolvedAt: sql.NullInt64{Int64: 20, Valid: true},
			ResolutionNote: "restored", ThreadID: 42, Ordinal: 1})
		return err
	})
	if len(dismissed) != 2 || openCount(t, d, 1) != 0 {
		t.Fatalf("dismissed %v, %d left open: want both of post 1's flags closed", dismissed, openCount(t, d, 1))
	}
	again, err := flagPost(t, d, 1, alice, "spam", "")
	if err != nil {
		t.Fatalf("a flag after a dismissal: %v", err)
	}
	var reopened int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		reopened, err = q.ReopenFlags(t.Context(), dismissed)
		return err
	})
	if reopened != 1 || openCount(t, d, 1) != 2 {
		t.Errorf("reopened %d, %d open: want bob's reopened and alice's newer flag standing for her", reopened, openCount(t, d, 1))
	}
	var old, fresh dbq.Flag
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		if old, err = q.GetFlag(t.Context(), first); err != nil {
			return err
		}
		fresh, err = q.GetFlag(t.Context(), again)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if old.Status != "dismissed" || fresh.Status != "open" {
		t.Errorf("alice's flags: old %s, new %s; want dismissed and open", old.Status, fresh.Status)
	}
}

// TestFlagClosesAndThreshold: DismissOpenFlagsForPost and
// ResolveOpenFlagsForPost close only the post's open flags and name them,
// ReopenFlags reopens only dismissed ones, and PostsAtFlagThreshold lists a
// post with at least threshold open flags that the projection shows.
func TestFlagClosesAndThreshold(t *testing.T) {
	d, _ := openTest(t)
	mod := newUser(t, d, "mod")
	var reporters []int64
	for _, h := range []string{"rep1", "rep2", "rep3"} {
		reporters = append(reporters, newUser(t, d, h))
	}
	for ord := int64(1); ord <= 3; ord++ {
		deleted := 0
		if ord == 3 {
			deleted = 1
		}
		mustExec(t, d, `INSERT INTO post_meta (thread_id, ordinal, author_id, created_at, deleted, log_offset) VALUES (42, ?, ?, 1, ?, 0)`,
			ord, mod, deleted)
	}
	// Post 1: three reporters. Post 2: two. Post 3: three, but deleted.
	for _, r := range reporters {
		for _, ord := range []int64{1, 3} {
			if _, err := flagPost(t, d, ord, r, "spam", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, r := range reporters[:2] {
		if _, err := flagPost(t, d, 2, r, "spam", ""); err != nil {
			t.Fatal(err)
		}
	}
	at := func(threshold int64) string {
		var rows []dbq.PostsAtFlagThresholdRow
		if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			var err error
			rows, err = q.PostsAtFlagThreshold(t.Context(), threshold)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(rows)
	}
	if got := at(3); got != "[{42 1}]" {
		t.Errorf("at threshold 3: %s, want post 1 only (post 3 is deleted)", got)
	}
	if got := at(2); got != "[{42 1} {42 2}]" {
		t.Errorf("at threshold 2: %s, want posts 1 and 2", got)
	}

	var resolved []int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		resolved, err = q.ResolveOpenFlagsForPost(t.Context(), dbq.ResolveOpenFlagsForPostParams{
			ResolvedBy: sql.NullInt64{Int64: mod, Valid: true}, ResolvedAt: sql.NullInt64{Int64: 30, Valid: true},
			ResolutionNote: "confirmed", ThreadID: 42, Ordinal: 1})
		return err
	})
	if len(resolved) != 3 || openCount(t, d, 1) != 0 || openCount(t, d, 2) != 2 {
		t.Errorf("resolved %v: want post 1's three, post 2 untouched", resolved)
	}
	var n int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		n, err = q.ReopenFlags(t.Context(), resolved)
		return err
	})
	if n != 0 || openCount(t, d, 1) != 0 {
		t.Errorf("ReopenFlags reopened %d resolved flags; only a dismissed flag reopens", n)
	}
	if got := at(2); got != "[{42 2}]" {
		t.Errorf("after the resolve, at threshold 2: %s, want post 2", got)
	}
}

// openCount is post ordinal of thread 42's open flags.
func openCount(t *testing.T, d *DB, ordinal int64) int64 {
	t.Helper()
	var n int64
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		n, err = q.OpenFlagCount(t.Context(), dbq.OpenFlagCountParams{ThreadID: 42, Ordinal: ordinal})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAuditLogAppendOnly is AC2, in two layers. The schema refuses UPDATE
// and DELETE, so nothing written anywhere can change a row; and no query in
// queries/ and no SQL string literal under internal/ tries to, so the refusal
// is never something a code path depends on hitting.
func TestAuditLogAppendOnly(t *testing.T) {
	d, _ := openTest(t)
	actor := newUser(t, d, "admin")
	var id int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.AppendAudit(t.Context(), dbq.AppendAuditParams{At: 1, ActorID: sql.NullInt64{Int64: actor, Valid: true},
			Action: "user.suspended", TargetType: "user", TargetID: "7", DetailsJson: `{"reason":"spam"}`})
		return err
	})
	for _, stmt := range []string{
		`UPDATE audit_log SET action = 'nothing' WHERE id = ?`,
		`DELETE FROM audit_log WHERE id = ?`,
	} {
		if err := exec(t, d, stmt, id); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: %v", stmt, err)
		}
	}
	for _, stmt := range []string{
		`INSERT OR REPLACE INTO audit_log (id, at, action) VALUES (?, 2, 'rewritten')`,
		`REPLACE INTO audit_log (id, at, action) VALUES (?, 2, 'rewritten')`,
	} {
		if err := exec(t, d, stmt, id); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s: %v", stmt, err)
		}
	}
	var action string
	if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT action FROM audit_log WHERE id = ?`, id).Scan(&action)
	}); err != nil || action != "user.suspended" {
		t.Errorf("the audit row now reads %q, %v", action, err)
	}
	if err := exec(t, d, `INSERT INTO audit_log (at, action, details_json) VALUES (1, 'x', 'not json')`); err == nil {
		t.Error("details that are not JSON were accepted")
	}

	// The static layer.
	writes := regexp.MustCompile(`(?is)\b(UPDATE\s+(OR\s+\w+\s+)?audit_log\b|DELETE\s+FROM\s+audit_log\b|REPLACE\s+INTO\s+audit_log\b|INSERT\s+OR\s+REPLACE\s+INTO\s+audit_log\b)`)
	queries, err := filepath.Glob("queries/*.sql")
	if err != nil || len(queries) == 0 {
		t.Fatalf("no query files found: %v", err)
	}
	for _, f := range queries {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := writes.Find(stripSQLComments(b)); m != nil {
			t.Errorf("%s: %q changes audit_log rows", f, m)
		}
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	literals := 0
	err = filepath.WalkDir(filepath.Join(root, "internal"), func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			literals++
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if m := writes.FindString(s); m != "" {
				t.Errorf("%s: %q changes audit_log rows", fset.Position(lit.Pos()), m)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if literals == 0 {
		t.Fatal("no string literals checked")
	}
	// The pattern still catches the forms it exists for.
	if !writes.MatchString("delete from audit_log where 1") || !writes.MatchString("UPDATE OR IGNORE audit_log SET x=1") {
		t.Fatal("the pattern no longer matches what it is meant to")
	}
}

// stripSQLComments drops -- comments, so a query file may say in prose what
// it must never do.
func stripSQLComments(b []byte) []byte {
	return regexp.MustCompile(`--[^\n]*`).ReplaceAll(b, nil)
}

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func newBlob(t *testing.T, d *DB, hash string) error {
	t.Helper()
	return try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.CreateBlob(t.Context(), dbq.CreateBlobParams{Hash: hash, Size: 10, Mime: "image/png", UploadedAt: 5})
	})
}

// TestQuarantinedBlobNotServable is AC3: a blob carries a refcount and a
// status, BlobForServing does not find a quarantined one, and the refcount
// never goes below zero.
func TestQuarantinedBlobNotServable(t *testing.T) {
	d, _ := openTest(t)
	if err := newBlob(t, d, hashA); err != nil {
		t.Fatal(err)
	}
	serve := func() error {
		return d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := q.BlobForServing(t.Context(), hashA)
			return err
		})
	}
	if err := serve(); err != nil {
		t.Fatalf("an ok blob is not servable: %v", err)
	}
	setStatus := func(status string) error {
		return try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := q.SetBlobStatus(t.Context(), dbq.SetBlobStatusParams{Status: status, Hash: hashA})
			return err
		})
	}
	if err := setStatus("quarantined"); err != nil {
		t.Fatal(err)
	}
	if err := serve(); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a quarantined blob: %v, want no rows", err)
	}
	if err := setStatus("deleted"); err == nil {
		t.Error(`status "deleted" was accepted`)
	}

	ref := func(add bool) (int64, error) {
		var n int64
		err := try(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
			var err error
			if add {
				n, err = q.AddBlobRef(t.Context(), hashA)
			} else {
				n, err = q.DropBlobRef(t.Context(), dbq.DropBlobRefParams{At: 99, Hash: hashA})
			}
			return err
		})
		return n, err
	}
	blob := func() dbq.Blob {
		var b dbq.Blob
		if err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT refcount, unreferenced_since FROM blobs WHERE hash = ?`, hashA).
				Scan(&b.Refcount, &b.UnreferencedSince)
		}); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if b := blob(); b.Refcount != 0 || b.UnreferencedSince.Int64 != 5 {
		t.Errorf("a new blob: %+v, want unreferenced since its upload at 5", b)
	}
	if _, err := ref(false); err == nil {
		t.Fatal("a refcount went below zero")
	}
	for range 2 {
		if _, err := ref(true); err != nil {
			t.Fatal(err)
		}
	}
	if b := blob(); b.Refcount != 2 || b.UnreferencedSince.Valid {
		t.Errorf("after two references: %+v", b)
	}
	if _, err := ref(false); err != nil {
		t.Fatal(err)
	}
	if b := blob(); b.Refcount != 1 || b.UnreferencedSince.Valid {
		t.Errorf("after dropping one of two: %+v", b)
	}
	if _, err := ref(false); err != nil {
		t.Fatal(err)
	}
	if b := blob(); b.Refcount != 0 || b.UnreferencedSince.Int64 != 99 {
		t.Errorf("after dropping the last: %+v, want unreferenced since 99", b)
	}
	if err := exec(t, d, `UPDATE blobs SET refcount = 1, unreferenced_since = 5 WHERE hash = ?`, hashA); err == nil {
		t.Error("a referenced blob marked unreferenced was accepted")
	}
	// Nothing collects a blob at zero with no timestamp, so the schema
	// refuses one (review 395).
	if err := exec(t, d, `UPDATE blobs SET unreferenced_since = NULL WHERE hash = ?`, hashA); err == nil {
		t.Error("an unreferenced blob with no timestamp was accepted")
	}

	for _, bad := range []string{"AAAA" + hashA[4:], hashA[:63], hashA + "a", "g" + hashA[1:]} {
		if err := newBlob(t, d, bad); err == nil {
			t.Errorf("hash %q was accepted", bad)
		}
	}
}

// TestSettingsTableWantsJSON: the table refuses a value that is not JSON.
// Keys and types are internal/settings's to check.
func TestSettingsTableWantsJSON(t *testing.T) {
	d, _ := openTest(t)
	if err := exec(t, d, `INSERT INTO settings (key, value_json, updated_at) VALUES ('site_name', 'Ketju', 1)`); err == nil {
		t.Error("a value that is not JSON was accepted")
	}
	mustExec(t, d, `INSERT INTO settings (key, value_json, updated_at) VALUES ('site_name', '"Ketju"', 1)`)
	if err := exec(t, d, `INSERT INTO settings (key, value_json, updated_at) VALUES ('', '1', 1)`); err == nil {
		t.Error("an empty key was accepted")
	}
}
