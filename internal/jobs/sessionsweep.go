package jobs

import (
	"context"

	"github.com/terva-sh/ketju/internal/auth/session"
)

// SessionSweep is the hourly job deleting expired sessions (docs/02
// "Background work"). What counts as expired is the session store's to
// decide, by the rule its lookup uses; see session.Store.Sweep.
func SessionSweep(s *session.Store) Job {
	return Job{Name: "session sweep", Every: session.SweepInterval, Run: func(ctx context.Context) (Result, error) {
		n, err := s.Sweep(ctx)
		return Result{Removed: n}, err
	}}
}
