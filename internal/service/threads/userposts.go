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
	"github.com/terva-sh/ketju/internal/render"
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

// Profile is GET /users/{handle} (docs/07): the account's author block, its
// bio, when it joined, and its post count as v may see them. It is here
// rather than in the users service because the count is the history's, and
// the history is read from the thread logs.
//
// Who is served which account is member.Account's rule. The status is shown
// to a moderator or admin and to nobody else. An anonymized account shows
// its placeholder handle and no prior identity: no display name, avatar or
// bio (member.Author). Its joined date and post count stay, since neither
// names anyone.
func (s *Service) Profile(ctx context.Context, v domain.Viewer, handle string) (dto.Profile, error) {
	u, err := member.Account(ctx, s.DB, v, handle)
	if err != nil {
		return dto.Profile{}, err
	}
	forums, err := s.Perm.ReadableForums(ctx, v)
	if err != nil {
		return dto.Profile{}, err
	}
	n, err := s.userPostCount(ctx, u.ID, forums)
	if err != nil {
		return dto.Profile{}, err
	}

	out := dto.Profile{User: member.Author(u), JoinedAt: dto.Millis(u.CreatedAt), PostCount: n}
	if u.BioSource != "" && !domain.Anonymized(u.Handle) {
		// A bio is not in a thread, so a quote in one has no post to point
		// at: every quoted post is reported missing, and the renderer draws
		// the quote as a plain blockquote rather than a link to an ordinal
		// on whatever page the reader is on.
		r, err := render.RenderWith(u.BioSource, render.Context{Posts: noPosts})
		if err != nil {
			return dto.Profile{}, err
		}
		out.BioHTML = r.HTML
	}
	if v.Staff() {
		status := u.Status
		out.Status = &status
	}
	return out, nil
}

var noPosts = render.ResolverFunc(func(uint32) (bool, bool) { return false, false })

// userPostCount is the profile's post count: exactly the posts a member's
// history of userID lists, for a viewer who may read forums. The history
// takes its candidates from post_meta and asks each post's index entry
// whether it is deleted, so the count does the same: a thread whose
// projection has reached the whole log has post_meta's deleted flags right,
// and its count is post_meta's; a thread whose projection trails the log,
// where a Delete or Restore may not have reached post_meta yet, is counted
// from its index. Telling the two apart costs a stat per thread, and only a
// thread that trails is opened. A quarantined thread counts nothing, as the
// history shows nothing from it.
//
// A deleted post never counts, whoever is looking, even though the author,
// moderators and admins see deleted posts in the history (docs/07).
func (s *Service) userPostCount(ctx context.Context, userID int64, forums []int64) (int, error) {
	var threads []views.ThreadPosts
	if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		threads, err = views.UserThreadPosts(ctx, tx, userID, forums)
		return err
	}); err != nil {
		return 0, err
	}
	n := 0
	for _, t := range threads {
		id := threadid.ID(t.ThreadID)
		if s.Quarantined != nil && s.Quarantined(id) {
			continue
		}
		size, err := s.Threads.LogSize(id)
		if err != nil {
			return 0, err
		}
		if size == t.Projected {
			n += int(t.NotDeleted)
			continue
		}
		if s.trace != nil {
			s.trace(pathIndex)
		}
		err = s.Threads.With(id, func(th *threadcache.Thread) error {
			snap, err := s.stable(th, func(*threadlog.Index, int64) ([]uint32, error) { return t.Ordinals, nil })
			if err != nil {
				return err
			}
			n += len(snap.keep(func(e threadlog.IndexEntry) bool { return historyShows(e.Flags, false, false) }).entries)
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	return n, nil
}

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
// post_meta says which posts there are, and may trail the log, so it is not
// asked whether a post is deleted: every candidate's index entry is, so a
// post deleted or restored since post_meta last heard is shown as it now
// is. Because that can leave out candidates, the page is filled by reading
// further batches until it holds a page of posts and one more, or the
// history ends; the cursor is the last post shown, so a page is short only
// at the end, and a history that ends has no cursor. A quarantined thread's
// posts are left out: the thread endpoints refuse that thread until an
// operator repairs it, and one broken thread should not refuse a whole
// history.
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
	authors := map[int64]dto.Author{u.ID: member.Author(u)}

	// shown is the page and one more, each with its position, so the
	// cursor can name the last one the page keeps.
	type item struct {
		post dto.UserPost
		key  views.PostKey
	}
	var shown []item
	for len(shown) <= size {
		var rows []views.UserPostRow
		if err := s.DB.Read(ctx, func(_ *dbq.Queries, tx *sql.Tx) error {
			var err error
			rows, err = views.UserPosts(ctx, tx, views.UserPostsParams{AuthorID: u.ID, Forums: forums,
				After: after, Limit: size + 1})
			return err
		}); err != nil {
			return out, err
		}
		rendered, err := s.historyBatch(ctx, rows, authors, staff, own)
		if err != nil {
			return out, err
		}
		for _, r := range rows {
			if p, ok := rendered[r.Key()]; ok && len(shown) <= size {
				shown = append(shown, item{
					post: dto.UserPost{
						Thread: dto.ThreadLink{ID: threadid.ID(r.ThreadID).String(), Slug: r.ThreadSlug, Title: r.ThreadTitle},
						Forum:  dto.ForumRef{Slug: r.ForumSlug, Name: r.ForumName},
						Post:   p,
					},
					key: r.Key(),
				})
			}
		}
		if len(rows) < size+1 {
			break // the history has no more
		}
		k := rows[len(rows)-1].Key()
		after = &k
	}
	if len(shown) > size {
		shown = shown[:size]
		out.NextCursor = encodePostCursor(shown[size-1].key)
	}
	for _, it := range shown {
		out.Posts = append(out.Posts, it.post)
	}
	return out, nil
}

// historyBatch renders the rows' posts that the history shows this viewer,
// keyed by position. Each thread is opened once, for all its posts in the
// batch, and its index, not post_meta, decides which are shown.
func (s *Service) historyBatch(ctx context.Context, rows []views.UserPostRow, authors map[int64]dto.Author,
	staff, own bool) (map[views.PostKey]dto.Post, error) {
	byThread := map[int64][]uint32{}
	var order []int64
	for _, r := range rows {
		if _, ok := byThread[r.ThreadID]; !ok {
			order = append(order, r.ThreadID)
		}
		byThread[r.ThreadID] = append(byThread[r.ThreadID], uint32(r.Ordinal))
	}
	created := map[views.PostKey]int64{}
	for _, r := range rows {
		created[views.PostKey{Thread: r.ThreadID, Ordinal: r.Ordinal}] = r.CreatedAt
	}
	out := map[views.PostKey]dto.Post{}
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
				k := views.PostKey{Thread: tid, Ordinal: int64(p.Ordinal)}
				k.At = created[k]
				out[k] = p
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
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
