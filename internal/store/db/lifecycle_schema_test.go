package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// The account lifecycle's schema (0012, TKT-01M29CKRFS).

// TestAnonymizedIsFinal is AC4's schema half: once anonymized, an account's
// handle, display name, avatar, bio, flag and status cannot change, by any
// UPDATE, so irreversible holds for every write path, the command line
// included. The fields nobody reads as a name still move.
func TestAnonymizedIsFinal(t *testing.T) {
	d, _ := openTest(t)
	id := newUser(t, d, "alice")
	write(t, d, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.AnonymizeUser(t.Context(), dbq.AnonymizeUserParams{UpdatedAt: 2, ID: id})
	})
	var handle, display, status string
	var anonymized int
	write(t, d, func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT handle, display_name, status, anonymized FROM users WHERE id = ?`,
			id).Scan(&handle, &display, &status, &anonymized)
	})
	if handle != fmt.Sprintf("deleted-user-%d", id) || display != "" || status != "deactivated" || anonymized != 1 {
		t.Fatalf("anonymized row: %q %q %q %d", handle, display, status, anonymized)
	}

	for _, set := range []string{
		"handle = 'alice'",
		"display_name = 'Alice'",
		"avatar_blob = 'abc'",
		"bio_source = 'hello'",
		"anonymized = 0",
		"status = 'active'",
		"status = 'suspended'",
	} {
		err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(), "UPDATE users SET "+set+" WHERE id = ?", id)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "an anonymized account cannot change") {
			t.Errorf("SET %s on an anonymized account: %v", set, err)
		}
	}
	// What names nobody is still the row's own business.
	write(t, d, func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET last_seen_at = 5, updated_at = 5 WHERE id = ?`, id)
		return err
	})
}

// TestAnonymizedNeedsThePlaceholder: the flag goes on only with the
// deleted-user-<id> handle, and never on an insert.
func TestAnonymizedNeedsThePlaceholder(t *testing.T) {
	d, _ := openTest(t)
	id := newUser(t, d, "bob")
	err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET anonymized = 1, display_name = '' WHERE id = ?`, id)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "deleted-user-<id>") {
		t.Errorf("anonymized under the old handle: %v", err)
	}
	err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET anonymized = 1, handle = 'deleted-user-999' WHERE id = ?`, id)
		return err
	})
	if err == nil {
		t.Error("anonymized under another account's placeholder")
	}
	err = d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO users (handle, display_name, anonymized, created_at, updated_at)
			VALUES ('carol', '', 1, 1, 1)`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "never created so") {
		t.Errorf("inserted anonymized: %v", err)
	}
}

// TestSetUserStatusWritesTheReason: a status change writes the reason and
// end with it, so one change's reason never outlives it.
func TestSetUserStatusWritesTheReason(t *testing.T) {
	d, _ := openTest(t)
	id := newUser(t, d, "dee")
	read := func() (string, string, sql.NullInt64) {
		var status, reason string
		var until sql.NullInt64
		write(t, d, func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT status, status_reason, status_until FROM users WHERE id = ?`,
				id).Scan(&status, &reason, &until)
		})
		return status, reason, until
	}
	write(t, d, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.SetUserStatus(t.Context(), dbq.SetUserStatusParams{Status: "suspended", StatusReason: "spam",
			StatusUntil: sql.NullInt64{Int64: 99, Valid: true}, UpdatedAt: 2, ID: id})
	})
	if s, r, u := read(); s != "suspended" || r != "spam" || u.Int64 != 99 {
		t.Errorf("suspended: %s %q %v", s, r, u)
	}
	write(t, d, func(q *dbq.Queries, _ *sql.Tx) error {
		return q.SetUserStatus(t.Context(), dbq.SetUserStatusParams{Status: "active", UpdatedAt: 3, ID: id})
	})
	if s, r, u := read(); s != "active" || r != "" || u.Valid {
		t.Errorf("unsuspended: %s %q %v", s, r, u)
	}
}
