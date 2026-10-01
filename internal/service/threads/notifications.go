package threads

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"math"
	"slices"
	"strconv"

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

// NotificationsPageSize is the rows on one page of the notification list,
// the thread list's 25 (docs/06).
const NotificationsPageSize = DefaultPageSize

// MaxMarkNotifications bounds the ids one POST /notifications/read names.
// A client marks what it shows, a page or two; up_to marks the rest.
const MaxMarkNotifications = 200

// Notifications is GET /notifications?cursor= (docs/07, TKT-01M29CRJ2T): a
// page of v's notifications, the newest written first.
//
// A post notification is served only while its post is visible to every
// reader, v may read its forum, and its thread is not muted for v, all
// decided now, moderators included (docs/04, run decision 2026-10-01). The
// query offers only rows whose post_meta row is live and whose thread is
// not muted, and VisibleNotifications decides the rest, as it does for the
// /me badge. A new_application is offered only while v is an active
// moderator or admin, and is served as read once its application is no
// longer pending, which is when the badge stops counting it. Rows are never
// deleted for any of this, so a restore or an unmute brings one back. No
// notification carries post text.
//
// Paging is a keyset on id descending. Rows the decision drops can leave a
// batch short, so batches are read until the page holds a page of rows and
// one more, or the list ends; a page is short only at the end, and a list
// that ends has no cursor.
func (s *Service) Notifications(ctx context.Context, v domain.Viewer, cursor string) (dto.NotificationList, error) {
	out := dto.NotificationList{Notifications: []dto.Notification{}}
	if err := member.Require(v, "read notifications"); err != nil {
		return out, err
	}
	before, err := decodeNotificationCursor(cursor)
	if err != nil {
		return out, err
	}
	size := s.NotificationsPage
	if size <= 0 {
		size = NotificationsPageSize
	}
	var shown []dto.Notification
	for len(shown) <= size {
		var rows []dbq.ListNotificationsRow
		var authors map[int64]dto.Author
		if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
			var err error
			rows, err = q.ListNotifications(ctx, dbq.ListNotificationsParams{UserID: v.UserID, Before: before,
				Lim: int64(size + 1)})
			if err != nil {
				return err
			}
			var actors []int64
			for _, r := range rows {
				if r.ActorID.Valid && !slices.Contains(actors, r.ActorID.Int64) {
					actors = append(actors, r.ActorID.Int64)
				}
			}
			authors, err = member.AuthorsByID(ctx, q, actors)
			return err
		}); err != nil {
			return out, err
		}
		keep, err := s.VisibleNotifications(ctx, v, notifiedOf(rows))
		if err != nil {
			return out, err
		}
		visible := map[views.PostKey]bool{}
		for _, t := range keep {
			for _, o := range t.Ordinals {
				visible[views.PostKey{Thread: t.Thread, Ordinal: o}] = true
			}
		}
		for _, r := range rows {
			if len(shown) > size {
				break
			}
			n := dto.Notification{ID: r.ID, Kind: r.Kind, At: dto.Millis(r.At), Read: r.ReadAt.Valid}
			if r.ThreadID.Valid {
				if !visible[views.PostKey{Thread: r.ThreadID.Int64, Ordinal: r.Ordinal.Int64}] {
					continue
				}
				o := uint32(r.Ordinal.Int64)
				n.Thread = &dto.ThreadLink{ID: threadid.ID(r.ThreadID.Int64).String(), Slug: r.ThreadSlug.String,
					Title: r.ThreadTitle.String}
				n.Forum = &dto.ForumRef{Slug: r.ForumSlug.String, Name: r.ForumName.String}
				n.Ordinal = &o
			}
			if r.Kind == domain.NotifyNewApplication && r.ApplicationStatus.String != "pending" {
				n.Read = true // decided: the badge no longer counts it, so neither does the list
			}
			if r.ActorID.Valid {
				a := authors[r.ActorID.Int64]
				n.Actor = &a
			}
			shown = append(shown, n)
		}
		if len(rows) < size+1 {
			break // the list has no more
		}
		before = rows[len(rows)-1].ID
	}
	if len(shown) > size {
		shown = shown[:size]
		out.NextCursor = encodeNotificationCursor(shown[size-1].ID)
	}
	out.Notifications = append(out.Notifications, shown...)
	return out, nil
}

// notifiedOf groups a page's post rows by thread, as MeCounts groups the
// badge's, for VisibleNotifications.
func notifiedOf(rows []dbq.ListNotificationsRow) []views.NotifiedThread {
	var out []views.NotifiedThread
	at := map[int64]int{}
	for _, r := range rows {
		if !r.ThreadID.Valid || !r.ForumID.Valid {
			continue
		}
		i, ok := at[r.ThreadID.Int64]
		if !ok {
			i = len(out)
			at[r.ThreadID.Int64] = i
			out = append(out, views.NotifiedThread{Thread: r.ThreadID.Int64, Forum: r.ForumID.Int64,
				Projected: r.LogLengthProjected.Int64})
		}
		out[i].Ordinals = append(out[i].Ordinals, r.Ordinal.Int64)
	}
	return out
}

// VisibleNotifications is the one decision of which post notifications v
// may be served now, for the notification list and the /me badge alike
// (docs/04, run decision 2026-10-01). It is given what the database already
// passed: rows whose post_meta row is live and whose thread v has not muted.
// It keeps, of those, the ones in a forum v may read, in a thread that is
// not quarantined, and, in a thread whose projection trails its log, whose
// post the index says is visible: a Delete or a moderator's hide that has
// not reached post_meta yet hides the row from both surfaces at once. A
// trailing thread whose unprojected tail holds a ThreadMoved keeps none: its
// forum is known only from the projection, which names the forum it left,
// so its rows wait until the projection reaches the move and the forum it
// is now in is the one checked (Terva review 1781). The index is read only
// for a thread that trails, which is one just appended to and so already
// open in the cache.
func (s *Service) VisibleNotifications(ctx context.Context, v domain.Viewer, threads []views.NotifiedThread) ([]views.NotifiedThread, error) {
	if len(threads) == 0 || !v.Member() {
		return nil, nil
	}
	var forums []int64
	for _, t := range threads {
		if !slices.Contains(forums, t.Forum) {
			forums = append(forums, t.Forum)
		}
	}
	readable, err := s.Perm.CanEach(ctx, v, forums, domain.PermRead)
	if err != nil {
		return nil, err
	}
	var out []views.NotifiedThread
	for _, t := range threads {
		id := threadid.ID(t.Thread)
		if !readable[t.Forum] || (s.Quarantined != nil && s.Quarantined(id)) {
			continue
		}
		size, err := s.Threads.LogSize(id)
		if err != nil {
			return nil, err
		}
		if size == t.Projected {
			out = append(out, t)
			continue
		}
		ordinals := make([]uint32, 0, len(t.Ordinals))
		for _, o := range t.Ordinals {
			ordinals = append(ordinals, uint32(o))
		}
		slices.Sort(ordinals)
		ordinals = slices.Compact(ordinals)
		kept := t
		kept.Ordinals = nil
		err = s.Threads.With(id, func(th *threadcache.Thread) error {
			snap, err := s.stable(th, func(*threadlog.Index, int64) ([]uint32, error) { return ordinals, nil })
			if err != nil {
				return err
			}
			// t.Forum is the forum as the projection had it. A ThreadMoved
			// past that point means the thread may now be somewhere v cannot
			// read, so its rows wait for the projection (Terva review 1781).
			if t.Projected > snap.logLength {
				return nil
			}
			moved, err := th.Reader.Moved(t.Projected, snap.logLength)
			if err != nil || moved {
				return err
			}
			for i, e := range snap.entries {
				if e.Flags.Visible() {
					kept.Ordinals = append(kept.Ordinals, int64(snap.ordinals[i]))
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(kept.Ordinals) > 0 {
			out = append(out, kept)
		}
	}
	return out, nil
}

// CountVisibleNotifications is how many of threads' notifications
// VisibleNotifications keeps: the post part of /me's badge, which
// users.Service reaches through its VisibleNotifications hook.
func (s *Service) CountVisibleNotifications(ctx context.Context, v domain.Viewer, threads []views.NotifiedThread) (int, error) {
	keep, err := s.VisibleNotifications(ctx, v, threads)
	n := 0
	for _, t := range keep {
		n += len(t.Ordinals)
	}
	return n, err
}

// MarkNotificationsRead is POST /notifications/read (docs/07): it marks v's
// own unread notifications read, those among ids, or with upTo set every
// one at or below that id, which is how a client marks the list it was
// shown by its first row. Exactly one of the two is given. Another member's
// ids, and ids already read, are passed over without a word: they name
// nothing of v's to mark.
func (s *Service) MarkNotificationsRead(ctx context.Context, v domain.Viewer, ids []int64, upTo *int64) (dto.NotificationsRead, error) {
	if err := member.Require(v, "mark notifications read"); err != nil {
		return dto.NotificationsRead{}, err
	}
	switch {
	case (ids == nil) == (upTo == nil):
		return dto.NotificationsRead{}, problem.Coded(problem.CodeBadRequest,
			errors.New("threads: mark notifications read by ids or by up_to, exactly one"))
	case len(ids) > MaxMarkNotifications:
		return dto.NotificationsRead{}, problem.Coded(problem.CodeBadRequest,
			errors.New("threads: too many notification ids in one request"))
	case ids != nil && len(ids) == 0:
		return dto.NotificationsRead{Marked: 0}, nil
	}
	at := sql.NullInt64{Int64: s.now().UnixMilli(), Valid: true}
	var n int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if upTo != nil {
			n, err = q.MarkNotificationsReadUpTo(ctx, dbq.MarkNotificationsReadUpToParams{At: at, UserID: v.UserID,
				UpTo: *upTo})
		} else {
			n, err = q.MarkNotificationsRead(ctx, dbq.MarkNotificationsReadParams{At: at, UserID: v.UserID, Ids: ids})
		}
		return err
	})
	if err != nil {
		return dto.NotificationsRead{}, err
	}
	return dto.NotificationsRead{Marked: int(n)}, nil
}

// A notification cursor is opaque to clients (docs/07): base64url, without
// padding, of the last id the page showed. It is a position, not a
// credential: the list is the viewer's own whatever it says.
func encodeNotificationCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

var errBadNotificationCursor = problem.Coded(problem.CodeBadRequest,
	errors.New("threads: the notification cursor is malformed"))

// decodeNotificationCursor reads a cursor into the id the next page reads
// below. Empty is the first page; anything that is not exactly what
// encodeNotificationCursor writes is bad-request.
func decodeNotificationCursor(text string) (int64, error) {
	if text == "" {
		return math.MaxInt64, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return 0, errBadNotificationCursor
	}
	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || id <= 0 || encodeNotificationCursor(id) != text {
		return 0, errBadNotificationCursor
	}
	return id, nil
}
