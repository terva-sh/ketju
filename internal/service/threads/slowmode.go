package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// MaxSlowModeSeconds is the longest slow mode a thread takes: a day. It
// guards against a typo and is not a policy; the thread_slow_mode table's
// CHECK says the same.
const MaxSlowModeSeconds = 24 * 60 * 60

// ActionSlowMode is the audit_log action SetSlowMode writes, against the
// thread, with "from" (the thread's previous value, null when it had none)
// and "to".
const ActionSlowMode = "thread.slow_mode"

// SetSlowMode is PUT /admin/threads/{id}/slow-mode (docs/08 "Primitives",
// TKT-01M29CRJ50): the minimum seconds between one member's posts in thread
// id, 0 for off. The value is kept in thread_slow_mode, not in the log, and
// overrides slow_mode.default_seconds, 0 included.
//
// The refusals come in moderate's order, all before anything is written:
// forbidden without moderate in the thread's forum; conflict on an archived
// thread (an archived forum is allowed, as it is for lock and pin); then
// validation-failed for seconds outside 0 to MaxSlowModeSeconds; then
// conflict for the value the thread already has. A thread with no row has
// no value of its own, so setting one there is a change even when it equals
// the site default: it pins the thread against a later change of the
// default.
//
// The row and its audit_log row are written in one transaction. The audit
// row is the service's to write, not the projection writer's, because no
// record is appended (docs/04 "audit_log"). ip is the request's address.
//
// The route is behind the moderates gate, which admits a member who
// moderates any forum, so the forum is decided here, at a log length, as
// moderate decides lock and pin (TKT-01M3RQP0TG). No record is appended, so
// the length cannot be an append's precondition. Instead, holding the
// thread, SetSlowMode reads the length L, projects the thread to L, and
// admits v from that row (admitAt). The transaction that writes the row
// checks that the log is still L before it commits, and otherwise rolls back
// and decides again. A move, or an archive, that lands before that check is
// judged; one that lands after it is ordered after the change. A thread
// written to without pause for threadAttempts decisions is conflict.
func (s *Service) SetSlowMode(ctx context.Context, v domain.Viewer, ip string, id threadid.ID, seconds int) (dto.SlowMode, error) {
	var out dto.SlowMode
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	// As appendDecided: only whether the thread exists is asked before it is
	// opened, and nothing about v is judged from a row not yet projected.
	if err := s.threadExists(ctx, id); err != nil {
		return out, err
	}
	for range threadAttempts {
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			a, at, err := s.admitAt(ctx, v, id, th)
			if err != nil {
				return err
			}
			if !a.canModerate {
				return problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
			}
			if th.Archived() {
				return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
			}
			if seconds < 0 || seconds > MaxSlowModeSeconds {
				return problem.Coded(problem.CodeValidationFailed,
					fmt.Errorf("threads: slow mode is 0 to %d seconds, not %d", MaxSlowModeSeconds, seconds))
			}
			now := s.now()
			return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
				var from any // null: the thread had no value of its own
				switch row, err := q.GetThreadSlowMode(ctx, int64(id)); {
				case err == nil && row.Seconds == int64(seconds):
					return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s's slow mode is already %d seconds", id, seconds))
				case err == nil:
					from = row.Seconds
				case !errors.Is(err, sql.ErrNoRows):
					return err
				}
				if err := q.PutThreadSlowMode(ctx, dbq.PutThreadSlowModeParams{
					ThreadID: int64(id), Seconds: int64(seconds),
					SetBy: sql.NullInt64{Int64: v.UserID, Valid: true}, SetAt: now.UnixMilli(),
				}); err != nil {
					return err
				}
				if _, err := audit.Append(ctx, q, now, audit.By{Actor: v.UserID, IP: ip}, ActionSlowMode, audit.Thread(id),
					map[string]any{"from": from, "to": seconds}); err != nil {
					return err
				}
				// Last, so the window in which a record can land unjudged is
				// only the commit.
				if th.Actor.LogLength() != at {
					return threadlog.ErrLogMoved
				}
				return nil
			})
		})
		if errors.Is(err, threadlog.ErrLogMoved) {
			continue
		}
		if err != nil {
			return out, err
		}
		out.Seconds = seconds
		return out, nil
	}
	return out, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s kept changing while this request was decided", id))
}

// effectiveSlowMode is thread id's slow mode in seconds: its own row, or
// slow_mode.default_seconds when it has none. It is read on every reply and
// every page, so a change applies to the next request.
func (s *Service) effectiveSlowMode(ctx context.Context, id threadid.ID) (int, error) {
	var seconds int
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := q.GetThreadSlowMode(ctx, int64(id))
		switch {
		case err == nil:
			seconds = int(row.Seconds)
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		seconds, err = settings.SlowModeDefaultSeconds.In(ctx, q)
		return err
	})
	return seconds, err
}

// slowModeWait is how long author must still wait to post in the thread
// whose index is ix, under a slow mode of seconds, at now: zero when they
// may post. The newest post is the index's, which is the log's.
func slowModeWait(ix *threadlog.Index, author int64, seconds int, now time.Time) (time.Duration, error) {
	if seconds <= 0 {
		return 0, nil
	}
	mine := ix.ByAuthor(uint64(author))
	if len(mine) == 0 {
		return 0, nil
	}
	last, err := ix.Entry(mine[len(mine)-1])
	if err != nil {
		return 0, err
	}
	return max(0, time.UnixMilli(last.CreatedMS).Add(time.Duration(seconds)*time.Second).Sub(now)), nil
}

// nextPostAt is the viewer block's next_post_at: when slow mode next lets
// a post, or nil when a may post now, is exempt as a moderator, or is not a
// member. Call it where the index is stable, as View's snapshot is.
func nextPostAt(a access, ix *threadlog.Index, seconds int, now time.Time) (*dto.Millis, error) {
	if !a.viewer.Member() || a.canModerate {
		return nil, nil
	}
	wait, err := slowModeWait(ix, a.viewer.UserID, seconds, now)
	if err != nil || wait <= 0 {
		return nil, err
	}
	at := dto.Millis(now.Add(wait).UnixMilli())
	return &at, nil
}
