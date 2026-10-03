package threads

import (
	"context"
	"database/sql"
	"errors"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// What a moved post and a split thread say about the other end of the move
// (TKT-01M29CYSAM): a moved post's moved_to, the source's "N posts were
// moved" notice, and the split thread's "split from" header.
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

// drawMoves fills, in place, moved_to on each of thread id's moved posts and
// move_notice on the first post of each move, for a's viewer. posts are as
// drawn or as the cache held them, which carry neither.
//
// It reads post_moves, a projection. A moved post whose row the projection
// has not written yet is drawn moved with no target, and a move whose
// source record is not projected yet has no notice: both fail closed, and
// the next request after the projection catches up draws them.
func (s *Service) drawMoves(ctx context.Context, a access, id threadid.ID, posts []dto.Post) error {
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
	if first == 0 {
		return nil
	}
	var (
		rows   []dbq.MovesFromRangeRow
		groups []dbq.MovesOutOfRow
	)
	if err := s.DB.Read(ctx, func(dq *dbq.Queries, _ *sql.Tx) error {
		var err error
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
	byOrdinal := make(map[int]dbq.MovesFromRangeRow, len(rows))
	for _, r := range rows {
		byOrdinal[int(r.SourceOrdinal)] = r
	}
	bySplit := make(map[int64]dbq.MovesOutOfRow, len(groups))
	for _, g := range groups {
		bySplit[g.SplitOffset] = g
	}
	links := s.moveLinks(a.viewer)
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

func nullOrdinal(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	o := int(n.Int64)
	return &o
}

// splitSource is the thread id was split from, if a split opened it: the
// source of its post 1, a split's first copy. ok is false for any other
// thread, and for a split whose copy the projection has not reached.
func splitSource(ctx context.Context, dq *dbq.Queries, id threadid.ID) (threadid.ID, bool, error) {
	src, err := dq.SplitSourceOf(ctx, int64(id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, err
	}
	return threadid.ID(src), true, nil
}

// drawSplitFrom is the split_from header of a thread split from src, for v:
// the link only when v may read src, whose title is its content.
func (s *Service) drawSplitFrom(ctx context.Context, v domain.Viewer, src threadid.ID) (*dto.SplitFrom, error) {
	l, err := s.moveLinks(v).link(ctx, src)
	if err != nil {
		return nil, err
	}
	return &dto.SplitFrom{Thread: l}, nil
}
