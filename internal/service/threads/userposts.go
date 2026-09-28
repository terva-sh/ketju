package threads

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"

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

// UserPostsPageSize is the posts on one page of a user's history, the
// thread list's 25 (docs/06).
const UserPostsPageSize = DefaultPageSize

// UserPosts is GET /users/{handle}/posts?cursor= (docs/07): the account's
// posts across forums, newest first, each rendered as the thread page
// renders it and with a link to its thread.
//
// Who is served which account is member.Account's rule. A post in a forum v
// cannot read is left out, and so is a moved post, which renders in its
// thread as a pointer. A deleted post is left out for a member; the author
// sees their own marked deleted, as the thread page shows it to them, with
// no body; a moderator or admin sees it marked deleted with its body, as
// the thread page's moderator view does. A post a moderator hid is a
// deleted post here, as it is on the thread page (docs/08: hiding is a
// Delete variant).
//
// Paging is a keyset on (created_at, thread, ordinal) descending, which
// post_meta's author index serves in order, so a post written while a
// reader pages lands above the cursor and nothing is seen twice.
//
// post_meta says which posts there are, and may trail the log; each post's
// index entry says what it is now, so a post deleted since post_meta last
// heard is still kept from a member. A quarantined thread's posts are left
// out: the thread endpoints refuse that thread until an operator repairs
// it, and one broken thread should not refuse a whole history.
func (s *Service) UserPosts(ctx context.Context, v domain.Viewer, handle, cursorText string) (dto.UserPosts, error) {
	out := dto.UserPosts{Posts: []dto.UserPost{}}
	u, err := member.Account(ctx, s.DB, v, handle)
	if err != nil {
		return out, err
	}
	after, err := decodePostCursor(cursorText)
	if err != nil {
		return out, err
	}
	forums, err := s.Perm.ReadableForums(ctx, v)
	if err != nil {
		return out, err
	}
	staff, own := v.Staff(), v.UserID == u.ID
	size := s.UserPostsPage
	if size <= 0 {
		size = UserPostsPageSize
	}

	var rows []views.UserPostRow
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = views.UserPosts(ctx, tx, views.UserPostsParams{AuthorID: u.ID, Forums: forums,
			WithDeleted: staff || own, After: after, Limit: size + 1})
		return err
	}); err != nil {
		return out, err
	}
	if len(rows) > size {
		rows = rows[:size]
		out.NextCursor = encodePostCursor(rows[len(rows)-1].Key())
	}

	// Each thread is opened once, for all of its posts on the page.
	byThread := map[int64][]uint32{}
	var order []int64
	for _, r := range rows {
		if _, ok := byThread[r.ThreadID]; !ok {
			order = append(order, r.ThreadID)
		}
		byThread[r.ThreadID] = append(byThread[r.ThreadID], uint32(r.Ordinal))
	}
	authors := map[int64]dto.Author{u.ID: member.Author(u)}
	type at struct {
		thread  int64
		ordinal int
	}
	rendered := map[at]dto.Post{}
	for _, tid := range order {
		id := threadid.ID(tid)
		if s.Quarantined != nil && s.Quarantined(id) {
			continue
		}
		ordinals := byThread[tid]
		slices.Sort(ordinals)
		err := s.Threads.With(id, func(th *threadcache.Thread) error {
			snap, err := s.stable(th, func(*threadlog.Index, int64) ([]uint32, error) { return ordinals, nil })
			if err != nil {
				return err
			}
			snap = snap.keep(func(e threadlog.IndexEntry) bool { return historyShows(e.Flags, staff, own) })
			posts, err := s.posts(ctx, th, id, snap, authors, Query{}, staff)
			if err != nil {
				return err
			}
			for _, p := range posts {
				rendered[at{tid, p.Ordinal}] = p
			}
			return nil
		})
		if err != nil {
			return out, err
		}
	}

	for _, r := range rows {
		p, ok := rendered[at{r.ThreadID, int(r.Ordinal)}]
		if !ok {
			continue
		}
		out.Posts = append(out.Posts, dto.UserPost{
			Thread: dto.ThreadLink{ID: threadid.ID(r.ThreadID).String(), Slug: r.ThreadSlug, Title: r.ThreadTitle},
			Forum:  dto.ForumRef{Slug: r.ForumSlug, Name: r.ForumName},
			Post:   p,
		})
	}
	return out, nil
}

// historyShows says whether a post whose entry has flags is on the history
// for a viewer who is staff, or is its author (own). A visible post is on
// it for everyone. A moved one is on it for no one: it is a pointer, and the
// post it became is in its new thread. A deleted or hidden one is on it for
// staff and its author, who see it as their thread page shows it to them.
func historyShows(f threadlog.EntryFlags, staff, own bool) bool {
	switch {
	case f.Visible():
		return true
	case f.Has(threadlog.EntryMoved):
		return false
	default:
		return staff || own
	}
}

// keep is the snapshot with only the posts whose entry ok accepts, in the
// same order, with the same length and reply counts.
func (snap snapshot) keep(ok func(threadlog.IndexEntry) bool) snapshot {
	out := snap
	out.ordinals, out.entries = nil, nil
	for i, e := range snap.entries {
		if ok(e) {
			out.ordinals = append(out.ordinals, snap.ordinals[i])
			out.entries = append(out.entries, e)
		}
	}
	return out
}

// A history cursor is opaque to clients (docs/07): base64url, without
// padding, of "at|thread|ordinal", the last post the page showed. Like the
// thread list's, it is a position and not a credential: one written by hand
// gets the page after that position, which its writer could read anyway. It
// does not name the account, since no response exposes a user id, so a
// cursor carried to another account's history is only a position there.
func encodePostCursor(k views.PostKey) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(k.At, 10) + "|" +
		strconv.FormatInt(k.Thread, 10) + "|" + strconv.FormatInt(k.Ordinal, 10)))
}

var errBadPostCursor = problem.Coded(problem.CodeBadRequest, errors.New("threads: the history cursor is malformed"))

// decodePostCursor reads a history cursor. Empty is the first page; anything
// that is not exactly what encodePostCursor writes is bad-request.
func decodePostCursor(text string) (*views.PostKey, error) {
	if text == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, errBadPostCursor
	}
	parts := strings.Split(string(b), "|")
	if len(parts) != 3 {
		return nil, errBadPostCursor
	}
	at, err1 := strconv.ParseInt(parts[0], 10, 64)
	th, err2 := strconv.ParseInt(parts[1], 10, 64)
	o, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || th <= 0 || o <= 0 {
		return nil, errBadPostCursor
	}
	k := views.PostKey{At: at, Thread: th, Ordinal: o}
	// Only the canonical spelling: that refuses "+5" and "05" for 5.
	if encodePostCursor(k) != text {
		return nil, errBadPostCursor
	}
	return &k, nil
}
