package jobs

import (
	"context"
	"time"

	"github.com/terva-sh/ketju/internal/service/account"
)

// SuspensionExpiryInterval is how often suspensions are checked for an end
// that has passed, so one is lifted within a minute of it (TKT-01M29CP2QK).
const SuspensionExpiryInterval = time.Minute

// SuspensionExpiry is the job lifting suspensions whose end has passed
// (docs/08 "Suspension"). Each is an unsuspension by no account, "via":
// "expiry"; the posts a suspension hid stay hidden until a moderator
// restores them. The rule is account.Service.ExpireSuspensions'.
func SuspensionExpiry(a *account.Service) Job {
	return Job{Name: "suspension expiry", Every: SuspensionExpiryInterval, Run: func(ctx context.Context) (Result, error) {
		n, err := a.ExpireSuspensions(ctx)
		return Result{Removed: n}, err
	}}
}
