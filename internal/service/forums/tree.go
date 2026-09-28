// Package forums answers questions about the forum tree.
package forums

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Service is the forums service.
type Service struct {
	DB     *db.DB
	Perm   *perm.Evaluator
	Logger *slog.Logger
}

// Tree is GET /forums (docs/07): the forums v may read, roots in position
// order with their children under them, each with its thread and post
// counts, v's unread count and its latest activity.
//
// A forum v cannot read is absent, not flagged: in a small community a list
// of forum names is itself information (TKT-01M29CA9A5). Its children go
// with it, even ones v could read, because a child listed at the root would
// say where it had been (settled in grooming). An anonymous viewer is
// evaluated as everyone, so public forums are all they see.
//
// The statements are fixed, however many forums there are: the forum rows,
// one perm batch (none when perm's cache already knows), then in one read
// the thread and post counts, v's unread counts, the last posts and their
// authors.
func (s *Service) Tree(ctx context.Context, v domain.Viewer) (dto.ForumTree, error) {
	var rows []dbq.Forum
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		rows, err = q.ListForums(ctx)
		return err
	}); err != nil {
		return dto.ForumTree{}, err
	}
	ids := make([]int64, len(rows))
	for i, f := range rows {
		ids[i] = f.ID
	}
	readable := map[int64]bool{}
	if len(ids) > 0 {
		var err error
		if readable, err = s.Perm.CanEach(ctx, v, ids, domain.PermRead); err != nil {
			return dto.ForumTree{}, err
		}
	}
	listed := s.listed(rows, readable)
	if len(listed) == 0 {
		return dto.ForumTree{Forums: []dto.Forum{}}, nil
	}

	var (
		counts []dbq.ForumThreadCount
		unread []dbq.ForumUnreadRow
		last   []dbq.ForumLastPostsRow
		users  []dbq.User
	)
	if err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		if counts, err = q.ForumThreadCounts(ctx); err != nil {
			return err
		}
		// Anonymous viewers have no read state, and nothing is unread to
		// them: an unread badge is a signed-in feature (docs/06).
		if v.Member() {
			if unread, err = q.ForumUnread(ctx, v.UserID); err != nil {
				return err
			}
		}
		ids := make([]int64, 0, len(listed))
		for _, f := range listed {
			ids = append(ids, f.row.ID)
			for _, c := range f.children {
				ids = append(ids, c.ID)
			}
		}
		if last, err = q.ForumLastPosts(ctx, ids); err != nil {
			return err
		}
		authors := make([]int64, 0, len(last))
		for _, l := range last {
			authors = append(authors, l.LastPostAuthorID)
		}
		if len(authors) > 0 {
			users, err = q.UsersByIDs(ctx, authors)
		}
		return err
	}); err != nil {
		return dto.ForumTree{}, err
	}

	threads, posts := map[int64]int{}, map[int64]int{}
	for _, c := range counts {
		threads[c.ForumID] = int(c.Threads)
		posts[c.ForumID] = int(c.LivePosts)
	}
	unreadBy := map[int64]int{}
	for _, u := range unread {
		unreadBy[u.ForumID] = int(u.Threads)
	}
	// An author the database does not have (a projection rebuilt against an
	// older users table, docs/04) is drawn as a deleted member, as on the
	// thread page.
	authors := map[int64]dto.Author{}
	for _, u := range users {
		authors[u.ID] = dto.Author{Handle: u.Handle, DisplayName: u.DisplayName,
			Avatar: u.AvatarBlob.String, Role: u.Role, Kind: u.Kind}
	}
	for _, l := range last {
		member.FillMissing(authors, l.LastPostAuthorID)
	}
	lastBy := map[int64]*dto.ForumLastPost{}
	for _, l := range last {
		lastBy[l.ForumID] = &dto.ForumLastPost{
			Thread: dto.ThreadLink{ID: threadid.ID(l.ThreadID).String(), Slug: l.Slug, Title: l.Title},
			Author: authors[l.LastPostAuthorID],
			At:     dto.Millis(l.LastPostAt),
		}
	}

	node := func(f dbq.Forum) dto.Forum {
		return dto.Forum{
			Slug:        f.Slug,
			Name:        f.Name,
			Description: f.Description,
			Visibility:  f.Visibility,
			Archived:    f.Archived != 0,
			ThreadCount: threads[f.ID],
			PostCount:   posts[f.ID],
			Unread:      unreadBy[f.ID],
			LastPost:    lastBy[f.ID],
			Children:    []dto.Forum{},
		}
	}
	out := dto.ForumTree{Forums: []dto.Forum{}}
	for _, f := range listed {
		n := node(f.row)
		for _, c := range f.children {
			n.Children = append(n.Children, node(c))
		}
		out.Forums = append(out.Forums, n)
	}
	return out, nil
}

// root is a listed root forum and its listed children, in order.
type root struct {
	row      dbq.Forum
	children []dbq.Forum
}

// listed builds the tree from ListForums's rows, which the query orders by
// position (roots, and each root's children), and keeps what readable
// allows: a root v may read, and under it the children v may read. It takes
// the roots first and the children second, so it keeps the rows' order
// within each level without relying on a root preceding its children.
//
// Forums nest one level (docs/04), which migration 0003's triggers enforce.
// A row whose parent is itself a child, or is not in the table, would be a
// broken database; it is logged and left out rather than failing the index
// or being lifted somewhere it does not belong.
func (s *Service) listed(rows []dbq.Forum, readable map[int64]bool) []root {
	isRoot := map[int64]bool{}
	at := map[int64]int{}
	var out []root
	for _, f := range rows {
		if f.ParentID.Valid {
			continue
		}
		isRoot[f.ID] = true
		if readable[f.ID] {
			at[f.ID] = len(out)
			out = append(out, root{row: f})
		}
	}
	for _, f := range rows {
		if !f.ParentID.Valid {
			continue
		}
		if !isRoot[f.ParentID.Int64] {
			s.log().Error("forums: a forum's parent is not a root forum; leaving it out of the tree",
				"forum", f.ID, "parent", f.ParentID.Int64)
			continue
		}
		i, ok := at[f.ParentID.Int64]
		if !ok || !readable[f.ID] {
			continue
		}
		out[i].children = append(out[i].children, f)
	}
	return out
}

func (s *Service) log() *slog.Logger {
	if s.Logger == nil {
		return slog.Default()
	}
	return s.Logger
}
