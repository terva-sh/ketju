package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// MaxReason is the longest reason a moderation action takes, in characters.
const MaxReason = 500

// The pin scopes (docs/08 "Primitives").
const (
	PinForum  = "forum"
	PinGlobal = "global"
)

// Lock is POST /admin/threads/{id}/lock (docs/07 "Moderation and admin"): a
// ThreadLocked record, after which the thread refuses members' posts and
// edits while moderators may still post (docs/08 "Locking semantics").
// Locking a locked thread is conflict and appends nothing. The reason is
// optional.
func (s *Service) Lock(ctx context.Context, v domain.Viewer, id threadid.ID, reason string) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, _ time.Time) (threadlog.Append, error) {
		if a.row.Locked != 0 {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is already locked", id))
		}
		if err := checkReason(reason); err != nil {
			return threadlog.Append{}, err
		}
		return threadlog.Append{Type: threadlog.TypeThreadLocked, Body: &threadlog.ThreadLockedBody{Reason: reason}}, nil
	})
}

// Unlock is POST /admin/threads/{id}/unlock: a ThreadUnlocked record.
// Unlocking a thread that is not locked is conflict.
func (s *Service) Unlock(ctx context.Context, v domain.Viewer, id threadid.ID, reason string) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, _ time.Time) (threadlog.Append, error) {
		if a.row.Locked == 0 {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is not locked", id))
		}
		if err := checkReason(reason); err != nil {
			return threadlog.Append{}, err
		}
		return threadlog.Append{Type: threadlog.TypeThreadUnlocked, Body: &threadlog.ThreadUnlockedBody{Reason: reason}}, nil
	})
}

// Pin is POST /admin/threads/{id}/pin: a ThreadPinned record with scope
// forum or global, until nil for no end. A pin replaces whatever pin the
// thread has. Pinning with the scope and end of the active pin is conflict,
// so the log keeps no record that changes nothing; an end that is not after
// now is validation-failed.
func (s *Service) Pin(ctx context.Context, v domain.Viewer, id threadid.ID, scope string, until *time.Time) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, now time.Time) (threadlog.Append, error) {
		var ms int64 // zero is indefinitely, in the log
		if until != nil {
			ms = until.UnixMilli()
		}
		if pinActive(a.row, now) && a.row.PinnedScope.String == scope &&
			a.row.PinnedUntil.Valid == (until != nil) && a.row.PinnedUntil.Int64 == ms {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict,
				fmt.Errorf("threads: %s already has this pin", id))
		}
		if err := checkScope(scope); err != nil {
			return threadlog.Append{}, err
		}
		if until != nil && ms <= now.UnixMilli() {
			return threadlog.Append{}, problem.Coded(problem.CodeValidationFailed,
				errors.New("threads: a pin's end must be in the future"))
		}
		return threadlog.Append{Type: threadlog.TypeThreadPinned, Body: &threadlog.ThreadPinnedBody{Scope: scope, Until: ms}}, nil
	})
}

// Unpin is POST /admin/threads/{id}/unpin: a ThreadUnpinned record naming
// the scope removed, so clearing a forum pin cannot clear a global one
// (docs/03). A scope that is not the active pin's is conflict, an expired
// pin included: it is no pin.
func (s *Service) Unpin(ctx context.Context, v domain.Viewer, id threadid.ID, scope string) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, now time.Time) (threadlog.Append, error) {
		if !pinActive(a.row, now) || a.row.PinnedScope.String != scope {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict,
				fmt.Errorf("threads: %s has no active %q pin", id, scope))
		}
		if err := checkScope(scope); err != nil {
			return threadlog.Append{}, err
		}
		return threadlog.Append{Type: threadlog.TypeThreadUnpinned, Body: &threadlog.ThreadUnpinnedBody{Scope: scope}}, nil
	})
}

// moderate is the four thread actions: the checks every one of them makes,
// then the record decide builds, appended with the viewer as its actor and
// projected before the answer. The refusals come in this order, all before
// the append, so a refusal writes nothing, to the log or to audit_log:
// forbidden without moderate in the thread's forum; conflict on an archived
// thread; then decide's own, conflict for an action that would change
// nothing and validation-failed for a bad request.
//
// decide reads the thread's state from the projection row. Two moderators
// locking at the same instant can both append; the projection is idempotent,
// so the cost is one redundant record, and that is accepted (settled in the
// plan for TKT-01M29CKRGS).
//
// Locking and pinning in an archived forum stay open to moderators (docs/08,
// TKT-01M3N24RHD): only the archived thread is refused.
func (s *Service) moderate(ctx context.Context, v domain.Viewer, id threadid.ID,
	decide func(a access, now time.Time) (threadlog.Append, error)) (dto.ThreadHeader, error) {
	var out dto.ThreadHeader
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if !a.canModerate {
		return out, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	if _, err := s.appendEvent(ctx, id, func(th *threadcache.Thread) (threadlog.Append, error) {
		if th.Archived() {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		now := s.now()
		rec, err := decide(a, now)
		if err != nil {
			return rec, err
		}
		rec.Flags, rec.Author, rec.TimeMS = flags, uint64(v.UserID), now.UnixMilli()
		return rec, nil
	}); err != nil {
		return out, err
	}
	return s.threadHeader(ctx, v, id)
}

// appendEvent appends the record build returns to thread id, under the
// thread's actor, and projects the thread synchronously through Project, as
// CreateThread does. build sees the open thread and may refuse, in which
// case nothing is appended.
//
// The projection is what writes a moderation record's audit_log row, in the
// transaction that projects it (internal/events/projection audit.go), so the
// row exists when this returns. It is also what access reads the thread's
// locked state from, so a member's reply sent right after a lock is refused.
// A failed projection is returned, and the handler answers internal; the bus
// delivers the same event to the projection writer, which retries until the
// record and its row are projected together.
//
// The moderation tickets that write thread records share it: rename and move
// (TKT-01M29CKRH9), a moderator's edit, delete and restore of another's post
// (TKT-01M29CRJ5J, through appendProjecting), and
// the flag auto-hide (TKT-01M29CP2PJ).
func (s *Service) appendEvent(ctx context.Context, id threadid.ID,
	build func(th *threadcache.Thread) (threadlog.Append, error)) (threadlog.Result, error) {
	return s.appendProjecting(ctx, id, func(th *threadcache.Thread) (threadlog.Append, bool, error) {
		rec, err := build(th)
		return rec, true, err
	})
}

// appendProjecting is appendEvent for a caller that decides from the open
// thread whether its record is a moderation record: build's bool says
// whether to project synchronously. Edit, Delete and Restore use it, and
// project only a record by somebody other than the post's author
// (TKT-01M29CRJ5J), so an author's own action keeps the asynchronous path.
func (s *Service) appendProjecting(ctx context.Context, id threadid.ID,
	build func(th *threadcache.Thread) (threadlog.Append, bool, error)) (threadlog.Result, error) {
	var (
		res     threadlog.Result
		project bool
	)
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		rec, p, err := build(th)
		if err != nil {
			return err
		}
		project = p
		res, err = s.Writer.Append(th, rec)
		return err
	})
	if err != nil {
		return res, err
	}
	if project && s.Project != nil {
		if err := s.Project(ctx, id, res.LogLength); err != nil {
			return res, fmt.Errorf("threads: projecting %s after a moderation record: %w", id, err)
		}
	}
	return res, nil
}

// threadHeader is thread id's header as its page now serves it to v: the
// projection row read again, the forum, the tags, and one index snapshot for
// the post count and log length.
func (s *Service) threadHeader(ctx context.Context, v domain.Viewer, id threadid.ID) (dto.ThreadHeader, error) {
	a, err := s.access(ctx, v, id)
	if err != nil {
		return dto.ThreadHeader{}, err
	}
	var (
		forum dbq.Forum
		tags  []string
	)
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if forum, err = q.GetForum(ctx, a.row.ForumID); err != nil {
			return fmt.Errorf("threads: the forum of %s: %w", id, err)
		}
		tags, err = q.ThreadTags(ctx, int64(id))
		return err
	}); err != nil {
		return dto.ThreadHeader{}, err
	}
	var (
		n         int
		logLength int64
	)
	if err := s.Threads.With(id, func(th *threadcache.Thread) error {
		th.Actor.Stable(func(l int64) { n, logLength = th.Index.Len(), l })
		return nil
	}); err != nil {
		return dto.ThreadHeader{}, err
	}
	return header(id, a.row, forum, tags, n, logLength, s.now()), nil
}

// pinActive reports whether row is pinned at now: a pin whose pinned_until
// has passed is no pin (views.activePin says the same in SQL).
func pinActive(row dbq.Thread, now time.Time) bool {
	return row.PinnedScope.Valid && (!row.PinnedUntil.Valid || row.PinnedUntil.Int64 > now.UnixMilli())
}

func checkScope(scope string) error {
	if scope != PinForum && scope != PinGlobal {
		return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: a pin's scope is forum or global, not %q", scope))
	}
	return nil
}

func checkReason(reason string) error {
	if utf8.RuneCountInString(reason) > MaxReason {
		return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: the reason is over %d characters", MaxReason))
	}
	return nil
}
