// A second door of every kind chokepoint_test.go knows, which it must find.
package other

import (
	"context"

	_ "github.com/terva-sh/ketju/internal/auth/session/sessiontest"
)

func sneak(ctx context.Context, q queries, db execer) {
	q.CreateSession(ctx, params{})
	db.ExecContext(ctx, `insert or ignore
		INTO sessions (id_hash, user_id, kind) VALUES (?, ?, 'user')`)
}

func promote(ctx context.Context, db execer) {
	db.ExecContext(ctx, "UPDATE sessions SET kind = 'user', user_id = ? WHERE id_hash = ?")
}

// Not a door: the columns a touch moves.
func touch(ctx context.Context, db execer) {
	db.ExecContext(ctx, "UPDATE sessions SET last_used_at = ? WHERE id_hash = ?")
}

// A schema-qualified spelling is the same table (review 499).
func qualified(ctx context.Context, db execer) {
	db.ExecContext(ctx, "INSERT INTO main.sessions (id_hash) VALUES (?)")
	db.ExecContext(ctx, `UPDATE "main"."sessions" SET user_id = ? WHERE id_hash = ?`)
}

// Not doors: a table whose name only starts with sessions, and two strings
// compared rather than joined.
func other(ctx context.Context, db execer) bool {
	db.ExecContext(ctx, "INSERT INTO sessions_archive (id_hash) VALUES (?)")
	return "INSERT INTO " == "sessions (id_hash)"
}

const table = "sessions"

var qualifiedTable = "main.sessions"

// SQL assembled from literals and constants is the same SQL (review 500).
func assembled(ctx context.Context, db execer) {
	db.ExecContext(ctx, "INSERT INTO "+table+" (id_hash) VALUES (?)")
	db.ExecContext(ctx, "INSERT INTO "+("main."+"sessions")+" (id_hash) VALUES (?)")
	db.ExecContext(ctx, "UPDATE "+qualifiedTable+" SET user_id = ?")
}

// An alias between the table and SET is the same table (review 500).
func aliased(ctx context.Context, db execer) {
	db.ExecContext(ctx, "UPDATE sessions AS s SET user_id = ? WHERE s.id_hash = ?")
	db.ExecContext(ctx, "UPDATE main.sessions s SET kind = 'user' WHERE s.id_hash = ?")
	// Not a door: an aliased touch.
	db.ExecContext(ctx, "UPDATE sessions AS s SET last_used_at = ? WHERE s.id_hash = ?")
}

// A local := is resolved like a constant (review 501).
func local(ctx context.Context, db execer) {
	t, n := "sessions", 1
	db.ExecContext(ctx, "INSERT INTO "+t+" (id_hash) VALUES (?)", n)
	set := "SET kind = 'user'"
	db.ExecContext(ctx, "UPDATE sessions "+set)
}

// A WHERE, a comma or a quote inside an expression does not end the SET
// clause (review 501).
func nested(ctx context.Context, db execer) {
	db.ExecContext(ctx, "UPDATE sessions SET last_used_at = (SELECT 1 WHERE 1), kind = 'user', user_id = ? WHERE id_hash = ?")
	db.ExecContext(ctx, "UPDATE sessions SET last_used_at = coalesce(?, ','), (user_id, [kind]) = (?, 'where') WHERE id_hash = ?")
	// A keyword inside a string or inside a longer word is not the keyword.
	db.ExecContext(ctx, "UPDATE sessions SET last_used_at = 'x where, y', last_used_at = elsewhere, last_used_at = fromage, user_id = ? WHERE id_hash = ?")
	// Not a door: all of it is last_used_at.
	db.ExecContext(ctx, "UPDATE sessions SET last_used_at = (SELECT max(x) FROM t WHERE y IN (1, 2)) WHERE id_hash = ?")
}
