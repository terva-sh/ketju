// A test may write a row directly; the scan leaves test files alone.
package other

func setup(ctx context.Context, q queries, db execer) {
	q.CreateSession(ctx, params{})
	db.ExecContext(ctx, "INSERT INTO sessions (id_hash) VALUES (?)")
}

// A test may sign someone in directly.
func signIn(ctx context.Context, s *session.Store) {
	s.IssuePreUser(ctx, 1, session.Meta{})
}
