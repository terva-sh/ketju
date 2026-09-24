// A third caller of insert: it writes whatever it likes and checks nothing,
// with no CreateSession in sight.
package session

func (s *Store) Backdoor(ctx context.Context, userID int64) (string, Session, error) {
	return s.insert(ctx, func(*dbq.Queries) (newSession, error) {
		return newSession{Kind: KindUser, UserID: userID}, nil
	})
}
