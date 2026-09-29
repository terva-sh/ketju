package jobs

import (
	"context"
	"time"

	"github.com/terva-sh/ketju/internal/service/threads"
)

// AutoHideEvery is the auto-hide sweep's interval (docs/08 "Flags").
const AutoHideEvery = time.Minute

// AutoHideSweep is the job that hides every post whose open flags are at
// or over flags.auto_hide_threshold and that is still shown
// (threads.Service.SweepAutoHide, TKT-01M29CP2PJ). A flag is committed
// before its hide is tried, so a hide whose append failed is owed by the
// flags themselves, and this finds it within a minute. It also enforces a
// lowered threshold. serve runs it once at startup as well.
func AutoHideSweep(s *threads.Service) Job {
	return Job{Name: "auto-hide sweep", Every: AutoHideEvery, Run: func(ctx context.Context) (Result, error) {
		n, err := s.SweepAutoHide(ctx)
		return Result{Attrs: []any{"hidden", n}}, err
	}}
}
