package threads

import (
	"context"
	"database/sql"
	"fmt"
	"math"
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
// its author, hidden by a moderator, moved to another thread, or
// hard-redacted, which no record may touch again (TKT-01M29CXJZ9).
const gone = threadlog.EntryDeleted | threadlog.EntryHiddenByMod | threadlog.EntryMoved | threadlog.EntryRedacted

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
			*r.to = secondsDuration(n)
		}
		return nil
	})
	return w, err
}

// secondsDuration is a count of seconds from a setting as a Duration. The
// settings take any count from 0 up, and one past what a Duration holds
// would overflow into a negative window, closed at once, or a negative slow
// mode wait, which is off. Such a count is the longest Duration there is,
// a window that does not close in practice (Terva review 1535, finding 2;
// TKT-01M3R33SR5). A negative count, which no setting's rule admits, is 0:
// closed, or off, as every caller already reads 0, and never a product that
// wrapped round to an open window.
//
// Every seconds setting this package turns into a Duration goes through
// here, so the next one cannot repeat the overflow.
func secondsDuration(n int) time.Duration {
	switch {
	case n <= 0:
		return 0
	case int64(n) > int64(math.MaxInt64/time.Second):
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(n) * time.Second
}

// within reports whether a post created at createdMS is still inside a
// window of d at now. A window is open for its first d and closed from d on,
// and a window of 0 is never open, not even to a post stamped after now by a
// clock that stepped back (review 1378 on PR #146). Such a post is inside
// every window that is open at all.
func within(createdMS int64, now time.Time, d time.Duration) bool {
	return d > 0 && now.Sub(time.UnixMilli(createdMS)) < d
}

// authorize says whether v may act on the post e describes, in a thread v has
// already been admitted to as a, at now.
//
// A viewer who may moderate the thread's forum may edit, delete or restore
// any post, their own or anyone's, bound by no window and without needing
// reply there (TKT-01M29CRJ5J; docs/08; the windows' exemption was settled
// in grooming for TKT-01M29CKRHS). The moderate permission is the grant. What
// makes a moderator's edit of somebody else's words acceptable is that it is
// visible: the record's actor, the post's marker and its history name it.
//
// Everyone else may act only on their own post, and not on one a moderator
// hid: the author did not ask for it to go (docs/08 "Restoring a
// moderator's delete"), and a Restore clears the flag, which is a
// moderator's decision. Editing also needs reply in the forum, since an edit
// writes new words there as a post does. Deleting and restoring one's own
// post do not.
//
// Last, the windows, measured from the post's creation (revision 1): an
// edit within posts.edit_window_seconds, a delete or a restore of one's own
// delete within posts.delete_window_seconds.
//
// First of all, though, an author's action on their own post in an archived
// forum is conflict to everyone but an admin, a moderator's own post
// included: it is posting, and nobody else may post there
// (readOnlyForum). A moderator's action on somebody else's post is
// moderation, which an archived forum leaves open.
//
// The archived-thread and locked refusals are the callers', since they
// depend on the thread rather than on the post.
func authorize(a access, e threadlog.IndexEntry, action postAction, now time.Time, w windows) error {
	if e.Author == uint64(a.viewer.UserID) {
		if err := readOnlyForum(a); err != nil {
			return err
		}
	}
	if a.canModerate {
		return nil
	}
	if e.Author != uint64(a.viewer.UserID) {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: only its author or a moderator may %s a post", action))
	}
	if e.Flags.Has(threadlog.EntryHiddenByMod) {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: a post hidden by a moderator is not its author's to %s", action))
	}
	if action == actionEdit && !a.canReply {
		return problem.Coded(problem.CodeForbidden,
			fmt.Errorf("threads: this viewer may not write in this thread's forum"))
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
//
// A moderator's edit of somebody else's post is an Edit whose actor is the
// moderator; the post keeps its author (TKT-01M29CRJ5J). It is never a grace
// edit, and it is projected before the answer, so its audit row exists when
// the handler replies.
func (s *Service) Edit(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, req EditRequest) (dto.CreatedPost, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.CreatedPost{}, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	if err := checkPost(req.Source, nil); err != nil {
		return dto.CreatedPost{}, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	w, err := s.authorWindows(ctx)
	if err != nil {
		return dto.CreatedPost{}, err
	}

	return s.changePost(ctx, id, ordinal, a, func(th *threadcache.Thread, e threadlog.IndexEntry) (threadlog.Append, error) {
		now := s.now()
		if err := authorize(a, e, actionEdit, now, w); err != nil {
			return threadlog.Append{}, err
		}
		switch {
		case th.Archived():
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		case a.row.Locked != 0 && !a.canModerate:
			return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is locked", id))
		}
		var replyTo *uint32
		if e.ReplyTo != 0 {
			replyTo = &e.ReplyTo
		}
		r, err := s.renderSource(ctx, th, req.Source, replyTo)
		if err != nil {
			return threadlog.Append{}, err
		}
		pb := postBody(req.Source, r)
		grace := isGrace(v, e, now, w)
		refuse := gone
		if grace {
			refuse |= threadlog.EntryEdited
		}
		return threadlog.Append{
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
		}, nil
	})
}

// Delete is DELETE /threads/{id}/posts/{ordinal} (docs/07): a Delete record,
// after which the post is a tombstone to everyone revealRemoved does not
// serve it to: a moderator, and the author of a post a moderator hid. The
// post's bytes stay in the log (docs/03, "Deletion is soft").
//
// A moderator's delete of somebody else's post writes by_moderator, which
// hides the post (hidden-by-mod) rather than deleting it, so that only a
// moderator may restore it (docs/08 "Restoring a moderator's delete",
// TKT-01M29CRJ5J). A moderator's delete of their own post is an ordinary
// delete.
//
// Deleting a post that is already gone is conflict and appends nothing.
func (s *Service) Delete(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, reason string) (dto.CreatedPost, error) {
	return s.setDeleted(ctx, v, id, ordinal, actionDelete, reason, func(e threadlog.IndexEntry, _ access) threadlog.Append {
		return threadlog.Append{
			Type:   threadlog.TypeDelete,
			Body:   &threadlog.DeleteBody{Reason: reason, ByModerator: e.Author != uint64(v.UserID)},
			Refuse: gone,
		}
	})
}

// Restore is POST /threads/{id}/posts/{ordinal}/restore (docs/07): a Restore
// record, after which the post reads at its current revision again.
//
// A moderator restores a post that is deleted or hidden by a moderator. An
// author restores only their own delete. authorize refuses the author a
// hidden post, and the append checks it again against the post as it
// stands when the record is written: it needs the deleted flag, and it
// refuses hidden-by-mod as well, so a post carrying both bits, which a
// record appended without the service's preconditions could leave, is still
// only a moderator's to restore, since a Restore clears both.
//
// Restoring a post that is not deleted or hidden, or one that was moved, is
// conflict and appends nothing.
//
// A moderator's restore dismisses the post's open flags first, under the
// post's flag lock, and reopens them only if the Restore was not appended
// (restoreDismissingFlags, TKT-01M29CP2PJ). That is how a post hidden
// pending review is cleared.
func (s *Service) Restore(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, reason string) (dto.CreatedPost, error) {
	return s.setDeleted(ctx, v, id, ordinal, actionRestore, reason, func(_ threadlog.IndexEntry, a access) threadlog.Append {
		return restoreRecord(a.canModerate, reason)
	})
}

// restoreRecord is the Restore a viewer appends, with its preconditions: a
// moderator's takes a post that is deleted or hidden, and anyone else's
// takes only a deleted one and refuses a hidden one.
func restoreRecord(moderator bool, reason string) threadlog.Append {
	rec := threadlog.Append{Type: threadlog.TypeRestore, Body: &threadlog.RestoreBody{Reason: reason}}
	if moderator {
		rec.RequireAny, rec.Refuse = threadlog.EntryDeleted|threadlog.EntryHiddenByMod, threadlog.EntryMoved|threadlog.EntryRedacted
	} else {
		rec.Require, rec.Refuse = threadlog.EntryDeleted, threadlog.EntryHiddenByMod|threadlog.EntryMoved|threadlog.EntryRedacted
	}
	return rec
}

// setDeleted is Delete and Restore: the same checks, then the record build
// makes for the post's entry, appended with the viewer as its actor.
// Archived threads are read-only to everyone; a lock does not stop an author
// taking their own post down or putting it back, since docs/08's lock
// refuses posts and edits.
//
// reason is the record's, and for a moderator's restore also the note the
// dismissed flags carry.
func (s *Service) setDeleted(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, action postAction, reason string,
	build func(e threadlog.IndexEntry, a access) threadlog.Append) (dto.CreatedPost, error) {
	if err := s.MayWrite(v); err != nil {
		return dto.CreatedPost{}, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return dto.CreatedPost{}, err
	}
	w, err := s.authorWindows(ctx)
	if err != nil {
		return dto.CreatedPost{}, err
	}

	run := func() (dto.CreatedPost, bool, error) {
		return s.changePostAppended(ctx, id, ordinal, a, func(th *threadcache.Thread, e threadlog.IndexEntry) (threadlog.Append, error) {
			now := s.now()
			if err := authorize(a, e, action, now, w); err != nil {
				return threadlog.Append{}, err
			}
			if th.Archived() {
				return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
			}
			rec := build(e, a)
			rec.Flags, rec.Author, rec.Ordinal, rec.TimeMS = flags, uint64(v.UserID), ordinal, now.UnixMilli()
			return rec, nil
		})
	}
	if action == actionRestore && a.canModerate {
		return s.restoreDismissingFlags(ctx, v, id, ordinal, reason, run)
	}
	// Every other delete or restore of one post holds its flag lock too, so
	// that a moderator queue action, which decides from the post's state
	// under the lock, sees no change to that state until it is done (Terva
	// review 1522 on PR #184).
	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()
	out, _, err := run()
	return out, err
}

// changePost appends the record build makes for post ordinal of thread id,
// from the post's entry as the open thread holds it, and answers with the
// post as a's viewer now reads it.
//
// The answer is read back straight after the append, in the same call on
// the open thread, before anything else is done (Terva review 1486 on PR
// #165), so it is the snapshot readBack describes. A record whose actor is
// not the post's author, which only a moderator may append, is then
// projected before the answer, as appendEvent projects, so the audit row the
// projection writer writes for it exists when the handler replies
// (TKT-01M29CRJ5J). The projection reads the log, not the answer, so it
// changes nothing the answer says. An author's own record writes no row and
// keeps the asynchronous path.
func (s *Service) changePost(ctx context.Context, id threadid.ID, ordinal uint32, a access,
	build func(th *threadcache.Thread, e threadlog.IndexEntry) (threadlog.Append, error)) (dto.CreatedPost, error) {
	out, _, err := s.changePostAppended(ctx, id, ordinal, a, build)
	return out, err
}

// changePostAppended is changePost, also saying whether the record was
// appended: true once the append returned, whatever failed after it. A
// moderator's restore reopens the flags it dismissed only when it is false
// (restoreDismissingFlags).
func (s *Service) changePostAppended(ctx context.Context, id threadid.ID, ordinal uint32, a access,
	build func(th *threadcache.Thread, e threadlog.IndexEntry) (threadlog.Append, error)) (dto.CreatedPost, bool, error) {
	var (
		out      dto.CreatedPost
		res      threadlog.Result
		appended bool
		project  bool
	)
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return err
		}
		rec, err := build(th, e)
		if err != nil {
			return err
		}
		project = rec.Author != e.Author
		if res, err = s.Writer.Append(th, rec); err != nil {
			return err
		}
		appended = true
		out, err = s.readBack(ctx, th, id, ordinal, a)
		return err
	})
	// A durable record is projected even when the read-back failed, so its
	// row does not wait on the bus for a request that answers an error.
	if appended && project {
		if perr := s.projectNow(ctx, id, res.LogLength); perr != nil && err == nil {
			err = perr
		}
	}
	if err != nil {
		return dto.CreatedPost{}, appended, err
	}
	return out, true, nil
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
