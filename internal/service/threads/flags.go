package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
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

// The reasons a flag may give (docs/08 "Flags"): the four the flags table's
// CHECK allows. Other needs a note saying what it is.
const (
	FlagSpam     = "spam"
	FlagAbuse    = "abuse"
	FlagOffTopic = "off-topic"
	FlagOther    = "other"
)

// MaxFlagNote is the longest note a flag takes, in characters.
const MaxFlagNote = 1000

// AutoHideReason is the reason the auto-hide's Delete carries.
const AutoHideReason = "hidden pending review"

// flagLockStripes is how many mutexes flagLocks spreads posts over.
const flagLockStripes = 256

// flagLocks is the one per-post lock the auto-hide, a moderator's restore
// and ConfirmHide hold (docs/08 "Flags", TKT-01M29CP2PJ; the moderator
// queue, TKT-01M29CRJ4E, holds it too). Without it the sweep could read a
// post's open flags just before a restore dismissed them and append its hide
// just after the Restore landed, hiding the post the moderator had just
// restored.
//
// It is in process, and that is enough: ketju serve is the only writer, and
// the data-directory lock keeps it to one process (docs/03 "Write path").
// Posts share stripes, which costs an unrelated post a short wait and
// nothing else. The zero value is ready. A caller holds one stripe at a time
// and takes it before the thread, never inside Threads.With, so no two
// orders exist to deadlock.
type flagLocks struct {
	stripes [flagLockStripes]sync.Mutex
}

// lock takes the stripe of post ordinal of thread id and returns its unlock.
func (l *flagLocks) lock(id threadid.ID, ordinal uint32) func() {
	h := uint64(id)*0x9e3779b97f4a7c15 ^ uint64(ordinal)*0xc2b2ae3d27d4eb4f
	m := &l.stripes[(h>>32)%flagLockStripes]
	m.Lock()
	return m.Unlock
}

// FlagRequest is a member's report of a post.
type FlagRequest struct {
	Ordinal uint32
	Reason  string
	Note    string
}

// errNothingToHide stops an auto-hide's append when the post is already
// down or its thread takes no records.
var errNothingToHide = errors.New("threads: nothing to hide")

// reopenTimeout bounds the reopen of a failed restore's flags, which runs
// on after the request's context is gone.
const reopenTimeout = 10 * time.Second

// errNoOpenFlags is a resolve that found no open flag on the post.
var errNoOpenFlags = errors.New("threads: the post has no open flag")

// Flag is POST /threads/{id}/flags (docs/07): a member's report of a post,
// then the auto-hide it may owe (docs/08 "Flags").
//
// The viewer needs a member session and read access to the thread. A reason
// outside the four, other without a note, or a note over MaxFlagNote is
// validation-failed; an ordinal the thread does not have is not-found; a
// post already removed or moved, or any post of an archived thread, is
// conflict. A member's second open flag on a post adds no row
// (flags_one_open_per_reporter), so the open count is a count of reporters.
//
// The answer is the same receipt for a new flag, a repeat and the flag that
// hid the post: a reporter learns no count, no reporter and no outcome. A
// repeat from a reporter whose flag is still open gets the receipt even
// after the post went down, so that it does not say the post is hidden
// (Terva review 1506); only a member with no open flag on a post that is not
// shown gets conflict.
//
// The flag is committed before the hide is tried, and the hide reads only
// committed flags. If the hide fails, the answer is unavailable with the
// flag kept, and AutoHideSweep hides the post within a minute.
//
// The check that the post is shown, the insert and the hide all hold the
// post's flag lock (Terva review 1504 on PR #176). A flag checked against
// the post before a moderator's restore therefore cannot be inserted after
// the restore dismissed the others, and hide the post the moderator just
// restored. A flag that waited for the restore checks the restored post and
// counts from zero.
func (s *Service) Flag(ctx context.Context, v domain.Viewer, id threadid.ID, req FlagRequest) (dto.FlagReceipt, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.FlagReceipt{}, err
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return dto.FlagReceipt{}, err
	}
	if err := checkFlag(req.Reason, req.Note); err != nil {
		return dto.FlagReceipt{}, err
	}
	unlock := s.flagLocks.lock(id, req.Ordinal)
	defer unlock()
	if s.beforeFlagCheck != nil {
		s.beforeFlagCheck(id, req.Ordinal)
	}
	shown := false
	if err := s.Threads.With(id, func(th *threadcache.Thread) error {
		if th.Archived() {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		e, err := th.Index.Entry(req.Ordinal)
		shown = err == nil && e.Flags.Visible()
		return err
	}); err != nil {
		return dto.FlagReceipt{}, err
	}
	if !shown {
		var repeat bool
		if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			repeat, err = q.HasOpenFlag(ctx, dbq.HasOpenFlagParams{ThreadID: int64(id), Ordinal: int64(req.Ordinal), ReporterID: v.UserID})
			return err
		}); err != nil {
			return dto.FlagReceipt{}, err
		}
		if repeat {
			return dto.FlagReceipt{Received: true}, nil
		}
		return dto.FlagReceipt{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: post %d of %s is not shown", req.Ordinal, id))
	}
	if err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateFlag(ctx, dbq.CreateFlagParams{ThreadID: int64(id), Ordinal: int64(req.Ordinal),
			ReporterID: v.UserID, Reason: req.Reason, Note: req.Note, CreatedAt: s.now().UnixMilli()})
		if errors.Is(err, sql.ErrNoRows) {
			return nil // this reporter's flag on the post is already open
		}
		return err
	}); err != nil {
		return dto.FlagReceipt{}, err
	}
	if _, err := s.enforceAutoHide(ctx, id, req.Ordinal); err != nil {
		return dto.FlagReceipt{}, problem.Coded(problem.CodeUnavailable,
			fmt.Errorf("threads: the flag on post %d of %s is kept, and the hide it owes is left to the sweep: %w",
				req.Ordinal, id, err))
	}
	return dto.FlagReceipt{Received: true}, nil
}

// checkFlag refuses a reason outside the four, other with no note, and a
// note over MaxFlagNote.
func checkFlag(reason, note string) error {
	switch reason {
	case FlagSpam, FlagAbuse, FlagOffTopic:
	case FlagOther:
		if strings.TrimSpace(note) == "" {
			return problem.Coded(problem.CodeValidationFailed, errors.New("threads: a flag for another reason says what it is"))
		}
	default:
		return problem.Coded(problem.CodeValidationFailed,
			fmt.Errorf("threads: a flag's reason is spam, abuse, off-topic or other, not %q", reason))
	}
	if utf8.RuneCountInString(note) > MaxFlagNote {
		return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: the note is over %d characters", MaxFlagNote))
	}
	return nil
}

// EnforceAutoHide hides post ordinal of thread id pending review if its open
// flags are at or over flags.auto_hide_threshold and it is still shown
// (docs/08 "Flags"). It reports whether it appended the hide.
//
// It is idempotent and reads only durable state: the committed flags and
// the thread's index. So it runs on every flag at or over the threshold,
// not only the one that crossed it, and AutoHideSweep can run it for any
// post at any time. It holds the post's flag lock from reading the count
// through the append, so a moderator's restore cannot land between them.
//
// The hide is a Delete with auto and actor 0, the system, refusing a post
// already gone, projected before this returns so that the post.auto_hidden
// row the projection writes exists. A quarantined or archived thread, a
// post already down, and a post that went down before the append do
// nothing.
func (s *Service) EnforceAutoHide(ctx context.Context, id threadid.ID, ordinal uint32) (bool, error) {
	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()
	return s.enforceAutoHide(ctx, id, ordinal)
}

// enforceAutoHide is EnforceAutoHide for a caller already holding the
// post's flag lock.
func (s *Service) enforceAutoHide(ctx context.Context, id threadid.ID, ordinal uint32) (bool, error) {
	if s.Quarantined != nil && s.Quarantined(id) {
		return false, nil
	}
	var threshold, open int64
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		n, err := settings.FlagAutoHideThreshold.In(ctx, q)
		if err != nil {
			return err
		}
		threshold = int64(n)
		open, err = q.OpenFlagCount(ctx, dbq.OpenFlagCountParams{ThreadID: int64(id), Ordinal: int64(ordinal)})
		return err
	}); err != nil {
		return false, err
	}
	if open < threshold {
		return false, nil
	}
	if s.afterFlagCount != nil {
		s.afterFlagCount(id, ordinal)
	}
	res, err := s.appendEvent(ctx, id, func(th *threadcache.Thread) (threadlog.Append, error) {
		if th.Archived() {
			return threadlog.Append{}, errNothingToHide
		}
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return threadlog.Append{}, err
		}
		if !e.Flags.Visible() {
			return threadlog.Append{}, errNothingToHide
		}
		return threadlog.Append{
			Type: threadlog.TypeDelete, Author: 0, Ordinal: ordinal, TimeMS: s.now().UnixMilli(),
			Body:   &threadlog.DeleteBody{Reason: AutoHideReason, Auto: true},
			Refuse: gone,
		}, nil
	})
	switch {
	case errors.Is(err, errNothingToHide), errors.Is(err, threadlog.ErrPostState):
		return false, nil
	case res.LogLength != 0:
		// Durable: a failed projection is the bus's to retry, and the
		// hide happened.
		return true, err
	}
	return false, err
}

// SweepAutoHide runs EnforceAutoHide for every post the flags say is owed a
// hide: at least flags.auto_hide_threshold open flags and not down in the
// projection. It is AutoHideSweep's work, every minute and once at startup,
// and it recovers a hide whose append failed after its flag was committed,
// and enforces a lowered threshold. It reports how many posts it hid. One
// post's failure does not stop the others; every failure is returned.
func (s *Service) SweepAutoHide(ctx context.Context) (int, error) {
	var owed []dbq.PostsAtFlagThresholdRow
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		n, err := settings.FlagAutoHideThreshold.In(ctx, q)
		if err != nil {
			return err
		}
		owed, err = q.PostsAtFlagThreshold(ctx, int64(n))
		return err
	}); err != nil {
		return 0, err
	}
	var (
		hidden int
		errs   []error
	)
	for _, p := range owed {
		if err := ctx.Err(); err != nil {
			return hidden, err
		}
		id := threadid.ID(p.ThreadID)
		ok, err := s.EnforceAutoHide(ctx, id, uint32(p.Ordinal))
		if ok {
			hidden++
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("post %d of %s: %w", p.Ordinal, id, err))
		}
	}
	return hidden, errors.Join(errs...)
}

// restoreDismissingFlags is a moderator's restore of post ordinal of thread
// id, under the post's flag lock (docs/08 "Flags", TKT-01M29CP2PJ).
//
// It first dismisses the post's open flags, in place, as note by v, keeping
// their ids. Then run appends the Restore. Only if run appended nothing,
// because the append was refused or failed, are exactly those flags
// reopened, since nothing happened to the post. Once the Restore is
// durable, a failed read-back or projection leaves them dismissed: the
// Restore happened, and flags left open beside it would let the sweep hide
// the post again. With the flags dismissed before the append and the lock
// held throughout, no sweep sees the restored post beside open flags.
//
// Every moderator's restore takes this path, not only one of a post hidden
// pending review: a post a moderator restores with open flags at the
// threshold would otherwise be hidden again by the next sweep, undoing the
// moderator's decision. A flag reported after the restore counts from zero.
//
// If the reopen itself fails, the post stays down with its flags
// dismissed. That fails closed, and the moderator, who got an error, can
// restore again.
func (s *Service) restoreDismissingFlags(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, note string,
	run func() (dto.CreatedPost, bool, error)) (dto.CreatedPost, error) {
	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()
	var dismissed []int64
	if err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		dismissed, err = q.DismissOpenFlagsForPost(ctx, dbq.DismissOpenFlagsForPostParams{
			ResolvedBy:     sql.NullInt64{Int64: v.UserID, Valid: true},
			ResolvedAt:     sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true},
			ResolutionNote: note, ThreadID: int64(id), Ordinal: int64(ordinal),
		})
		return err
	}); err != nil {
		return dto.CreatedPost{}, err
	}
	out, appended, err := run()
	if !appended && len(dismissed) > 0 {
		// Not the request's context: an append that failed because the
		// request was cancelled must still have its flags reopened (Terva
		// review 1506). Bounded, so a stuck database cannot hold the lock.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reopenTimeout)
		defer cancel()
		if rerr := s.DB.Write(rctx, func(q *dbq.Queries, _ *sql.Tx) error {
			_, err := q.ReopenFlags(rctx, dismissed)
			return err
		}); rerr != nil {
			err = errors.Join(err, fmt.Errorf("threads: reopening the flags a failed restore of post %d of %s dismissed: %w",
				ordinal, id, rerr))
		}
	}
	return out, err
}

// ConfirmHide is a moderator's review of a post hidden pending review that
// keeps it hidden: a moderator's Delete (by_moderator), which turns the
// system's hide into the moderator's, then the post's open flags resolved
// in place (docs/08 "Flags", TKT-01M29CP2PJ). The moderator queue
// (TKT-01M29CRJ4E) gives it a route.
//
// It holds the post's flag lock. The Delete requires auto-hidden and
// refuses deleted and moved, so a post not pending review is conflict and
// nothing changes. The sweep acts only on a shown post, so a failure
// between the Delete and the resolve leaves nothing for it to hide again.
//
// That failure is recoverable by asking again (Terva review 1504 on PR
// #176). A post already hidden by a moderator, no longer pending review,
// with flags still open, has its flags resolved and nothing appended: that
// is a confirmation whose resolve failed, or a hide by hand the queue is
// now keeping. The same post with no open flag is conflict, since there is
// nothing left to confirm.
func (s *Service) ConfirmHide(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, reason string) (dto.CreatedPost, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.CreatedPost{}, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	if !a.canModerate {
		return dto.CreatedPost{}, problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
	}
	if err := checkReason(reason); err != nil {
		return dto.CreatedPost{}, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()
	resolve := func() error {
		return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			ids, err := q.ResolveOpenFlagsForPost(ctx, dbq.ResolveOpenFlagsForPostParams{
				ResolvedBy:     sql.NullInt64{Int64: v.UserID, Valid: true},
				ResolvedAt:     sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true},
				ResolutionNote: reason, ThreadID: int64(id), Ordinal: int64(ordinal),
			})
			if err == nil && len(ids) == 0 {
				return errNoOpenFlags
			}
			return err
		})
	}
	var (
		hidden bool
		out    dto.CreatedPost
	)
	if err := s.Threads.With(id, func(th *threadcache.Thread) error {
		// An archived thread is read-only, flags included (Terva review
		// 1506), whichever branch the confirmation would take.
		if th.Archived() {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return err
		}
		f := e.Flags
		hidden = f.Has(threadlog.EntryHiddenByMod) && f&(threadlog.EntryAutoHidden|threadlog.EntryDeleted|threadlog.EntryMoved) == 0
		if hidden {
			out, err = s.readBack(ctx, th, id, ordinal, a)
		}
		return err
	}); err != nil {
		return dto.CreatedPost{}, err
	}
	if hidden {
		switch err := resolve(); {
		case errors.Is(err, errNoOpenFlags):
			return dto.CreatedPost{}, problem.Coded(problem.CodeConflict,
				fmt.Errorf("threads: post %d of %s is already a moderator's hide with no open flag", ordinal, id))
		case err != nil:
			return dto.CreatedPost{}, err
		}
		return out, nil
	}
	out, appended, err := s.changePostAppended(ctx, id, ordinal, a, func(th *threadcache.Thread, _ threadlog.IndexEntry) (threadlog.Append, error) {
		if th.Archived() {
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		return threadlog.Append{
			Type: threadlog.TypeDelete, Flags: flags, Author: uint64(v.UserID), Ordinal: ordinal,
			TimeMS:  s.now().UnixMilli(),
			Body:    &threadlog.DeleteBody{Reason: reason, ByModerator: true},
			Require: threadlog.EntryAutoHidden,
			Refuse:  threadlog.EntryDeleted | threadlog.EntryMoved,
		}, nil
	})
	if !appended {
		return dto.CreatedPost{}, err
	}
	if rerr := resolve(); rerr != nil && !errors.Is(rerr, errNoOpenFlags) {
		err = errors.Join(err, fmt.Errorf("threads: resolving the flags of post %d of %s after its hide: %w", ordinal, id, rerr))
	}
	if err != nil {
		return dto.CreatedPost{}, err
	}
	return out, nil
}
