package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
// now is validation-failed. Before those, a pin with scope global, or one
// that would replace an active global pin, is forbidden to a viewer who is
// not a moderator or admin by role (staffPin).
func (s *Service) Pin(ctx context.Context, v domain.Viewer, id threadid.ID, scope string, until *time.Time) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, now time.Time) (threadlog.Append, error) {
		if err := staffPin(a, scope, now); err != nil {
			return threadlog.Append{}, err
		}
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
// pin included: it is no pin. Before that, an unpin with scope global, or
// any unpin while a global pin is active, is forbidden to a viewer who is
// not a moderator or admin by role (staffPin).
func (s *Service) Unpin(ctx context.Context, v domain.Viewer, id threadid.ID, scope string) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, now time.Time) (threadlog.Append, error) {
		if err := staffPin(a, scope, now); err != nil {
			return threadlog.Append{}, err
		}
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

// Retag is PUT /admin/threads/{id}/tags (docs/07 "Retagging threads"): one
// ThreadTagged record whose tags replace the thread's whole set (docs/03),
// checked and ordered as on create by NormalizeTags. An empty set removes
// every tag. The retagger moderates the forum, so a reserved tag may be set
// or removed (docs/08 "Thread tags"). Refusals come in moderate's order,
// all before the append: forbidden without moderate in the thread's forum,
// conflict on an archived thread, then validation-failed for a set
// NormalizeTags refuses, and conflict for the set the thread already has,
// so the log keeps no record that changes nothing. A retag in an archived
// forum is allowed, as a lock is.
//
// Every retag is audited, with no tag text, by the projection.
func (s *Service) Retag(ctx context.Context, v domain.Viewer, id threadid.ID, tags []string) (dto.ThreadHeader, error) {
	return s.moderate(ctx, v, id, func(a access, _ time.Time) (threadlog.Append, error) {
		set, err := NormalizeTags(tags)
		if err != nil {
			return threadlog.Append{}, err
		}
		var have []string
		if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			have, err = q.ThreadTags(ctx, a.row.ID)
			return err
		}); err != nil {
			return threadlog.Append{}, err
		}
		if slices.Equal(set, have) {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s already has these tags", id))
		}
		return threadlog.Append{Type: threadlog.TypeThreadTagged, Body: &threadlog.ThreadTaggedBody{Tags: set}}, nil
	})
}

// Archive is POST /admin/threads/{id}/archive (docs/07 "Archiving
// threads"): a ThreadArchived record with the reason, after which the
// thread is read-only to everyone, admins included, and v1 has no unarchive
// (docs/08 "Locking semantics", docs/15; maintainer, 2026-10-02,
// TKT-01M3YSQQ5X). Only an admin, by role, archives (docs/08
// "Primitives"); the route's gate says so and this says it again, so no
// caller relies on the route alone. Refusals come in this order, all before
// the append: forbidden to anyone but an admin, conflict on a thread
// already archived, validation-failed for a reason over MaxReason, then
// conflict while a move of posts into or out of the thread is finishing.
//
// The actor seals itself as it writes the record, so nothing lands after
// it, however close behind. MarkArchived then moves the thread to the front
// of the cache's eviction order. The projection writes thread.archived.
func (s *Service) Archive(ctx context.Context, v domain.Viewer, id threadid.ID, reason string) (dto.ThreadHeader, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.ThreadHeader{}, err
	}
	// No move of posts into or out of the thread is decided or written while
	// it is archived, and one still finishing refuses the archive, so the
	// finisher never finds its target sealed (TKT-01M29CXJZZ).
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	h, err := s.appendDecided(ctx, v, id, func(a access, th *threadcache.Thread, _ time.Time) (threadlog.Append, error) {
		if v.Role != domain.RoleAdmin {
			return threadlog.Append{}, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: only an admin may archive %s", id))
		}
		if th.Archived() || a.row.Archived != 0 {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is already archived", id))
		}
		if err := checkReason(reason); err != nil {
			return threadlog.Append{}, err
		}
		if err := s.moveUnfinished(ctx, id); err != nil {
			return threadlog.Append{}, err
		}
		return threadlog.Append{Type: threadlog.TypeThreadArchived, Body: &threadlog.ThreadArchivedBody{Reason: reason}}, nil
	})
	if err != nil {
		return h, err
	}
	s.Threads.MarkArchived(id)
	return h, nil
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
		if a.canModerate {
			if th.Archived() {
				return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
			}
		} else if err := authorRenameCheck(a, th.Archived(), window, now); err != nil {
			return threadlog.Append{}, err
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

// authorRenameCheck is what a thread's author, without moderate, must pass
// to rename it at now, in Rename's order: conflict on an archived thread, in
// an archived forum (readOnlyForum) and in a locked thread; forbidden
// without reply in the forum; and rename-window-closed once window has
// passed since the thread was started. Rename and the viewer block's
// rename_until (renameUntil) both call it, so the offer and the refusal
// cannot disagree (TKT-01M3QXV399).
func authorRenameCheck(a access, archived bool, window time.Duration, now time.Time) error {
	id := threadid.ID(a.row.ID)
	if archived {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
	}
	if err := readOnlyForum(a); err != nil {
		return err
	}
	if a.row.Locked != 0 {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is locked", id))
	}
	if !a.canReply {
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not write in %s's forum", id))
	}
	if !within(a.row.CreatedAt, now, window) {
		return problem.Coded(problem.CodeRenameWindowClosed, fmt.Errorf("threads: the rename window of %s has passed", window))
	}
	return nil
}

// renameUntil is the viewer block's rename_until: the end of a's rename
// window, when a renames as the thread's author (mayOwnRename) and passes
// authorRenameCheck at now. It is nil for everyone else, and so always nil
// for a moderator, who may rename at any time. window is renameWindow's,
// clamped to the longest Duration, so the end is at most about 292 years
// after the thread was started and never overflows.
func renameUntil(a access, archived bool, window time.Duration, now time.Time) *dto.Millis {
	if !mayOwnRename(a) || authorRenameCheck(a, archived, window, now) != nil {
		return nil
	}
	at := dto.Millis(a.row.CreatedAt + window.Milliseconds())
	return &at
}

// mayOwnRename reports whether a renames as the thread's author: a member
// who started it and does not moderate its forum.
func mayOwnRename(a access) bool {
	return a.viewer.Member() && !a.canModerate && a.row.AuthorID == a.viewer.UserID
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
	// A move of posts judges both threads' forums; none changes while one
	// is decided (TKT-01M29CXJZZ).
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
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

// renameWindow is threads.rename_window_seconds, through secondsDuration,
// so a count too large for a Duration is the longest window there is.
func (s *Service) renameWindow(ctx context.Context) (time.Duration, error) {
	var n int
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = settings.RenameWindowSeconds.In(ctx, q)
		return err
	})
	if err != nil {
		return 0, err
	}
	return secondsDuration(n), nil
}

// threadExists answers not-found for a thread with no projection row, as
// access does, and asks nothing about the viewer.
func (s *Service) threadExists(ctx context.Context, id threadid.ID) error {
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.GetThread(ctx, int64(id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s has no projection row", id))
	}
	return err
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
// decide may refuse, and then nothing is appended. The refusal is answered
// only if the log is still the length it was decided at, and otherwise the
// request is decided again (stillAt). The answer is the
// thread's header as its page now serves it, once the record is projected,
// which is also what writes its audit row.
//
// A thread written to without pause for threadAttempts decisions is
// conflict: it lost the race every time.
func (s *Service) appendDecided(ctx context.Context, v domain.Viewer, id threadid.ID,
	decide func(a access, th *threadcache.Thread, now time.Time) (threadlog.Append, error)) (dto.ThreadHeader, error) {
	var out dto.ThreadHeader
	// Only whether the thread exists is asked before it is opened, so a
	// thread id that names nothing is not-found, as access answers it,
	// rather than a failure to open a log. Whether v may act is never
	// judged from the row before it is projected: a move already in the
	// log may have taken the thread into a forum v can read, or out of one
	// (Terva review 1535, finding 1).
	if err := s.threadExists(ctx, id); err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	for range threadAttempts {
		var res threadlog.Result
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			a, at, err := s.admitAt(ctx, v, id, th)
			if err != nil {
				return err
			}
			now := s.now()
			rec, err := decide(a, th, now)
			if err != nil {
				return stillAt(th, at, err)
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

// stillAt answers err, a refusal made once v was admitted at log length at,
// only if the log is still that length, and ErrLogMoved otherwise, so the
// request is decided again. A refusal is an answer, and decide may read state
// newer than at, such as the thread cache's archived flag or, in SetSlowMode,
// the value in its transaction. Checking the length after it keeps a moderator
// whom a move has just taken the thread from from being told why an action
// would change nothing there (Terva review 1570, finding 1). Access errors
// are not passed through here: a thread hidden from v stays not-found
// however busy its log is.
func stillAt(th *threadcache.Thread, at int64, err error) error {
	if err != nil && !errors.Is(err, threadlog.ErrLogMoved) && th.Actor.LogLength() != at {
		return threadlog.ErrLogMoved
	}
	return err
}

// admitAt reads the log length L of the open thread th, projects thread id
// up to L, and admits v from the row that gives. Whatever the caller decides
// from the answer is decided at L: appendDecided appends at L
// (threadlog.Append.AtLength), and SetSlowMode, which appends nothing,
// checks the length again inside its transaction.
func (s *Service) admitAt(ctx context.Context, v domain.Viewer, id threadid.ID, th *threadcache.Thread) (access, int64, error) {
	at := th.Actor.LogLength()
	// A quarantined thread's log is shorter than its projection and cannot
	// be projected; access answers it from the row, unavailable to a reader
	// and not-found to anyone else.
	if s.Quarantined == nil || !s.Quarantined(id) {
		if err := s.projectNow(ctx, id, at); err != nil {
			return access{}, 0, err
		}
	}
	a, err := s.access(ctx, v, id)
	return a, at, err
}

// moderate is the four thread actions, lock, unlock, pin and unpin: the
// checks every one of them makes, then the record decide builds, appended
// with the viewer as its actor and projected before the answer. The
// refusals come in this order, all before the append, so a refusal writes
// nothing, to the log or to audit_log: forbidden without moderate in the
// thread's forum; conflict on an archived thread; then decide's own:
// forbidden for a global pin to a viewer who is not staff by role
// (staffPin), conflict for an action that would change nothing, and
// validation-failed for a bad request.
//
// The routes are behind the moderates gate, which admits a member who
// moderates any forum, so the forum is decided here, at a log length,
// through appendDecided. A move that lands between the decision and the
// append fails the append, and the request is decided again against the
// forum the move took the thread to, so a moderator of one forum cannot lock
// or pin a thread that has just left it (docs/08 "Decided 2026-09-30",
// TKT-01M3RQP0TG). Two moderators locking at the same instant are decided
// one after the other, and the second answers conflict.
//
// Locking and pinning in an archived forum stay open to moderators (docs/08,
// TKT-01M3N24RHD): only the archived thread is refused.
func (s *Service) moderate(ctx context.Context, v domain.Viewer, id threadid.ID,
	decide func(a access, now time.Time) (threadlog.Append, error)) (dto.ThreadHeader, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.ThreadHeader{}, err
	}
	return s.appendDecided(ctx, v, id, func(a access, th *threadcache.Thread, now time.Time) (threadlog.Append, error) {
		if !a.canModerate {
			return threadlog.Append{}, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
		}
		if th.Archived() {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		return decide(a, now)
	})
}

// staffPin refuses a global pin to a viewer who is not a moderator or admin
// by role: pinning with scope global, and any pin or unpin while a global pin
// is active, which would take it down or replace it. A global pin heads the
// thread list of every forum whose readers can read the thread
// (views.PinnedThreads), so it changes forums other than the thread's, and
// moderating the thread's forum is not moderating those (TKT-01M3RQP0TG). A
// forum moderator by grant still pins and unpins with scope forum.
func staffPin(a access, scope string, now time.Time) error {
	if a.viewer.Staff() {
		return nil
	}
	if scope == PinGlobal || (pinActive(a.row, now) && a.row.PinnedScope.String == PinGlobal) {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: only a moderator or admin by role may change a global pin on %s", threadid.ID(a.row.ID)))
	}
	return nil
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
// The flag auto-hide (TKT-01M29CP2PJ) uses it. Lock, unlock, pin, unpin,
// rename and move decide at a log length instead, through appendDecided,
// because who may take them depends on the forum the thread is in, and a
// move changes that (TKT-01M29CKRH9, TKT-01M3RQP0TG). A moderator's
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
		if rec.Type == threadlog.TypeEdit {
			e, err := th.Index.Entry(rec.Ordinal)
			if err != nil {
				return err
			}
			if err := s.copyPending(ctx, th, rec.Ordinal, e); err != nil {
				return err
			}
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
		forum   dbq.Forum
		starter dto.Author
		tags    []string
	)
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if forum, err = q.GetForum(ctx, a.row.ForumID); err != nil {
			return fmt.Errorf("threads: the forum of %s: %w", id, err)
		}
		if starter, err = starterOf(ctx, q, a.row.AuthorID); err != nil {
			return err
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
	return header(id, a.row, forum, starter, tags, n, logLength, s.now()), nil
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
