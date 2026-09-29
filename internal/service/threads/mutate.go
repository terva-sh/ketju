package threads

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// EditRequest is a new revision of an existing post.
type EditRequest struct {
	Source string
	// Reason is shown in the post's history. Empty for none.
	Reason string
}

// postAction names what a viewer is trying to do to a post, for authorize.
type postAction string

const (
	actionEdit    postAction = "edit"
	actionDelete  postAction = "delete"
	actionRestore postAction = "restore"
)

// gone is the flags a post has once it no longer shows as itself: deleted by
// its author, hidden by a moderator, or moved to another thread.
const gone = threadlog.EntryDeleted | threadlog.EntryHiddenByMod | threadlog.EntryMoved

// windows is the site's author windows (docs/08 "Primitives"), read once per
// request before the thread is opened.
type windows struct {
	edit, delete, grace time.Duration
}

// authorWindows reads the three settings in one transaction.
func (s *Service) authorWindows(ctx context.Context) (windows, error) {
	var w windows
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, r := range []struct {
			key settings.Key[int]
			to  *time.Duration
		}{
			{settings.EditWindowSeconds, &w.edit},
			{settings.DeleteWindowSeconds, &w.delete},
			{settings.GraceEditSeconds, &w.grace},
		} {
			n, err := r.key.In(ctx, q)
			if err != nil {
				return err
			}
			*r.to = time.Duration(n) * time.Second
		}
		return nil
	})
	return w, err
}

// within reports whether a post created at createdMS is still inside a
// window of d at now. A window is open for its first d and closed from d on,
// so a window of 0 is never open. A post stamped after now, by a clock that
// stepped back, is inside every window.
func within(createdMS int64, now time.Time, d time.Duration) bool {
	return now.Sub(time.UnixMilli(createdMS)) < d
}

// authorize says whether v may act on the post e describes, in a thread v has
// already been admitted to as a, at now.
//
// This is the seam the moderation tickets extend. TKT-01M29CKRHS added the
// author windows; a moderator's reach over every post is TKT-01M29CRJ5J.
// For now only the post's author may act.
//
// A post a moderator hid is not its author's to touch: the author did not
// ask for it to go (docs/08, "hidden-by-mod"), and a Restore clears the flag,
// which is a moderator's decision. Nothing sets the flag in this build, so
// this is a guard for when something does.
//
// Editing also needs reply in the forum, since an edit writes new words
// there as a post does. Deleting and restoring one's own post do not.
//
// Last, the windows, measured from the post's creation (revision 1): an
// edit within posts.edit_window_seconds, a delete or a restore of one's own
// delete within posts.delete_window_seconds (docs/08 "Restoring a
// moderator's delete"). A viewer who may moderate the forum is bound by
// neither (docs/08, settled in grooming for TKT-01M29CKRHS).
func authorize(a access, e threadlog.IndexEntry, action postAction, now time.Time, w windows) error {
	if e.Author != uint64(a.viewer.UserID) {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: only its author may %s a post", action))
	}
	if e.Flags.Has(threadlog.EntryHiddenByMod) {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: a post hidden by a moderator is not its author's to %s", action))
	}
	if action == actionEdit && !a.canReply {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: this viewer may not write in this thread's forum"))
	}
	if a.canModerate {
		return nil
	}
	switch action {
	case actionEdit:
		if !within(e.CreatedMS, now, w.edit) {
			return problem.Coded(problem.CodeEditWindowClosed,
				fmt.Errorf("threads: the edit window of %s has passed", w.edit))
		}
	case actionDelete, actionRestore:
		if !within(e.CreatedMS, now, w.delete) {
			return problem.Coded(problem.CodeDeleteWindowClosed,
				fmt.Errorf("threads: the delete window of %s has passed", w.delete))
		}
	}
	return nil
}

// isGrace says whether v's edit of the post e describes, at now, is a grace
// edit: its author's, within posts.grace_edit_seconds of posting, on a post
// with no visible edit yet (docs/08 "Primitives", TKT-01M29CKRHS). The
// decision is written into the Edit rather than taken on read, so the index,
// the projection and the response cache all read it from the log and a
// later change to the setting restyles nothing.
//
// A grace edit never follows a visible one. The edited flag is the index's
// record of a visible edit, and the Edit is appended refusing it, so one
// that lands between this check and the append makes the grace edit a
// conflict rather than letting it follow.
func isGrace(v domain.Viewer, e threadlog.IndexEntry, now time.Time, w windows) bool {
	return e.Author == uint64(v.UserID) && !e.Flags.Has(threadlog.EntryEdited) && within(e.CreatedMS, now, w.grace)
}

// Edit is PATCH /threads/{id}/posts/{ordinal} (docs/07): an Edit record with
// the new source, which the index then points at as the post's current
// revision. The earlier revisions stay in the log for history.
//
// The source is checked and rendered as a reply's is, against the thread and
// the post's own reply_to, and the Edit records the reply_to and mentions
// that render found. An edit of a deleted, hidden or moved post is conflict:
// the actor refuses it, so a Delete that lands between the checks here and
// the append cannot be silently undone by the edit. The window, the grace
// decision and the record's time all read one clock reading.
func (s *Service) Edit(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, req EditRequest) (dto.CreatedPost, error) {
	var out dto.CreatedPost
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if err := checkPost(req.Source, nil); err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	w, err := s.authorWindows(ctx)
	if err != nil {
		return out, err
	}

	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return err
		}
		now := s.now()
		if err := authorize(a, e, actionEdit, now, w); err != nil {
			return err
		}
		switch {
		case th.Archived():
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		case a.row.Locked != 0 && !a.canModerate:
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is locked", id))
		}
		var replyTo *uint32
		if e.ReplyTo != 0 {
			replyTo = &e.ReplyTo
		}
		r, err := s.renderSource(ctx, th, req.Source, replyTo)
		if err != nil {
			return err
		}
		pb := postBody(req.Source, r)
		grace := isGrace(v, e, now, w)
		refuse := gone
		if grace {
			refuse |= threadlog.EntryEdited
		}
		if _, err := s.Writer.Append(th, threadlog.Append{
			Type: threadlog.TypeEdit, Flags: flags, Author: uint64(v.UserID), Ordinal: ordinal,
			Body: &threadlog.EditBody{
				Source: req.Source, Reason: req.Reason,
				// Written although the index would keep it anyway, so a
				// build that reads a zero as "replies to nothing" reads
				// this edit the same way.
				ReplyTo:  pb.ReplyTo,
				Mentions: &pb.Mentions,
				Grace:    grace,
			},
			TimeMS: now.UnixMilli(),
			Refuse: refuse,
		}); err != nil {
			return err
		}
		out, err = s.readBack(ctx, th, id, ordinal, a)
		return err
	})
	return out, err
}

// Delete is DELETE /threads/{id}/posts/{ordinal} (docs/07): a Delete record,
// after which the post is a tombstone to everyone but a moderator. The
// post's bytes stay in the log (docs/03, "Deletion is soft").
//
// Deleting a post that is already gone is conflict and appends nothing.
func (s *Service) Delete(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, reason string) (dto.CreatedPost, error) {
	return s.setDeleted(ctx, v, id, ordinal, actionDelete, threadlog.Append{
		Type: threadlog.TypeDelete, Body: &threadlog.DeleteBody{Reason: reason},
		Refuse: gone,
	})
}

// Restore is POST /threads/{id}/posts/{ordinal}/restore (docs/07): a Restore
// record, after which the post reads at its current revision again.
//
// Restoring a post that is not deleted, or one that was moved, is conflict
// and appends nothing.
func (s *Service) Restore(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, reason string) (dto.CreatedPost, error) {
	return s.setDeleted(ctx, v, id, ordinal, actionRestore, threadlog.Append{
		Type: threadlog.TypeRestore, Body: &threadlog.RestoreBody{Reason: reason},
		Require: threadlog.EntryDeleted, Refuse: threadlog.EntryMoved,
	})
}

// setDeleted is Delete and Restore: the same checks, then rec appended with
// the viewer as its actor. Archived threads are read-only to everyone; a
// lock does not stop an author taking their own post down or putting it
// back, since docs/08's lock refuses posts and edits.
func (s *Service) setDeleted(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, action postAction, rec threadlog.Append) (dto.CreatedPost, error) {
	var out dto.CreatedPost
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	w, err := s.authorWindows(ctx)
	if err != nil {
		return out, err
	}

	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return err
		}
		now := s.now()
		if err := authorize(a, e, action, now, w); err != nil {
			return err
		}
		if th.Archived() {
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		}
		rec.Flags, rec.Author, rec.Ordinal, rec.TimeMS = flags, uint64(v.UserID), ordinal, now.UnixMilli()
		if _, err := s.Writer.Append(th, rec); err != nil {
			return err
		}
		out, err = s.readBack(ctx, th, id, ordinal, a)
		return err
	})
	return out, err
}

// readBack answers a write with the post as the viewer now reads it, from
// one snapshot of the index taken after the append returned (review 1119 on
// PR #95). The post, its revision and the log length all come from that one
// snapshot, so they describe the same log. It is at least as new as the
// append, and a write to the same post that lands in between shows in the
// answer rather than being mixed into it: a Delete or Restore is not a
// revision, so the frame the append wrote could not supply the post's
// revision anyway.
func (s *Service) readBack(ctx context.Context, th *threadcache.Thread, id threadid.ID, ordinal uint32, a access) (dto.CreatedPost, error) {
	snap, err := s.stable(th, func(ix *threadlog.Index, logLength int64) ([]uint32, error) {
		return []uint32{ordinal}, nil
	})
	if err != nil {
		return dto.CreatedPost{}, err
	}
	posts, err := s.assemble(ctx, th, id, snap, a)
	if err != nil {
		return dto.CreatedPost{}, fmt.Errorf("threads: reading back post %d of %s: %w", ordinal, id, err)
	}
	p := posts[0]
	return dto.CreatedPost{Post: p, Ordinal: ordinal, Revision: uint32(p.Revision), LogLength: snap.logLength}, nil
}
