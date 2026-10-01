package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// The viewer-state endpoints (TKT-01M29CA9CE): a member's read position in a
// thread and their draft reply to it. Each is the viewer's own row, reached
// only through a thread the viewer can read, so each admits the viewer with
// access first and a thread hidden from them is not-found here as it is on
// the thread page.
//
// They live beside the thread endpoints rather than in a package of their
// own because access is the rule they share, and a second copy of it is the
// copy that drifts.

// MayKeepState refuses a viewer who has no viewer state to keep: anyone who
// is not a member. The handlers ask it before reading a request, as they ask
// MayWrite.
func (s *Service) MayKeepState(v domain.Viewer) error {
	return member.Require(v, "keep a read position or a draft")
}

// postCount is how many posts thread id has, read from its index. The index
// rather than the projection's post_count, which trails it by the time the
// bus takes: a client that has just been shown a post may say so at once.
func (s *Service) postCount(id threadid.ID) (int, error) {
	var n int
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		n = th.Index.Len()
		return nil
	})
	return n, err
}

// MarkRead is PUT /threads/{id}/read (docs/06 "Read tracking", docs/07): the
// viewer has read up to ordinal. The position only moves forward, so a
// beacon that arrives late, out of order or from another device leaves a
// higher position where it is; the answer is the read boundary as it now
// stands, the position moved past the posts the viewer's mark made read.
// An ordinal past the thread's last post is read as the last post, since a
// client cannot have read a post that does not exist.
func (s *Service) MarkRead(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32) (dto.ReadPosition, error) {
	var out dto.ReadPosition
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	if ordinal < 1 {
		return out, problem.Coded(problem.CodeBadRequest, errors.New("threads: a read position names a post, from 1"))
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return out, err
	}
	n, err := s.postCount(id)
	if err != nil {
		return out, err
	}
	if int64(ordinal) > int64(n) {
		ordinal = uint32(n)
	}
	// The read-back shares the write transaction, which holds the one write
	// connection from BEGIN IMMEDIATE, so no other beacon lands between the
	// upsert and the answer.
	var boundary int64
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := q.UpsertReadPosition(ctx, dbq.UpsertReadPositionParams{
			UserID: v.UserID, ThreadID: int64(id),
			LastReadOrdinal: int64(ordinal), LastReadAt: s.now().UnixMilli(),
		}); err != nil {
			return err
		}
		var err error
		boundary, err = readBoundary(ctx, q, v, id)
		return err
	})
	if err != nil {
		return out, err
	}
	return dto.ReadPosition{LastReadOrdinal: int(boundary)}, nil
}

// readBoundary is v's read boundary in thread id, the integer the thread
// page's viewer.last_read_ordinal is (dbq's ReadBoundary): the stored
// position, moved past the posts v's site-level mark made read. A thread
// with no projection row has no boundary past the stored position, which
// is then read alone.
func readBoundary(ctx context.Context, q *dbq.Queries, v domain.Viewer, id threadid.ID) (int64, error) {
	b, err := q.ReadBoundary(ctx, dbq.ReadBoundaryParams{UserID: v.UserID, ThreadID: int64(id)})
	if err == nil {
		return b.Boundary, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	rp, err := q.GetReadPosition(ctx, dbq.GetReadPositionParams{UserID: v.UserID, ThreadID: int64(id)})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rp.LastReadOrdinal, err
}

// boundaryOf is the thread page's read boundary and the time that set it,
// from b (docs/06 "Read tracking", TKT-01M29CTY7K). When the site-level
// mark moved the boundary past the stored position, the mark is what set
// the viewer's place, so its time is the one the divider shows. Otherwise
// it is the stored position's time, and nil with no row.
func boundaryOf(b dbq.ReadBoundaryRow) (int64, *dto.Millis) {
	var at *dto.Millis
	switch {
	case b.Boundary > b.LastReadOrdinal && b.MarkAllReadAt.Valid:
		m := dto.Millis(b.MarkAllReadAt.Int64)
		at = &m
	case b.LastReadAt.Valid:
		m := dto.Millis(b.LastReadAt.Int64)
		at = &m
	}
	return b.Boundary, at
}

// MarkUnread is DELETE /threads/{id}/read (docs/07): the viewer marks the
// thread unread after ordinal, or the whole thread when ordinal is nil. It
// is the only way a position moves back, and it never moves one forward:
// asking to mark unread from a post the viewer has not reached changes
// nothing. The answer is the read boundary as it now stands, which the
// thread page will show.
//
// A post written before the viewer's users.mark_all_read_at stays read
// whatever the position says, because docs/04's rule reads a post as unread
// only when it is past the position and after the mark. So the answer is
// never below the posts the mark made read (TKT-01M29CTY7K).
func (s *Service) MarkUnread(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal *uint32) (dto.ReadPosition, error) {
	var out dto.ReadPosition
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return out, err
	}
	var to int64
	if ordinal != nil {
		to = int64(*ordinal)
	}
	now := s.now().UnixMilli()
	var pos int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		key := dbq.EnsureReadPositionParams{UserID: v.UserID, ThreadID: int64(id), LastReadAt: now}
		if err := q.EnsureReadPosition(ctx, key); err != nil {
			return err
		}
		if _, err := q.LowerReadPosition(ctx, dbq.LowerReadPositionParams{
			Ordinal: to, LastReadAt: now, UserID: v.UserID, ThreadID: int64(id),
		}); err != nil {
			return err
		}
		var err error
		pos, err = readBoundary(ctx, q, v, id)
		return err
	})
	if err != nil {
		return out, err
	}
	return dto.ReadPosition{LastReadOrdinal: int(pos)}, nil
}

// Watches and mutes (docs/04 "watches", docs/07, TKT-01M29CRJ29). A level
// is the viewer's own row on a thread or a forum, reached only through
// something the viewer can read, so each admits the viewer with the access
// its page uses first: a thread or forum hidden from them is not-found here
// as on its page, and nothing about it is written or said. Watching needs
// only read, so an archived thread or forum may be watched, and a member
// who may not reply may still mute.
//
// The empty level clears the row: a thread with none follows its forum, and
// a forum with none adds nothing. Any other level than watch or mute is
// validation-failed, before anything is written.

// SetThreadWatch is POST /threads/{id}/watch: it sets the viewer's own
// level on the thread, and answers both levels as they then stand, read in
// the write's transaction.
func (s *Service) SetThreadWatch(ctx context.Context, v domain.Viewer, id threadid.ID, level string) (dto.ThreadWatch, error) {
	var out dto.ThreadWatch
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if err := checkWatchLevel(level); err != nil {
		return out, err
	}
	thread := sql.NullInt64{Int64: int64(id), Valid: true}
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if level == domain.WatchNone {
			err = q.ClearThreadWatch(ctx, dbq.ClearThreadWatchParams{UserID: v.UserID, ThreadID: thread})
		} else {
			err = q.SetThreadWatch(ctx, dbq.SetThreadWatchParams{UserID: v.UserID, ThreadID: thread, Level: level,
				Now: s.now().UnixMilli()})
		}
		if err != nil {
			return err
		}
		if out.Watch, err = threadWatchLevel(ctx, q, v, id); err != nil {
			return err
		}
		out.ForumWatch, err = forumWatchLevel(ctx, q, v, a.row.ForumID)
		return err
	})
	return out, err
}

// SetForumWatch is POST /forums/{slug}/watch: it sets the viewer's level on
// the forum, which covers the forum's own threads and not its children's,
// and answers the level as it then stands. The forum is admitted as its
// thread list admits it.
func (s *Service) SetForumWatch(ctx context.Context, v domain.Viewer, slug string, level string) (dto.ForumWatch, error) {
	var out dto.ForumWatch
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return out, err
	}
	if err := checkWatchLevel(level); err != nil {
		return out, err
	}
	key := sql.NullInt64{Int64: forum.ID, Valid: true}
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if level == domain.WatchNone {
			err = q.ClearForumWatch(ctx, dbq.ClearForumWatchParams{UserID: v.UserID, ForumID: key})
		} else {
			err = q.SetForumWatch(ctx, dbq.SetForumWatchParams{UserID: v.UserID, ForumID: key, Level: level,
				Now: s.now().UnixMilli()})
		}
		if err != nil {
			return err
		}
		out.Watch, err = forumWatchLevel(ctx, q, v, forum.ID)
		return err
	})
	return out, err
}

// checkWatchLevel refuses a level that is not watch, mute or none, as
// docs/07's field errors name a value outside a closed set.
func checkWatchLevel(level string) error {
	if domain.ValidWatchLevel(level) {
		return nil
	}
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers,
		Fields: []forms.FieldError{{Field: "level", Code: forms.CodeNotAnOption}}}
}

// threadWatchLevel is v's own level on thread id, "" with no row.
func threadWatchLevel(ctx context.Context, q *dbq.Queries, v domain.Viewer, id threadid.ID) (string, error) {
	level, err := q.ThreadWatchLevel(ctx, dbq.ThreadWatchLevelParams{
		UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true}})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WatchNone, nil
	}
	return level, err
}

// forumWatchLevel is v's level on forum id, "" with no row.
func forumWatchLevel(ctx context.Context, q *dbq.Queries, v domain.Viewer, id int64) (string, error) {
	level, err := q.ForumWatchLevel(ctx, dbq.ForumWatchLevelParams{
		UserID: v.UserID, ForumID: sql.NullInt64{Int64: id, Valid: true}})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WatchNone, nil
	}
	return level, err
}

// Drafts (docs/07 "Drafts"): a reply draft keyed by thread (TKT-01M29CA9CE),
// and a new-thread draft keyed by forum (TKT-01M3K9XBJA). Each key carries a
// revision that only grows: a save adds one, and a delete of a live draft
// adds one and keeps the row as a tombstone, so a draft deleted and started
// again answers a higher revision than any copy of the old one. The queries
// compute it from the stored value, inside the write's transaction, and no
// clock enters it. A tombstone reads as no draft.
//
// When a read answers false, or a delete answers, the revision given is the
// key's as it stands: a tombstone's, or 0 for a key that has never been
// saved. The handlers send it as the Draft-Revision header on the 204.
//
// Both kinds keep the composer's attachments (TKT-01M3V73DP6, docs/07
// "Drafts", decided by the run 2026-10-01). A save checks them as a post's
// are, through checkAttachments, against the forum the post would go to,
// but adds no reference: blob GC and ketju redact keep a blob a draft names
// instead (DeleteCollectableBlob). A tombstone holds none.

// DraftRequest is a draft reply as the composer holds it.
type DraftRequest struct {
	// Source may be empty: a draft holding only a reply target is still the
	// composer's state. Its length is bounded by the request body limit,
	// which is the post limit, so a draft can always be posted.
	Source string
	// ReplyTo is the post the reply chip names, or nil for none.
	ReplyTo *uint32
	// Attachments are blob hashes the viewer uploaded, held to a post's
	// attach rules (Attachments.Check). A save replaces the whole list, so
	// none clears a stored one.
	Attachments []string
}

// Draft is GET /drafts/{thread_id} (docs/07): the viewer's draft reply to the
// thread, and false when they have none. With false, only the Draft's
// Revision is set: the key's revision, which a tombstone keeps.
func (s *Service) Draft(ctx context.Context, v domain.Viewer, id threadid.ID) (dto.Draft, bool, error) {
	var out dto.Draft
	if err := s.MayKeepState(v); err != nil {
		return out, false, err
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return out, false, err
	}
	var row dbq.Draft
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.GetThreadDraft(ctx, threadDraftKey(v, id))
		if err != nil || row.Deleted != 0 {
			return err
		}
		out, err = draftDTO(ctx, q, v, id, row)
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return dto.Draft{}, false, nil
	case err != nil:
		return dto.Draft{}, false, err
	case row.Deleted != 0:
		return dto.Draft{Revision: row.Revision}, false, nil
	}
	return out, true, nil
}

// SaveDraft is PUT /drafts/{thread_id} (docs/06 "Composer", docs/07): the
// viewer's one draft for the thread takes req, whichever device sent it, and
// the answer is the draft as stored, one revision on.
//
// Saving needs reply in the thread's forum, since a draft is a reply being
// written. A reply target must name a post the thread has; a deleted one is
// accepted, as the reply path accepts it. Attachments are checked as a
// reply's are, in the thread's forum.
func (s *Service) SaveDraft(ctx context.Context, v domain.Viewer, id threadid.ID, req DraftRequest) (dto.Draft, error) {
	var out dto.Draft
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if !a.canReply {
		return out, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: %s is not open to this viewer's replies", id))
	}
	replyTo := sql.NullInt64{}
	if req.ReplyTo != nil {
		n, err := s.postCount(id)
		if err != nil {
			return out, err
		}
		if *req.ReplyTo < 1 || int64(*req.ReplyTo) > int64(n) {
			return out, problem.Coded(problem.CodeValidationFailed,
				fmt.Errorf("threads: %s has no post %d to reply to", id, *req.ReplyTo))
		}
		replyTo = sql.NullInt64{Int64: int64(*req.ReplyTo), Valid: true}
	}
	list, column, err := s.draftAttachmentList(ctx, v, a.row.ForumID, req.Attachments)
	if err != nil {
		return out, err
	}
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := s.recheckAttachments(ctx, q, v, list); err != nil {
			return err
		}
		row, err := q.UpsertThreadDraft(ctx, dbq.UpsertThreadDraftParams{
			UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true},
			Source: req.Source, ReplyTo: replyTo, Attachments: column, Now: s.now().UnixMilli(),
		})
		if err != nil {
			return err
		}
		out, err = draftDTO(ctx, q, v, id, row)
		return err
	})
	if err != nil {
		return dto.Draft{}, err
	}
	return out, nil
}

// DeleteDraft is DELETE /drafts/{thread_id} (docs/07): the viewer discards
// their draft for the thread, and the answer is the key's revision after it.
// Discarding a draft that is not there is not an error and changes nothing,
// so a client that retries a delete, or deletes after posting, needs no
// special case.
func (s *Service) DeleteDraft(ctx context.Context, v domain.Viewer, id threadid.ID) (int64, error) {
	if err := s.MayKeepState(v); err != nil {
		return 0, err
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	return s.tombstone(ctx, func(q *dbq.Queries) (dbq.Draft, error) {
		key := threadDraftKey(v, id)
		if _, err := q.DeleteThreadDraft(ctx, dbq.DeleteThreadDraftParams{Now: now, UserID: key.UserID, ThreadID: key.ThreadID}); err != nil {
			return dbq.Draft{}, err
		}
		return q.GetThreadDraft(ctx, key)
	})
}

// ForumDraftRequest is a new thread as its form holds it.
type ForumDraftRequest struct {
	// Title is the title as typed, or nil for none, which clears a stored
	// one: a save replaces the whole draft.
	Title *string
	// Source may be empty, as for a reply draft.
	Source string
	// Attachments are as for a reply draft, checked in the forum.
	Attachments []string
}

// ForumDraft is GET /forums/{slug}/draft (docs/07 "Drafts"): the viewer's
// new-thread draft in the forum, and false when they have none. With false,
// only the ForumDraft's Revision is set, as for Draft. Reading needs only
// read access to the forum.
func (s *Service) ForumDraft(ctx context.Context, v domain.Viewer, slug string) (dto.ForumDraft, bool, error) {
	var out dto.ForumDraft
	if err := s.MayKeepState(v); err != nil {
		return out, false, err
	}
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return out, false, err
	}
	var row dbq.Draft
	err = s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		row, err = q.GetForumDraft(ctx, forumDraftKey(v, forum))
		if err != nil || row.Deleted != 0 {
			return err
		}
		out, err = forumDraftDTO(ctx, q, v, forum, row)
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return dto.ForumDraft{}, false, nil
	case err != nil:
		return dto.ForumDraft{}, false, err
	case row.Deleted != 0:
		return dto.ForumDraft{Revision: row.Revision}, false, nil
	}
	return out, true, nil
}

// SaveForumDraft is PUT /forums/{slug}/draft (docs/07 "Drafts"): the viewer's
// one new-thread draft for the forum takes req, and the answer is the draft
// as stored, one revision on.
//
// Saving is refused as creating a thread there would be (forumForThread): an
// archived forum is conflict to everyone but an admin, and it needs
// create_thread. The title is held to a thread title's rules but two: it may
// be empty, and it is kept as typed rather than trimmed, since it is not
// finished. Attachments are checked as a new thread's are, in this forum.
func (s *Service) SaveForumDraft(ctx context.Context, v domain.Viewer, slug string, req ForumDraftRequest) (dto.ForumDraft, error) {
	var out dto.ForumDraft
	if err := s.MayKeepState(v); err != nil {
		return out, err
	}
	forum, err := s.forumForThread(ctx, v, slug)
	if err != nil {
		return out, err
	}
	title := sql.NullString{}
	if req.Title != nil {
		if err := checkDraftTitle(*req.Title); err != nil {
			return out, err
		}
		title = sql.NullString{String: *req.Title, Valid: true}
	}
	list, column, err := s.draftAttachmentList(ctx, v, forum.ID, req.Attachments)
	if err != nil {
		return out, err
	}
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := s.recheckAttachments(ctx, q, v, list); err != nil {
			return err
		}
		row, err := q.UpsertForumDraft(ctx, dbq.UpsertForumDraftParams{
			UserID: v.UserID, ForumID: sql.NullInt64{Int64: forum.ID, Valid: true},
			Title: title, Source: req.Source, Attachments: column, Now: s.now().UnixMilli(),
		})
		if err != nil {
			return err
		}
		out, err = forumDraftDTO(ctx, q, v, forum, row)
		return err
	})
	if err != nil {
		return dto.ForumDraft{}, err
	}
	return out, nil
}

// DeleteForumDraft is DELETE /forums/{slug}/draft (docs/07 "Drafts"): as
// DeleteDraft, for the viewer's new-thread draft in the forum. It needs only
// read access, so a member can discard a draft in a forum since archived.
func (s *Service) DeleteForumDraft(ctx context.Context, v domain.Viewer, slug string) (int64, error) {
	if err := s.MayKeepState(v); err != nil {
		return 0, err
	}
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return 0, err
	}
	now := s.now().UnixMilli()
	return s.tombstone(ctx, func(q *dbq.Queries) (dbq.Draft, error) {
		key := forumDraftKey(v, forum)
		if _, err := q.DeleteForumDraft(ctx, dbq.DeleteForumDraftParams{Now: now, UserID: key.UserID, ForumID: key.ForumID}); err != nil {
			return dbq.Draft{}, err
		}
		return q.GetForumDraft(ctx, key)
	})
}

// tombstone runs del, which tombstones a draft and reads its key back, in
// one write transaction, so no other save lands between the two, and
// answers the key's revision: 0 when it has no row.
func (s *Service) tombstone(ctx context.Context, del func(q *dbq.Queries) (dbq.Draft, error)) (int64, error) {
	var rev int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		row, err := del(q)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		rev = row.Revision
		return err
	})
	return rev, err
}

// checkDraftTitle is checkTitle for a title still being written: a control
// character or more than MaxTitle characters is refused, but an empty title
// is not, and nothing is trimmed.
func checkDraftTitle(title string) error {
	if strings.ContainsFunc(title, unicode.IsControl) {
		return problem.Coded(problem.CodeValidationFailed, errors.New("threads: the draft's title holds a control character"))
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		return problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: the draft's title is over %d characters", MaxTitle))
	}
	return nil
}

func threadDraftKey(v domain.Viewer, id threadid.ID) dbq.GetThreadDraftParams {
	return dbq.GetThreadDraftParams{UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true}}
}

func forumDraftKey(v domain.Viewer, forum dbq.Forum) dbq.GetForumDraftParams {
	return dbq.GetForumDraftParams{UserID: v.UserID, ForumID: sql.NullInt64{Int64: forum.ID, Valid: true}}
}

// draftAttachmentList checks a draft's attachments as a post to forumID
// would have them checked (checkAttachments), and answers the list that
// passed and its drafts.attachments column. It adds no reference.
func (s *Service) draftAttachmentList(ctx context.Context, v domain.Viewer, forumID int64, hashes []string) ([]string, string, error) {
	list, err := s.checkAttachments(ctx, v, forumID, hashes)
	if err != nil {
		return nil, "", err
	}
	column, err := encodeAttachments(list)
	return list, column, err
}

// recheckAttachments is Attachments.Recheck of list, which
// draftAttachmentList answered, through q, the write that saves the draft.
// An empty list asks nothing, and a list is only ever non-empty when the
// service has a blob store (checkAttachments).
func (s *Service) recheckAttachments(ctx context.Context, q *dbq.Queries, v domain.Viewer, list []string) error {
	if len(list) == 0 {
		return nil
	}
	return s.Attachments.Recheck(ctx, q, v.UserID, list)
}

// draftDTO and forumDraftDTO draw a draft through q, the transaction that
// read or wrote it.
func draftDTO(ctx context.Context, q *dbq.Queries, v domain.Viewer, id threadid.ID, row dbq.Draft) (dto.Draft, error) {
	attachments, err := draftAttachments(ctx, q, v, row.Attachments)
	if err != nil {
		return dto.Draft{}, err
	}
	d := dto.Draft{ThreadID: id.String(), Source: row.Source, Attachments: attachments,
		UpdatedAt: dto.Millis(row.UpdatedAt), Revision: row.Revision}
	if row.ReplyTo.Valid {
		r := uint32(row.ReplyTo.Int64)
		d.ReplyTo = &r
	}
	return d, nil
}

func forumDraftDTO(ctx context.Context, q *dbq.Queries, v domain.Viewer, forum dbq.Forum, row dbq.Draft) (dto.ForumDraft, error) {
	attachments, err := draftAttachments(ctx, q, v, row.Attachments)
	if err != nil {
		return dto.ForumDraft{}, err
	}
	d := dto.ForumDraft{Forum: forum.Slug, Source: row.Source, Attachments: attachments,
		UpdatedAt: dto.Millis(row.UpdatedAt), Revision: row.Revision}
	if row.Title.Valid {
		t := row.Title.String
		d.Title = &t
	}
	return d, nil
}
