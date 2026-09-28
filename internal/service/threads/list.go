package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// ListPageSize is the threads on a page of a forum's list, the posts page's
// constant (settled in grooming for TKT-01M29CA9AG). Pinned threads head the
// first page and are not counted in it.
const ListPageSize = DefaultPageSize

// ListQuery is a thread list request's parameters (docs/07): the cursor from
// the previous page's next_cursor, and the sort.
type ListQuery struct {
	Cursor string
	Sort   string
}

// List is GET /forums/{slug}/threads (docs/06 "Thread list views", docs/07).
//
// The first page opens with the pins: global pins from every forum v may
// read, then this forum's own (settled in grooming: global pins sort first in
// every forum). A pin whose pinned_until has passed is no pin, and its thread
// sorts with the rest. The rest is keyset-paged by last_post_at, or by
// created_at for ?sort=created, newest first. The pins are fixed when the
// first page is served: every page leaves out exactly the forum's threads
// that page showed as pins, which its cursor carries, so a pin that ends
// between pages is not shown twice. At most views.MaxPinned that v may see
// head the page; any past that sort with the rest.
//
// Everything comes from projections (internal/store/db/views): no thread log
// is opened, and the service needs no thread cache to answer.
func (s *Service) List(ctx context.Context, v domain.Viewer, slug string, q ListQuery) (dto.ThreadList, error) {
	sort, err := parseSort(q.Sort)
	if err != nil {
		return dto.ThreadList{}, err
	}
	// The forum first, so a forum hidden from v is not-found whatever the
	// cursor says, and the cursor is checked against the forum it names.
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return dto.ThreadList{}, err
	}
	cur, err := decodeCursor(sort, forum.ID, q.Cursor)
	if err != nil {
		return dto.ThreadList{}, err
	}
	var after *views.Key
	var shown []int64 // this forum's threads the first page showed as pins
	if cur != nil {
		after, shown = &cur.Key, cur.Pinned
	}
	now := s.now().UnixMilli()

	// The pins first, then the rows that leave them out, in two reads: the
	// pins are cut to what v may see and to views.MaxPinned between them, in
	// that order, so a hidden forum's pins never take a visible pin's place.
	// Leaving out exactly the pins shown keeps the two reads from disagreeing
	// if a pin changes between them.
	var pinned, rows []views.ThreadRow
	if after == nil {
		if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
			var err error
			pinned, err = views.PinnedThreads(ctx, tx, forum.ID, v.UserID, now)
			return err
		}); err != nil {
			return dto.ThreadList{}, err
		}
		if pinned, err = s.readablePins(ctx, v, forum.ID, pinned); err != nil {
			return dto.ThreadList{}, err
		}
		pinned = pinned[:min(len(pinned), views.MaxPinned)]
		for _, p := range pinned {
			if p.ForumID == forum.ID {
				shown = append(shown, p.ID)
			}
		}
	}
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = views.ThreadList(ctx, tx, views.ListParams{ForumID: forum.ID, UserID: v.UserID, Sort: sort,
			After: after, Pinned: shown, Limit: ListPageSize + 1})
		return err
	}); err != nil {
		return dto.ThreadList{}, err
	}
	next := ""
	if len(rows) > ListPageSize {
		rows = rows[:ListPageSize]
		next = cursor{Sort: sort, Forum: forum.ID, Key: rows[len(rows)-1].Key(sort), Pinned: shown}.encode()
	}

	all := append(pinned, rows...)
	tags, authors, err := s.listDetails(ctx, all)
	if err != nil {
		return dto.ThreadList{}, err
	}
	out := dto.ThreadList{Forum: &dto.ForumRef{Slug: forum.Slug, Name: forum.Name}, Threads: []dto.ThreadRow{},
		NextCursor: next}
	for i, r := range all {
		row := dto.ThreadRow{
			ID:             threadid.ID(r.ID).String(),
			Slug:           r.Slug,
			Title:          r.Title,
			Tags:           append([]string{}, tags[r.ID]...),
			Author:         authors[r.AuthorID],
			ReplyCount:     int(max(r.PostCount-1, 0)),
			LastPostAuthor: authors[r.LastPostAuthorID],
			LastPostAt:     dto.Millis(r.LastPostAt),
			Locked:         r.Locked,
		}
		// Only the pins that head the page are shown as pinned: a row
		// further down is there because its pin has expired.
		if i < len(pinned) {
			row.Pinned = &dto.Pinned{Scope: r.PinnedScope.String}
			if r.PinnedUntil.Valid {
				until := dto.Millis(r.PinnedUntil.Int64)
				row.Pinned.Until = &until
			}
		}
		// An anonymous viewer has no read state (docs/06), whatever the
		// query made of user 0.
		if v.Member() {
			row.Unread = int(r.Unread)
			row.LastReadOrdinal = int(r.LastReadOrdinal)
		}
		out.Threads = append(out.Threads, row)
	}
	return out, nil
}

// readablePins keeps the pins v may see: this forum's, which the caller has
// already let v read, and global ones from other forums v may read, decided
// in one batch.
func (s *Service) readablePins(ctx context.Context, v domain.Viewer, forumID int64, pins []views.ThreadRow) ([]views.ThreadRow, error) {
	var foreign []int64
	for _, p := range pins {
		if p.ForumID != forumID {
			foreign = append(foreign, p.ForumID)
		}
	}
	if len(foreign) == 0 {
		return pins, nil
	}
	readable, err := s.Perm.CanEach(ctx, v, foreign, domain.PermRead)
	if err != nil {
		return nil, err
	}
	kept := pins[:0:0]
	for _, p := range pins {
		if p.ForumID == forumID || readable[p.ForumID] {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

// listDetails loads a page's tags and the authors and last posters, one
// statement each. An author the database does not have is drawn as a
// deleted member, as on the thread page.
func (s *Service) listDetails(ctx context.Context, rows []views.ThreadRow) (map[int64][]string, map[int64]dto.Author, error) {
	tags := map[int64][]string{}
	authors := map[int64]dto.Author{}
	if len(rows) == 0 {
		return tags, authors, nil
	}
	ids := make([]int64, 0, len(rows))
	people := make([]int64, 0, 2*len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		people = append(people, r.AuthorID, r.LastPostAuthorID)
	}
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		ts, err := q.TagsForThreads(ctx, ids)
		if err != nil {
			return err
		}
		for _, t := range ts {
			tags[t.ThreadID] = append(tags[t.ThreadID], t.Tag)
		}
		us, err := q.UsersByIDs(ctx, people)
		for _, u := range us {
			authors[u.ID] = dto.Author{Handle: u.Handle, DisplayName: u.DisplayName,
				Avatar: u.AvatarBlob.String, Role: u.Role, Kind: u.Kind}
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	member.FillMissing(authors, people...)
	return tags, authors, nil
}

// readForum finds the forum slug names and admits v to read it, as access
// admits v to a thread: unknown or hidden from v is not-found, as if it did
// not exist; readable-when-signed-in is unauthenticated to an anonymous
// viewer and forbidden to a member without read.
func (s *Service) readForum(ctx context.Context, v domain.Viewer, slug string) (dbq.Forum, error) {
	var forum dbq.Forum
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		forum, err = q.GetForumBySlug(ctx, slug)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return forum, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: no forum %q", slug))
	}
	if err != nil {
		return forum, err
	}
	switch d, err := s.Perm.Check(ctx, v, forum.ID, domain.PermRead); {
	case err != nil:
		return forum, err
	case d == perm.Hidden:
		return forum, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: forum %q is hidden from this viewer", slug))
	case d == perm.Deny && !v.Member():
		return forum, problem.Coded(problem.CodeUnauthenticated, fmt.Errorf("threads: forum %q needs a signed-in reader", slug))
	case d == perm.Deny:
		return forum, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: forum %q is not readable by this viewer", slug))
	}
	return forum, nil
}
