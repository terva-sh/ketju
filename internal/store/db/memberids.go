package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MemberIDFloor is the highest member id the database has handed out or been
// told is taken: users.id's AUTOINCREMENT high-water mark in sqlite_sequence
// (migration 0010). The next member is given an id above it. Zero means no
// member has ever been created and no floor was set.
func MemberIDFloor(ctx context.Context, tx *sql.Tx) (int64, error) {
	var seq int64
	err := tx.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name = 'users'`).Scan(&seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("db: reading the member id floor: %w", err)
	}
	return seq, nil
}

// RaiseMemberIDFloor makes every member created from now on get an id above
// n, and returns the floor before and after. It only ever raises the floor: n
// at or below it changes nothing, so running it again, or over a database
// ahead of n, is a no-op. ketju reindex calls it with the largest member id
// the thread logs name, so a database restored from an older backup never
// gives a new member an id an old post already names (TKT-01M3KB7FXC).
//
// The SQL is hand-written because sqlite_sequence is SQLite's own table and
// not part of the schema sqlc compiles.
func RaiseMemberIDFloor(ctx context.Context, tx *sql.Tx, n int64) (before, after int64, err error) {
	before, err = MemberIDFloor(ctx, tx)
	if err != nil || n <= before {
		return before, before, err
	}
	// sqlite_sequence has no key on name, so the row is updated when it
	// exists and inserted when it does not: it does not until users has had
	// a row since 0010.
	res, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'users'`, n)
	if err != nil {
		return before, before, fmt.Errorf("db: raising the member id floor: %w", err)
	}
	if k, err := res.RowsAffected(); err != nil {
		return before, before, fmt.Errorf("db: raising the member id floor: %w", err)
	} else if k == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sqlite_sequence (name, seq) VALUES ('users', ?)`, n); err != nil {
			return before, before, fmt.Errorf("db: setting the member id floor: %w", err)
		}
	}
	return before, n, nil
}
