package projection

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/render"
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

// decision is what the writer does with one record type: its apply function.
// TestEveryRecordTypeHasADecision holds every type in the catalogue to having
// one and every other type to having none, so a type added to the catalogue
// cannot slip through unprojected. Since split and merge landed
// (TKT-01M29CXJZZ) no number is held without a body, so there is no longer a
// reserved type to state a reason for.
type decision struct {
	apply applyFunc
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
	threadlog.TypeRedacted:       {apply: applyRedacted},
	threadlog.TypePostsSplitOut:  {apply: applySplitOut},
	threadlog.TypePostsMergedIn:  {apply: applyMergedIn},
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
	// The source post's "continued in" note is this row; the source's log
	// is never written (TKT-01M29CYSC0).
	if c := b.Continues; c != nil {
		if c.ThreadID == 0 || c.Ordinal == 0 || c.ThreadID == uint64(f.thread) {
			return logFault{fmt.Errorf("a ThreadCreated at offset %d continues %d/%d", f.offset, c.ThreadID, c.Ordinal)}
		}
		if err := q.UpsertContinuation(ctx, dbq.UpsertContinuationParams{ThreadID: int64(f.thread),
			SourceThreadID: int64(c.ThreadID), SourceOrdinal: int64(c.Ordinal)}); err != nil {
			return err
		}
	}
	return replaceTags(ctx, q, f.thread, b.Tags)
}

// applyPost writes the post's row and makes it the thread's latest post.
// post_count is the ordinal: ordinals are dense and one-based, and deleted or
// moved posts keep theirs, so the latest ordinal is the count (docs/03).
//
// A copy, a Post with an origin, is applyCopy's: it is the latest post only
// in a thread its move opened.
func applyPost(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.PostBody)
	if f.h.Ordinal == 0 {
		return logFault{fmt.Errorf("a Post at offset %d carries ordinal 0", f.offset)}
	}
	if b.Origin != nil && (b.Origin.ThreadID == 0 || b.Origin.Ordinal == 0) {
		return logFault{fmt.Errorf("a Post at offset %d names origin %d/%d", f.offset, b.Origin.ThreadID, b.Origin.Ordinal)}
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
	latest := true
	if b.Origin != nil {
		var err error
		if latest, err = applyCopy(ctx, q, f, b.Origin); err != nil {
			return err
		}
	}
	if latest {
		if err := one(q.SetThreadLastPost(ctx, dbq.SetThreadLastPostParams{
			PostCount:        int64(f.h.Ordinal),
			LastPostAt:       f.h.TimestampMS,
			LastPostAuthorID: int64(f.h.Actor),
			LastPostOrdinal:  int64(f.h.Ordinal),
			ID:               int64(f.thread),
		}))(f, "thread"); err != nil {
			return err
		}
	} else if err := one(q.SetThreadPostCount(ctx, dbq.SetThreadPostCountParams{
		PostCount: int64(f.h.Ordinal), ID: int64(f.thread),
	}))(f, "thread"); err != nil {
		return err
	}
	if err := replaceAttachments(ctx, q, f, b.Attachments); err != nil {
		return err
	}
	if err := replaceMentions(ctx, q, f, b.Mentions); err != nil {
		return err
	}
	// A split or merge copy records no backlinks, as the notifier generates
	// nothing for one: how its quotations resolve in its new thread is
	// TKT-01M29CXJZZ's. Any rows the ordinal held are cleared, so a replay
	// over a reused row cannot leave them.
	if b.Origin != nil {
		return replaceQuotes(ctx, q, f, "")
	}
	return replaceQuotes(ctx, q, f, b.Source)
}

// applyEdit moves the post to its new revision. created_at and author stay:
// they belong to revision 1, as in the index (threadlog applyEdit).
//
// A grace Edit (TKT-01M29CKRHS) leaves edited_at null on a post not yet
// edited, as the index leaves its edited flag clear, and moves it to its own
// time on one that is, as the index keeps the flag and moves to the new
// revision. So edited_at is always the index's edited flag at the current
// revision's time, which is what a thread page serves.
func applyEdit(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.EditBody)
	var (
		n   int64
		err error
	)
	if b.Grace {
		n, err = q.SetPostRevisionKeepEdited(ctx, dbq.SetPostRevisionKeepEditedParams{
			CurrentRevision: revision(f.h),
			EditedAt:        f.h.TimestampMS,
			ReplyTo:         replyTo(b.ReplyTo),
			LogOffset:       f.offset,
			ThreadID:        int64(f.thread),
			Ordinal:         int64(f.h.Ordinal),
		})
	} else {
		n, err = q.SetPostRevision(ctx, dbq.SetPostRevisionParams{
			CurrentRevision: revision(f.h),
			EditedAt:        sql.NullInt64{Int64: f.h.TimestampMS, Valid: true},
			ReplyTo:         replyTo(b.ReplyTo),
			LogOffset:       f.offset,
			ThreadID:        int64(f.thread),
			Ordinal:         int64(f.h.Ordinal),
		})
	}
	if err := one(n, err)(f, "post"); err != nil {
		return err
	}
	// An Edit's source is the post's whole new text, so its quotations
	// replace the post's, grace edits included.
	if err := replaceQuotes(ctx, q, f, b.Source); err != nil {
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

// applyTagged checks for the thread row first: an empty tag list touches no
// row, so without the check a ThreadTagged for a thread the projection lacks
// would pass unnoticed (review 390).
func applyTagged(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.ThreadTaggedBody)
	if _, err := q.GetThread(ctx, int64(f.thread)); errors.Is(err, sql.ErrNoRows) {
		return one(0, nil)(f, "thread")
	} else if err != nil {
		return err
	}
	return replaceTags(ctx, q, f.thread, b.Tags)
}

func applyArchived(ctx context.Context, q *dbq.Queries, f frame) error {
	return one(q.SetThreadArchived(ctx, int64(f.thread)))(f, "thread")
}

// applyRedacted is a hard redaction's marker (docs/03 "Hard redaction",
// TKT-01M29CXJZ9). For a post, the post is deleted and has no mentions, no
// attachments and no quote backlinks, which is what a reindex of the
// rewritten log leaves: its blanked Post names no one, lists no blob
// (TKT-01M29CXK0K) and quotes nothing (TKT-01M3YME9AZ). So catching up by
// this one frame and reindexing the new log agree.
// A title marker changes no row: the ThreadRenamed the command appends after
// it sets the title, through applyRenamed.
func applyRedacted(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.RedactedBody)
	if b.Title {
		if f.h.Ordinal != 0 || b.Ordinal != 0 {
			return logFault{fmt.Errorf("a title Redacted at offset %d names ordinal %d", f.offset, f.h.Ordinal)}
		}
		return nil
	}
	if f.h.Ordinal == 0 || b.Ordinal != f.h.Ordinal {
		return logFault{fmt.Errorf("a Redacted at offset %d names ordinal %d in its body and %d in its frame",
			f.offset, b.Ordinal, f.h.Ordinal)}
	}
	if err := one(q.SetPostDeleted(ctx, dbq.SetPostDeletedParams{
		Deleted: 1, ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal),
	}))(f, "post"); err != nil {
		return err
	}
	if err := q.DeletePostAttachments(ctx, dbq.DeletePostAttachmentsParams{
		ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)}); err != nil {
		return err
	}
	if err := q.DeleteMentions(ctx, dbq.DeleteMentionsParams{ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)}); err != nil {
		return err
	}
	// The blanked source quotes nothing, so a reindex of the rewritten log
	// leaves the post no backlinks either (TKT-01M3YME9AZ).
	return replaceQuotes(ctx, q, f, "")
}

// Moves (docs/03 "How a move is written", TKT-01M29CXJZZ). A move writes a
// PostsSplitOut to the source and a PostsMergedIn, then the copies, to the
// target, and the two threads are projected independently and in either
// order. So each side upserts its own columns of the post_moves rows and
// reads nothing the other side writes, apart from source_revision, which
// IsCarriedEdit treats as unknown until the source is projected.

// applySplitOut marks every post it names moved, records each one's move
// from the source's side, and recomputes the thread's last post, which may
// have been one of them.
func applySplitOut(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.PostsSplitOutBody)
	if f.h.Ordinal != 0 {
		return logFault{fmt.Errorf("a PostsSplitOut at offset %d names ordinal %d", f.offset, f.h.Ordinal)}
	}
	for _, o := range b.Ordinals {
		// Before SetPostMoved, though either order reads the same revision.
		n, err := q.UpsertSplitMove(ctx, dbq.UpsertSplitMoveParams{
			TargetThreadID: int64(b.TargetThreadID), SplitOffset: f.offset,
			SourceThreadID: int64(f.thread), SourceOrdinal: int64(o),
		})
		if err == nil && n == 1 {
			n, err = q.SetPostMoved(ctx, dbq.SetPostMovedParams{ThreadID: int64(f.thread), Ordinal: int64(o)})
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return logFault{fmt.Errorf("a PostsSplitOut at offset %d moves post %d, which the projection does not have",
				f.offset, o)}
		}
	}
	return setListedLastPost(ctx, q, f)
}

// setListedLastPost sets the thread's last_post_* to its newest post that
// is neither moved out nor a copy merged into it, the posts that set them
// when they were projected (applyPost, applyCopy). A thread left with no
// such post is as its ThreadCreated left it: its creation, by its author,
// at ordinal 0, which no list shows. post_count stays the latest ordinal.
func setListedLastPost(ctx context.Context, q *dbq.Queries, f frame) error {
	t, err := q.GetThread(ctx, int64(f.thread))
	if errors.Is(err, sql.ErrNoRows) {
		return one(0, nil)(f, "thread")
	} else if err != nil {
		return err
	}
	last := dbq.SetThreadLastPostParams{
		PostCount: t.PostCount, LastPostAt: t.CreatedAt, LastPostAuthorID: t.AuthorID, ID: t.ID,
	}
	switch p, err := q.NewestListedPost(ctx, t.ID); {
	case err == nil:
		last.LastPostAt, last.LastPostAuthorID, last.LastPostOrdinal = p.CreatedAt, p.AuthorID, p.Ordinal
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	return one(q.SetThreadLastPost(ctx, last))(f, "thread")
}

// applyMergedIn records the move of each source ordinal from the target's
// side. It opens the thread when the thread holds no post yet, which is a
// split's new thread: its copies then set its last post (applyCopy).
func applyMergedIn(ctx context.Context, q *dbq.Queries, f frame) error {
	b := f.body.(*threadlog.PostsMergedInBody)
	if f.h.Ordinal != 0 {
		return logFault{fmt.Errorf("a PostsMergedIn at offset %d names ordinal %d", f.offset, f.h.Ordinal)}
	}
	t, err := q.GetThread(ctx, int64(f.thread))
	if errors.Is(err, sql.ErrNoRows) {
		return one(0, nil)(f, "thread")
	} else if err != nil {
		return err
	}
	for _, o := range b.Ordinals {
		if o == 0 {
			return logFault{fmt.Errorf("a PostsMergedIn at offset %d names ordinal 0", f.offset)}
		}
		if err := q.UpsertMergedMove(ctx, dbq.UpsertMergedMoveParams{
			SourceThreadID: int64(b.SourceThreadID), SourceOrdinal: int64(o),
			TargetThreadID: int64(f.thread), MergedOffset: f.offset, OpensThread: flag(t.PostCount == 0),
		}); err != nil {
			return err
		}
	}
	return nil
}

// applyCopy records where a copied Post landed and says whether it becomes
// the thread's last post. Only in a thread its move opened, and only when
// the thread has no last post yet or the copy is at least as new as it: a
// copy keeps its original's time, which is older than a split thread's
// creation. A copy merged into a thread that had posts never moves its last
// post, even when it is newer (docs/06), and only raises post_count.
func applyCopy(ctx context.Context, q *dbq.Queries, f frame, origin *threadlog.PostOrigin) (bool, error) {
	key := dbq.GetPostMoveParams{SourceThreadID: int64(origin.ThreadID), SourceOrdinal: int64(origin.Ordinal)}
	n, err := q.SetCopyMove(ctx, dbq.SetCopyMoveParams{
		SourceThreadID: key.SourceThreadID, SourceOrdinal: key.SourceOrdinal,
		TargetThreadID: int64(f.thread), TargetOrdinal: int64(f.h.Ordinal), CopyOffset: f.offset,
	})
	if err != nil {
		return false, err
	}
	if n == 0 {
		// A Post with an origin that no PostsMergedIn here names: no
		// move's copy, so an ordinary post (Terva review 2011).
		return true, nil
	}
	m, err := q.GetPostMove(ctx, key)
	if err != nil {
		return false, err
	}
	if m.OpensThread == 0 {
		return false, nil
	}
	t, err := q.GetThread(ctx, int64(f.thread))
	if errors.Is(err, sql.ErrNoRows) {
		return false, one(0, nil)(f, "thread")
	} else if err != nil {
		return false, err
	}
	return t.LastPostOrdinal == 0 || f.h.TimestampMS >= t.LastPostAt, nil
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

// replaceAttachments sets a post's post_attachments rows to the hashes its
// Post record lists, in order (TKT-01M29CXK0K). A hash named twice keeps its
// first position. Only a Post sets them: an Edit has no attachments field.
func replaceAttachments(ctx context.Context, q *dbq.Queries, f frame, hashes []string) error {
	if err := q.DeletePostAttachments(ctx, dbq.DeletePostAttachmentsParams{
		ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)}); err != nil {
		return err
	}
	for i, h := range hashes {
		if err := q.InsertPostAttachment(ctx, dbq.InsertPostAttachmentParams{
			ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal), Hash: h, Position: int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// replaceQuotes sets a post's post_quotes rows to the posts source quotes
// (render.QuotedOrdinals, TKT-01M3YME9AZ). The rows depend on the source
// alone, so a replay or a reindex writes the same ones; whether a quoting
// post is visible is decided where they are read.
func replaceQuotes(ctx context.Context, q *dbq.Queries, f frame, source string) error {
	if err := q.DeletePostQuotes(ctx, dbq.DeletePostQuotesParams{
		ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal)}); err != nil {
		return err
	}
	for _, o := range render.QuotedOrdinals(source, f.h.Ordinal) {
		if err := q.InsertPostQuote(ctx, dbq.InsertPostQuoteParams{
			ThreadID: int64(f.thread), Ordinal: int64(f.h.Ordinal), QuotedOrdinal: int64(o),
		}); err != nil {
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
			// The log's fault when the rows are its own projection from the
			// start, as in a reindex; a lost row's, when they are not.
			return logFault{fmt.Errorf("%s at offset %d (ordinal %d) names a %s the projection does not have",
				f.h.Type, f.offset, f.h.Ordinal, what)}
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
