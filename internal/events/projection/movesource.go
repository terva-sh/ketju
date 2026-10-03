package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// learnSourceRevisions fills in the source revision of the moves out of the
// thread a copy came from, when f is an Edit of a copy whose source's
// PostsSplitOut is not projected yet (Terva review 2014, TKT-01M29CXJZZ).
//
// Whether an Edit of a copy is carried or new turns on that revision
// (IsCarriedEdit), and an unknown one counts as carried. In a rebuild that
// projects the target before the source, a moderator's edit made after the
// move finished would then be taken for a carried one, and its post.edited
// row would never be written: the source's split, projected later, does
// not revisit the target. So the revision is read here from the source's
// log, where it is fixed, since a moved post accepts no later record: it is
// the last revision of a Post or Edit of the post there, which is the value
// the split will write. One scan of the source's log serves every move out
// of it whose revision is unknown.
//
// A source log that is missing or unreadable leaves the revisions unknown,
// as before; the projection of the target does not fail for another
// thread's log.
func (w *Writer) learnSourceRevisions(ctx context.Context, q *dbq.Queries, f frame) error {
	if _, ok := f.body.(*threadlog.EditBody); !ok {
		return nil
	}
	m, err := q.MoveOfCopy(ctx, dbq.MoveOfCopyParams{
		TargetThreadID: int64(f.thread), TargetOrdinal: sql.NullInt64{Int64: int64(f.h.Ordinal), Valid: true}})
	if errors.Is(err, sql.ErrNoRows) || err == nil && m.SourceRevision.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	source := threadid.ID(m.SourceThreadID)
	want, err := q.MovesWithoutSourceRevision(ctx, m.SourceThreadID)
	if err != nil || len(want) == 0 {
		return err
	}
	revs, err := w.lastRevisions(source, want)
	if err != nil {
		w.log.Warn("projection: a copy's source log could not be read for its revision; its edits count as carried until the source is projected",
			"source", source.String(), "err", err)
		return nil
	}
	for o, rev := range revs {
		if err := q.SetMoveSourceRevision(ctx, dbq.SetMoveSourceRevisionParams{
			SourceRevision: sql.NullInt64{Int64: rev, Valid: true},
			SourceThreadID: m.SourceThreadID, SourceOrdinal: o,
		}); err != nil {
			return err
		}
	}
	return nil
}

// lastRevisions reads source's log and answers, for each of ordinals that
// it holds a Post for, the revision of its last Post or Edit.
func (w *Writer) lastRevisions(source threadid.ID, ordinals []int64) (map[int64]int64, error) {
	f, err := os.Open(source.LogPath(w.root))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	var bad error
	err = threadlog.ScanFrames(f, fi.Size(), func(r threadlog.ScanRecord) bool {
		if r.Err != nil {
			// A torn tail ends the log, as recovery cuts it. Anything
			// else could hide a later revision, so nothing is learned.
			if !r.Fatal || !errors.Is(r.Err, threadlog.ErrShortBuffer) {
				bad = fmt.Errorf("frame at offset %d: %w", r.Offset, r.Err)
			}
			return false
		}
		h := r.Header
		if h.Type != threadlog.TypePost && h.Type != threadlog.TypeEdit {
			return true
		}
		if _, ok := slices.BinarySearch(ordinals, int64(h.Ordinal)); ok {
			out[int64(h.Ordinal)] = revision(h)
		}
		return true
	})
	if err == nil {
		err = bad
	}
	return out, err
}
