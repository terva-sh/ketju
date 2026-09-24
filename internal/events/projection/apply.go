package projection

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// frame is one record as apply sees it: where it sits, its header, and its
// decoded body.
type frame struct {
	thread threadid.ID
	offset int64
	end    int64 // offset of the next frame
	h      threadlog.FrameHeader
	body   any
}

// applyFunc projects one record. Every one sets absolute values, never
// increments, so a range applied twice converges on the rows it produced the
// first time. That is what makes replay after a crash safe.
type applyFunc func(ctx context.Context, q *dbq.Queries, f frame) error

// decision is what the writer does with one record type: an apply function,
// or a stated reason it has none. TestEveryRecordTypeHasADecision holds every
// known and reserved type to having one, so a type added to the catalogue
// cannot slip through unprojected without somebody writing down why.
type decision struct {
	apply  applyFunc
	reason string // set when apply is nil
}

var decisions = map[threadlog.RecordType]decision{
	threadlog.TypeThreadCreated:  {apply: applyThreadCreated},
	threadlog.TypePost:           {apply: applyPost},
	threadlog.TypeEdit:           {apply: applyEdit},
	threadlog.TypeDelete:         {apply: applyDeleted(true)},
	threadlog.TypeRestore:        {apply: applyDeleted(false)},
	threadlog.TypeThreadRenamed:  {apply: applyRenamed},
	threadlog.TypeThreadMoved:    {apply: applyMoved},
	threadlog.TypeThreadLocked:   {apply: applyLocked(true)},
	threadlog.TypeThreadUnlocked: {apply: applyLocked(false)},
	threadlog.TypeThreadPinned:   {apply: applyPinned},
	threadlog.TypeThreadUnpinned: {apply: applyUnpinned},
	threadlog.TypeThreadTagged:   {apply: applyTagged},
	threadlog.TypeThreadArchived: {apply: applyArchived},

	// Held for Phase 4 (docs/03 "Reserved record type numbers"). This build
	// has no body for them and skips them, as the index does (apply.go uses
	// SkipUnknown, and TestEveryCatalogueTypeIsAdvisory gives the reason: a
	// skipped split leaves a stale render, not a disclosure). The projection
	// follows the index so the two folds of one log cannot disagree.
	threadlog.TypePostsSplitOut: {reason: "reserved for Phase 4; skipped, as the index skips it"},
	threadlog.TypePostsMergedIn: {reason: "reserved for Phase 4; skipped, as the index skips it"},
}

func applyThreadCreated(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadCreatedBody)
	if err := q.UpsertThread(ctx, dbq.UpsertThreadParams{
		ID:                 int64(f.thread),
		ForumID:            int64(b.ForumID),
		Slug:               b.Slug,
		Title:              b.Title,
		AuthorID:           int64(f.h.Actor),
		CreatedAt:          f.h.TimestampMS,
		LastPostAt:         f.h.TimestampMS,
		LastPostAuthorID:   int64(f.h.Actor),
		LogLengthProjected: f.end,
	}); err != nil {
		return err
	}
	return replaceTags(ctx, q, f.thread, b.Tags)
}

// applyPost writes the post's row and makes it the thread's latest post.
// post_count is the ordinal: ordinals are dense and one-based, and deleted or
// moved posts keep theirs, so the latest ordinal is the count (docs/03).
func applyPost(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.PostBody)
	if f.h.Ordinal == 0 {
		return fmt.Errorf("a Post at offset %d carries ordinal 0", f.offset)
	}
	if err := q.UpsertPostMeta(ctx, dbq.UpsertPostMetaParams{
		ThreadID:        int64(f.thread),
		Ordinal:         int64(f.h.Ordinal),
		AuthorID:        int64(f.h.Actor),
		CreatedAt:       f.h.TimestampMS,
		CurrentRevision: revision(f.h),
		ReplyTo:         replyTo(b.ReplyTo),
		LogOffset:       f.offset,
	}); err != nil {
		return err
	}
	if err := one(q.SetThreadLastPost(ctx, dbq.SetThreadLastPostParams{
		PostCount:        int64(f.h.Ordinal),
		LastPostAt:       f.h.TimestampMS,
		LastPostAuthorID: int64(f.h.Actor),
		LastPostOrdinal:  int64(f.h.Ordinal),
		ID:               int64(f.thread),
	}))(f, "thread"); err != nil {
		return err
	}
	return replaceMentions(ctx, q, f, b.Mentions)
}

// applyEdit moves the post to its new revision. created_at and author stay:
// they belong to revision 1, as in the index (threadlog applyEdit).
func applyEdit(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.EditBody)
	if err := one(q.SetPostRevision(ctx, dbq.SetPostRevisionParams{
		CurrentRevision: revision(f.h),
		EditedAt:        sql.NullInt64{Int64: f.h.TimestampMS, Valid: true},
		ReplyTo:         replyTo(b.ReplyTo),
		LogOffset:       f.offset,
		ThreadID:        int64(f.thread),
		Ordinal:         int64(f.h.Ordinal),
	}))(f, "post"); err != nil {
		return err
	}
	if b.Mentions == nil {
		return nil // the key is absent: the post keeps its mentions
	}
	return replaceMentions(ctx, q, f, *b.Mentions)
}

func applyDeleted(deleted bool) applyFunc {
	return func(ctx context.Context, q *dbq.Queries, f frame) error {
		// log_offset stays on the current revision's record; a Delete is
		// not a revision (review 389).
		return one(q.SetPostDeleted(ctx, dbq.SetPostDeletedParams{
			Deleted: flag(deleted), ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal),
		}))(f, "post")
	}
}

func applyRenamed(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadRenamedBody)
	return one(q.SetThreadTitle(ctx, dbq.SetThreadTitleParams{Title: b.Title, Slug: b.Slug, ID: int64(f.thread)}))(f, "thread")
}

func applyMoved(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadMovedBody)
	return one(q.SetThreadForum(ctx, dbq.SetThreadForumParams{ForumID: int64(b.ForumID), ID: int64(f.thread)}))(f, "thread")
}

func applyLocked(locked bool) applyFunc {
	return func(ctx context.Context, q *dbq.Queries, f frame) error {
		return one(q.SetThreadLocked(ctx, dbq.SetThreadLockedParams{Locked: flag(locked), ID: int64(f.thread)}))(f, "thread")
	}
}

func applyPinned(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadPinnedBody)
	return one(q.SetThreadPinned(ctx, dbq.SetThreadPinnedParams{
		PinnedScope: sql.NullString{String: b.Scope, Valid: true},
		// Zero is "indefinitely" in the log and NULL in the row.
		PinnedUntil: sql.NullInt64{Int64: b.Until, Valid: b.Until != 0},
		ID:          int64(f.thread),
	}))(f, "thread")
}

func applyUnpinned(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadUnpinnedBody)
	return one(q.UnpinThread(ctx, dbq.UnpinThreadParams{
		Scope: sql.NullString{String: b.Scope, Valid: true}, ID: int64(f.thread),
	}))(f, "thread")
}

func applyTagged(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadTaggedBody)
	return replaceTags(ctx, q, f.thread, b.Tags)
}

func applyArchived(ctx context.Context, q *dbq.Queries, f frame) error {
	return one(q.SetThreadArchived(ctx, int64(f.thread)))(f, "thread")
}

// replaceTags sets the tag set wholesale, as ThreadTagged means (docs/03).
func replaceTags(ctx context.Context, q *dbq.Queries, id threadid.ID, tags []string) error {
	if err := q.DeleteThreadTags(ctx, int64(id)); err != nil {
		return err
	}
	for _, tag := range tags {
		if err := q.InsertThreadTag(ctx, dbq.InsertThreadTagParams{ThreadID: int64(id), Tag: tag}); err != nil {
			return err
		}
	}
	return nil
}

func replaceMentions(ctx context.Context, q *dbq.Queries, f frame, users []uint64) error {
	if err := q.DeleteMentions(ctx, dbq.DeleteMentionsParams{ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)}); err != nil {
		return err
	}
	for _, u := range users {
		if err := q.InsertMention(ctx, dbq.InsertMentionParams{
			ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal), MentionedUserID: int64(u),
		}); err != nil {
			return err
		}
	}
	return nil
}

// one checks that an update found its row. A record whose post or thread row
// does not exist means the log disagrees with itself (an Edit before its
// Post), which the index refuses too; applying it to nothing would hide that.
func one(n int64, err error) func(frame, string) error {
	return func(f frame, what string) error {
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%s at offset %d (ordinal %d) names a %s the projection does not have",
				f.h.Type, f.offset, f.h.Ordinal, what)
		}
		return nil
	}
}

// revision mirrors threadlog's revisionOf: a zero revision in the frame is
// revision 1.
func revision(h threadlog.FrameHeader) int64 {
	if h.Revision == 0 {
		return 1
	}
	return int64(h.Revision)
}

func replyTo(ordinal uint32) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(ordinal), Valid: ordinal != 0}
}

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
