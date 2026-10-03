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

// FinishMovesEvery is the move finisher's interval.
const FinishMovesEvery = time.Minute

// FinishMoves is the job that completes a move of posts whose target run
// failed after its source record was written (threads.Service.FinishMoves,
// docs/03 "How a move is written", TKT-01M29CXJZZ). The request answered
// unavailable, saying the move would finish, and this finishes it within a
// minute. serve runs it once before serving as well.
func FinishMoves(s *threads.Service) Job {
	return Job{Name: "move finisher", Every: FinishMovesEvery, Run: func(ctx context.Context) (Result, error) {
		n, err := s.FinishMoves(ctx)
		return Result{Attrs: []any{"finished", n}}, err
	}}
}

// FlagAuditCatchUpEvery is the flag audit catch-up's interval.
const FlagAuditCatchUpEvery = time.Minute

// FlagAuditCatchUp is the job that writes the flag.dismissed row a
// moderator queue restore still owes (threads.Service.FlagAuditCatchUp,
// TKT-01M29CRJ4E). A restore's row is written after its append, and if that
// write fails the request answers 200 with the flags marked as owed it, so
// this finds them within a minute or two and nothing waits on a moderator
// retrying.
func FlagAuditCatchUp(s *threads.Service) Job {
	return Job{Name: "flag audit catch-up", Every: FlagAuditCatchUpEvery, Run: func(ctx context.Context) (Result, error) {
		n, err := s.FlagAuditCatchUp(ctx)
		return Result{Attrs: []any{"written", n}}, err
	}}
}
