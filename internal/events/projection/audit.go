package projection

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The audit actions of moderation the thread log records (docs/04
// "audit_log"). The writer, not the service that appended the record,
// writes each row, in the transaction that projects the record
// (TKT-01M29CKRGS): the append and a database row cannot share a
// transaction, and the projection is the one place every record is applied
// exactly once in effect and retried until it is.
const (
	ActionThreadLocked   = "thread.locked"
	ActionThreadUnlocked = "thread.unlocked"
	ActionThreadPinned   = "thread.pinned"
	ActionThreadUnpinned = "thread.unpinned"
	ActionThreadMoved    = "thread.moved"
	ActionThreadRenamed  = "thread.renamed"
	ActionThreadRetagged = "thread.retagged"
	ActionPostEdited     = "post.edited"
	ActionPostDeleted    = "post.deleted"
	ActionPostHidden     = "post.hidden"
	ActionPostAutoHidden = "post.auto_hidden"
	ActionPostRestored   = "post.restored"
)

// viaLog is what a row names as having acted when the record's actor is 0,
// which is no account.
const viaLog = "thread-log"

// prior is what a record's audit row needs from the rows as they stood
// before the record was applied: for a ThreadMoved, the forum the thread
// left, which the move then overwrites.
type prior struct {
	forum int64
}

// readPrior reads prior for f through q, ahead of f's apply and in the same
// transaction. The row is the log's fold up to f, on a live projection and
// on a reindex alike, so the forum it names is the one the log had put the
// thread in before this move. A missing row is left to apply to report.
func readPrior(ctx context.Context, q *dbq.Queries, f frame) (prior, error) {
	if _, ok := f.body.(*threadlog.ThreadMovedBody); !ok {
		return prior{}, nil
	}
	t, err := q.GetThread(ctx, int64(f.thread))
	if errors.Is(err, sql.ErrNoRows) {
		return prior{}, nil
	}
	return prior{forum: t.ForumID}, err
}

// auditFor says whether f, already projected through q, is a moderation
// action, and if so the row's action, target and details. p is what the
// rows held before f was applied.
//
// Which records are audited is decided from the record and the rows it was
// projected against, never by the caller that appended it, so an author's
// own action cannot produce a row:
//   - always: a lock, unlock, pin, unpin, move or retag (ThreadTagged,
//     which only a moderator writes; TKT-01M29CYSCP);
//   - when the actor is not the thread's author: a rename;
//   - when the actor is not the post's author: an Edit, Delete or Restore,
//     a Delete with by_moderator being post.hidden and one with auto,
//     whose actor is 0, the system, post.auto_hidden (TKT-01M29CP2PJ);
//   - never: ThreadCreated, whose tags are the author's own, Post, and
//     ThreadArchived until its own ticket decides.
//
// A rename's row carries no title, old or new, and a retag's no tags: only
// the thread and the record's offset, which writeAudit adds. The log is the
// record of what the title and the tags were, and audit_log is append-only,
// so member-written text copied into it could never be removed by a hard
// redaction (docs/03 "Hard redaction", maintainer decision 2026-09-29 on
// TKT-01M3NS147C). A move's row names both forums by id, which are not user
// text.
func auditFor(ctx context.Context, q *dbq.Queries, f frame, p prior) (action string, target audit.Target, details map[string]any, ok bool, err error) {
	thread := audit.Thread(f.thread)
	switch b := f.body.(type) {
	case *threadlog.ThreadLockedBody:
		return ActionThreadLocked, thread, map[string]any{"reason": b.Reason}, true, nil
	case *threadlog.ThreadUnlockedBody:
		return ActionThreadUnlocked, thread, map[string]any{"reason": b.Reason}, true, nil
	case *threadlog.ThreadPinnedBody:
		return ActionThreadPinned, thread, map[string]any{"scope": b.Scope, "until": until(b.Until)}, true, nil
	case *threadlog.ThreadUnpinnedBody:
		return ActionThreadUnpinned, thread, map[string]any{"scope": b.Scope, "until": until(b.Until)}, true, nil
	case *threadlog.ThreadMovedBody:
		return ActionThreadMoved, thread, map[string]any{"from_forum": p.forum, "to_forum": int64(b.ForumID)}, true, nil
	case *threadlog.ThreadRenamedBody:
		t, err := q.GetThread(ctx, int64(f.thread))
		if err != nil || t.AuthorID == int64(f.h.Actor) {
			return "", target, nil, false, err
		}
		return ActionThreadRenamed, thread, map[string]any{}, true, nil
	case *threadlog.ThreadTaggedBody:
		return ActionThreadRetagged, thread, map[string]any{}, true, nil
	case *threadlog.EditBody:
		return postAction(ctx, q, f, ActionPostEdited, map[string]any{"reason": b.Reason, "revision": revision(f.h)})
	case *threadlog.DeleteBody:
		// A moderator's Delete hides the post rather than deleting it
		// (TKT-01M29CRJ5J), and its row says so. A Delete by somebody
		// else without by_moderator is in no log this server writes.
		// A hide written by a suspension names it, by the id of its
		// user.suspended row (TKT-01M29CP2QK).
		// The auto-hide (TKT-01M29CP2PJ) is the system's, actor 0, and its
		// row carries no flag count and no reporter: moderators read those
		// from flags.
		action := ActionPostDeleted
		switch {
		case b.Auto:
			action = ActionPostAutoHidden
		case b.ByModerator:
			action = ActionPostHidden
		}
		details := map[string]any{"reason": b.Reason}
		if b.Suspension != 0 {
			details["suspension"] = b.Suspension
		}
		return postAction(ctx, q, f, action, details)
	case *threadlog.RestoreBody:
		return postAction(ctx, q, f, ActionPostRestored, map[string]any{"reason": b.Reason})
	}
	return "", target, nil, false, nil
}

// postAction is auditFor for a record about a post: audited only when its
// actor is not the post's author.
func postAction(ctx context.Context, q *dbq.Queries, f frame, action string, details map[string]any) (string, audit.Target, map[string]any, bool, error) {
	author, err := q.PostAuthor(ctx, dbq.PostAuthorParams{ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)})
	if err != nil || author == int64(f.h.Actor) {
		return "", audit.Target{}, nil, false, err
	}
	return action, audit.Post(f.thread, f.h.Ordinal), details, true, nil
}

// until is a pin's end as a row states it: null for indefinitely, which the
// log writes as zero.
func until(ms int64) any {
	if ms == 0 {
		return nil
	}
	return ms
}

// writeAudit writes f's audit row, if f is a moderation action, through q:
// inside the transaction projecting f, so the row commits exactly when the
// record's projection does, and a failure rolls both back for the retry.
//
// It is idempotent across a replay. Reindex empties the projection tables
// but never audit_log, and replays every record, so a row is first looked
// for by its target, its action and the record's log offset.
//
// The actor is the frame's. A log may name a member the database lacks, as
// one restored from a backup older than the logs does, and actor_id
// references users, so such an actor is written as NULL with the log's id
// in the details as "log_actor", rather than failing the projection on a
// foreign key. Actor 0 is no account, named as the thread log.
func writeAudit(ctx context.Context, q *dbq.Queries, f frame, p prior) error {
	action, target, details, ok, err := auditFor(ctx, q, f, p)
	if err != nil || !ok {
		return err
	}
	details["thread"] = f.thread.String()
	details["log_offset"] = f.offset
	seen, err := q.AuditExistsForRecord(ctx, dbq.AuditExistsForRecordParams{
		TargetType: target.Type, TargetID: target.ID, Action: action, LogOffset: f.offset,
	})
	if err != nil || seen {
		return err
	}
	by := audit.System(viaLog)
	if actor := int64(f.h.Actor); actor != 0 {
		switch _, err := q.GetUser(ctx, actor); {
		case err == nil:
			by = audit.By{Actor: actor}
		case errors.Is(err, sql.ErrNoRows):
			details["log_actor"] = actor
			by = audit.By{}
		default:
			return err
		}
	}
	_, err = audit.Append(ctx, q, time.UnixMilli(f.h.TimestampMS), by, action, target, details)
	return err
}
