// Sign-in paths the issuer list does not name, which the scan must report.
package other

func letIn(ctx context.Context, s *session.Store) {
	s.IssuePreUserWith(ctx, session.Meta{}, nil)
	s.IssueUser(ctx, 1, session.Meta{})
	s.IssueUser(ctx, 2, session.Meta{})
}

// A method value handed on is a caller too.
func passOn(s *session.Store) any {
	return s.IssueUserWith
}
