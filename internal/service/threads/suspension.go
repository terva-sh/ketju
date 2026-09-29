package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// A suspension's "hide their posts", and the restore that undoes it
// (docs/08 "Suspension", TKT-01M29CP2QK). Both write the thread logs, so
// everything that already understands a moderator's hide applies unchanged:
// tombstones, the author's view, forum totals, profile counts, the audit
// rows, and the response cache, whose key moves with the log length.
//
// Who may suspend whom is internal/service/account's to decide. What these
// add is the per-thread rule every moderator action has: a thread is
// touched only when v may moderate its forum, as a moderator's own delete
// or restore of the post would need. A thread v may not moderate, or may not
// see, is passed over and counted nowhere, so the answer says nothing about
// a forum hidden from v. An archived thread is read-only to everyone and is
// passed over too.
//
// The decision is tied to the log it was made on (Terva review 1495 on PR
// #169). Under the thread, the projection is brought up to the log's length
// L and v is admitted from the row it then reads, so a ThreadMoved in the
// log is in the forum v is judged against. Every record is then appended at
// the length the one before it left, starting from L
// (threadlog.Append.AtLength). A record from anyone else, a move included,
// makes the next append fail with ErrLogMoved, and the thread is decided
// again from the start.

// SuspensionReason is the reason a suspension's Delete and Restore records
// carry. The suspension's own reason is the member's to read, not the
// thread's, so it is not copied into the log.
const SuspensionReason = "suspension"

// threadAttempts bounds how many times one thread is decided again because
// another record landed between the decision and an append. It is a bound
// against a thread written to without pause, not against cost.
const threadAttempts = 16

// ErrSomeThreadsUnavailable is the answer when a hide or restore finished
// every thread it could reach, and one or more could not be: quarantined,
// failing to open, or written to without pause. Running it again once they
// are repaired or quiet finishes the job.
var ErrSomeThreadsUnavailable = errors.New("threads: some of the author's threads could not be reached")

// threadChange is the work on one thread, given the open thread, the frame
// flags for v's records, and the log length at which v was admitted. It
// appends each record at the length the previous one left, starting from
// at, and answers how many posts it changed and the log length after the
// last. An append refused with ErrLogMoved is returned as it is, and the
// thread is decided again.
type threadChange func(th *threadcache.Thread, flags threadlog.FrameFlags, at int64) (int, int64, error)

// HidePostsOf hides every visible post by author, in every thread v may
// moderate, with a moderator's Delete that carries suspension, the id of
// the suspension's audit row. It answers how many posts it hid.
//
// Each Delete refuses a post that is already deleted, hidden or moved when
// it is written, so a second run hides nothing twice and finishes what a
// failed one left. Each thread with a new record is projected before the
// next is started, so the post.hidden rows exist when this returns. A
// failure part-way answers the posts hidden so far with the error.
func (s *Service) HidePostsOf(ctx context.Context, v domain.Viewer, author, suspension int64) (int, error) {
	if err := s.suspensionPreconditions(v, author); err != nil {
		return 0, err
	}
	if suspension <= 0 {
		return 0, fmt.Errorf("threads: hiding posts needs a suspension's audit id, not %d", suspension)
	}
	return s.eachAuthorThread(ctx, v, author, func(th *threadcache.Thread, flags threadlog.FrameFlags, at int64) (int, int64, error) {
		n := 0
		for _, o := range th.Index.ByAuthor(uint64(author)) {
			e, err := th.Index.Entry(o)
			if err != nil {
				return n, at, err
			}
			if !e.Flags.Visible() {
				continue
			}
			res, err := s.Writer.Append(th, threadlog.Append{
				Type: threadlog.TypeDelete, Flags: flags, Author: uint64(v.UserID), Ordinal: o,
				TimeMS: s.now().UnixMilli(),
				Body:   &threadlog.DeleteBody{Reason: SuspensionReason, ByModerator: true, Suspension: uint64(suspension)},
				Refuse: gone, AtLength: at,
			})
			if errors.Is(err, threadlog.ErrPostState) {
				continue // nothing was written, so the log is still at at
			}
			if err != nil {
				return n, at, err
			}
			n, at = n+1, res.LogLength
		}
		return n, at, nil
	})
}

// RestorePostsOf restores exactly the posts by author that a suspension
// hid: those whose Delete in force carries one of suspensions, the audit
// ids of the account's suspensions, in every thread v may moderate. A post
// a moderator hid for its own sake, or hid again after it was restored,
// stays hidden. It answers how many posts it restored.
//
// Which Delete is in force is in the log and not the index, so the thread
// is walked up to the length v was admitted at, and each Restore is
// appended at the length the walk, or the Restore before it, left. No
// record the walk did not read can land between the decision and the
// write. The Restore also needs hidden-by-mod and refuses moved, as a
// moderator's restore does.
func (s *Service) RestorePostsOf(ctx context.Context, v domain.Viewer, author int64, suspensions []int64) (int, error) {
	if err := s.suspensionPreconditions(v, author); err != nil {
		return 0, err
	}
	ours := map[uint64]bool{}
	for _, id := range suspensions {
		if id > 0 {
			ours[uint64(id)] = true
		}
	}
	if len(ours) == 0 {
		return 0, nil
	}
	return s.eachAuthorThread(ctx, v, author, func(th *threadcache.Thread, flags threadlog.FrameFlags, at int64) (int, int64, error) {
		mine := th.Index.ByAuthor(uint64(author))
		isMine := make(map[uint32]bool, len(mine))
		for _, o := range mine {
			isMine[o] = true
		}
		inForce := map[uint32]uint64{} // ordinal -> the suspension of its Delete in force, 0 for none
		if err := th.Reader.Removals(threadlog.FileHeaderSize, at, func(o uint32, d *threadlog.DeleteBody) {
			switch {
			case !isMine[o]:
			case d == nil:
				delete(inForce, o)
			default:
				inForce[o] = d.Suspension
			}
		}); err != nil {
			return 0, at, err
		}
		n := 0
		for _, o := range mine {
			if sid, ok := inForce[o]; !ok || !ours[sid] {
				continue
			}
			res, err := s.Writer.Append(th, threadlog.Append{
				Type: threadlog.TypeRestore, Flags: flags, Author: uint64(v.UserID), Ordinal: o,
				TimeMS: s.now().UnixMilli(), Body: &threadlog.RestoreBody{Reason: SuspensionReason},
				Require: threadlog.EntryHiddenByMod, Refuse: threadlog.EntryMoved, AtLength: at,
			})
			if errors.Is(err, threadlog.ErrPostState) {
				continue // moved, or not hidden by a moderator: not a suspension's to restore
			}
			if err != nil {
				return n, at, err
			}
			n, at = n+1, res.LogLength
		}
		return n, at, nil
	})
}

// suspensionPreconditions are what HidePostsOf and RestorePostsOf ask
// before any thread: v is a member, and not the author, since a moderator's
// Delete of their own post would be an author's delete marked as a hide.
func (s *Service) suspensionPreconditions(v domain.Viewer, author int64) error {
	if err := s.MayWrite(v); err != nil {
		return err
	}
	if author == v.UserID {
		return problem.Coded(problem.CodeForbidden, errors.New("threads: a suspension's hide is never of one's own posts"))
	}
	return nil
}

// eachAuthorThread runs change on every thread holding a post by author
// that v may moderate and is not archived, and answers the total change
// reports. Each thread with a new record is projected before the next.
//
// A thread v may not see or moderate is passed over silently. One that is
// quarantined, fails to open, or keeps being written to past
// threadAttempts is passed over too, and once every other thread is done
// the answer is ErrSomeThreadsUnavailable, so the caller knows to run it
// again. Any other failure stops the walk with the posts changed so far.
func (s *Service) eachAuthorThread(ctx context.Context, v domain.Viewer, author int64, change threadChange) (int, error) {
	var ids []int64
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		ids, err = q.ThreadsWithPostsBy(ctx, author)
		return err
	}); err != nil {
		return 0, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return 0, err
	}
	var total, unreached int
	for _, raw := range ids {
		n, reached, err := s.authorThread(ctx, v, threadid.ID(raw), flags, change)
		total += n
		if err != nil {
			return total, err
		}
		if !reached {
			unreached++
		}
	}
	if unreached > 0 {
		return total, problem.Coded(problem.CodeUnavailable,
			fmt.Errorf("%w: %d of them", ErrSomeThreadsUnavailable, unreached))
	}
	return total, nil
}

// authorThread is eachAuthorThread's work on thread id: decide under the
// thread, from the projection brought up to the log's length, and run
// change there, again from the start whenever a record lands between the
// decision and an append. reached is false for a thread that could not be
// finished and should be tried again later; a thread v may not moderate is
// reached, since there is nothing for v to do there.
//
// v is admitted from the projection row before the thread is opened too,
// so a thread in a forum v may not see or moderate is passed over without
// being opened or projected. Whether its log opens, or is quarantined,
// then never reaches the answer, which would otherwise tell v something
// about a forum hidden from them (Terva review 1499 on PR #172). The row
// can trail the log, which only makes this first check pass over a thread
// v could moderate. The check under the thread is the one that admits.
func (s *Service) authorThread(ctx context.Context, v domain.Viewer, id threadid.ID, flags threadlog.FrameFlags,
	change threadChange) (total int, reached bool, err error) {
	if a, err := s.access(ctx, v, id); err != nil || !a.canModerate {
		reached, err := admitted(err)
		return 0, reached, err
	}
	for range threadAttempts {
		var (
			n       int
			last    int64
			entered bool
		)
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			entered = true
			at := th.Actor.LogLength()
			// The row access reads is the log's up to at, a ThreadMoved
			// included, so the forum v is judged against is the one the
			// appends below land in.
			if err := s.projectNow(ctx, id, at); err != nil {
				return err
			}
			a, err := s.access(ctx, v, id)
			if err != nil {
				return err
			}
			if !a.canModerate || th.Archived() {
				return nil
			}
			n, last, err = change(th, flags, at)
			return err
		})
		total += n
		if n > 0 {
			if perr := s.projectNow(ctx, id, last); perr != nil && err == nil {
				err = perr
			}
		}
		switch {
		case err == nil:
			return total, true, nil
		case errors.Is(err, threadlog.ErrLogMoved):
			continue // decide again, on the log as it now stands
		case !entered:
			return total, false, nil // the thread could not be opened
		}
		reached, err := admitted(err)
		return total, reached, err
	}
	return total, false, nil
}

// admitted sorts access's refusal of a thread for authorThread: one that is
// not v's to moderate is reached, with nothing to do; a quarantined one is
// not reached; anything else is an error that stops the walk.
func admitted(err error) (reached bool, _ error) {
	if err == nil {
		return true, nil
	}
	switch problem.FromError(err) {
	case problem.CodeNotFound, problem.CodeForbidden, problem.CodeUnauthenticated:
		return true, nil
	case problem.CodeUnavailable:
		return false, nil
	}
	return false, err
}
