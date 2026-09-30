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
	"github.com/terva-sh/ketju/internal/settings"
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

// Rename is PATCH /threads/{id} (docs/07): a ThreadRenamed record with the
// new title and the slug Slug makes of it. The old slug keeps working,
// because a thread is found by its id and a client corrects a stale slug
// (docs/07 "a stale slug redirects").
//
// A viewer with moderate in the thread's forum may rename it at any time.
// The thread's author, without moderate, may rename it for
// threads.rename_window_seconds after starting it, as they may edit their
// own post (docs/08 "Primitives", TKT-01M29CKRH9). Refusals come in this
// order, all before the append:
//   - forbidden to anyone else;
//   - conflict on an archived thread;
//   - for the author: conflict in an archived forum (readOnlyForum) and in
//     a locked thread, forbidden without reply in the forum, and
//     rename-window-closed once the window has passed;
//   - validation-failed for a title checkTitle refuses, and conflict for
//     the title the thread already has, so the log keeps no record that
//     changes nothing.
//
// Only a moderator's rename of somebody else's thread is audited, and its
// row names no title; the projection decides both (internal/events/
// projection audit.go).
func (s *Service) Rename(ctx context.Context, v domain.Viewer, id threadid.ID, title string) (dto.ThreadHeader, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.ThreadHeader{}, err
	}
	window, err := s.renameWindow(ctx)
	if err != nil {
		return dto.ThreadHeader{}, err
	}
	return s.appendDecided(ctx, v, id, func(a access, th *threadcache.Thread, now time.Time) (threadlog.Append, error) {
		author := a.row.AuthorID == v.UserID
		if !a.canModerate && !author {
			return threadlog.Append{}, problem.Coded(problem.CodeForbidden,
				fmt.Errorf("threads: only its author or a moderator may rename %s", id))
		}
		if th.Archived() {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		if !a.canModerate {
			if err := readOnlyForum(a); err != nil {
				return threadlog.Append{}, err
			}
			if a.row.Locked != 0 {
				return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is locked", id))
			}
			if !a.canReply {
				return threadlog.Append{}, problem.Coded(problem.CodeForbidden,
					fmt.Errorf("threads: this viewer may not write in %s's forum", id))
			}
			if !within(a.row.CreatedAt, now, window) {
				return threadlog.Append{}, problem.Coded(problem.CodeRenameWindowClosed,
					fmt.Errorf("threads: the rename window of %s has passed", window))
			}
		}
		t, err := checkTitle(title)
		if err != nil {
			return threadlog.Append{}, err
		}
		if t == a.row.Title {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s already has this title", id))
		}
		return threadlog.Append{Type: threadlog.TypeThreadRenamed, Body: &threadlog.ThreadRenamedBody{Title: t, Slug: Slug(t)}}, nil
	})
}

// Move is POST /admin/threads/{id}/move (docs/07): a ThreadMoved record
// naming the forum whose slug is forumSlug. It needs moderate in both
// forums, so a moderator of one forum cannot push a thread into a forum
// they could not move it back out of (settled in grooming for
// TKT-01M29CKRH9). Refusals come in this order, all before the append:
//   - forbidden without moderate in the thread's forum;
//   - conflict on an archived thread;
//   - not-found for a destination that does not exist or is hidden from v,
//     and forbidden for one v cannot read, as readForum answers;
//   - forbidden without moderate in the destination;
//   - conflict for an archived destination, to everyone but an admin. The
//     flag is read from the destination's row on this request, not from the
//     decision cache. Moving a thread out of an archived forum is open to
//     its moderators (maintainer decision 2026-09-29 on TKT-01M3N24RHD);
//   - conflict for the forum the thread is already in.
//
// Every move is audited with the forum the thread left and the one it went
// to, by the projection.
func (s *Service) Move(ctx context.Context, v domain.Viewer, id threadid.ID, forumSlug string) (dto.ThreadHeader, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.ThreadHeader{}, err
	}
	return s.appendDecided(ctx, v, id, func(a access, th *threadcache.Thread, _ time.Time) (threadlog.Append, error) {
		if !a.canModerate {
			return threadlog.Append{}, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
		}
		if th.Archived() {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		dest, err := s.readForum(ctx, v, forumSlug)
		if err != nil {
			return threadlog.Append{}, err
		}
		can, err := s.Perm.Can(ctx, v, dest.ID, domain.PermModerate)
		if err != nil {
			return threadlog.Append{}, err
		}
		if !can {
			return threadlog.Append{}, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate forum %q", forumSlug))
		}
		if dest.Archived != 0 && v.Role != domain.RoleAdmin {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: forum %q is archived", forumSlug))
		}
		if dest.ID == a.row.ForumID {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is already in %q", id, forumSlug))
		}
		return threadlog.Append{Type: threadlog.TypeThreadMoved, Body: &threadlog.ThreadMovedBody{ForumID: uint64(dest.ID)}}, nil
	})
}

// renameWindow is threads.rename_window_seconds.
func (s *Service) renameWindow(ctx context.Context) (time.Duration, error) {
	var n int
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = settings.RenameWindowSeconds.In(ctx, q)
		return err
	})
	return time.Duration(n) * time.Second, err
}

// appendDecided appends the record decide builds, deciding on the log the
// record lands in: under the thread it reads the log's length L, projects
// the thread up to L, admits v from the row that gives, and appends at L
// (threadlog.Append.AtLength), with v as the actor. A record that lands
// between the decision and the append fails it with ErrLogMoved, and the
// request is decided again, so a move is always judged against the forum
// the thread is in when its record lands: two moderators moving one thread
// at once cannot both pass on a forum the first move just took it out of.
// The suspension decides the same way (authorThread).
//
// decide may refuse, and then nothing is appended. The answer is the
// thread's header as its page now serves it, once the record is projected,
// which is also what writes its audit row.
//
// A thread written to without pause for threadAttempts decisions is
// conflict: it lost the race every time.
func (s *Service) appendDecided(ctx context.Context, v domain.Viewer, id threadid.ID,
	decide func(a access, th *threadcache.Thread, now time.Time) (threadlog.Append, error)) (dto.ThreadHeader, error) {
	var out dto.ThreadHeader
	// Admitted once from the row as it stands, so a thread that does not
	// exist, or is hidden from v or quarantined, is answered as access
	// answers it before the thread is opened.
	if _, err := s.access(ctx, v, id); err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	for range threadAttempts {
		var res threadlog.Result
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			at := th.Actor.LogLength()
			if err := s.projectNow(ctx, id, at); err != nil {
				return err
			}
			a, err := s.access(ctx, v, id)
			if err != nil {
				return err
			}
			now := s.now()
			rec, err := decide(a, th, now)
			if err != nil {
				return err
			}
			rec.Flags, rec.Author, rec.TimeMS, rec.AtLength = flags, uint64(v.UserID), now.UnixMilli(), at
			res, err = s.Writer.Append(th, rec)
			return err
		})
		if errors.Is(err, threadlog.ErrLogMoved) {
			continue
		}
		if err != nil {
			return out, err
		}
		if err := s.projectNow(ctx, id, res.LogLength); err != nil {
			return out, err
		}
		return s.threadHeader(ctx, v, id)
	}
	return out, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s kept changing while this request was decided", id))
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
// The flag auto-hide (TKT-01M29CP2PJ) shares it. Rename and move
// (TKT-01M29CKRH9) decide at a log length instead, through appendDecided,
// because a move's refusal depends on the forum the thread is in. A moderator's
// edit, delete and restore of another's post (TKT-01M29CRJ5J) append through
// changePost, which reads the answer back first, and project through
// projectNow as this does.
func (s *Service) appendEvent(ctx context.Context, id threadid.ID,
	build func(th *threadcache.Thread) (threadlog.Append, error)) (threadlog.Result, error) {
	var res threadlog.Result
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		rec, err := build(th)
		if err != nil {
			return err
		}
		res, err = s.Writer.Append(th, rec)
		return err
	})
	if err != nil {
		return res, err
	}
	return res, s.projectNow(ctx, id, res.LogLength)
}

// projectNow projects thread id up to n through Project, synchronously, for
// a moderation record whose audit row must exist when the request answers.
// No Project is no synchronous projection, as in a service built without one.
func (s *Service) projectNow(ctx context.Context, id threadid.ID, n int64) error {
	if s.Project == nil {
		return nil
	}
	if err := s.Project(ctx, id, n); err != nil {
		return fmt.Errorf("threads: projecting %s after a moderation record: %w", id, err)
	}
	return nil
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
