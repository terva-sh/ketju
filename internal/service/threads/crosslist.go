package threads

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// CrossList is GET /unread, /new, /participated and /watching (docs/06
// "Thread list views", docs/07, TKT-01M29CTY7K): the threads from every
// forum v may read that are in list for v, 25 to a page, keyset-paged by
// cursor.
//
// Each list is a member's: anyone else is refused as member.Require
// refuses them. The forums are the ones v may read on this request
// (perm.ReadableForums), so a forum made restricted, a thread moved into a
// forum v cannot read, or a group grant removed drops rows on the next
// request; the rules for each list are views.CrossList's, the ones /me's
// badges count by. A quarantined thread is dropped, as every endpoint
// refuses it. Rows carry no post body, only what a forum's list shows, with
// the thread's forum named.
//
// The answer is per viewer through and through, and goes in no cache.
func (s *Service) CrossList(ctx context.Context, v domain.Viewer, list views.List, cursorText string) (dto.ThreadList, error) {
	if err := member.Require(v, "read the "+list.String()+" list"); err != nil {
		return dto.ThreadList{}, err
	}
	after, err := decodeCrossCursor(list, cursorText)
	if err != nil {
		return dto.ThreadList{}, err
	}
	forums, err := s.Perm.ReadableForums(ctx, v)
	if err != nil {
		return dto.ThreadList{}, err
	}
	var rows []views.ThreadRow
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = views.CrossList(ctx, tx, views.CrossParams{List: list, UserID: v.UserID, Forums: forums,
			After: after, Limit: ListPageSize + 1})
		return err
	}); err != nil {
		return dto.ThreadList{}, err
	}
	next := ""
	if len(rows) > ListPageSize {
		rows = rows[:ListPageSize]
		next = crossCursor{List: list, Key: list.Key(rows[len(rows)-1])}.encode()
	}
	// After the cursor is taken, so a page that drops one still continues
	// from the last row it read.
	kept := rows[:0:0]
	for _, r := range rows {
		if s.Quarantined != nil && s.Quarantined(threadid.ID(r.ID)) {
			continue
		}
		kept = append(kept, r)
	}

	tags, authors, err := s.listDetails(ctx, kept)
	if err != nil {
		return dto.ThreadList{}, err
	}
	names, err := s.forumRefs(ctx, kept)
	if err != nil {
		return dto.ThreadList{}, err
	}
	out := dto.ThreadList{Threads: []dto.ThreadRow{}, NextCursor: next}
	for _, r := range kept {
		forum := names[r.ForumID]
		out.Threads = append(out.Threads, dto.ThreadRow{
			ID:             threadid.ID(r.ID).String(),
			Slug:           r.Slug,
			Title:          r.Title,
			Tags:           append([]string{}, tags[r.ID]...),
			Author:         authors[r.AuthorID],
			ReplyCount:     int(max(r.PostCount-1, 0)),
			LastPostAuthor: authors[r.LastPostAuthorID],
			LastPostAt:     dto.Millis(r.LastPostAt),
			Locked:         r.Locked,
			Forum:          &forum,
			// No list holds a muted thread, so every row's badge is its
			// count.
			Unread:          int(r.Unread),
			LastReadOrdinal: int(r.LastReadOrdinal),
		})
	}
	return out, nil
}

// forumRefs names the forums of rows, in one statement.
func (s *Service) forumRefs(ctx context.Context, rows []views.ThreadRow) (map[int64]dto.ForumRef, error) {
	out := map[int64]dto.ForumRef{}
	if len(rows) == 0 {
		return out, nil
	}
	var ids []int64
	for _, r := range rows {
		ids = append(ids, r.ForumID)
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		fs, err := q.ForumsByIDs(ctx, ids)
		for _, f := range fs {
			out[f.ID] = dto.ForumRef{Slug: f.Slug, Name: f.Name}
		}
		return err
	})
	return out, err
}

// MarkAllRead is PUT /me/read, the site-level "mark all read" (docs/06
// "Read tracking", TKT-01M29CTY7K): users.mark_all_read_at moves to now,
// never back, so every post written up to now is read. No read position
// moves. The answer is the mark as it now stands.
func (s *Service) MarkAllRead(ctx context.Context, v domain.Viewer) (dto.MarkedRead, error) {
	if err := s.MayKeepState(v); err != nil {
		return dto.MarkedRead{}, err
	}
	var at int64
	err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		at, err = q.SetMarkAllRead(ctx, dbq.SetMarkAllReadParams{Now: s.now().UnixMilli(), UserID: v.UserID})
		return err
	})
	if err != nil {
		return dto.MarkedRead{}, err
	}
	return dto.MarkedRead{At: dto.Millis(at)}, nil
}

// MarkForumRead is PUT /forums/{slug}/read, the forum-level "mark all read"
// (docs/06 "Read tracking", TKT-01M29CTY7K): v's position in each of the
// forum's threads moves to its last post, forward only, in one statement.
// users.mark_all_read_at does not move, so other forums stay as they were.
// The forum admits v as its thread list does: a forum hidden from v is
// not-found, and one v may not read is forbidden. The answer is the time
// the forum was marked.
func (s *Service) MarkForumRead(ctx context.Context, v domain.Viewer, slug string) (dto.MarkedRead, error) {
	if err := s.MayKeepState(v); err != nil {
		return dto.MarkedRead{}, err
	}
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return dto.MarkedRead{}, err
	}
	now := s.now().UnixMilli()
	if err := s.DB.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.MarkForumRead(ctx, dbq.MarkForumReadParams{UserID: v.UserID, Now: now, ForumID: forum.ID})
		return err
	}); err != nil {
		return dto.MarkedRead{}, err
	}
	return dto.MarkedRead{At: dto.Millis(now)}, nil
}

// crossCursor is where a cross-forum list's next page starts: after Key, in
// List's order.
type crossCursor struct {
	List views.List
	Key  views.Key
}

// A cross cursor is opaque to clients, as a forum list's is: base64url,
// without padding, of "list|at|id", where list is the list's name. Like
// that one, it is a position, not a credential.
func (c crossCursor) encode() string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join([]string{c.List.String(),
		strconv.FormatInt(c.Key.At, 10), strconv.FormatInt(c.Key.ID, 10)}, "|")))
}

var errBadCrossCursor = problem.Coded(problem.CodeBadRequest,
	errors.New("threads: the cursor is malformed, or was made for another list"))

// decodeCrossCursor reads a cursor for list. Empty is the first page.
// Anything that is not exactly what encode writes for list is bad-request:
// a cursor from another list, or from a forum's list, whose key would mean
// something else, above all.
func decodeCrossCursor(list views.List, text string) (*views.Key, error) {
	if text == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, errBadCrossCursor
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 3 || parts[0] != list.String() {
		return nil, errBadCrossCursor
	}
	at, err1 := strconv.ParseInt(parts[1], 10, 64)
	id, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 {
		return nil, errBadCrossCursor
	}
	c := crossCursor{List: list, Key: views.Key{At: at, ID: id}}
	if c.encode() != text {
		return nil, errBadCrossCursor
	}
	return &c.Key, nil
}
