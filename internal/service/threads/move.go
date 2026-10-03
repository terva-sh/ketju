package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Moving posts between threads: the protocol under the split and merge
// routes (TKT-01M29CXJZZ; the routes are TKT-01M29CYSAM and TKT-01M29CYSBA).
// docs/03 "How a move is written" fixes the records and their order:
//
//  1. for a split only, the new thread's header and ThreadCreated;
//  2. the source's PostsSplitOut, which commits the move;
//  3. the target's PostsMergedIn and a copy of each moved post, its Post
//     with an origin and then its Edits, as one contiguous run.
//
// A crash after 2 leaves the posts hidden in the source and not yet shown in
// the target, never readable in both. finishMoves completes any move from
// the logs alone: at startup before serving, after a move whose target run
// failed, and every minute.
//
// Every move, every thread move (Move) and every archive (Archive) holds the
// service's moveMu, so no thread changes forum or becomes archived while a
// move into or out of it is being decided or written. docs/02 runs one
// process per data directory, so a process-wide lock is enough.

// NewThread is what a split writes for the thread it opens: its forum, by
// slug, its title and its tags. The moderator is its author. A split's
// ThreadCreated writes no continues (docs/03, maintainer decision
// 2026-10-02, TKT-01M29CYSC0): its PostsMergedIn says where its posts came
// from.
type NewThread struct {
	Forum string
	Title string
	Tags  []string
}

// MoveRequest names a move: posts of Source, by ordinal, into Target, an
// existing thread, or into a new thread Split describes. Exactly one of
// Target and Split is set.
type MoveRequest struct {
	Source   threadid.ID
	Ordinals []uint32
	Target   threadid.ID
	Split    *NewThread
}

// MoveResult is where a move put the posts: the target, the source ordinals
// moved, ascending, and each one's copy in the target, in the same order.
type MoveResult struct {
	Target threadid.ID
	Moved  []uint32
	Copies []uint32
}

// pendingMove names a committed move by its source and the offset of its
// PostsSplitOut there; Service.movesPending maps each one not yet finished
// in this process to its target.
type pendingMove struct {
	source threadid.ID
	offset int64
}

// errMoveUnfinished answers a move whose source record is written and
// whose target run is not: the move is committed, and finishMoves completes
// it (docs/03 "Recovery reads the logs alone").
var errMoveUnfinished = errors.New("threads: the move is recorded and will finish shortly")

// runAppender is the Writer's run, which *posts.Writer has. A test writer
// that wraps only Append cannot move posts.
type runAppender interface {
	AppendRun(th *threadcache.Thread, run []threadlog.Append) ([]threadlog.Result, error)
}

// movePosts moves req's posts and answers where they went. The refusals,
// all before anything is written:
//   - validation-failed for no post, a post named twice, or more than
//     threadlog.MaxMoveOrdinals; bad-request for a request naming both a
//     target and a new thread, or neither, and for a merge into its own
//     source;
//   - a split's new thread refused as creating one would be: a title or
//     tags refused, a forum not found or not readable;
//   - not-found, unauthenticated or forbidden as for any read of either
//     thread;
//   - forbidden without moderate in the source's forum, and without it in
//     the target's (docs/08 "Decided 2026-09-30": a move between forums
//     needs moderate in both, by role or through a group);
//   - conflict when either thread is archived, moderators and admins
//     included, or the target forum is archived, except to an admin;
//   - not-found for a post the source does not hold, and conflict for one
//     not shown as itself: only posts shown as themselves move.
//
// The source decides at a log length, as appendDecided does, so a record
// that lands between the decision and the append makes it decide again. If
// the target run fails once the source record is written, the move is
// committed: the answer is unavailable, saying it will finish.
func (s *Service) movePosts(ctx context.Context, v domain.Viewer, req MoveRequest) (MoveResult, error) {
	var out MoveResult
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	ords := slices.Clone(req.Ordinals)
	slices.Sort(ords)
	switch {
	case len(ords) == 0:
		return out, problem.Coded(problem.CodeValidationFailed, errors.New("threads: a move names no post"))
	case len(ords) > threadlog.MaxMoveOrdinals:
		return out, problem.Coded(problem.CodeValidationFailed,
			fmt.Errorf("threads: a move names %d posts, at most %d", len(ords), threadlog.MaxMoveOrdinals))
	case len(slices.Compact(slices.Clone(ords))) != len(ords):
		return out, problem.Coded(problem.CodeValidationFailed, errors.New("threads: a move names a post twice"))
	case req.Split != nil && req.Target != 0:
		// The destination is one or the other, never both: a split's new
		// thread is the one checked, and a target beside it would be
		// written to unchecked (Terva review 2018).
		return out, problem.Coded(problem.CodeBadRequest, errors.New("threads: a move names both a target and a new thread"))
	case req.Split == nil && req.Target == 0:
		return out, problem.Coded(problem.CodeBadRequest, errors.New("threads: a move names no destination"))
	case req.Split == nil && req.Target == req.Source:
		return out, problem.Coded(problem.CodeBadRequest, errors.New("threads: a merge into its own thread"))
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}

	s.moveMu.Lock()
	defer s.moveMu.Unlock()

	if err := s.threadExists(ctx, req.Source); err != nil {
		return out, err
	}
	var split *splitThread
	if req.Split != nil {
		if split, err = s.checkSplit(ctx, v, flags, *req.Split); err != nil {
			return out, err
		}
	} else if err := s.threadExists(ctx, req.Target); err != nil {
		return out, err
	}

	target := req.Target
	var splitAt int64
	for range threadAttempts {
		err = s.Threads.With(req.Source, func(src *threadcache.Thread) error {
			a, at, err := s.admitAt(ctx, v, req.Source, src)
			if err != nil {
				return err
			}
			if err := s.decideMove(ctx, v, a, src, ords); err != nil {
				return stillAt(src, at, err)
			}
			if split == nil {
				if err := s.admitTarget(ctx, v, target); err != nil {
					return stillAt(src, at, err)
				}
			} else if target == 0 {
				// Step 1, once: the new thread, with no post yet, which
				// no list shows until its copies land.
				if target, err = s.openSplit(ctx, v, flags, split); err != nil {
					return err
				}
			}
			res, err := s.Writer.Append(src, threadlog.Append{
				Type: threadlog.TypePostsSplitOut, Flags: flags, Author: uint64(v.UserID),
				TimeMS: s.now().UnixMilli(), AtLength: at,
				Body: &threadlog.PostsSplitOutBody{Ordinals: ords, TargetThreadID: uint64(target)},
			})
			if err != nil {
				return err
			}
			// The record was appended at at, so that is where it starts.
			// From here the move is committed, and whatever fails next
			// leaves it to finish (Terva review 2007, finding 2).
			splitAt = at
			return s.projectNow(ctx, req.Source, res.LogLength)
		})
		if splitAt == 0 && errors.Is(err, threadlog.ErrLogMoved) {
			continue
		}
		break
	}
	if splitAt == 0 {
		if err != nil && !errors.Is(err, threadlog.ErrLogMoved) {
			return out, err
		}
		return out, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s kept changing while the move was decided", req.Source))
	}
	// The move is committed. Until it is finished it is held in memory as
	// well as in post_moves, whose rows the source's projection writes and
	// which may not be written yet if that projection failed: Archive and
	// FinishMoves read both.
	if s.movesPending == nil {
		s.movesPending = map[pendingMove]threadid.ID{}
	}
	s.movesPending[pendingMove{req.Source, splitAt}] = target
	var copies map[uint32]uint32
	if err == nil && s.afterSplitOut != nil {
		err = s.afterSplitOut(req.Source, target)
	}
	if err == nil {
		copies, err = s.finishMove(ctx, req.Source, splitAt)
	}
	if err != nil {
		s.logger().Warn("a move's target run failed; it will be finished from the logs",
			"source", req.Source.String(), "target", target.String(), "err", err)
		return out, problem.Coded(problem.CodeUnavailable, fmt.Errorf("%w: %v", errMoveUnfinished, err))
	}
	delete(s.movesPending, pendingMove{req.Source, splitAt})
	out.Target, out.Moved = target, ords
	for _, o := range ords {
		out.Copies = append(out.Copies, copies[o])
	}
	return out, nil
}

// Split moves posts of source, by ordinal, into a new thread titled title in
// the forum with slug forum, which v starts (docs/06 "Moderator flow for
// drift", TKT-01M29CYSAM). It answers both threads' headers as their pages
// now serve them to v. Every refusal is movePosts', before anything is
// written; a split whose source record is written and whose copies are not
// is unavailable, and finishes from the logs. The new thread starts
// untagged.
func (s *Service) Split(ctx context.Context, v domain.Viewer, source threadid.ID, ordinals []uint32, forum, title string) (dto.MovedPosts, error) {
	var out dto.MovedPosts
	res, err := s.movePosts(ctx, v, MoveRequest{Source: source, Ordinals: ordinals,
		Split: &NewThread{Forum: forum, Title: title}})
	if err != nil {
		return out, err
	}
	if out.Source, err = s.threadHeader(ctx, v, source); err != nil {
		return out, err
	}
	out.Thread, err = s.threadHeader(ctx, v, res.Target)
	return out, err
}

// Merge moves posts of source, by ordinal, to the end of the existing
// thread into names by its base32 id, in their source order (docs/06
// "Moderator flow for drift", TKT-01M29CYSBA). It answers both threads'
// headers as their pages now serve them to v, and the ordinal of the first
// copy in the target. into is checked before
// anything else: one that is not a thread id is validation-failed, as is
// the source itself; one that names no thread, or a thread v may not read,
// is not-found. Every other refusal is movePosts', before anything is
// written. A merge whose source record is written and whose copies are not
// is unavailable, and finishes from the logs.
func (s *Service) Merge(ctx context.Context, v domain.Viewer, source threadid.ID, ordinals []uint32, into string) (dto.MergedPosts, error) {
	var out dto.MergedPosts
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	target, err := threadid.Parse(into)
	if err != nil {
		return out, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: merge target %q: %w", into, err))
	}
	if target == source {
		return out, problem.Coded(problem.CodeValidationFailed, errors.New("threads: a merge into its own thread"))
	}
	res, err := s.movePosts(ctx, v, MoveRequest{Source: source, Ordinals: ordinals, Target: target})
	if err != nil {
		return out, err
	}
	// The first copy is the move's own answer, not the header's count: a
	// reply landing in the target before the header is read would make
	// that late (Terva review 2058).
	out.FirstCopy = int(res.Copies[0])
	if s.afterMerge != nil {
		s.afterMerge(target)
	}
	if out.Source, err = s.threadHeader(ctx, v, source); err != nil {
		return out, err
	}
	out.Target, err = s.threadHeader(ctx, v, target)
	return out, err
}

// decideMove holds the source to the move's rules, as the source stands at
// the length v was admitted at.
func (s *Service) decideMove(ctx context.Context, v domain.Viewer, a access, src *threadcache.Thread, ords []uint32) error {
	if !a.canModerate {
		return problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", src.ID))
	}
	if src.Archived() || a.row.Archived != 0 {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", src.ID))
	}
	for _, o := range ords {
		e, err := src.Index.Entry(o)
		if err != nil {
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s has no post %d", src.ID, o))
		}
		if !e.Flags.Visible() {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: post %d of %s is not shown as itself, so it cannot move", o, src.ID))
		}
		// A copy whose move is unfinished still owes carried Edits, which
		// the actor would refuse once it is moved on, leaving the first
		// move unfinished for good (Terva review 2007, finding 1).
		if err := s.copyPending(ctx, src, o, e); err != nil {
			return err
		}
	}
	return nil
}

// admitTarget holds a merge's target to the move's rules: v reads it and
// moderates its forum, it is not archived, and its forum is not archived
// unless v is an admin. It is projected first, as the source is. A target v
// may not read is not-found, whatever access said, so a merge never tells
// a thread that is hidden from v apart from one that does not exist
// (TKT-01M29CYSBA).
func (s *Service) admitTarget(ctx context.Context, v domain.Viewer, target threadid.ID) error {
	return s.Threads.With(target, func(dst *threadcache.Thread) error {
		a, _, err := s.admitAt(ctx, v, target, dst)
		switch problem.FromError(err) {
		case problem.CodeForbidden, problem.CodeUnauthenticated:
			return problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s is not readable by this viewer: %w", target, err))
		}
		if err != nil {
			return err
		}
		if !a.canModerate {
			return problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", target))
		}
		if dst.Archived() || a.row.Archived != 0 {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", target))
		}
		if a.forumArchived && v.Role != domain.RoleAdmin {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s's forum is archived", target))
		}
		return nil
	})
}

// splitThread is a split's new thread, checked.
type splitThread struct {
	forum dbq.Forum
	title string
	tags  []string
}

// checkSplit refuses a split's new thread as starting one would be refused,
// and further: v must moderate its forum, which must not be archived unless
// v is an admin.
func (s *Service) checkSplit(ctx context.Context, v domain.Viewer, flags threadlog.FrameFlags, n NewThread) (*splitThread, error) {
	forum, err := s.readForum(ctx, v, n.Forum)
	if err != nil {
		return nil, err
	}
	title, err := checkTitle(n.Title)
	if err != nil {
		return nil, err
	}
	tags, err := s.checkTags(ctx, v, forum.ID, flags, n.Tags)
	if err != nil {
		return nil, err
	}
	can, err := s.Perm.Can(ctx, v, forum.ID, domain.PermModerate)
	if err != nil {
		return nil, err
	}
	if !can {
		return nil, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate forum %q", n.Forum))
	}
	if forum.Archived != 0 && v.Role != domain.RoleAdmin {
		return nil, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: forum %q is archived", n.Forum))
	}
	return &splitThread{forum: forum, title: title, tags: tags}, nil
}

// openSplit writes a split's new thread: its header and ThreadCreated, by
// v, and projects it. It holds no post until the move's copies land.
func (s *Service) openSplit(ctx context.Context, v domain.Viewer, flags threadlog.FrameFlags, n *splitThread) (threadid.ID, error) {
	id, err := s.IDs.New()
	if err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	th, err := s.Threads.Create(id, uint64(n.forum.ID), now)
	if err != nil {
		return 0, err
	}
	defer s.Threads.Release(th)
	res, err := s.Writer.Append(th, threadlog.Append{
		Type: threadlog.TypeThreadCreated, Flags: flags, Author: uint64(v.UserID), TimeMS: now,
		Body: &threadlog.ThreadCreatedBody{Title: n.title, Slug: Slug(n.title), ForumID: uint64(n.forum.ID), Tags: n.tags},
	})
	if err != nil {
		return 0, err
	}
	return id, s.projectNow(ctx, id, res.LogLength)
}

// finishMove completes the move whose PostsSplitOut is at offset split in
// source's log, from the logs alone, and answers each moved source ordinal's
// copy in the target. It appends, in one run at the target's length, what
// the target lacks: the PostsMergedIn if it has none, each copy it lacks, and
// each copy's Edits from the source revisions above the copy's, up to the
// source's revision, which is fixed because a moved post takes no record. A
// move already finished writes nothing. The caller holds moveMu.
//
// It reads post_moves for what the target holds, after projecting both
// threads to their lengths, and so answers from the logs: post_moves is a
// projection of them (docs/03 "Recovery reads the logs alone").
func (s *Service) finishMove(ctx context.Context, source threadid.ID, split int64) (map[uint32]uint32, error) {
	for range threadAttempts {
		var copies map[uint32]uint32
		err := s.Threads.With(source, func(src *threadcache.Thread) error {
			srcLen := src.Actor.LogLength()
			if err := s.projectNow(ctx, source, srcLen); err != nil {
				return err
			}
			hdr, body, err := src.Reader.RecordAt(split, srcLen)
			if err != nil {
				return err
			}
			so, ok := body.(*threadlog.PostsSplitOutBody)
			if !ok {
				return fmt.Errorf("threads: %s has no PostsSplitOut at %d", source, split)
			}
			target := threadid.ID(so.TargetThreadID)
			return s.Threads.With(target, func(dst *threadcache.Thread) error {
				var err error
				copies, err = s.finishInto(ctx, src, srcLen, hdr, so, dst)
				return err
			})
		})
		if errors.Is(err, threadlog.ErrLogMoved) {
			continue
		}
		return copies, err
	}
	return nil, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s's move target kept changing", source))
}

// finishInto is finishMove with both threads open: src read up to srcLen,
// and hdr and so the source's PostsSplitOut.
func (s *Service) finishInto(ctx context.Context, src *threadcache.Thread, srcLen int64,
	hdr threadlog.FrameHeader, so *threadlog.PostsSplitOutBody, dst *threadcache.Thread) (map[uint32]uint32, error) {
	// The target's length, then its post count: a post landing between
	// the two fails the run's AtLength, and the move decides again.
	at := dst.Actor.LogLength()
	if err := s.projectNow(ctx, dst.ID, at); err != nil {
		return nil, err
	}
	next := uint32(dst.Index.Len()) + 1

	ords := slices.Clone(so.Ordinals)
	slices.Sort(ords)
	rows, err := s.movesOf(ctx, src.ID, ords)
	if err != nil {
		return nil, err
	}
	chains, err := src.Reader.Chains(ords, srcLen)
	if err != nil {
		return nil, err
	}

	// Where each moved post's copy is, or will be: reply_to is renumbered to
	// it when it names a post moved with it, and dropped otherwise.
	copies := make(map[uint32]uint32, len(ords))
	merged := false
	for _, o := range ords {
		r, ok := rows[o]
		if ok && r.TargetThreadID == int64(dst.ID) && r.TargetOrdinal.Valid {
			copies[o] = uint32(r.TargetOrdinal.Int64)
		}
		if ok && r.TargetThreadID == int64(dst.ID) && r.MergedOffset.Valid {
			merged = true
		}
	}
	for _, o := range ords {
		if _, ok := copies[o]; !ok {
			copies[o] = next
			next++
		}
	}

	var run []threadlog.Append
	if !merged {
		run = append(run, threadlog.Append{
			Type: threadlog.TypePostsMergedIn, Flags: hdr.Flags, Author: hdr.Actor, TimeMS: s.now().UnixMilli(),
			Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(src.ID), Ordinals: ords},
		})
	}
	for _, o := range ords {
		chain := chains[o]
		if len(chain) == 0 || chain[0].Post == nil {
			return nil, fmt.Errorf("threads: %s has no Post for moved post %d", src.ID, o)
		}
		srcEntry, err := src.Index.Entry(o)
		if err != nil {
			return nil, err
		}
		upTo := uint32(srcEntry.Revision)
		have := uint32(0) // the copy's revision, 0 when there is no copy yet
		if r, ok := rows[o]; ok && r.TargetOrdinal.Valid && r.TargetThreadID == int64(dst.ID) {
			e, err := dst.Index.Entry(copies[o])
			if err != nil {
				return nil, err
			}
			have = uint32(e.Revision)
		}
		// reply_to is renumbered only when it names a post moved with this
		// one and ahead of it, whose copy is written earlier in the run and
		// so has its ordinal fixed before this record names it. A copy
		// still to be written may land at another ordinal than the one
		// predicted, if a crash cuts the run and a post arrives in the
		// target before the finish, and a record already written could not
		// be corrected (Terva review 2024). A post replies only to an
		// earlier post, so this drops only what a log written some other
		// way holds.
		//
		// An Edit's reply_to of zero keeps the post's, so an Edit cannot
		// drop one. When the post's last reply_to is not renumbered, the
		// copy replies to nothing at every revision, rather than keep a
		// renumbered earlier one (Terva review 2020).
		ahead := func(r uint32) bool { return r != 0 && r < o && slices.Contains(ords, r) }
		replyOf := func(r uint32) uint32 {
			if ahead(r) {
				return copies[r]
			}
			return 0
		}
		if last := lastReplyTo(chain); last != 0 && !ahead(last) {
			replyOf = func(uint32) uint32 { return 0 }
		}
		for _, c := range chain {
			if c.Header.Revision <= have || c.Header.Revision > upTo {
				continue
			}
			a := threadlog.Append{Flags: c.Header.Flags, Author: c.Header.Actor, TimeMS: c.Header.TimestampMS}
			switch {
			case c.Post != nil:
				b := *c.Post
				b.ReplyTo = replyOf(b.ReplyTo)
				b.Origin = &threadlog.PostOrigin{ThreadID: uint64(src.ID), Ordinal: o}
				a.Type, a.Body = threadlog.TypePost, &b
			case c.Edit != nil:
				b := *c.Edit
				b.ReplyTo = replyOf(b.ReplyTo)
				a.Type, a.Ordinal, a.Body = threadlog.TypeEdit, copies[o], &b
			}
			run = append(run, a)
		}
	}
	if len(run) == 0 {
		return copies, nil
	}
	run[0].AtLength = at
	ra, ok := s.Writer.(runAppender)
	if !ok {
		return nil, errors.New("threads: this writer cannot append a run")
	}
	res, err := ra.AppendRun(dst, run)
	if err != nil {
		return nil, err
	}
	return copies, s.projectNow(ctx, dst.ID, res[len(res)-1].LogLength)
}

// lastReplyTo is the reply_to a post's chain leaves it with: its Post's,
// replaced by each Edit that names one (an Edit's zero keeps it).
func lastReplyTo(chain []threadlog.ChainRecord) uint32 {
	var r uint32
	for _, c := range chain {
		switch {
		case c.Post != nil:
			r = c.Post.ReplyTo
		case c.Edit != nil && c.Edit.ReplyTo != 0:
			r = c.Edit.ReplyTo
		}
	}
	return r
}

// movesOf is source's post_moves rows for ords, by source ordinal.
func (s *Service) movesOf(ctx context.Context, source threadid.ID, ords []uint32) (map[uint32]dbq.PostMove, error) {
	out := make(map[uint32]dbq.PostMove, len(ords))
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, o := range ords {
			r, err := q.GetPostMove(ctx, dbq.GetPostMoveParams{SourceThreadID: int64(source), SourceOrdinal: int64(o)})
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			out[o] = r
		}
		return nil
	})
	return out, err
}

// FinishMoves completes every unfinished move from the logs alone (docs/03
// "Recovery reads the logs alone"): serve runs it after projection catch-up
// and before the listener opens, and every minute. A move whose source or
// target is quarantined waits, and so does one whose source PostsSplitOut is
// not projected yet: the projection's catch-up reaches it, and the next pass
// finishes it. It answers how many moves it finished and the first error,
// having tried every move.
func (s *Service) FinishMoves(ctx context.Context) (int, error) {
	s.moveMu.Lock()
	defer s.moveMu.Unlock()
	var rows []dbq.UnfinishedMovesRow
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.UnfinishedMoves(ctx)
		return err
	}); err != nil {
		return 0, err
	}
	type split struct {
		source, target threadid.ID
		offset         int64
	}
	var splits []split
	seen := map[split]bool{}
	add := func(sp split) {
		if !seen[sp] {
			seen[sp] = true
			splits = append(splits, sp)
		}
	}
	for _, r := range rows {
		if r.SplitOffset.Valid {
			add(split{threadid.ID(r.SourceThreadID), threadid.ID(r.TargetThreadID), r.SplitOffset.Int64})
		}
	}
	// A move this process committed whose source projection has not
	// written its rows yet.
	for k, target := range s.movesPending {
		add(split{k.source, target, k.offset})
	}
	n := 0
	var first error
	for _, sp := range splits {
		if s.Quarantined != nil && (s.Quarantined(sp.source) || s.Quarantined(sp.target)) {
			continue
		}
		if _, err := s.finishMove(ctx, sp.source, sp.offset); err != nil {
			if first == nil {
				first = fmt.Errorf("threads: finishing the move out of %s at %d: %w", sp.source, sp.offset, err)
			}
			continue
		}
		delete(s.movesPending, pendingMove{sp.source, sp.offset})
		n++
	}
	return n, first
}

// unfinishedMoves is UnfinishedMovesForThread for id: the moves into or out
// of id that are not finished.
func (s *Service) unfinishedMoves(ctx context.Context, id threadid.ID) ([]dbq.UnfinishedMovesForThreadRow, error) {
	var rows []dbq.UnfinishedMovesForThreadRow
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.UnfinishedMovesForThread(ctx, int64(id))
		return err
	})
	return rows, err
}

// copyPending refuses an Edit of post ordinal of th, whose entry is e,
// while the move that copied it here is unfinished: until then its
// revisions up to the source's must be the carried ones, and a new Edit
// would take a revision a carried Edit needs (docs/03 "How a move is
// written"). A post that is not a copy, and a finished copy, pass. A Delete
// or Restore takes no revision and is not refused, so a suspension or an
// auto-hide still reaches a copy whose move is finishing.
//
// A copy is unfinished only when a crash split its run, since a run lands
// in one batch, and startup finishes every move before serving; this is
// for a move startup could not finish, and for one whose run failed while
// serving, until the minute job finishes it.
func (s *Service) copyPending(ctx context.Context, th *threadcache.Thread, ordinal uint32, e threadlog.IndexEntry) error {
	if !e.Flags.Has(threadlog.EntryCopied) {
		return nil
	}
	// The copy's row is written when its Post is projected, which a run
	// cut short by a failure may not have been yet.
	if err := s.projectNow(ctx, th.ID, th.Actor.LogLength()); err != nil {
		return err
	}
	rows, err := s.unfinishedMoves(ctx, th.ID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.TargetThreadID == int64(th.ID) && r.TargetOrdinal.Valid && r.TargetOrdinal.Int64 == int64(ordinal) {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: post %d of %s is still being moved here", ordinal, th.ID))
		}
	}
	return nil
}

// moveUnfinished refuses an archive of id while a move into or out of it is
// unfinished: the finisher could never append to an archived target, and
// the posts would stay hidden in the source (decided by the run,
// 2026-10-02, TKT-01M29CXJZZ). It reads post_moves and the moves this
// process committed and has not finished, whose rows the source's
// projection may not have written (Terva review 2007, finding 2). After a
// restart, startup catch-up projects every source before serving. The
// caller holds moveMu.
func (s *Service) moveUnfinished(ctx context.Context, id threadid.ID) error {
	for k, target := range s.movesPending {
		if k.source == id || target == id {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: a move into or out of %s is still finishing", id))
		}
	}
	rows, err := s.unfinishedMoves(ctx, id)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: a move into or out of %s is still finishing", id))
	}
	return nil
}
