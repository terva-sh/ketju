package main

import (
	"fmt"
	"io"
	"slices"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Moves of posts between threads, as fsck sees them (TKT-01M29CXJZZ). A
// move writes the source's PostsSplitOut first and the target's
// PostsMergedIn and copies after, so a data directory read between the two,
// by a crash or by a backup taken mid-move, holds a split whose copies are
// not all in its target. ketju serve finishes such a move before it serves
// (docs/03 "How a move is written"), so fsck reports it as a warning and not
// as damage, and the exit code does not change for it.

// noteMove records what fsckMoves needs from one frame: each post's last
// revision, each copy's origin, and each PostsSplitOut. A split whose body
// does not decode is damage, because the index cannot fold it and the
// thread does not open; a Post body that does not decode is left to the
// reader that serves it.
func (r *threadReport) noteMove(rec threadlog.ScanRecord) error {
	h := rec.Header
	switch h.Type {
	case threadlog.TypePost, threadlog.TypeEdit:
		if r.revisions == nil {
			r.revisions = map[uint32]uint32{}
		}
		r.revisions[h.Ordinal] = h.Revision
		if h.Type != threadlog.TypePost {
			return nil
		}
		v, err := threadlog.DecodeBody(h.Type, h.Encoding, rec.Body)
		if err != nil {
			return nil
		}
		// A move's copy is the first Post naming its origin after the
		// PostsMergedIn that names it; any other Post with an origin is no
		// move's, as the projection reads it (Terva review 2020).
		if b, ok := v.(*threadlog.PostBody); ok && b.Origin != nil {
			from := fsckPostRef{threadid.ID(b.Origin.ThreadID), b.Origin.Ordinal}
			if r.awaiting[from] {
				delete(r.awaiting, from)
				if r.copyOf == nil {
					r.copyOf = map[uint32]fsckPostRef{}
				}
				r.copyOf[h.Ordinal] = from
			}
		}
	case threadlog.TypePostsMergedIn:
		v, err := threadlog.DecodeBody(h.Type, h.Encoding, rec.Body)
		if err != nil {
			return nil
		}
		if b, ok := v.(*threadlog.PostsMergedInBody); ok {
			if r.awaiting == nil {
				r.awaiting = map[fsckPostRef]bool{}
			}
			for _, o := range b.Ordinals {
				r.awaiting[fsckPostRef{threadid.ID(b.SourceThreadID), o}] = true
			}
		}
	case threadlog.TypePostsSplitOut:
		v, err := threadlog.DecodeBody(h.Type, h.Encoding, rec.Body)
		if err == nil {
			err = threadlog.CheckMove(v, uint64(r.id))
		}
		if err != nil {
			return fmt.Errorf("the PostsSplitOut at %d: %w", rec.Offset, err)
		}
		b := v.(*threadlog.PostsSplitOutBody)
		r.splits = append(r.splits, fsckSplit{offset: rec.Offset, target: threadid.ID(b.TargetThreadID),
			ordinals: slices.Sorted(slices.Values(b.Ordinals))})
	}
	return nil
}

// checkSplits warns of a split that names a post no move could: one the
// split moved a second time, or one not shown as itself. The index folds
// such a split anyway (threadlog.Index.Apply is lenient on state), and the
// actor refuses to write one, so it means a log written some other way.
// Whether a post was shown is read from the index after the whole log,
// with the moved flag taken away: no record lands on a moved post after
// its split.
func (r *threadReport) checkSplits(log io.ReaderAt, size int64) {
	rec, err := threadlog.Open(log, size, nil)
	if err != nil {
		r.Error = err.Error()
		return
	}
	moved := map[uint32]bool{}
	for _, s := range r.splits {
		for _, o := range s.ordinals {
			if moved[o] {
				r.MoveWarnings = append(r.MoveWarnings,
					fmt.Sprintf("the PostsSplitOut at %d moves post %d, which an earlier split moved", s.offset, o))
				continue
			}
			moved[o] = true
			e, err := rec.Index.Entry(o)
			if err != nil {
				r.MoveWarnings = append(r.MoveWarnings,
					fmt.Sprintf("the PostsSplitOut at %d names post %d, which the thread does not hold", s.offset, o))
				continue
			}
			if !(e.Flags &^ threadlog.EntryMoved).Visible() {
				r.MoveWarnings = append(r.MoveWarnings,
					fmt.Sprintf("the PostsSplitOut at %d moves post %d, which was not shown as itself", s.offset, o))
			}
		}
	}
}

// fsckMoves matches every split with its target: a split is finished when
// its target holds a copy of each post it moved, at the original's
// revision or later. A target with no log, or not readable, leaves the
// split unfinished too, since serve could not finish it either.
func fsckMoves(sum *fsckSummary) {
	byID := map[threadid.ID]*threadReport{}
	for i := range sum.Reports {
		if r := &sum.Reports[i]; r.hasID && r.clean() {
			byID[r.id] = r
		}
	}
	for i := range sum.Reports {
		r := &sum.Reports[i]
		if len(r.MoveWarnings) > 0 {
			sum.MoveWarnings++
		}
		for _, s := range r.splits {
			target := byID[s.target]
			var copies map[fsckPostRef]uint32 // origin -> the copy's revision
			if target != nil {
				copies = map[fsckPostRef]uint32{}
				for o, from := range target.copyOf {
					copies[from] = target.revisions[o]
				}
			}
			missing := 0
			for _, o := range s.ordinals {
				rev, ok := copies[fsckPostRef{r.id, o}]
				if !ok || rev < r.revisions[o] {
					missing++
				}
			}
			if missing == 0 {
				continue
			}
			what := "does not yet hold"
			if target == nil {
				what = "has no readable log for"
			}
			r.UnfinishedMoves = append(r.UnfinishedMoves, fmt.Sprintf(
				"the move at %d into %s is unfinished: the target %s %d of its %d post(s); ketju serve finishes it at start",
				s.offset, s.target, what, missing, len(s.ordinals)))
			sum.UnfinishedMoves++
		}
	}
}
