// A test may write a row directly; the scan leaves test files alone.
package other

func setup(ctx context.Context, q queries, db execer) {
	q.CreateSession(ctx, params{})
	db.ExecContext(ctx, "INSERT INTO sessions (id_hash) VALUES (?)")
}
