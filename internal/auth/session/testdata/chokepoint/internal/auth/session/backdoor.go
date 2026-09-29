// A third caller of insert: it writes whatever it likes and checks nothing,
// with no CreateSession in sight.
package session

func (s *Store) Backdoor(ctx context.Context, userID int64) (string, Session, error) {
	return s.insert(ctx, func(*dbq.Queries) (newSession, error) {
		return newSession{Kind: KindUser, UserID: userID}, nil
	})
}

// Inside the package, calling an issuer is not a new sign-in path.
func (s *Store) inside(ctx context.Context) {
	s.IssueUser(ctx, 1, Meta{})
}
