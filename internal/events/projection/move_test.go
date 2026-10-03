package projection

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Tests for the projection of a move (TKT-01M29CXJZZ, docs/03 "How a move
// is written"): the source's PostsSplitOut, the target's PostsMergedIn and
// the copies after it, projected independently and in either order.

// Times the move tests date their records at, so two fixtures built the same
// way write the same bytes.
const (
	tSourcePost = 1_700_000_100_000 // + ordinal * 1000
	tTargetPost = 1_700_000_050_000 // + ordinal * 1000: older than every source post
	tEdit       = 1_700_000_300_000
	tMove       = 1_700_000_400_000
)

// threadAt creates a thread whose ThreadCreated is dated ms, and returns
// its id and the log length after it.
func (f *fixture) threadAt(title string, ms int64) (threadid.ID, int64) {
	f.t.Helper()
	id := threadid.ID(uint64(1_700_000_000_000)<<16 | f.next.Add(1))
	th, err := f.cache.Create(id, 7, ms)
	if err != nil {
		f.t.Fatal(err)
	}
	f.cache.Release(th)
	return id, f.append(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 3, TimeMS: ms,
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: strings.ToLower(title), ForumID: 7}})
}

// postAt appends author's post dated ms and returns the log length after it.
func (f *fixture) postAt(id threadid.ID, author uint64, ms int64) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author, TimeMS: ms,
		Body: &threadlog.PostBody{Source: fmt.Sprintf("by %d at %d", author, ms)}})
}

// editAt appends editor's edit of ordinal dated ms.
func (f *fixture) editAt(id threadid.ID, editor uint64, ordinal uint32, ms int64) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: editor, Ordinal: ordinal, TimeMS: ms,
		Body: &threadlog.EditBody{Source: fmt.Sprintf("edited by %d", editor)}})
}

func (f *fixture) splitOut(id, target threadid.ID, ords ...uint32) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePostsSplitOut, Author: moderator, TimeMS: tMove,
		Body: &threadlog.PostsSplitOutBody{Ordinals: ords, TargetThreadID: uint64(target)}})
}

func (f *fixture) mergedIn(id, source threadid.ID, ords ...uint32) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePostsMergedIn, Author: moderator, TimeMS: tMove,
		Body: &threadlog.PostsMergedInBody{SourceThreadID: uint64(source), Ordinals: ords}})
}

// copyOf appends the copy of source's post ordinal into id, keeping the
// original's author and time as a move does.
func (f *fixture) copyOf(id, source threadid.ID, ordinal uint32, author uint64, ms int64) int64 {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author, TimeMS: ms,
		Body: &threadlog.PostBody{Source: "copied", Origin: &threadlog.PostOrigin{ThreadID: uint64(source), Ordinal: ordinal}}})
}

// lastPost is a thread's post_count and last_post_* columns.
type lastPost struct {
	count, at, author, ordinal int64
}

func (f *fixture) lastPost(id threadid.ID) lastPost {
	f.t.Helper()
	var l lastPost
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		t, err := q.GetThread(f.t.Context(), int64(id))
		l = lastPost{t.PostCount, t.LastPostAt, t.LastPostAuthorID, t.LastPostOrdinal}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return l
}

func (f *fixture) move(source threadid.ID, ordinal uint32) dbq.PostMove {
	f.t.Helper()
	var m dbq.PostMove
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		m, err = q.GetPostMove(f.t.Context(), dbq.GetPostMoveParams{SourceThreadID: int64(source), SourceOrdinal: int64(ordinal)})
		return err
	})
	if err != nil {
		f.t.Fatalf("the move of %s/%d: %v", source, ordinal, err)
	}
	return m
}

func (f *fixture) movedOrdinals(id threadid.ID) []int64 {
	f.t.Helper()
	var out []int64
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rs, err := tx.QueryContext(f.t.Context(), `SELECT ordinal FROM post_meta WHERE thread_id = ? AND moved = 1 ORDER BY ordinal`, int64(id))
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var o int64
			if err := rs.Scan(&o); err != nil {
				return err
			}
			out = append(out, o)
		}
		return rs.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// moveLogs is a source with posts 1 to 4, post 2 edited once by a
// moderator, whose posts 2 and 3 are split out to a target that already
// held two posts, older than every source post. Nothing is projected.
type moveLogs struct {
	src, dst       threadid.ID
	srcEnd, dstEnd int64
}

func writeMove(f *fixture) moveLogs {
	f.t.Helper()
	var m moveLogs
	m.src, _ = f.threadAt("Source", 1_700_000_000_000)
	m.dst, _ = f.threadAt("Target", 1_700_000_000_000)
	for o := uint64(1); o <= 4; o++ {
		f.postAt(m.src, 10+o, tSourcePost+int64(o)*1000)
	}
	f.editAt(m.src, moderator, 2, tEdit)
	for o := uint64(1); o <= 2; o++ {
		f.postAt(m.dst, 20+o, tTargetPost+int64(o)*1000)
	}
	m.srcEnd = f.splitOut(m.src, m.dst, 2, 3)
	f.mergedIn(m.dst, m.src, 2, 3)
	f.copyOf(m.dst, m.src, 2, 12, tSourcePost+2000)
	f.editAt(m.dst, moderator, 3, tEdit)
	m.dstEnd = f.copyOf(m.dst, m.src, 3, 13, tSourcePost+3000)
	return m
}

// TestMoveProjectionsAnyOrder is criterion 3 for the projection: the
// source and the target of a move project to the same rows whichever is
// projected first, and a rebuild from the logs alone agrees with both.
func TestMoveProjectionsAnyOrder(t *testing.T) {
	var dumps []string
	for _, order := range []string{"source first", "target first"} {
		t.Run(order, func(t *testing.T) {
			f := newFixture(t)
			m := writeMove(f)
			if order == "source first" {
				f.project(m.src, m.srcEnd)
				f.project(m.dst, m.dstEnd)
			} else {
				f.project(m.dst, m.dstEnd)
				f.project(m.src, m.srcEnd)
			}
			if got := f.movedOrdinals(m.src); !slices.Equal(got, []int64{2, 3}) {
				t.Errorf("moved in the source: %v, want [2 3]", got)
			}
			if got := f.movedOrdinals(m.dst); len(got) != 0 {
				t.Errorf("moved in the target: %v, want none", got)
			}
			// The source's last post is post 4, which stayed; the target's
			// is its own post 2, though both copies are newer.
			if got, want := f.lastPost(m.src), (lastPost{4, tSourcePost + 4000, 14, 4}); got != want {
				t.Errorf("source last post %+v, want %+v", got, want)
			}
			if got, want := f.lastPost(m.dst), (lastPost{4, tTargetPost + 2000, 22, 2}); got != want {
				t.Errorf("target last post %+v, want %+v", got, want)
			}
			for _, c := range []struct {
				ordinal, revision, target int64
			}{{2, 2, 3}, {3, 1, 4}} {
				r := f.move(m.src, uint32(c.ordinal))
				if r.SourceRevision != (sql.NullInt64{Int64: c.revision, Valid: true}) ||
					r.TargetThreadID != int64(m.dst) || r.TargetOrdinal != (sql.NullInt64{Int64: c.target, Valid: true}) ||
					!r.SplitOffset.Valid || !r.MergedOffset.Valid || r.OpensThread != 0 {
					t.Errorf("the move of source post %d is %+v", c.ordinal, r)
				}
			}
			if n := len(f.unfinished()); n != 0 {
				t.Errorf("%d moves are unfinished after both threads are projected", n)
			}
			f.agreesWithIndex(m.src)
			f.agreesWithIndex(m.dst)
			live := f.dump()
			f.clear()
			f.project(m.src, m.srcEnd)
			f.project(m.dst, m.dstEnd)
			if rebuilt := f.dump(); rebuilt != live {
				t.Errorf("a rebuild from the logs differs:\n%s\n---\n%s", live, rebuilt)
			}
			dumps = append(dumps, live)
		})
	}
	if len(dumps) == 2 && dumps[0] != dumps[1] {
		t.Errorf("the two orders project different rows:\n%s\n---\n%s", dumps[0], dumps[1])
	}
}

func (f *fixture) unfinished() []dbq.UnfinishedMovesRow {
	f.t.Helper()
	var rows []dbq.UnfinishedMovesRow
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.UnfinishedMoves(f.t.Context())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

// TestUnfinishedMoves: a move row is finished only when its copy is
// projected and has reached the source post's revision, which is unknown,
// and so unfinished, until the source's split is projected. A later edit of
// a finished copy leaves it finished. An Edit of a copy at or below the
// source's revision, or while that is unknown, is carried.
func TestUnfinishedMoves(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	other, otherEnd := f.threadAt("Other", 1_700_000_000_000)
	f.project(other, otherEnd)
	f.postAt(src, 11, tSourcePost+1000)
	f.postAt(src, 12, tSourcePost+2000)
	f.editAt(src, moderator, 2, tEdit)
	f.postAt(dst, 21, tTargetPost+1000)

	type state struct {
		all            []int64 // source ordinals unfinished
		ofSrc, ofDst   int64
		splits         int
		carried2, new3 bool // whether an Edit of the copy of post 2 at revision 2, and at 3, is carried
	}
	check := func(step string, want state) {
		t.Helper()
		var got state
		err := f.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
			ctx := t.Context()
			all, err := q.UnfinishedMoves(ctx)
			if err != nil {
				return err
			}
			offsets := map[int64]bool{}
			for _, r := range all {
				got.all = append(got.all, r.SourceOrdinal)
				if r.SplitOffset.Valid {
					offsets[r.SplitOffset.Int64] = true
				}
			}
			got.splits = len(offsets)
			for _, c := range []struct {
				id  threadid.ID
				out *int64
			}{{src, &got.ofSrc}, {dst, &got.ofDst}} {
				rows, err := q.UnfinishedMovesForThread(ctx, int64(c.id))
				if err != nil {
					return err
				}
				*c.out = int64(len(rows))
			}
			if rows, err := q.UnfinishedMovesForThread(ctx, int64(other)); err != nil || len(rows) != 0 {
				t.Errorf("%s: a thread no move names has %d unfinished (%v)", step, len(rows), err)
			}
			if got.carried2, err = q.IsCarriedEdit(ctx, dbq.IsCarriedEditParams{TargetThreadID: int64(dst), TargetOrdinal: 2, Revision: 2}); err != nil {
				return err
			}
			got.new3, err = q.IsCarriedEdit(ctx, dbq.IsCarriedEditParams{TargetThreadID: int64(dst), TargetOrdinal: 2, Revision: 3})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.all, want.all) || got.ofSrc != want.ofSrc || got.ofDst != want.ofDst ||
			got.splits != want.splits || got.carried2 != want.carried2 || got.new3 != want.new3 {
			t.Errorf("%s: %+v, want %+v", step, got, want)
		}
	}

	check("before any move", state{})
	// The target's run first: the copy of post 2 is projected but the
	// source's revision is not known, so every Edit of it is carried, and no
	// split can be listed to finish from.
	f.mergedIn(dst, src, 1, 2)
	f.project(dst, f.copyOf(dst, src, 2, 12, tSourcePost+2000))
	check("target's copy of post 2, no split", state{all: []int64{1, 2}, ofSrc: 2, ofDst: 2, carried2: true, new3: true})
	// Then the split: post 2 was at revision 2, the copy is at 1.
	f.project(src, f.splitOut(src, dst, 1, 2))
	check("split projected", state{all: []int64{1, 2}, ofSrc: 2, ofDst: 2, splits: 1, carried2: true})
	f.project(dst, f.editAt(dst, moderator, 2, tEdit))
	check("copy of post 2 edited", state{all: []int64{1}, ofSrc: 1, ofDst: 1, splits: 1, carried2: true})
	f.project(dst, f.copyOf(dst, src, 1, 11, tSourcePost+1000))
	check("every copy written", state{carried2: true})
	// A later edit takes the copy past the source's revision: still
	// finished, and not carried.
	f.project(dst, f.editAt(dst, moderator, 2, tEdit+1000))
	check("finished copy edited again", state{carried2: true})
}

// TestMergeDoesNotMoveLastPostBackwards: copies older than the target's
// posts leave its last post where it was.
func TestMergeDoesNotMoveLastPostBackwards(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	f.postAt(src, 11, tTargetPost)
	f.project(src, f.splitOut(src, dst, 1))
	f.postAt(dst, 21, tSourcePost+1000)
	end := f.postAt(dst, 22, tSourcePost+2000)
	f.project(dst, end)
	before := f.lastPost(dst)
	f.mergedIn(dst, src, 1)
	f.project(dst, f.copyOf(dst, src, 1, 11, tTargetPost))
	want := before
	want.count = 3
	if got := f.lastPost(dst); got != want {
		t.Errorf("after merging an older post: %+v, want %+v", got, want)
	}
}

// TestMergeOfNewerPostDoesNotBump: a copy newer than every post of a
// thread that already had posts raises post_count and leaves last_post_*
// alone (docs/06: merged posts land at the end and are not new activity).
func TestMergeOfNewerPostDoesNotBump(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	f.postAt(dst, 21, tTargetPost+1000)
	f.project(dst, f.postAt(dst, 22, tTargetPost+2000))
	f.postAt(src, 11, tSourcePost+1000)
	f.project(src, f.splitOut(src, dst, 1))
	f.mergedIn(dst, src, 1)
	f.project(dst, f.copyOf(dst, src, 1, 11, tSourcePost+1000))
	if got, want := f.lastPost(dst), (lastPost{3, tTargetPost + 2000, 22, 2}); got != want {
		t.Errorf("after merging a newer post: %+v, want %+v", got, want)
	}
	if m := f.move(src, 1); m.OpensThread != 0 {
		t.Errorf("a merge into a thread with posts opened it: %+v", m)
	}
}

// TestSplitAfterMergeDoesNotBump: a merged copy is newer than the thread's
// latest post, and an older unrelated post is then split out. The recompute
// passes over the copy, so last_post_* stays on the ordinary post.
func TestSplitAfterMergeDoesNotBump(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	elsewhere, _ := f.threadAt("Elsewhere", 1_700_000_000_000)
	for o := int64(1); o <= 3; o++ {
		f.postAt(dst, uint64(20+o), tTargetPost+o*1000)
	}
	f.postAt(src, 11, tSourcePost+1000)
	f.project(src, f.splitOut(src, dst, 1))
	f.mergedIn(dst, src, 1)
	f.copyOf(dst, src, 1, 11, tSourcePost+1000)
	f.project(dst, f.splitOut(dst, elsewhere, 2))
	if got, want := f.lastPost(dst), (lastPost{4, tTargetPost + 3000, 23, 3}); got != want {
		t.Errorf("after splitting out an older post: %+v, want %+v", got, want)
	}
}

// TestSplitThreadLastPostIsNewestCopy: a split's new thread is created
// after every post it receives, and its PostsMergedIn opens it, so its
// copies set its last post, the newest of them, with an ordinal above 0 so
// the lists show it.
func TestSplitThreadLastPostIsNewestCopy(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	for o := int64(1); o <= 3; o++ {
		f.postAt(src, uint64(10+o), tSourcePost+o*1000)
	}
	split, splitEnd := f.threadAt("Split", tMove)
	f.project(split, splitEnd)
	if got := f.lastPost(split); got.ordinal != 0 {
		t.Fatalf("a new thread with no post has last post %+v", got)
	}
	f.project(src, f.splitOut(src, split, 2, 3))
	f.mergedIn(split, src, 2, 3)
	f.copyOf(split, src, 2, 12, tSourcePost+2000)
	f.project(split, f.copyOf(split, src, 3, 13, tSourcePost+3000))
	if got, want := f.lastPost(split), (lastPost{2, tSourcePost + 3000, 13, 2}); got != want {
		t.Errorf("the split thread's last post is %+v, want %+v", got, want)
	}
	for _, o := range []uint32{2, 3} {
		if m := f.move(src, o); m.OpensThread != 1 {
			t.Errorf("the move of %d did not open the split thread: %+v", o, m)
		}
	}
	var listed int64
	if err := f.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(t.Context(), `SELECT threads FROM forum_thread_counts WHERE forum_id = 7`).Scan(&listed)
	}); err != nil {
		t.Fatal(err)
	}
	if listed != 2 {
		t.Errorf("the forum counts %d threads with a post, want the source and the split", listed)
	}
}

// TestSplitRecomputesSourceLastPost: splitting out the newest posts moves
// the source's last post back to the newest that stayed, and splitting out
// every post leaves it as its ThreadCreated did, at ordinal 0. post_count
// stays the latest ordinal throughout.
func TestSplitRecomputesSourceLastPost(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	for o := int64(1); o <= 4; o++ {
		f.postAt(src, uint64(10+o), tSourcePost+o*1000)
	}
	// A deleted post still counts, as it does when it is written.
	f.append(src, threadlog.Append{Type: threadlog.TypeDelete, Author: 12, Ordinal: 2, Body: &threadlog.DeleteBody{}})
	f.project(src, f.splitOut(src, dst, 3, 4))
	if got, want := f.lastPost(src), (lastPost{4, tSourcePost + 2000, 12, 2}); got != want {
		t.Errorf("after splitting out 3 and 4: %+v, want %+v", got, want)
	}
	f.project(src, f.splitOut(src, dst, 1))
	if got, want := f.lastPost(src), (lastPost{4, tSourcePost + 2000, 12, 2}); got != want {
		t.Errorf("after splitting out 1: %+v, want %+v", got, want)
	}
	f.append(src, threadlog.Append{Type: threadlog.TypeRestore, Author: 12, Ordinal: 2, Body: &threadlog.RestoreBody{}})
	f.project(src, f.splitOut(src, dst, 2))
	if got, want := f.lastPost(src), (lastPost{4, 1_700_000_000_000, 3, 0}); got != want {
		t.Errorf("after splitting out every post: %+v, want %+v", got, want)
	}
	f.agreesWithIndex(src)
}

// TestPostsMovedAuditRow: a PostsSplitOut writes one posts.moved row
// against the source, naming both threads and the count. The target's
// PostsMergedIn, its copies, and a moderator's Edit carried onto a copy
// write none, whichever thread is projected first; a moderator's later
// edit of the finished copy is audited as any edit is.
func TestPostsMovedAuditRow(t *testing.T) {
	f := newFixture(t)
	f.users(author, moderator)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	f.postAt(src, author, tSourcePost+1000)
	f.postAt(src, author, tSourcePost+2000)
	f.editAt(src, moderator, 2, tEdit)
	splitAt := f.logSize(src)
	srcEnd := f.splitOut(src, dst, 1, 2)
	f.mergedIn(dst, src, 1, 2)
	f.copyOf(dst, src, 1, author, tSourcePost+1000)
	f.copyOf(dst, src, 2, author, tSourcePost+2000)
	// The target first, so the carried Edit meets a move whose source
	// revision is not known yet.
	f.project(dst, f.editAt(dst, moderator, 2, tEdit))
	f.project(src, srcEnd)
	later := f.logSize(dst)
	f.project(dst, f.editAt(dst, moderator, 2, tEdit+5000))

	type key struct{ action, target string }
	var got []key
	var moved auditRow
	for _, r := range f.auditRows() {
		got = append(got, key{r.Action, r.ID})
		if r.Action == ActionPostsMoved {
			moved = r
		}
	}
	want := []key{
		{ActionPostEdited, audit.Post(src, 2).ID},
		{ActionPostsMoved, audit.Thread(src).ID},
		{ActionPostEdited, audit.Post(dst, 2).ID},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("audit rows %v, want %v", got, want)
	}
	wantMoved := auditRow{At: tMove, Actor: actor(moderator), Action: ActionPostsMoved, Type: "thread", ID: src.String(),
		Details: map[string]any{"from_thread": src.String(), "to_thread": dst.String(), "count": float64(2),
			"thread": src.String(), "log_offset": float64(splitAt)}}
	if fmt.Sprint(moved) != fmt.Sprint(wantMoved) {
		t.Errorf("the posts.moved row is %+v, want %+v", moved, wantMoved)
	}
	for _, r := range f.auditRows() {
		if r.ID == audit.Post(dst, 2).ID && r.Details["log_offset"] != float64(later) {
			t.Errorf("the target's post.edited row is for offset %v, want the later edit at %d", r.Details["log_offset"], later)
		}
	}
	// A reindex adds none.
	f.clear()
	f.project(src, srcEnd)
	f.project(dst, f.logSize(dst))
	if n := len(f.auditRows()); n != len(want) {
		t.Errorf("a reindex left %d audit rows, want %d", n, len(want))
	}
}

// TestOriginWithoutMoveIsNoCopy (Terva review 2011): a Post with an origin
// that no PostsMergedIn ahead of it names, such as one written by hand, is
// an ordinary post: it writes no post_moves row, is never unfinished, and
// sets its thread's last post. A second such Post naming a moved post
// leaves that move's row on its real copy.
func TestOriginWithoutMoveIsNoCopy(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	other, _ := f.threadAt("Elsewhere", 1_700_000_000_000)
	f.project(src, f.postAt(src, 11, tSourcePost+1000))
	f.project(other, f.copyOf(other, src, 1, 11, tSourcePost+1000))
	err := f.db.Read(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.GetPostMove(t.Context(), dbq.GetPostMoveParams{SourceThreadID: int64(src), SourceOrdinal: 1})
		return err
	})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a hand-written copy wrote a move row: %v", err)
	}
	if u := f.unfinished(); len(u) != 0 {
		t.Fatalf("a hand-written copy reads as an unfinished move: %+v", u)
	}
	if got := f.lastPost(other); got.ordinal != 1 {
		t.Errorf("a hand-written copy is not its thread's last post: %+v", got)
	}

	// The real move, then another hand-written copy of the moved post.
	f.project(src, f.splitOut(src, dst, 1))
	f.mergedIn(dst, src, 1)
	f.project(dst, f.copyOf(dst, src, 1, 11, tSourcePost+1000))
	f.project(other, f.copyOf(other, src, 1, 11, tSourcePost+1000))
	if m := f.move(src, 1); m.TargetThreadID != int64(dst) || !m.TargetOrdinal.Valid || m.TargetOrdinal.Int64 != 1 {
		t.Errorf("the move's row after a stray copy: %+v", m)
	}
	if u := f.unfinished(); len(u) != 0 {
		t.Errorf("the finished move reads as unfinished: %+v", u)
	}
}

// TestTargetFirstAuditsALaterEdit (Terva review 2014): rebuilt from the logs
// with the target projected wholly before the source, a moderator's edit of
// a copy made after its move finished still writes its post.edited row,
// and the carried edit still writes none. The target reads the source's
// revision from the source's log.
func TestTargetFirstAuditsALaterEdit(t *testing.T) {
	f := newFixture(t)
	f.users(author, moderator)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Target", 1_700_000_000_000)
	f.postAt(src, author, tSourcePost+1000)
	f.editAt(src, moderator, 1, tEdit)
	srcEnd := f.splitOut(src, dst, 1)
	f.mergedIn(dst, src, 1)
	f.copyOf(dst, src, 1, author, tSourcePost+1000)
	f.editAt(dst, moderator, 1, tEdit) // carried
	later := f.logSize(dst)
	dstEnd := f.editAt(dst, moderator, 1, tEdit+5000) // new, after the move

	f.project(dst, dstEnd)
	f.project(src, srcEnd)

	var dstRows []float64
	n := 0
	for _, r := range f.auditRows() {
		n++
		if r.Action == ActionPostEdited && r.ID == audit.Post(dst, 1).ID {
			dstRows = append(dstRows, r.Details["log_offset"].(float64))
		}
	}
	if !slices.Equal(dstRows, []float64{float64(later)}) {
		t.Errorf("the target's post.edited rows are at offsets %v, want only the later edit at %d", dstRows, later)
	}
	if n != 3 {
		t.Errorf("%d audit rows, want the source's edit, the move and the target's later edit", n)
	}
	if m := f.move(src, 1); !m.SourceRevision.Valid || m.SourceRevision.Int64 != 2 {
		t.Errorf("the move's source revision is %+v, want 2", m.SourceRevision)
	}
}

// TestSplitRecomputesByTime (Terva review 2014): in a thread a move opened,
// copies keep their originals' times, which need not rise with their
// ordinals. A later split out of it recomputes the last post by time, as
// the copies chose it.
func TestSplitRecomputesByTime(t *testing.T) {
	f := newFixture(t)
	src, _ := f.threadAt("Source", 1_700_000_000_000)
	dst, _ := f.threadAt("Split", 1_700_000_000_000)
	away, _ := f.threadAt("Away", 1_700_000_000_000)
	f.postAt(src, 11, tSourcePost+200)
	f.postAt(src, 12, tSourcePost+300)
	f.postAt(src, 13, tSourcePost+50)
	f.project(src, f.splitOut(src, dst, 1, 2, 3))
	f.mergedIn(dst, src, 1, 2, 3)
	f.copyOf(dst, src, 1, 11, tSourcePost+200)
	f.copyOf(dst, src, 2, 12, tSourcePost+300)
	f.project(dst, f.copyOf(dst, src, 3, 13, tSourcePost+50))
	if got := f.lastPost(dst); got.ordinal != 2 {
		t.Fatalf("the split thread's last post is %+v, want the newest copy, 2", got)
	}
	f.project(dst, f.splitOut(dst, away, 1))
	if got := f.lastPost(dst); got.ordinal != 2 || got.at != tSourcePost+300 || got.author != 12 {
		t.Errorf("after splitting copy 1 out: %+v, want copy 2, the newest left", got)
	}
}
