package threads

import (
	"context"
	"database/sql"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// What a moved post and a split thread say about the other end of the move
// (TKT-01M29CYSAM): a moved post's moved_to, the source's "N posts were
// moved" notice, and the split thread's "split from" header. A reply as a new
// thread is drawn the same way (TKT-01M29CYSC0): the source post's
// "continued in" note, and the new thread's "continued from" header.
//
// Each names another thread, and docs/15 "Invariants, added" lets it name
// that thread only to a viewer who may read it; everyone else learns that
// posts moved and nothing more. The response cache is keyed by viewer class,
// and whether a viewer reads another forum is not in that key, so none of it
// is ever stored with the cached posts. It is drawn here, per request, after
// the posts are taken from the cache or drawn, and before the ETag is taken
// over the body.

// moveLinks admits one viewer to the threads a request's moves name, each
// once.
type moveLinks struct {
	s     *Service
	v     domain.Viewer
	links map[threadid.ID]*dto.ThreadLink
}

func (s *Service) moveLinks(v domain.Viewer) *moveLinks {
	return &moveLinks{s: s, v: v, links: map[threadid.ID]*dto.ThreadLink{}}
}

// link is thread id as a link for the viewer, or nil when they may not read
// it, by the thread page's own admission (access). A thread that cannot be
// read just now, quarantined or with no post projected yet, is nil too,
// which fails closed. An error is a failure to decide, never a no.
func (m *moveLinks) link(ctx context.Context, id threadid.ID) (*dto.ThreadLink, error) {
	if l, ok := m.links[id]; ok {
		return l, nil
	}
	a, err := m.s.access(ctx, m.v, id)
	var l *dto.ThreadLink
	switch {
	case err == nil:
		l = &dto.ThreadLink{ID: id.String(), Slug: a.row.Slug, Title: a.row.Title}
	case refusal(err):
	default:
		return nil, err
	}
	m.links[id] = l
	return l, nil
}

// refusal is whether err is access saying no, rather than failing to say.
func refusal(err error) bool {
	switch problem.FromError(err) {
	case problem.CodeNotFound, problem.CodeUnauthenticated, problem.CodeForbidden, problem.CodeUnavailable:
		return true
	}
	return false
}

// drawMoves fills, in place, moved_to on each of thread id's moved posts,
// move_notice on the first post of each move out of it, and merge_notice on
// the first copy of each merge into it, for a's viewer. posts are as drawn
// or as the cache held them, which carry none of them.
//
// It reads post_moves, a projection. A moved post whose row the projection
// has not written yet is drawn moved with no target, and a move whose
// record is not projected yet on either side has no notice there: each
// fails closed, and the next request after the projection catches up draws
// them.
//
// snap is the snapshot the posts were drawn from, whose index entries say
// which of them are copies: only a copy can be a merge's first copy, so a
// page with none reads no merges, and a thread nothing was ever moved into
// pays no statement for them.
//
// It also fills continued_in, the threads continuing each post that a's
// viewer may read, from thread_continuations. snap.continued says whether
// the thread has any, when the caller has read it (the thread page's header
// does); a thread the header says has none pays no statement for them.
func (s *Service) drawMoves(ctx context.Context, a access, id threadid.ID, snap snapshot, posts []dto.Post) error {
	if err := s.drawContinued(ctx, a, id, snap, posts); err != nil {
		return err
	}
	lo, hi := 0, 0
	for i, e := range snap.entries {
		if !e.Flags.Has(threadlog.EntryCopied) {
			continue
		}
		o := int(snap.ordinals[i])
		if lo == 0 || o < lo {
			lo = o
		}
		hi = max(hi, o)
	}
	first, last := 0, 0
	for _, p := range posts {
		if !p.Moved {
			continue
		}
		if first == 0 || p.Ordinal < first {
			first = p.Ordinal
		}
		last = max(last, p.Ordinal)
	}
	var (
		rows   []dbq.MovesFromRangeRow
		groups []dbq.MovesOutOfRow
		merges []dbq.MergesIntoRangeRow
	)
	if lo == 0 && first == 0 {
		return nil
	}
	if err := s.DB.Read(ctx, func(dq *dbq.Queries, _ *sql.Tx) error {
		var err error
		if lo != 0 {
			merges, err = dq.MergesIntoRange(ctx, dbq.MergesIntoRangeParams{TargetThreadID: int64(id),
				First: int64(lo), Last: int64(hi)})
		}
		if err != nil || first == 0 {
			return err
		}
		rows, err = dq.MovesFromRange(ctx, dbq.MovesFromRangeParams{SourceThreadID: int64(id),
			First: int64(first), Last: int64(last)})
		if err != nil {
			return err
		}
		groups, err = dq.MovesOutOf(ctx, int64(id))
		return err
	}); err != nil {
		return err
	}
	links := s.moveLinks(a.viewer)
	if err := s.drawMerges(ctx, links, posts, merges); err != nil {
		return err
	}
	if first == 0 {
		return nil
	}
	byOrdinal := make(map[int]dbq.MovesFromRangeRow, len(rows))
	for _, r := range rows {
		byOrdinal[int(r.SourceOrdinal)] = r
	}
	bySplit := make(map[int64]dbq.MovesOutOfRow, len(groups))
	for _, g := range groups {
		bySplit[g.SplitOffset] = g
	}
	for i := range posts {
		p := &posts[i]
		r, ok := byOrdinal[p.Ordinal]
		if !p.Moved || !ok {
			continue
		}
		l, err := links.link(ctx, threadid.ID(r.TargetThreadID))
		if err != nil {
			return err
		}
		copyAt := nullOrdinal(r.TargetOrdinal)
		if l != nil {
			p.MovedTo = &dto.MovedTo{Thread: *l, Ordinal: copyAt}
		}
		g, ok := bySplit[r.SplitOffset.Int64]
		if !r.SplitOffset.Valid || !ok || int(g.FirstOrdinal) != p.Ordinal {
			continue
		}
		p.MoveNotice = &dto.MoveNotice{Count: int(g.Posts)}
		if l != nil {
			// The move's posts are copied in source order, so its first
			// post's copy is its first copy.
			p.MoveNotice.Thread, p.MoveNotice.Ordinal = l, copyAt
		}
	}
	return nil
}

// drawMerges sets merge_notice on the first copy of each merge on the page:
// how many posts it brought, and, to a viewer who may read the source, the
// source and the first moved post there.
func (s *Service) drawMerges(ctx context.Context, links *moveLinks, posts []dto.Post, merges []dbq.MergesIntoRangeRow) error {
	if len(merges) == 0 {
		return nil
	}
	byCopy := make(map[int]dbq.MergesIntoRangeRow, len(merges))
	for _, m := range merges {
		byCopy[int(m.FirstCopy)] = m
	}
	for i := range posts {
		p := &posts[i]
		m, ok := byCopy[p.Ordinal]
		if !ok {
			continue
		}
		l, err := links.link(ctx, threadid.ID(m.SourceThreadID))
		if err != nil {
			return err
		}
		p.MergeNotice = &dto.MoveNotice{Count: int(m.Posts)}
		if l != nil {
			at := int(m.FirstSource)
			p.MergeNotice.Thread, p.MergeNotice.Ordinal = l, &at
		}
	}
	return nil
}

// drawContinued fills continued_in on posts, for a's viewer: each thread
// continuing a post, by the order the threads were started, that the viewer
// may read. A thread they may not read is left out, so a viewer who reads
// none of them sees no note (docs/06 "Reply as a new thread").
func (s *Service) drawContinued(ctx context.Context, a access, id threadid.ID, snap snapshot, posts []dto.Post) error {
	if len(posts) == 0 || snap.continued == continuedNone {
		return nil
	}
	lo, hi := posts[0].Ordinal, posts[0].Ordinal
	for _, p := range posts {
		lo, hi = min(lo, p.Ordinal), max(hi, p.Ordinal)
	}
	var rows []dbq.ContinuationsInRangeRow
	if err := s.DB.Read(ctx, func(dq *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = dq.ContinuationsInRange(ctx, dbq.ContinuationsInRangeParams{SourceThreadID: int64(id),
			First: int64(lo), Last: int64(hi)})
		return err
	}); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	links := s.moveLinks(a.viewer)
	byOrdinal := map[int][]dto.ThreadLink{}
	for _, r := range rows {
		l, err := links.link(ctx, threadid.ID(r.ThreadID))
		if err != nil {
			return err
		}
		if l != nil {
			byOrdinal[int(r.SourceOrdinal)] = append(byOrdinal[int(r.SourceOrdinal)], *l)
		}
	}
	for i := range posts {
		if ls, ok := byOrdinal[posts[i].Ordinal]; ok {
			posts[i].ContinuedIn = ls
		}
	}
	return nil
}

// continuedGate is what a caller knows of whether any thread continues a
// post of the thread it is drawing: nothing, or what the header read.
type continuedGate uint8

const (
	continuedUnknown continuedGate = iota
	continuedNone
	continuedSome
)

func nullOrdinal(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	o := int(n.Int64)
	return &o
}

// headerLinks is what a thread's header says of the threads it came from,
// read in one statement (ThreadLinksOf): the thread a split opened it from,
// the post a reply as a new thread continues, and whether any thread
// continues a post of it. A split whose copy the projection has not reached
// reads as no split, and a continuation not yet projected as none, which
// fails closed.
type headerLinks struct {
	splitFrom threadid.ID // 0 for none
	continues threadid.ID // 0 for none
	ordinal   int
	continued continuedGate
}

func readHeaderLinks(ctx context.Context, dq *dbq.Queries, id threadid.ID) (headerLinks, error) {
	r, err := dq.ThreadLinksOf(ctx, int64(id))
	if err != nil {
		return headerLinks{}, err
	}
	h := headerLinks{splitFrom: threadid.ID(r.SplitSource), continues: threadid.ID(r.ContinuesThread),
		ordinal: int(r.ContinuesOrdinal), continued: continuedNone}
	if r.Continued != 0 {
		h.continued = continuedSome
	}
	return h, nil
}

// draw sets split_from and continued_from on h, for v. split_from says a
// split opened the thread to everyone, linked only when v may read the
// source, whose title is its content; continued_from is there only when v
// may read the source (docs/06 "Reply as a new thread").
func (l headerLinks) draw(ctx context.Context, s *Service, v domain.Viewer, h *dto.ThreadHeader) error {
	links := s.moveLinks(v)
	if l.splitFrom != 0 {
		t, err := links.link(ctx, l.splitFrom)
		if err != nil {
			return err
		}
		h.SplitFrom = &dto.SplitFrom{Thread: t}
	}
	if l.continues != 0 {
		t, err := links.link(ctx, l.continues)
		if err != nil {
			return err
		}
		if t != nil {
			h.ContinuedFrom = &dto.ContinuedFrom{Thread: *t, Ordinal: l.ordinal}
		}
	}
	return nil
}
