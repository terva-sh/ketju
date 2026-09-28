package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
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
// higher position where it is; the answer is the position as it now stands.
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
	var rp dbq.ReadPosition
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		if err := q.UpsertReadPosition(ctx, dbq.UpsertReadPositionParams{
			UserID: v.UserID, ThreadID: int64(id),
			LastReadOrdinal: int64(ordinal), LastReadAt: s.now().UnixMilli(),
		}); err != nil {
			return err
		}
		var err error
		rp, err = q.GetReadPosition(ctx, dbq.GetReadPositionParams{UserID: v.UserID, ThreadID: int64(id)})
		return err
	})
	if err != nil {
		return out, err
	}
	return dto.ReadPosition{LastReadOrdinal: int(rp.LastReadOrdinal)}, nil
}

// MarkUnread is DELETE /threads/{id}/read (docs/07): the viewer marks the
// thread unread after ordinal, or the whole thread when ordinal is nil. It
// is the only way a position moves back, and it never moves one forward:
// asking to mark unread from a post the viewer has not reached changes
// nothing. The answer is the position as it now stands.
//
// A post written before the viewer's users.mark_all_read_at stays read
// whatever the position says, because docs/04's rule reads a post as unread
// only when it is past the position and after the mark.
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
		var err error
		pos, err = q.LowerReadPosition(ctx, dbq.LowerReadPositionParams{
			Ordinal: to, LastReadAt: now, UserID: v.UserID, ThreadID: int64(id),
		})
		return err
	})
	if err != nil {
		return out, err
	}
	return dto.ReadPosition{LastReadOrdinal: int(pos)}, nil
}

// DraftRequest is a draft reply as the composer holds it.
type DraftRequest struct {
	// Source may be empty: a draft holding only a reply target is still the
	// composer's state. Its length is bounded by the request body limit,
	// which is the post limit, so a draft can always be posted.
	Source string
	// ReplyTo is the post the reply chip names, or nil for none.
	ReplyTo *uint32
}

// Draft is GET /drafts/{thread_id} (docs/07): the viewer's draft reply to the
// thread, and false when they have none.
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
		row, err = q.GetThreadDraft(ctx, dbq.GetThreadDraftParams{
			UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true},
		})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	return draftDTO(id, row), true, nil
}

// SaveDraft is PUT /drafts/{thread_id} (docs/06 "Composer", docs/07): the
// viewer's one draft for the thread takes req, whichever device sent it, and
// the answer is the draft as stored.
//
// Saving needs reply in the thread's forum, since a draft is a reply being
// written. A reply target must name a post the thread has; a deleted one is
// accepted, as the reply path accepts it.
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
	var row dbq.Draft
	err = s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		row, err = q.UpsertThreadDraft(ctx, dbq.UpsertThreadDraftParams{
			UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true},
			Source: req.Source, ReplyTo: replyTo, Now: s.now().UnixMilli(),
		})
		return err
	})
	if err != nil {
		return out, err
	}
	return draftDTO(id, row), nil
}

// DeleteDraft is DELETE /drafts/{thread_id} (docs/07): the viewer discards
// their draft for the thread. Discarding a draft that is not there is not an
// error, so a client that retries a delete, or deletes after posting, needs
// no special case.
func (s *Service) DeleteDraft(ctx context.Context, v domain.Viewer, id threadid.ID) error {
	if err := s.MayKeepState(v); err != nil {
		return err
	}
	if _, err := s.access(ctx, v, id); err != nil {
		return err
	}
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.DeleteThreadDraft(ctx, dbq.DeleteThreadDraftParams{
			UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true},
		})
		return err
	})
}

func draftDTO(id threadid.ID, row dbq.Draft) dto.Draft {
	d := dto.Draft{ThreadID: id.String(), Source: row.Source, UpdatedAt: dto.Millis(row.UpdatedAt)}
	if row.ReplyTo.Valid {
		r := uint32(row.ReplyTo.Int64)
		d.ReplyTo = &r
	}
	return d
}
