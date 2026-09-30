package views

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// auditRow is one audit_log row for insertAudit. actor 0 is NULL.
type auditRow struct {
	id, at, actor       int64
	action, typ, target string
}

// auditWorld is a database with two members, 1 and 2, and the rows given.
func auditWorld(t *testing.T, rows ...auditRow) *db.DB {
	t.Helper()
	d := open(t)
	exec(t, d,
		`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
			VALUES (1, 'ann', 'Ann', 'active', 'admin', 'human', '{}', 1, 1)`,
		`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
			VALUES (2, 'ben', 'Ben', 'active', 'moderator', 'human', '{}', 1, 1)`)
	insertAudit(t, d, rows...)
	return d
}

func insertAudit(t *testing.T, d *db.DB, rows ...auditRow) {
	t.Helper()
	var stmts []string
	for _, r := range rows {
		actor := "NULL"
		if r.actor != 0 {
			actor = fmt.Sprint(r.actor)
		}
		stmts = append(stmts, fmt.Sprintf(`INSERT INTO audit_log (id, at, actor_id, action, target_type, target_id)
			VALUES (%d, %d, %s, '%s', '%s', '%s')`, r.id, r.at, actor, r.action, r.typ, r.target))
	}
	exec(t, d, stmts...)
}

func auditPage(t *testing.T, d *db.DB, f AuditFilter) []int64 {
	t.Helper()
	if f.Limit == 0 {
		f.Limit = 100
	}
	var rows []dbq.AuditLog
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = AuditPage(t.Context(), tx, f)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// auditFixture is ten rows. at runs backwards at id 6, as a command-line
// write with a clock behind the server's would, so a filter on time and the
// id order disagree there.
var auditFixture = []auditRow{
	{1, 100, 1, "user.suspended", "user", "2"},
	{2, 200, 2, "thread.locked", "thread", "3yymw47g00001"},
	{3, 300, 0, "user.unsuspended", "user", "2"},
	{4, 400, 1, "user.promoted", "user", "2"},
	{5, 500, 2, "post.hidden", "post", "3yymw47g00001/4"},
	{6, 150, 1, "post.redacted", "post", "3yymw47g00001/4"},
	{7, 700, 0, "post.auto_hidden", "post", "3yymw47g00001/5"},
	{8, 800, 1, "setting.changed", "setting", "registration.mode"},
	{9, 900, 2, "thread.moved", "thread", "3yymw47g00001"},
	{10, 1000, 1, "userx.odd", "user", "1"},
}

// TestAuditPageFilters is the first criterion's filters, each alone and
// combined, newest first.
func TestAuditPageFilters(t *testing.T) {
	d := auditWorld(t, auditFixture...)
	for _, c := range []struct {
		name string
		f    AuditFilter
		want []int64
	}{
		{"none", AuditFilter{}, []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}},
		{"actor", AuditFilter{Actor: 2}, []int64{9, 5, 2}},
		{"system", AuditFilter{System: true}, []int64{7, 3}},
		{"an actor with no rows", AuditFilter{Actor: 99}, []int64{}},
		{"action", AuditFilter{Action: "user.suspended"}, []int64{1}},
		{"action is exact without the prefix flag", AuditFilter{Action: "user."}, []int64{}},
		// "userx.odd" starts with "user" but not with "user.".
		{"action prefix", AuditFilter{Action: "user.", ActionPrefix: true}, []int64{4, 3, 1}},
		{"target type", AuditFilter{TargetType: "post"}, []int64{7, 6, 5}},
		{"target", AuditFilter{TargetType: "post", TargetID: "3yymw47g00001/4"}, []int64{6, 5}},
		{"from, inclusive", AuditFilter{From: 800}, []int64{10, 9, 8}},
		{"to, exclusive", AuditFilter{To: 200}, []int64{6, 1}},
		{"from and to", AuditFilter{From: 150, To: 400}, []int64{6, 3, 2}},
		{"actor and action prefix", AuditFilter{Actor: 1, Action: "post.", ActionPrefix: true}, []int64{6}},
		{"system and target", AuditFilter{System: true, TargetType: "user", TargetID: "2"}, []int64{3}},
		{"action and time", AuditFilter{Action: "post.", ActionPrefix: true, From: 400}, []int64{7, 5}},
		{"everything", AuditFilter{Actor: 2, Action: "thread.", ActionPrefix: true, TargetType: "thread",
			TargetID: "3yymw47g00001", From: 100, To: 950}, []int64{9, 2}},
		{"before", AuditFilter{Before: 5}, []int64{4, 3, 2, 1}},
		{"before with a filter", AuditFilter{Before: 9, Actor: 2}, []int64{5, 2}},
		{"limit", AuditFilter{Limit: 3}, []int64{10, 9, 8}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := auditPage(t, d, c.f); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestAuditPageKeyset: paging by the last id seen returns every row once, in
// order, and a row appended between two pages appears on neither, since it
// is newer than the first page's first row.
func TestAuditPageKeyset(t *testing.T) {
	d := auditWorld(t, auditFixture...)
	var seen []int64
	before := int64(0)
	for page := 0; ; page++ {
		got := auditPage(t, d, AuditFilter{Before: before, Limit: 3})
		if len(got) == 0 {
			break
		}
		seen = append(seen, got...)
		before = got[len(got)-1]
		if page == 0 {
			insertAudit(t, d, auditRow{11, 1100, 1, "user.demoted", "user", "2"})
		}
		if page > 10 {
			t.Fatal("paging does not end")
		}
	}
	if want := []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}; !slices.Equal(seen, want) {
		t.Errorf("paged %v, want %v", seen, want)
	}
	if got := auditPage(t, d, AuditFilter{Limit: 1}); !slices.Equal(got, []int64{11}) {
		t.Errorf("a fresh first page is %v, want the new row", got)
	}
}

// TestAuditQueriesUseAnIndex: the actor, system, action, action prefix and
// target filters, with and without a cursor, search an index rather than
// scanning audit_log, which grows with every moderation action and is never
// pruned.
func TestAuditQueriesUseAnIndex(t *testing.T) {
	d := auditWorld(t, auditFixture...)
	for _, c := range []struct {
		name  string
		f     AuditFilter
		index string
	}{
		{"actor", AuditFilter{Actor: 1}, "audit_log_actor"},
		{"actor after a cursor", AuditFilter{Actor: 1, Before: 5}, "audit_log_actor"},
		{"system", AuditFilter{System: true}, "audit_log_actor"},
		{"action", AuditFilter{Action: "user.promoted"}, "audit_log_action"},
		{"action after a cursor", AuditFilter{Action: "user.promoted", Before: 5}, "audit_log_action"},
		{"action prefix", AuditFilter{Action: "user.", ActionPrefix: true}, "audit_log_action"},
		{"target type", AuditFilter{TargetType: "user"}, "audit_log_target"},
		{"target", AuditFilter{TargetType: "user", TargetID: "2"}, "audit_log_target"},
		{"target after a cursor", AuditFilter{TargetType: "user", TargetID: "2", Before: 5}, "audit_log_target"},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := auditPlan(t, d, c.f)
			if strings.Contains(plan, "SCAN audit_log") || !strings.Contains(plan, "INDEX "+c.index+" ") {
				t.Errorf("the %s filter does not search %s:\n%s", c.name, c.index, plan)
			}
		})
	}
}

// auditPlan is EXPLAIN QUERY PLAN of the statement AuditPage runs for f.
func auditPlan(t *testing.T, d *db.DB, f AuditFilter) string {
	t.Helper()
	f.Limit = 50
	var lines []string
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		q, args := auditQuery(f)
		rows, err := tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				return err
			}
			lines = append(lines, detail)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
