package threads

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// Replies paths, as Service.trace reports them.
const (
	pathIndex    = "index"
	pathPostMeta = "post_meta"
)

// Post serves one post: the popover for a quote or reply link to another
// page, and a permalink's target (docs/07). It is the same assembly as the
// thread page for one ordinal, so a tombstone is a tombstone here too.
func (s *Service) Post(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, ifNoneMatch string) (Served[dto.Post], error) {
	var res Served[dto.Post]
	a, err := s.access(ctx, v, id)
	if err != nil {
		return res, err
	}
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
			return []uint32{ordinal}, nil
		})
		if err != nil {
			return err
		}
		posts, err := s.assemble(ctx, th, id, snap, a)
		if err != nil {
			return err
		}
		res.Body = posts[0]
		return res.finish(id, snap.logLength, ifNoneMatch)
	})
	return res, err
}

// Replies serves a post's direct replies in ordinal order, for the inline
// expander (docs/06).
//
// Which replies there are is answered from the index when the thread is
// already open, and from post_meta's reply_to index when it is not
// (TKT-01M29CA9C2). The two must agree, and TestRepliesPathsAgree holds them
// to it. post_meta is a projection and may trail the log, so it is believed
// only when it has reached the length being served; a reply the projection
// has not seen yet would otherwise be missing. The bodies are read from the
// log either way, since the expander renders them.
func (s *Service) Replies(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, ifNoneMatch string) (Served[dto.PostReplies], error) {
	var res Served[dto.PostReplies]
	a, err := s.access(ctx, v, id)
	if err != nil {
		return res, err
	}

	th, open := s.Threads.Peek(id)
	var fromMeta []uint32
	projected := int64(-1)
	if open {
		defer s.Threads.Release(th)
	} else {
		// Read before opening, in one transaction, so the ordinals and the
		// length they are as of agree.
		if err := s.DB.Read(ctx, func(dq *dbq.Queries, tx *sql.Tx) error {
			row, err := dq.GetThread(ctx, int64(id))
			if err != nil {
				return err
			}
			rows, err := dq.RepliesTo(ctx, dbq.RepliesToParams{ThreadID: int64(id),
				ReplyTo: sql.NullInt64{Int64: int64(ordinal), Valid: true}})
			if err != nil {
				return err
			}
			fromMeta = make([]uint32, len(rows))
			for i, o := range rows {
				fromMeta[i] = uint32(o)
			}
			projected = row.LogLengthProjected
			return nil
		}); err != nil {
			return res, err
		}
	}

	serve := func(th *threadcache.Thread) error {
		path := pathIndex
		snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
			if _, err := ix.Entry(ordinal); err != nil {
				return nil, err
			}
			if !open && projected == logLength {
				path = pathPostMeta
				return fromMeta, nil
			}
			path = pathIndex
			return ix.Replies(ordinal), nil
		})
		if err != nil {
			return err
		}
		if s.trace != nil {
			s.trace(path)
		}
		posts, err := s.assemble(ctx, th, id, snap, a)
		if err != nil {
			return err
		}
		res.Body = dto.PostReplies{Ordinal: int(ordinal), Posts: posts}
		return res.finish(id, snap.logLength, ifNoneMatch)
	}
	if open {
		err = serve(th)
	} else {
		err = s.Threads.With(id, serve)
	}
	return res, err
}

// History serves a post's revisions, each with its actor, time, reason and
// source; the client diffs them (docs/08 "Post history").
//
// Moderators may always read it, and members when public_edit_history is on.
// It is a members' feature, so an anonymous viewer is unauthenticated. A
// deleted post's history is a tombstone to everyone but a moderator: its old
// revisions are its content by another route.
func (s *Service) History(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, ifNoneMatch string) (Served[dto.PostHistory], error) {
	var res Served[dto.PostHistory]
	a, err := s.access(ctx, v, id)
	if err != nil {
		return res, err
	}
	if !a.canModerate {
		if !a.viewer.Member() {
			return res, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("threads: post history needs a signed-in reader"))
		}
		public, err := settings.PublicEditHistory.Get(ctx, s.DB)
		if err != nil {
			return res, err
		}
		if !public {
			return res, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: public_edit_history is off"))
		}
	}

	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
			return []uint32{ordinal}, nil
		})
		if err != nil {
			return err
		}
		e := snap.entries[0]
		res.Body = dto.PostHistory{Ordinal: int(ordinal), Revisions: []dto.PostRevision{},
			Deleted: !e.Flags.Visible()}
		if res.Body.Deleted && !a.canModerate {
			return res.finish(id, snap.logLength, ifNoneMatch)
		}

		revs, err := th.Reader.History(ordinal, snap.logLength)
		if err != nil {
			return err
		}
		ids := map[int64]bool{}
		for _, r := range revs {
			ids[int64(r.Actor)] = true
		}
		actors, err := s.users(ctx, ids)
		if err != nil {
			return err
		}
		for _, r := range revs {
			res.Body.Revisions = append(res.Body.Revisions, dto.PostRevision{
				Revision: int(r.Revision), Actor: actors[int64(r.Actor)],
				At: dto.Millis(r.TimeMS), Reason: r.Reason, Source: r.Source})
		}
		return res.finish(id, snap.logLength, ifNoneMatch)
	})
	return res, err
}

// stable takes a snapshot of the ordinals pick chooses, inside one
// Actor.Stable pass so the ordinals, their entries and the length agree.
func (s *Service) stable(th *threadcache.Thread, pick func(ix *threadlog.Index, logLength int64) ([]uint32, error)) (snapshot, error) {
	var (
		snap snapshot
		err  error
	)
	th.Actor.Stable(func(logLength int64) {
		var ordinals []uint32
		if ordinals, err = pick(th.Index, logLength); err != nil {
			return
		}
		err = snap.take(th.Index, logLength, ordinals)
	})
	return snap, err
}

// assemble loads the snapshot's authors and renders its posts for a.
func (s *Service) assemble(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot, a access) ([]dto.Post, error) {
	authors, err := s.users(ctx, snap.authors())
	if err != nil {
		return nil, err
	}
	return s.posts(ctx, th, id, snap, authors, Query{}, a.class == ClassModerator)
}
