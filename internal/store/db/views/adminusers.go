package views

import (
	"context"
	"database/sql"
	"strings"
)

// AdminUserFilter narrows the admin users list (TKT-01M29CXK1X). An empty
// field does not filter.
type AdminUserFilter struct {
	// Prefix matches the start of the handle, case-insensitively, as the
	// handle column's NOCASE collation compares.
	Prefix string
	Role   string
	Status string
	Kind   string
}

// AdminUserRow is one account in the list.
type AdminUserRow struct {
	ID          int64
	Handle      string
	DisplayName string
	Avatar      sql.NullString
	Role        string
	Kind        string
	Status      string
	CreatedAt   int64
	LastSeenAt  sql.NullInt64
	Anonymized  int64
}

// AdminUsers is up to limit accounts matching f with a handle after after,
// in handle order: a keyset page of the users list. The filters that are
// set are ANDed, which is why this is a view rather than a sqlc query.
//
// Order and the keyset use the handle's NOCASE collation, the unique index
// the handle column has, so a page seeks rather than scans. The prefix is a
// range on the same index, as UsersByHandlePrefix's: every handle character
// sorts below '{', so [prefix, prefix+"{") holds exactly the handles that
// start with it.
func AdminUsers(ctx context.Context, tx *sql.Tx, f AdminUserFilter, after string, limit int) ([]AdminUserRow, error) {
	var where []string
	var args []any
	if f.Prefix != "" {
		where = append(where, "handle >= ? AND handle < ?")
		args = append(args, f.Prefix, f.Prefix+"{")
	}
	for _, c := range []struct{ col, v string }{{"role", f.Role}, {"status", f.Status}, {"kind", f.Kind}} {
		if c.v != "" {
			where = append(where, c.col+" = ?")
			args = append(args, c.v)
		}
	}
	if after != "" {
		where = append(where, "handle > ?")
		args = append(args, after)
	}
	q := `SELECT id, handle, display_name, avatar_blob, role, kind, status, created_at, last_seen_at, anonymized FROM users`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY handle LIMIT ?"
	args = append(args, limit)

	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUserRow
	for rows.Next() {
		var r AdminUserRow
		if err := rows.Scan(&r.ID, &r.Handle, &r.DisplayName, &r.Avatar, &r.Role, &r.Kind, &r.Status,
			&r.CreatedAt, &r.LastSeenAt, &r.Anonymized); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
