package views

import (
	"context"
	"database/sql"
	"strings"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// AuditFilter narrows the admin audit view (TKT-01M29CP2R4, docs/07 "The
// audit log"). A zero field does not filter; the filters that are set are
// ANDed, which is why this is a view rather than a sqlc query.
type AuditFilter struct {
	// Actor selects the rows one member did. System selects the rows no
	// account did, actor_id IS NULL: the command line, a job, the auto-hide,
	// a thread-log record with no actor. At most one of the two is set.
	Actor  int64
	System bool

	// Action selects one action, or with ActionPrefix every action that
	// starts with it, such as "user.".
	Action       string
	ActionPrefix bool

	// TargetType selects rows against one type of thing, and TargetID,
	// which needs TargetType, one thing of it.
	TargetType string
	TargetID   string

	// From is the first millisecond included and To the first one not, as
	// Unix ms against the row's at. 0 does not bound.
	From, To int64

	// Before is the keyset cursor: only rows with a smaller id. 0 starts at
	// the newest row.
	Before int64

	// Limit is how many rows to return at most.
	Limit int
}

// AuditPage is up to f.Limit rows of audit_log matching f, newest first by
// id.
//
// Paging is on id rather than at: ids are assigned in insert order and never
// reused, since no row is ever deleted, so id is the insertion order with no
// ties, while at can tie and a command-line write can carry a clock that
// went backwards. A row appended between two pages is newer than the first
// page's first row, so it appears on neither and nothing repeats.
//
// Each filter has an index ending in id (migrations 0006 and 0019), and
// TestAuditQueriesUseAnIndex holds the actor, action and target filters to
// a search. An action prefix is a range on the action index: every action
// is <noun>.<verb> in lower case, and '/' sorts just after '.', so
// [prefix, prefix-with-its-final-'.'-as-'/') holds exactly the actions that
// start with it.
func AuditPage(ctx context.Context, tx *sql.Tx, f AuditFilter) ([]dbq.AuditLog, error) {
	q, args := auditQuery(f)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dbq.AuditLog
	for rows.Next() {
		var r dbq.AuditLog
		if err := rows.Scan(&r.ID, &r.At, &r.ActorID, &r.Action, &r.TargetType, &r.TargetID, &r.DetailsJson,
			&r.Ip); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// auditQuery is the statement AuditPage runs for f, and its arguments. It is
// separate so TestAuditQueriesUseAnIndex can explain exactly what runs.
func auditQuery(f AuditFilter) (string, []any) {
	var where []string
	var args []any
	switch {
	case f.System:
		where = append(where, "actor_id IS NULL")
	case f.Actor != 0:
		where = append(where, "actor_id = ?")
		args = append(args, f.Actor)
	}
	switch {
	case f.Action == "":
	case f.ActionPrefix:
		where = append(where, "action >= ? AND action < ?")
		args = append(args, f.Action, prefixEnd(f.Action))
	default:
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}
	if f.TargetType != "" {
		where = append(where, "target_type = ?")
		args = append(args, f.TargetType)
		if f.TargetID != "" {
			where = append(where, "target_id = ?")
			args = append(args, f.TargetID)
		}
	}
	if f.From != 0 {
		where = append(where, "at >= ?")
		args = append(args, f.From)
	}
	if f.To != 0 {
		where = append(where, "at < ?")
		args = append(args, f.To)
	}
	if f.Before != 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	q := `SELECT id, at, actor_id, action, target_type, target_id, details_json, ip FROM audit_log`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT ?"
	return q, append(args, f.Limit)
}

// prefixEnd is the first string after every string that starts with prefix:
// prefix with its last byte raised by one. The caller's prefix is non-empty
// and ends in '.', so the last byte is never 0xff.
func prefixEnd(prefix string) string {
	b := []byte(prefix)
	b[len(b)-1]++
	return string(b)
}
