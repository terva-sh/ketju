// Package dbtest holds test helpers over the database that more than one
// package's tests need. It is imported only from _test files.
package dbtest

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// wallClock names the columns that record when a row was written rather than
// what the log says. Two correct projections of the same logs differ in them,
// so DumpProjections leaves them out.
var wallClock = map[string]bool{"updated_at": true}

// DumpProjections writes every row of every projection table as canonical
// text: tables in db.ProjectionTables order, rows in primary-key order,
// columns in schema order, wall-clock columns left out. Two databases whose
// projections agree dump identically, which is the check behind "rebuilding
// from the logs produces identical rows" (TKT-01M29BYYJ8, TKT-01M29C2WW5,
// TKT-01M29C5Y25).
//
// The columns are read from the schema, so a column added to a projection
// table is dumped without anyone remembering to add it here.
func DumpProjections(t testing.TB, d *db.DB) string {
	t.Helper()
	var b strings.Builder
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		for _, table := range db.ProjectionTables {
			cols, keys, err := columns(tx, t, table)
			if err != nil {
				return err
			}
			// Identifiers from the schema, not from input; quoted anyway.
			query := fmt.Sprintf(`SELECT %s FROM "%s" ORDER BY %s`,
				quoteAll(cols), table, quoteAll(keys))
			rows, err := tx.QueryContext(t.Context(), query)
			if err != nil {
				return err
			}
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			n := 0
			for rows.Next() {
				if err := rows.Scan(ptrs...); err != nil {
					rows.Close()
					return err
				}
				fmt.Fprintf(&b, "%s:", table)
				for i, c := range cols {
					fmt.Fprintf(&b, " %s=%s", c, format(vals[i]))
				}
				b.WriteByte('\n')
				n++
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s: %d rows\n", table, n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("dumping projections: %v", err)
	}
	return b.String()
}

// columns returns table's dumped columns in schema order and its primary key
// columns in key order.
func columns(tx *sql.Tx, t testing.TB, table string) (cols, keys []string, err error) {
	rows, err := tx.QueryContext(t.Context(), `SELECT name, pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	type key struct {
		name string
		pos  int
	}
	var pk []key
	for rows.Next() {
		var name string
		var pos int
		if err := rows.Scan(&name, &pos); err != nil {
			return nil, nil, err
		}
		if pos > 0 {
			pk = append(pk, key{name, pos})
		}
		if !wallClock[name] {
			cols = append(cols, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(cols) == 0 {
		return nil, nil, fmt.Errorf("dbtest: table %s has no columns; is it missing from the schema?", table)
	}
	if len(pk) == 0 {
		return nil, nil, fmt.Errorf("dbtest: table %s has no primary key, so its rows have no canonical order", table)
	}
	slices.SortFunc(pk, func(a, b key) int { return a.pos - b.pos })
	for _, k := range pk {
		keys = append(keys, k.name)
	}
	return cols, keys, nil
}

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = `"` + n + `"`
	}
	return strings.Join(q, ", ")
}

// format spells a value so that types which print alike stay apart: a text
// "1" and an integer 1 are different rows.
func format(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("x'%x'", v)
	case string:
		return fmt.Sprintf("%q", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
