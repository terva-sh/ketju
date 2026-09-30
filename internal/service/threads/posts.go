package threads

import (
	"context"
	"database/sql"
	"errors"
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
// It is a members' feature, so an anonymous viewer is unauthenticated. The
// author of a post a moderator removed may also read that post's history
// whatever the setting says, for as long as it stays removed: they are served
// the post, and a moderator may have edited it before removing it
// (TKT-01M29CKRJ9, docs/08). That admits them to that post alone.
//
// A removed post's history is a tombstone, `deleted` with no revisions, to
// everyone revealRemoved refuses: its old revisions are its content by
// another route.
func (s *Service) History(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, ifNoneMatch string) (Served[dto.PostHistory], error) {
	var res Served[dto.PostHistory]
	a, err := s.access(ctx, v, id)
	if err != nil {
		return res, err
	}
	public := true
	if !a.canModerate {
		if !a.viewer.Member() {
			return res, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("threads: post history needs a signed-in reader"))
		}
		if public, err = settings.PublicEditHistory.Get(ctx, s.DB); err != nil {
			return res, err
		}
	}
	refused := problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: public_edit_history is off"))

	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
			return []uint32{ordinal}, nil
		})
		if err != nil {
			if !public && errors.Is(err, threadlog.ErrNoSuchOrdinal) {
				// Refused as it was before the entry was read, so a
				// member without the setting learns nothing new here.
				return refused
			}
			return err
		}
		e := snap.entries[0]
		removed := !e.Flags.Visible()
		// A redacted post's history is its revisions with no content, and
		// only a moderator is served even that; to anyone else it is the
		// tombstone a deleted post's is (TKT-01M29CXJZ9).
		redacted := e.Flags.Has(threadlog.EntryRedacted)
		reveal := removed && (revealRemoved(a, e) || redacted && a.canModerate)
		if !public && !reveal {
			return refused
		}
		res.Body = dto.PostHistory{Ordinal: int(ordinal), Revisions: []dto.PostRevision{}, Deleted: removed, Redacted: redacted}
		if removed && !reveal {
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
			if redacted {
				// Blank in the log already; said here too, so no reader
				// depends on the rewrite having been complete.
				r.Source, r.Reason = "", ""
			}
			res.Body.Revisions = append(res.Body.Revisions, dto.PostRevision{
				Revision: int(r.Revision), Actor: actors[int64(r.Actor)],
				At: dto.Millis(r.TimeMS), Reason: r.Reason, Source: r.Source})
		}
		return res.finish(id, snap.logLength, ifNoneMatch)
	})
	return res, err
}

// Source serves a post's current revision as markdown, for editing
// (TKT-01M3QJYV4Z): GET /threads/{id}/posts/{ordinal}/source.
//
// Access to the thread is the thread page's, and a member session is
// required. Who may read the source is sourceRefusal's to say. The author
// windows, a lock and an archive are not checked: they refuse the edit, not
// the reading of words the reader could edit now or later, and PATCH
// refuses the edit itself.
//
// It never touches the response cache and carries no ETag; the handler
// answers no-store, so no answer is kept for anyone.
func (s *Service) Source(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32) (dto.PostSource, error) {
	var out dto.PostSource
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if !a.viewer.Member() {
		return out, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("threads: a post's source needs a signed-in reader"))
	}
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
			return []uint32{ordinal}, nil
		})
		if err != nil {
			return err
		}
		e := snap.entries[0]
		if err := sourceRefusal(a, e); err != nil {
			return err
		}
		revs, err := th.Reader.History(ordinal, snap.logLength)
		if err != nil {
			return err
		}
		if len(revs) == 0 || revs[len(revs)-1].Revision != uint32(e.Revision) {
			// The index points at a revision the log does not end on. Serving
			// an older source would have the edit overwrite a revision the
			// editor never saw.
			return fmt.Errorf("threads: post %d of %s is at revision %d, and its log holds %d revisions",
				ordinal, id, e.Revision, len(revs))
		}
		cur := revs[len(revs)-1]
		out = dto.PostSource{Ordinal: int(ordinal), Revision: int(cur.Revision), Source: cur.Source}
		return nil
	})
	return out, err
}

// sourceRefusal says why a, already admitted to the thread as a member, may
// not read the source of the post e describes, or nil when they may
// (TKT-01M3QJYV4Z). The readers are whoever may edit the post now or later,
// as authorize has it:
//   - a moderator of the forum, of a post that is visible or whose content
//     revealRemoved serves them, which is every removed post but a moved or
//     a hard-redacted one (TKT-01M29CXJZ9); anything else is conflict,
//     having nothing to edit;
//   - anyone else who did not write the post is forbidden, before any
//     removal is looked at, so the refusal says nothing of the post's state;
//   - the author is forbidden a post a moderator hid, a hide pending review
//     included, as authorize refuses their edit of it, although
//     revealRemoved shows them its body; and conflict for any other post
//     that is not visible, deleted by them, moved or redacted, having
//     nothing to edit.
//
// A removed post's source therefore goes only to a viewer revealRemoved
// gives its content to, and of those only to a moderator. A redacted post's
// goes to nobody: revealRemoved refuses it to everyone, and it is not
// visible.
func sourceRefusal(a access, e threadlog.IndexEntry) error {
	if a.canModerate {
		if e.Flags.Visible() || revealRemoved(a, e) {
			return nil
		}
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: this post has no source here to edit"))
	}
	switch {
	case !a.viewer.Member() || e.Author != uint64(a.viewer.UserID):
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: only its author or a moderator may read a post's source"))
	case e.Flags.Has(threadlog.EntryHiddenByMod):
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: a post hidden by a moderator is not its author's to edit"))
	case !e.Flags.Visible():
		return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: this post has no source here to edit"))
	}
	return nil
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
	return s.posts(ctx, th, id, snap, authors, Query{}, a)
}
