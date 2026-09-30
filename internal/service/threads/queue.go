package threads

// The moderator queue (docs/08 "Flags", docs/07 "Moderation and admin",
// TKT-01M29CRJ4E): open flags, one item per flagged post, and pending
// applications, in one list by newest time first; and the actions that
// resolve a flagged post in place.
//
// It lives in this package, not a package of its own, because every action
// on a flagged post holds the post's flag lock, the one lock Flag,
// EnforceAutoHide, SweepAutoHide, a moderator's Restore and ConfirmHide hold
// (flags.go). ResolvePost and FlagAuditCatchUp are the only way outside
// this package to take it, so no second mechanism can serialize a post.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// QueuePageSize is how many items one page of the queue holds.
const QueuePageSize = 25

// The actions a moderator takes on a flagged post.
const (
	// QueueResolve is "Keep post": the flags were read and the post stays.
	// On a post hidden pending review it shows the post again.
	QueueResolve = "resolve"
	// QueueDismiss closes the flags as unfounded. On a post hidden pending
	// review it shows the post again (maintainer decision 2026-09-29).
	QueueDismiss = "dismiss"
	// QueueRemove hides the post as a moderator's hide and resolves the
	// flags.
	QueueRemove = "remove"
)

// The audit actions the queue writes, one row per action, naming the post
// and the flags it closed (docs/04 "audit_log").
const (
	ActionFlagResolved  = "flag.resolved"
	ActionFlagDismissed = "flag.dismissed"
)

// FlagAuditGrace is how long a flag owed its audit row waits before
// FlagAuditCatchUp writes it, so the job does not race the request that is
// about to.
const FlagAuditGrace = time.Minute

// Resolution is a moderator's action on a flagged post.
type Resolution struct {
	Action string
	// Note is the flags' resolution_note and the Restore's or Delete's
	// reason. Optional, at most MaxReason characters.
	Note string
	// IP is the request's address, for the audit row.
	IP string
}

// errHandled is the conflict a queue action answers when the post has no
// open flag left: another moderator's action, or the auto-hide's restore
// path, closed them first.
var errHandled = errors.New("threads: another moderator has already handled this post")

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// queueScope is what the queue shows v: the forums whose flags v may act on
// (perm.ModeratedForums), and whether v may review applications
// (member.RequireStaff, as the application service decides). A viewer with
// neither has no queue.
func (s *Service) queueScope(ctx context.Context, v domain.Viewer) ([]int64, bool, error) {
	if !v.Member() {
		return nil, false, nil
	}
	forums, err := s.Perm.ModeratedForums(ctx, v)
	if err != nil {
		return nil, false, err
	}
	return forums, member.RequireStaff(v, "review applications") == nil, nil
}

// QueueCount is GET /me's queue: how many items Queue lists for v across
// every page, or nil for a viewer with no queue, which is anyone
// perm.ModeratesAny does not admit (TKT-01M29CRJ4E). A viewer with no queue
// pays ModeratedForums' one statement on a cold permission cache and none
// on a warm one; a viewer with a queue pays one count statement besides.
func (s *Service) QueueCount(ctx context.Context, v domain.Viewer) (*int, error) {
	forums, staff, err := s.queueScope(ctx, v)
	if err != nil || (len(forums) == 0 && !staff) {
		return nil, err
	}
	var n int
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		n, err = views.QueueCount(ctx, tx, forums, staff)
		return err
	}); err != nil {
		return nil, err
	}
	return &n, nil
}

// Queue is GET /admin/queue: one page of v's queue after cursor. A flag
// item is a post in a forum v may moderate, with every flag open on it, and
// the post as v reads it; an application item is a pending application,
// for staff. The list is newest first by the item's time: a post's newest
// open flag, or when the application was submitted.
//
// A viewer who is not signed in is unauthenticated, and one with no queue
// forbidden, as the moderates gate answers before this runs.
func (s *Service) Queue(ctx context.Context, v domain.Viewer, cursor string) (dto.Queue, error) {
	out := dto.Queue{Items: []dto.QueueItem{}}
	if err := member.Require(v, "read the moderator queue"); err != nil {
		return out, err
	}
	forums, staff, err := s.queueScope(ctx, v)
	if err != nil {
		return out, err
	}
	if len(forums) == 0 && !staff {
		return out, problem.Coded(problem.CodeForbidden, errors.New("threads: this viewer has no moderator queue"))
	}
	after, err := parseQueueCursor(cursor)
	if err != nil {
		return out, err
	}
	size := s.QueuePage
	if size <= 0 {
		size = QueuePageSize
	}
	var (
		rows    []views.QueueRow
		reports []views.QueueReport
		more    bool
	)
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		if rows, err = views.QueueItems(ctx, tx, forums, staff, after, size+1); err != nil {
			return err
		}
		if more = len(rows) > size; more {
			rows = rows[:size]
		}
		var posts []views.QueuePost
		for _, r := range rows {
			if r.Kind == views.QueueKindFlag {
				posts = append(posts, views.QueuePost{Thread: r.K1, Ordinal: r.K2})
			}
		}
		reports, err = views.QueueReports(ctx, tx, posts)
		return err
	}); err != nil {
		return out, err
	}
	if more {
		out.NextCursor = queueCursor(rows[len(rows)-1])
	}

	reporters := map[int64]bool{}
	byPost := map[views.QueuePost][]views.QueueReport{}
	for _, r := range reports {
		reporters[r.Reporter] = true
		k := views.QueuePost{Thread: r.Thread, Ordinal: r.Ordinal}
		byPost[k] = append(byPost[k], r)
	}
	authors, err := s.users(ctx, reporters)
	if err != nil {
		return out, err
	}

	for _, r := range rows {
		item := dto.QueueItem{CreatedAt: dto.Millis(r.At)}
		switch r.Kind {
		case views.QueueKindApplication:
			item.Kind = dto.QueueKindApplication
			app := dto.AdminApplicationRow{ID: r.K1, Status: "pending", CreatedAt: dto.Millis(r.At),
				EmailVerified: r.Verified.Int64 != 0}
			if r.Provider.Valid {
				app.Provider = new(r.Provider.String)
			}
			if r.Email.Valid {
				app.Email = new(r.Email.String)
			}
			item.Application = &app
		default:
			item.Kind = dto.QueueKindFlag
			id := threadid.ID(r.K1)
			f := &dto.QueueFlag{Thread: dto.QueueThread{ID: id.String(), Slug: r.Slug, Title: r.Title},
				Ordinal: int(r.K2), Flags: []dto.QueueReport{}}
			for _, rep := range byPost[views.QueuePost{Thread: r.K1, Ordinal: r.K2}] {
				f.Flags = append(f.Flags, dto.QueueReport{ID: rep.ID, Reason: rep.Reason, Note: rep.Note,
					Reporter: authors[rep.Reporter], CreatedAt: dto.Millis(rep.CreatedAt)})
			}
			// The post as v reads it, through the same access check as the
			// thread page: a moderator's view shows a hidden post's words.
			// A thread that cannot be read just now does not take the
			// whole queue down with it.
			if p, err := s.Post(ctx, v, id, uint32(r.K2), ""); err == nil {
				f.Post = &p.Body
			} else {
				s.logger().Warn("moderator queue: a flagged post could not be read",
					"thread", id.String(), "ordinal", r.K2, "err", err)
			}
			item.Flag = f
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// queueCursor is the cursor after r: "<at>:<kind>:<k1>:<k2>" in base64url.
func queueCursor(r views.QueueRow) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(r.At, 10) + ":" +
		strconv.FormatInt(r.Kind, 10) + ":" + strconv.FormatInt(r.K1, 10) + ":" + strconv.FormatInt(r.K2, 10)))
}

func parseQueueCursor(cursor string) (views.QueueCursor, error) {
	if cursor == "" {
		return views.QueueCursor{}, nil
	}
	bad := problem.Coded(problem.CodeBadRequest, fmt.Errorf("threads: queue cursor %q is malformed", cursor))
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return views.QueueCursor{}, bad
	}
	parts := strings.Split(string(b), ":")
	if len(parts) != 4 {
		return views.QueueCursor{}, bad
	}
	var n [4]int64
	for i, p := range parts {
		if n[i], err = strconv.ParseInt(p, 10, 64); err != nil {
			return views.QueueCursor{}, bad
		}
	}
	return views.QueueCursor{At: n[0], Kind: n[1], K1: n[2], K2: n[3], Set: true}, nil
}

// postState is a flagged post as the queue decides on it.
type postState int

const (
	stateVisible    postState = iota
	stateAutoHidden           // hidden pending review
	stateHandHidden           // hidden by a moderator
	stateGone                 // deleted by its author, or moved

	// stateAny is no state, for closeFlags: close whatever the post's
	// state is now.
	stateAny postState = -1
)

func stateOf(f threadlog.EntryFlags) postState {
	switch {
	case f&(threadlog.EntryDeleted|threadlog.EntryMoved) != 0:
		return stateGone
	case f.Has(threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden):
		return stateAutoHidden
	case f.Has(threadlog.EntryHiddenByMod):
		return stateHandHidden
	}
	return stateVisible
}

// ResolvePost is POST /admin/queue/posts/{id}/{ordinal}: a moderator's
// action on a flagged post (docs/08 "Flags", TKT-01M29CRJ4E). The flags are
// closed in place with the moderator, the time and the note; the item leaves
// the queue; and one flag.resolved or flag.dismissed row records the flags
// it closed, what was asked, and what happened to the post.
//
// The viewer must be able to moderate the thread's forum. Everything after
// that holds the post's flag lock and decides from the state read under it:
//
//   - No open flag is conflict: another moderator acted first, and waited
//     for nothing but the lock. A retry of a restore whose audit row failed
//     is the one exception, below.
//   - An archived thread is conflict, as ConfirmHide answers: it is
//     read-only, its flags included. The queue does not list one.
//   - dismiss on a post hidden pending review restores it through the
//     moderator's restore path, which dismisses every flag open under the
//     lock before the Restore, and reopens exactly those if the append
//     fails. Anywhere else dismiss closes the flags and changes nothing.
//   - resolve ("Keep post") on a post hidden pending review takes the same
//     restore path. On a post a moderator hid by hand it is conflict, since
//     keeping it is a restore of that moderator's decision, for the thread
//     page. Anywhere else it closes the flags.
//   - remove on a shown post appends a moderator's Delete refusing a post
//     already gone; on a post hidden pending review, the Delete that makes
//     the hide a moderator's, requiring the auto-hide. Then the flags are
//     resolved. On a post already down it resolves the flags and appends
//     nothing.
//
// Each append carries the state it was decided on as its precondition, and
// a close with no append reads the state again before its write
// (closeFlags). A delete or restore of one post from the thread page holds
// the lock too, so what can still land outside it is a suspension's hide
// or restore of an author's posts; that is ErrPostState, or closeFlags'
// conflict, with the flags left open. A failed append leaves the flags open
// too. Where the flags are closed without a Restore, the close and the
// audit row are one write.
//
// The restore path writes its row only once the Restore append succeeded,
// since audit_log is append-only and a row cannot be withdrawn. The flags
// were dismissed before the append with audit_pending set, and the row's
// write clears it. If that write fails, the action happened: the answer is
// 200, and the row is owed, which recoverFlagAudit writes on the next
// request for the post or FlagAuditCatchUp within a minute or two.
func (s *Service) ResolvePost(ctx context.Context, v domain.Viewer, id threadid.ID, ordinal uint32, r Resolution) (dto.QueueResolution, error) {
	var out dto.QueueResolution
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	switch r.Action {
	case QueueResolve, QueueDismiss, QueueRemove:
	default:
		return out, problem.Coded(problem.CodeValidationFailed,
			fmt.Errorf("threads: a queue action is resolve, dismiss or remove, not %q", r.Action))
	}
	if err := checkReason(r.Note); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if !a.canModerate {
		return out, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not moderate %s's forum", id))
	}
	frame, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}

	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()

	var (
		entry    threadlog.IndexEntry
		archived bool
	)
	if err := s.Threads.With(id, func(th *threadcache.Thread) error {
		archived = th.Archived()
		var err error
		entry, err = th.Index.Entry(ordinal)
		return err
	}); err != nil {
		return out, err
	}
	recovered, err := s.recoverFlagAudit(ctx, id, ordinal)
	if err != nil {
		return out, err
	}
	var open int64
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		open, err = q.OpenFlagCount(ctx, dbq.OpenFlagCountParams{ThreadID: int64(id), Ordinal: int64(ordinal)})
		return err
	}); err != nil {
		return out, err
	}
	if open == 0 {
		// The retry of a request whose Restore landed and whose row
		// failed: this moderator's Restore is in the log, its row is
		// written now, and the post is still shown. Any other request did
		// not happen, and neither did one whose recovered restore was
		// somebody else's.
		if mine := recovered[v.UserID]; len(mine) > 0 && r.Action != QueueRemove && entry.Flags.Visible() {
			return dto.QueueResolution{Action: r.Action, Post: dto.QueuePostRestored, Flags: mine}, nil
		}
		return out, problem.Coded(problem.CodeConflict, errHandled)
	}
	if archived {
		return out, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
	}
	if s.afterQueueRead != nil {
		s.afterQueueRead(id, ordinal)
	}

	by := audit.By{Actor: v.UserID, IP: r.IP}
	state := stateOf(entry.Flags)
	switch r.Action {
	case QueueDismiss:
		switch state {
		case stateAutoHidden:
			return s.queueRestore(ctx, v, a, frame, id, ordinal, r)
		case stateVisible:
			return s.closeFlags(ctx, by, id, ordinal, r, state, dto.QueuePostUnchanged)
		default:
			return s.closeFlags(ctx, by, id, ordinal, r, state, dto.QueuePostAlreadyHidden)
		}
	case QueueResolve:
		switch state {
		case stateAutoHidden:
			return s.queueRestore(ctx, v, a, frame, id, ordinal, r)
		case stateHandHidden:
			return out, problem.Coded(problem.CodeConflict,
				fmt.Errorf("threads: post %d of %s was hidden by a moderator: restore it from the thread page first", ordinal, id))
		case stateVisible:
			return s.closeFlags(ctx, by, id, ordinal, r, state, dto.QueuePostUnchanged)
		default:
			return s.closeFlags(ctx, by, id, ordinal, r, state, dto.QueuePostAlreadyHidden)
		}
	default: // QueueRemove
		var rec threadlog.Append
		switch state {
		case stateAutoHidden:
			rec = threadlog.Append{
				Body:    &threadlog.DeleteBody{Reason: r.Note, ByModerator: true},
				Require: threadlog.EntryAutoHidden,
				Refuse:  threadlog.EntryDeleted | threadlog.EntryMoved,
			}
		case stateVisible:
			// A moderator's hide whoever wrote the post, the remover
			// included: the queue's remove is a moderation decision, and
			// its record says so, as the auto-hidden case's does (Terva
			// review 1522 on PR #184). The thread page's delete of one's
			// own post stays an author's delete.
			//
			// The precondition is the shown state it was decided on: every
			// bit that takes a post down is refused, so a hide that landed
			// outside the lock since, a suspension's or the auto-hide's
			// (hidden-by-mod, with auto-hidden), is ErrPostState, which is
			// conflict with the flags open (Terva review 1524;
			// TestStateChangeIsConflictNotSuccess). This is gone, spelled out.
			rec = threadlog.Append{
				Body: &threadlog.DeleteBody{Reason: r.Note, ByModerator: true},
				Refuse: threadlog.EntryDeleted | threadlog.EntryHiddenByMod | threadlog.EntryAutoHidden |
					threadlog.EntryMoved,
			}
		default:
			return s.closeFlags(ctx, by, id, ordinal, r, state, dto.QueuePostAlreadyHidden)
		}
		rec.Type, rec.Flags, rec.Author, rec.Ordinal = threadlog.TypeDelete, frame, uint64(v.UserID), ordinal
		_, appended, err := s.changePostAppended(ctx, id, ordinal, a, func(th *threadcache.Thread, _ threadlog.IndexEntry) (threadlog.Append, error) {
			if th.Archived() {
				return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
			}
			rec.TimeMS = s.now().UnixMilli()
			return rec, nil
		})
		if !appended {
			return out, err // the flags are still open, and the post as it was
		}
		// The hide is durable. Resolve the flags even if its read-back or
		// projection failed, so the item says what is true; if the resolve
		// fails, the item stays, showing the post hidden, and the next
		// remove resolves it as already hidden, with no second Delete.
		res, rerr := s.closeFlags(ctx, by, id, ordinal, r, stateAny, dto.QueuePostHidden)
		if err = errors.Join(err, rerr); err != nil {
			return out, err
		}
		return res, nil
	}
}

// closeFlags closes the post's open flags, resolved or dismissed as r's
// action says, and writes the row that records it, in one write. The flags
// found are those open now, under the lock, so none reported since the read
// is left out. No open flag at all is conflict.
//
// decided is the post state the action was decided on, and post what the
// row says about the post from it. The state is read again with the thread
// held, just before the write, and a change is conflict with the flags left
// open (Terva review 1522 on PR #184). A delete or restore of one post holds
// the flag lock, so none lands in between; what is left is a suspension's
// hide or restore of every post by one author, which walks threads without
// the per-post lock and which this check narrows to the write itself.
// stateAny skips the check, for the close after the queue's own Delete.
func (s *Service) closeFlags(ctx context.Context, by audit.By, id threadid.ID, ordinal uint32, r Resolution,
	decided postState, post string) (dto.QueueResolution, error) {
	now := s.now().UnixMilli()
	action := ActionFlagResolved
	if r.Action == QueueDismiss {
		action = ActionFlagDismissed
	}
	out := dto.QueueResolution{Action: r.Action, Post: post}
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		if decided == stateAny {
			return s.closeFlagsWrite(ctx, by, id, ordinal, r, action, now, post, &out)
		}
		e, err := th.Index.Entry(ordinal)
		if err != nil {
			return err
		}
		if stateOf(e.Flags) != decided {
			return problem.Coded(problem.CodeConflict,
				fmt.Errorf("threads: post %d of %s changed while the queue decided on it", ordinal, id))
		}
		return s.closeFlagsWrite(ctx, by, id, ordinal, r, action, now, post, &out)
	})
	if err != nil {
		return dto.QueueResolution{}, err
	}
	return out, nil
}

// closeFlagsWrite is closeFlags' write.
func (s *Service) closeFlagsWrite(ctx context.Context, by audit.By, id threadid.ID, ordinal uint32, r Resolution,
	action string, now int64, post string, out *dto.QueueResolution) error {
	return s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var (
			ids []int64
			err error
		)
		who, at := sql.NullInt64{Int64: by.Actor, Valid: true}, sql.NullInt64{Int64: now, Valid: true}
		if action == ActionFlagDismissed {
			ids, err = q.DismissOpenFlagsForPost(ctx, dbq.DismissOpenFlagsForPostParams{ResolvedBy: who, ResolvedAt: at,
				ResolutionNote: r.Note, ThreadID: int64(id), Ordinal: int64(ordinal)})
		} else {
			ids, err = q.ResolveOpenFlagsForPost(ctx, dbq.ResolveOpenFlagsForPostParams{ResolvedBy: who, ResolvedAt: at,
				ResolutionNote: r.Note, ThreadID: int64(id), Ordinal: int64(ordinal)})
		}
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return problem.Coded(problem.CodeConflict, errHandled)
		}
		out.Flags = ids
		return flagAudit(ctx, q, time.UnixMilli(now), by, action, id, ordinal, ids, r.Action, r.Note, post, false)
	})
}

// queueRestore is dismiss and resolve on a post hidden pending review: the
// moderator's restore path (restoreDismissingFlagsLocked), with the flags
// marked as owed their row, a Restore requiring the auto-hide, and then the
// flag.dismissed row. The flags end dismissed, whichever was asked, since
// the restore path is the one way to lift an auto-hide without racing the
// sweep; the row's action says which it was.
func (s *Service) queueRestore(ctx context.Context, v domain.Viewer, a access, frame threadlog.FrameFlags,
	id threadid.ID, ordinal uint32, r Resolution) (dto.QueueResolution, error) {
	// The Restore carries the dismissal's millisecond, so that recovery can
	// tell this Restore from anybody else's (recoverFlagAudit).
	at := s.now().UnixMilli()
	run := func() (dto.CreatedPost, bool, error) {
		return s.changePostAppended(ctx, id, ordinal, a, func(th *threadcache.Thread, _ threadlog.IndexEntry) (threadlog.Append, error) {
			if th.Archived() {
				return threadlog.Append{}, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
			}
			return threadlog.Append{
				Type: threadlog.TypeRestore, Flags: frame, Author: uint64(v.UserID), Ordinal: ordinal,
				TimeMS:  at,
				Body:    &threadlog.RestoreBody{Reason: r.Note},
				Require: threadlog.EntryAutoHidden,
				Refuse:  threadlog.EntryMoved,
			}, nil
		})
	}
	_, ids, appended, err := s.restoreDismissingFlagsLocked(ctx, v, id, ordinal, r.Note, at,
		sql.NullString{String: r.Action, Valid: true}, run)
	if !appended {
		return dto.QueueResolution{}, err // the flags were reopened
	}
	out := dto.QueueResolution{Action: r.Action, Post: dto.QueuePostRestored, Flags: ids}
	if len(ids) > 0 {
		by := audit.By{Actor: v.UserID, IP: r.IP}
		if aerr := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			return flagAudit(ctx, q, s.now(), by, ActionFlagDismissed, id, ordinal, ids, r.Action, r.Note,
				dto.QueuePostRestored, false)
		}); aerr != nil {
			// The Restore happened, and the flags are marked as owed
			// their row: a retry or FlagAuditCatchUp writes it.
			s.logger().Warn("moderator queue: the restore happened, and its flag.dismissed row is owed",
				"thread", id.String(), "ordinal", ordinal, "flags", ids, "err", aerr)
		}
	}
	if err != nil {
		// Durable, but its read-back or projection failed. The flags stay
		// dismissed, and the row is written; the error is the handler's.
		return dto.QueueResolution{}, err
	}
	return out, nil
}

// flagAudit writes the row that records a queue action on flags ids of a
// post, and links the flags to it, clearing any audit_pending marker, in
// q's transaction.
func flagAudit(ctx context.Context, q *dbq.Queries, at time.Time, by audit.By, action string, id threadid.ID, ordinal uint32,
	ids []int64, asked, note, post string, recovered bool) error {
	details := map[string]any{"flags": ids, "action": asked, "note": note, "post": post}
	if recovered {
		details["recovered"] = true
	}
	auditID, err := audit.Append(ctx, q, at, by, action, audit.Post(id, ordinal), details)
	if err != nil {
		return err
	}
	_, err = q.SetFlagsAudit(ctx, dbq.SetFlagsAuditParams{AuditID: sql.NullInt64{Int64: auditID, Valid: true}, Ids: ids})
	return err
}

// recoverFlagAudit settles the post's flags that are owed their row, for a
// caller holding the post's flag lock, and answers the ids whose row it
// wrote, by the moderator the row names. Only a flag with audit_pending set
// is owed one (Terva review 1371); a null audit_id alone means nothing,
// since every flag closed any other way, or before migration 0015, has one.
//
// A pending flag was dismissed by the queue's restore path before its
// Restore append, which carries the dismissal's moderator as its actor and
// the dismissal's millisecond as its time. If that Restore is in the post's
// log, it landed, and the row is written as the moderator would have
// written it: their id, the dismissal's time, the note, and "recovered":
// true. If it is not, the append failed and so did the reopen after it, so
// the flags are reopened now and no row is written, because nothing
// happened. The post being shown proves nothing, since another moderator's
// restore shows it too, and neither does a post.restored row, which the
// projection does not write for a moderator restoring their own post (Terva
// review 1522 on PR #184).
func (s *Service) recoverFlagAudit(ctx context.Context, id threadid.ID, ordinal uint32) (map[int64][]int64, error) {
	var owed []dbq.FlagsOwedAuditRow
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		owed, err = q.FlagsOwedAudit(ctx, dbq.FlagsOwedAuditParams{ThreadID: int64(id), Ordinal: int64(ordinal)})
		return err
	}); err != nil || len(owed) == 0 {
		return nil, err
	}
	// One restore dismissed its flags in one statement, so they share the
	// moderator, the time, the note and the action. Group them, in case a
	// second restore's row failed too.
	type key struct {
		by, at       int64
		note, action string
	}
	var order []key
	groups := map[key][]int64{}
	for _, f := range owed {
		k := key{f.ResolvedBy, f.ResolvedAt, f.ResolutionNote, f.OwedAction}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f.ID)
	}
	written := map[int64][]int64{}
	for _, k := range order {
		ids := groups[k]
		var landed bool
		if err := s.Threads.With(id, func(th *threadcache.Thread) error {
			var err error
			landed, err = th.Reader.Restored(threadlog.FileHeaderSize, th.Actor.LogLength(), ordinal, uint64(k.by), k.at)
			return err
		}); err != nil {
			return written, err
		}
		if !landed {
			s.logger().Warn("moderator queue: reopening flags a failed restore left dismissed",
				"thread", id.String(), "ordinal", ordinal, "flags", ids)
			if err := s.reopenFlags(ctx, ids); err != nil {
				return written, err
			}
			continue
		}
		if err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			return flagAudit(ctx, q, time.UnixMilli(k.at), audit.By{Actor: k.by}, ActionFlagDismissed, id, ordinal, ids,
				k.action, k.note, dto.QueuePostRestored, true)
		}); err != nil {
			return written, err
		}
		written[k.by] = append(written[k.by], ids...)
	}
	return written, nil
}

// FlagAuditCatchUp settles every flag a queue restore left owed its row since
// before FlagAuditGrace ago, under each post's flag lock, as a retry of the
// request would (recoverFlagAudit). It is jobs.FlagAuditCatchUp's work, so
// nothing depends on a moderator retrying. It reports how many flags it
// wrote a row for. One post's failure does not stop the others; every
// failure is returned.
func (s *Service) FlagAuditCatchUp(ctx context.Context) (int, error) {
	var posts []dbq.PostsOwedFlagAuditRow
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		posts, err = q.PostsOwedFlagAudit(ctx, sql.NullInt64{Int64: s.now().Add(-FlagAuditGrace).UnixMilli(), Valid: true})
		return err
	}); err != nil {
		return 0, err
	}
	var (
		n    int
		errs []error
	)
	for _, p := range posts {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		id, ordinal := threadid.ID(p.ThreadID), uint32(p.Ordinal)
		got, err := s.catchUpPost(ctx, id, ordinal)
		n += got
		if err != nil {
			errs = append(errs, fmt.Errorf("post %d of %s: %w", ordinal, id, err))
		}
	}
	return n, errors.Join(errs...)
}

// catchUpPost is FlagAuditCatchUp for one post, under its lock.
func (s *Service) catchUpPost(ctx context.Context, id threadid.ID, ordinal uint32) (int, error) {
	unlock := s.flagLocks.lock(id, ordinal)
	defer unlock()
	written, err := s.recoverFlagAudit(ctx, id, ordinal)
	n := 0
	for _, ids := range written {
		n += len(ids)
	}
	return n, err
}
