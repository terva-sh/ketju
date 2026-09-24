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

// Not a door: a table whose name only starts with sessions.
func other(ctx context.Context, db execer) {
	db.ExecContext(ctx, "INSERT INTO sessions_archive (id_hash) VALUES (?)")
}
