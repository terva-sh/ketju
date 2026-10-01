package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/sqltypes"
)

// write runs fn in a write transaction and fails the test on error.
func write(t *testing.T, d *DB, fn func(q *dbq.Queries, tx *sql.Tx) error) {
	t.Helper()
	if err := d.Write(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

func newUser(t *testing.T, d *DB, handle string) int64 {
	t.Helper()
	var id int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateUser(t.Context(), dbq.CreateUserParams{
			Handle: handle, DisplayName: handle, Status: "active", Role: "member", Kind: "human",
			CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	return id
}

func newIdentity(t *testing.T, d *DB, userID sql.NullInt64, subject string) int64 {
	t.Helper()
	var id int64
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		var err error
		id, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{
			UserID: userID, ProviderKey: "main", Subject: subject, Issuer: "https://idp.example",
			CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	return id
}

// TestHandleUniqueIgnoresCase is AC1: "drew" and then "Drew", which differ
// only in case, cannot both be stored.
//
// Two things stop the second, and the test checks each on its own, so that
// removing either one fails it. The CHECK refuses an uppercase handle outright.
// Behind it, the UNIQUE index on handle is NOCASE, so even a row that got past
// the CHECK would collide; that is asserted from the index definition, and by
// looking "drew" up as "DREW".
func TestHandleUniqueIgnoresCase(t *testing.T) {
	d, _ := openTest(t)
	newUser(t, d, "drew")

	err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := q.CreateUser(t.Context(), dbq.CreateUserParams{
			Handle: "Drew", DisplayName: "again", Status: "active", Role: "member", Kind: "human",
			CreatedAt: 1, UpdatedAt: 1,
		})
		return err
	})
	if err == nil {
		t.Fatal("'Drew' was stored beside 'drew'")
	}

	var collation string
	err = d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		u, err := q.GetUserByHandle(t.Context(), "DREW")
		if err != nil {
			return err
		}
		if u.Handle != "drew" {
			t.Errorf("lookup by DREW found %q, want drew", u.Handle)
		}
		return tx.QueryRowContext(t.Context(), `
			SELECT x.coll
			FROM pragma_index_list('users') AS l, pragma_index_xinfo(l.name) AS x
			WHERE l."unique" = 1 AND x.name = 'handle'`).Scan(&collation)
	})
	if err != nil {
		t.Fatal(err)
	}
	if collation != "NOCASE" {
		t.Fatalf("the UNIQUE index on users.handle uses collation %q, want NOCASE", collation)
	}
}

// TestHandleRuleMatchesTheSchema runs domain's shared handle table against the
// users CHECK, so the Go rule and the schema rule cannot drift apart.
func TestHandleRuleMatchesTheSchema(t *testing.T) {
	d, _ := openTest(t)
	for _, c := range domain.HandleCases {
		err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := q.CreateUser(t.Context(), dbq.CreateUserParams{
				Handle: c.Handle, DisplayName: "x", Status: "active", Role: "member", Kind: "human",
				CreatedAt: 1, UpdatedAt: 1,
			})
			return err
		})
		// A grandfathered handle is refused to a new account but stored,
		// because an account may already hold it.
		want := c.Valid || c.Grandfathered
		if (err == nil) != want {
			t.Errorf("schema accepted=%v for %q, want %v (%s): %v", err == nil, c.Handle, want, c.Why, err)
		}
		if want && err != nil && !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("%q refused for a reason other than the CHECK: %v", c.Handle, err)
		}
	}
}

// TestSessionUserIdentityShape is AC2.
func TestSessionUserIdentityShape(t *testing.T) {
	d, _ := openTest(t)
	user := sql.NullInt64{Int64: newUser(t, d, "drew"), Valid: true}
	ident := sql.NullInt64{Int64: newIdentity(t, d, sql.NullInt64{}, "applicant-sub"), Valid: true}
	none := sql.NullInt64{}

	for i, c := range []struct {
		name           string
		kind           string
		user, identity sql.NullInt64
		ok             bool
	}{
		{"pre_user with an identity only", "pre_user", none, ident, true},
		{"pre_user with neither", "pre_user", none, none, false},
		{"pre_user carrying a user", "pre_user", user, ident, false},
		{"user with a user", "user", user, none, true},
		{"user with a user and an identity", "user", user, ident, true},
		{"user without a user", "user", none, ident, false},
		{"unknown kind", "guest", user, none, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var h sqltypes.SessionHash
			h[0] = byte(i + 1)
			err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
				return q.CreateSession(t.Context(), dbq.CreateSessionParams{
					IDHash: h, UserID: c.user, IdentityID: c.identity, Kind: c.kind,
					CreatedAt: 1, ExpiresAt: 2, LastUsedAt: 1,
				})
			})
			if (err == nil) != c.ok {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, c.ok, err)
			}
		})
	}
}

// TestSessionIDColumnIsHashOnly is AC3's schema half. The column takes exactly
// a 32-byte BLOB: not a shorter one, and not the base64 text a cookie carries,
// so the plaintext id has nowhere to go. The query half is a compile-time
// fact: every generated session query takes sqltypes.SessionHash, a [32]byte,
// so a string cookie value does not type-check. That hashing happens before
// the store is TKT-01M29C5Y3E's to prove end to end.
func TestSessionIDColumnIsHashOnly(t *testing.T) {
	d, _ := openTest(t)
	user := newUser(t, d, "drew")
	for _, c := range []struct {
		name string
		id   any
		ok   bool
	}{
		{"32-byte blob", make([]byte, 32), true},
		{"31-byte blob", make([]byte, 31), false},
		{"33-byte blob", make([]byte, 33), false},
		{"base64 text of a 256-bit id", "q83vEjRWeJq83vEjRWeJq83vEjRWeJq83vEjRWeJq80=", false},
		{"32 characters of text", strings.Repeat("a", 32), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(t.Context(),
					`INSERT INTO sessions (id_hash, user_id, kind, created_at, expires_at, last_used_at)
					 VALUES (?, ?, 'user', 1, 2, 1)`, c.id, user)
				return err
			})
			if (err == nil) != c.ok {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, c.ok, err)
			}
		})
	}

	// And the typed path round-trips.
	var h sqltypes.SessionHash
	copy(h[:], "0123456789abcdef0123456789abcdef")
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.CreateSession(t.Context(), dbq.CreateSessionParams{
			IDHash: h, UserID: sql.NullInt64{Int64: user, Valid: true}, Kind: "user",
			CreatedAt: 1, ExpiresAt: 2, LastUsedAt: 1,
		})
	})
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		s, err := q.GetSession(t.Context(), h)
		if err == nil && s.IDHash != h {
			t.Errorf("round trip gave %x, want %x", s.IDHash, h)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoCascadingDeletes is AC4, written over every table so it also guards
// the migrations that come after this one: users are deactivated, never
// removed, and nothing is deleted behind the caller's back (docs/04).
//
// The one exception is named here, so a second needs a decision of its own:
// applications.identity_id is ON DELETE SET NULL, because identities are
// removed (account linking, anonymization) and an application outlives its
// identity (0011, docs/04 "applications"). It deletes nothing.
//
// The second is blob_uploads.hash, ON DELETE CASCADE (0020, TKT-01M29CXK0K):
// a blob row is deleted only by the collector and by ketju redact, each
// when nothing names the blob, and its upload rows name nothing without it.
// The cascade removes them in the deleting transaction, so neither deleter
// has to know the table exists (TestCollectorRemovesBlobUploads,
// TestRedactRemovesBlobUploads).
func TestNoCascadingDeletes(t *testing.T) {
	d, _ := openTest(t)
	allowed := map[string]string{
		"applications.identity_id -> identities": "SET NULL",
		"blob_uploads.hash -> blobs":             "CASCADE",
	}
	var bad []string
	fks := 0
	err := d.Read(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `
			SELECT m.name, f."from", f."table", f.on_delete, f.on_update
			FROM sqlite_schema AS m, pragma_foreign_key_list(m.name) AS f
			WHERE m.type = 'table'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table, from, to, onDelete, onUpdate string
			if err := rows.Scan(&table, &from, &to, &onDelete, &onUpdate); err != nil {
				return err
			}
			fks++
			fk := table + "." + from + " -> " + to
			want, ok := allowed[fk]
			if !ok {
				want = "NO ACTION"
			}
			delete(allowed, fk)
			if onDelete != want || onUpdate != "NO ACTION" {
				bad = append(bad, fk+": ON DELETE "+onDelete+", ON UPDATE "+onUpdate+"; want ON DELETE "+want)
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if fks == 0 {
		t.Fatal("found no foreign keys at all, so this test is checking nothing")
	}
	for _, b := range bad {
		t.Errorf("%s; every other foreign key must be NO ACTION", b)
	}
	for fk := range allowed {
		t.Errorf("the allowed exception %s no longer exists; remove it here", fk)
	}

	// And NO ACTION is enforced: a user with a session cannot be deleted.
	user := newUser(t, d, "kept")
	var h sqltypes.SessionHash
	h[0] = 0xAA
	write(t, d, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.CreateSession(t.Context(), dbq.CreateSessionParams{
			IDHash: h, UserID: sql.NullInt64{Int64: user, Valid: true}, Kind: "user",
			CreatedAt: 1, ExpiresAt: 2, LastUsedAt: 1,
		})
	})
	err = d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "DELETE FROM users WHERE id = ?", user)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("deleting a user with a session: %v, want a FOREIGN KEY failure", err)
	}
}

// TestHandleMustBeText: SQLite stores a BLOB bound to a TEXT column as a
// BLOB, and a BLOB's length counts every byte while GLOB stops at a NUL. The
// generated query binds a string, so this goes around it, as any hand-written
// SQL could.
//
// Two clauses refuse a BLOB, and this test cannot tell them apart:
// typeof(handle) = 'text', which says so on purpose, and the first-character
// check, because substr() of a BLOB is a BLOB and never matches the GLOB. The
// second is an accident of SQLite's semantics, which is why the first exists
// (Terva review 377).
func TestHandleMustBeText(t *testing.T) {
	d, _ := openTest(t)
	for _, h := range [][]byte{[]byte("abc\x00!"), []byte("drew")} {
		err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
			_, err := tx.ExecContext(t.Context(),
				`INSERT INTO users (handle, display_name, created_at, updated_at) VALUES (?, 'x', 1, 1)`, h)
			return err
		})
		if err == nil {
			t.Errorf("a BLOB handle %q was stored", h)
		}
	}
}
