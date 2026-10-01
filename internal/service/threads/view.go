// Package threads is the service behind the thread endpoints (docs/02
// "Read a thread page", docs/07 "Thread response shape").
//
// It returns values from internal/dto and nothing HTTP-shaped beyond the
// problem codes its errors carry, so the JSON handler and the future HTML
// handler render the same page from the same call (docs/07 "Path to a non-JS
// client").
package threads

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/service/threads/lens"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// DefaultPageSize is docs/06's decided default: the page size of a viewer who
// has not chosen one, and of anyone not signed in. A member's own choice, 10
// to 50, reaches View as Query.PageSize (users.PageSize, TKT-01M29CCC0X).
const DefaultPageSize = 25

// Viewer classes (docs/07): what a response's cacheable part depends on.
// Moderators see tombstone contents; members and anonymous viewers do not.
// The author of a post a moderator removed sees that post, so a page holding
// one is drawn for the class ClassAuthor followed by their user id, and is
// shared with no one else (see pageClass).
const (
	ClassAnon      = "anon"
	ClassMember    = "member"
	ClassModerator = "moderator"
	ClassAuthor    = "author:"
)

// revealRemoved says whether a gets the content of the removed post whose
// entry is e: its body, and its revisions in History (TKT-01M29CKRJ9).
//
// It answers only for a removed entry, one that is not Visible, and is false
// for a visible one: a visible post's content goes to everyone admitted to
// the thread, and its history to whoever History admits, as before. A
// moderator gets every removed post except a moved one, which has no content
// here. The author gets their own post while a moderator's removal holds it
// (docs/08, decided 2026-09-28: "The author still sees the post, marked as
// removed by a moderator"), and while the post is hidden pending review
// (docs/08 "Flags", TKT-01M29CP2PJ), and not one they deleted themselves.
// Anyone else gets nothing.
//
// A redacted post goes to nobody, moderators included: a hard redaction
// left nothing of it in the log (docs/03 "Hard redaction", TKT-01M29CXJZ9).
func revealRemoved(a access, e threadlog.IndexEntry) bool {
	if e.Flags.Has(threadlog.EntryRedacted) {
		return false
	}
	switch removal(e.Flags) {
	case "":
		return false
	case dto.RemovalModerator, dto.RemovalReview:
		return a.canModerate || a.viewer.Member() && e.Author == uint64(a.viewer.UserID)
	default:
		// The author's own delete, and anything this build does not name:
		// moderators only, which fails closed.
		return a.canModerate
	}
}

// removal is dto.Post.Removal for an entry's flags: who took the post down,
// or "" for a visible post and a moved one. The deleted flag wins over
// hidden-by-mod, since the author asked for the post to go. The service never
// sets both, but a log appended without its preconditions may. A post hidden
// by a moderator's flag with auto-hidden beside it is hidden pending review
// (TKT-01M29CP2PJ): revealRemoved serves it as it serves a moderator's hide,
// to its author and to moderators, and the removal says why.
func removal(f threadlog.EntryFlags) string {
	switch {
	case f.Visible(), f.Has(threadlog.EntryMoved):
		return ""
	case f.Has(threadlog.EntryDeleted):
		return dto.RemovalAuthor
	case f.Has(threadlog.EntryAutoHidden):
		return dto.RemovalReview
	default:
		return dto.RemovalModerator
	}
}

// pageClass is the viewer class the posts in snap are drawn and cached for.
// It is the access check's class, except for a viewer who is served a removed
// post only because they wrote it: then it is ClassAuthor and their user id,
// so those posts are cached for them alone. A page holding no such post keeps
// the shared class.
func pageClass(a access, snap snapshot) string {
	if a.class == ClassModerator {
		return a.class
	}
	for _, e := range snap.entries {
		if revealRemoved(a, e) {
			return ClassAuthor + strconv.FormatInt(a.viewer.UserID, 10)
		}
	}
	return a.class
}

// Service reads threads.
type Service struct {
	DB      *db.DB
	Perm    *perm.Evaluator
	Threads *threadcache.Cache
	Render  *render.Cache
	// Responses caches a thread page's posts per viewer class (docs/07). Nil
	// caches nothing.
	Responses *respcache.Cache

	// TestLenses serves a lens kind with a fixed filter, ahead of the
	// resolvers in lenses.go. It is for tests in other packages, which can
	// drive the lens framework end to end before a kind has a resolver; the
	// server leaves it nil.
	TestLenses map[lens.Kind]lens.Filter

	// Quarantined reports a thread whose log startup catch-up found shorter
	// than its projection (projection.Writer.Quarantined). Every endpoint
	// answers it 503 until an operator repairs it, and every other thread
	// keeps serving (docs/03 "Durability and recovery"). Nil quarantines
	// nothing.
	Quarantined func(threadid.ID) bool

	// Writer appends and publishes every post and thread a request writes:
	// a *posts.Writer in the server. Tests wrap one to fail or hold an
	// append.
	Writer Appender
	// IDs names new threads.
	IDs *threadid.Generator
	// Project projects a thread up to a log length before CreateThread
	// answers, and before Reply answers a post with attachments
	// (projection.Writer.ProjectThread). Nil leaves it to the bus.
	Project func(ctx context.Context, id threadid.ID, logLength int64) error
	// Attachments checks and counts a post's attachments: a
	// *blobs.Service in the server. Nil refuses any attachment.
	Attachments Attachments
	// Now is the clock posts are stamped with and slow mode measures by. Nil
	// is time.Now.
	Now func() time.Time

	// UserPostsPage is the posts on one page of a user's history; zero is
	// UserPostsPageSize. Tests set it to page through a few posts.
	UserPostsPage int
	// QueuePage is the items on one page of the moderator queue; zero is
	// QueuePageSize. Tests set it to page through a few items.
	QueuePage int

	// trace, when set, is told which path Replies answered from. Tests use it
	// to prove both paths ran.
	trace func(path string)

	// flagLocks is the per-post lock of the auto-hide, a moderator's restore
	// and ConfirmHide (flags.go).
	flagLocks flagLocks
	// afterFlagCount, when set, is called by EnforceAutoHide between reading
	// a post's open flags and appending its hide, still under the lock.
	// Tests use it to hold the sweep there.
	afterFlagCount func(id threadid.ID, ordinal uint32)
	// beforeFlagCheck, when set, is called by Flag once it holds the post's
	// flag lock, before it checks the post. Tests use it to hold a flag
	// there.
	beforeFlagCheck func(id threadid.ID, ordinal uint32)
	// afterQueueRead, when set, is called by ResolvePost once it has read
	// the post's state under the flag lock, before it acts on it. Tests use
	// it to change the post there.
	afterQueueRead func(id threadid.ID, ordinal uint32)

	// Logger reports what a request cannot answer with: an audit row owed
	// after an action that happened (queue.go). Nil is slog.Default().
	Logger *slog.Logger

	// SearchIndexing reports whether the search index may be missing posts
	// because it trails the logs (search.Indexer.Indexing), for Search's
	// indexing flag. Nil is never indexing.
	SearchIndexing func() bool
}

// Appender is what the service appends through: *posts.Writer, the one
// seam that appends and publishes (internal/service/posts).
type Appender interface {
	Append(th *threadcache.Thread, a threadlog.Append) (threadlog.Result, error)
}

// Query is what a thread page request asks for. Zero values mean "not given".
type Query struct {
	// Page is one-based.
	Page int
	// Ordinal is a permalink: the page holding it is served. It wins over
	// Page when both are given.
	Ordinal uint32
	// Lens is the ?lens= value as given (TKT-01M29CRJ1P). A value that names
	// no lens this build serves, the empty one included, is the canonical
	// page, never an error (lenses.go).
	Lens string
	// After narrows the page to posts whose current revision was written at
	// or after this log length: new posts and edits since a client's copy.
	After    int64
	HasAfter bool
	// PageSize is the posts per page; zero means DefaultPageSize.
	PageSize int
}

// Served is a response body with its ETag, or the news that the client's
// copy is current.
type Served[T any] struct {
	ETag string
	// NotModified means ifNoneMatch matched, and Body is the zero value.
	NotModified bool
	Body        T
}

// Result is a served thread page.
type Result = Served[dto.ThreadPage]

// View serves one page of a thread to v.
//
// The page is assembled from one snapshot of the index (see snapshot) and
// its ETag hashes the result (see etag), so a 304 still assembles the page,
// mostly from the render cache, and saves the transfer.
func (s *Service) View(ctx context.Context, v domain.Viewer, id threadid.ID, q Query, ifNoneMatch string) (Result, error) {
	size := q.PageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return Result{}, err
	}
	v, row, canModerate := a.viewer, a.row, a.canModerate

	var (
		forum      dbq.Forum
		tags       []string
		lastRead   int64
		readAt     *dto.Millis
		watch      string
		forumWatch string
		authorW    *dto.AuthorWindows
	)
	err = s.DB.Read(ctx, func(dq *dbq.Queries, tx *sql.Tx) error {
		var err error
		if forum, err = dq.GetForum(ctx, row.ForumID); err != nil {
			return fmt.Errorf("threads: the forum of %s: %w", id, err)
		}
		if tags, err = dq.ThreadTags(ctx, int64(id)); err != nil {
			return err
		}
		if !v.Member() {
			return nil
		}
		rp, err := dq.GetReadPosition(ctx, dbq.GetReadPositionParams{UserID: v.UserID, ThreadID: int64(id)})
		switch {
		case err == nil:
			lastRead = rp.LastReadOrdinal
			at := dto.Millis(rp.LastReadAt)
			readAt = &at
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		// Both levels are the viewer's own, so they are in the viewer
		// block, which is never cached (TKT-01M29CRJ29).
		if watch, err = threadWatchLevel(ctx, dq, v, id); err != nil {
			return err
		}
		forumWatch, err = forumWatchLevel(ctx, dq, v, row.ForumID)
		// The author windows are per viewer, so they are in the viewer block,
		// which is never cached, and the ETag, which hashes it, moves when
		// they change. A viewer who may moderate the forum is bound by
		// neither, and is sent none (TKT-01M3PTT97X).
		if err != nil || canModerate {
			return err
		}
		authorW, err = viewerWindows(ctx, dq)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	// Access came first, so a thread hidden from v is not-found whatever
	// the lens says (docs/07 "Access"). The lens resolves after the viewer's
	// read position is read, so the unread lens costs no statement of its
	// own. A nil lens is the canonical page.
	rl, err := s.resolveLens(ctx, lensViewer{access: a, lastRead: lastRead}, q.Lens)
	if err != nil {
		return Result{}, err
	}

	// Slow mode is per thread and next_post_at per viewer, so both are in
	// the viewer block, which is never cached; the posts do not depend on
	// either (TKT-01M29CRJ50).
	slow, err := s.effectiveSlowMode(ctx, id)
	if err != nil {
		return Result{}, err
	}
	canUpload, err := s.canUpload(ctx, a)
	if err != nil {
		return Result{}, err
	}
	viewer := dto.ThreadViewer{
		LastReadOrdinal: int(lastRead),
		LastReadAt:      readAt,
		Watch:           watch,
		ForumWatch:      forumWatch,
		CanReply:        a.canReply && (row.Locked == 0 || canModerate) && readOnlyForum(a) == nil,
		CanUpload:       canUpload,
		CanModerate:     canModerate,
		SlowModeSeconds: slow,
		AuthorWindows:   authorW,
		ForumReadOnly:   readOnlyForum(a) != nil,
	}
	now := s.now()
	// The rename window is read only for the one viewer it can matter to,
	// so no other page view pays for the read (TKT-01M3QXV399).
	var window time.Duration
	if mayOwnRename(a) {
		if window, err = s.renameWindow(ctx); err != nil {
			return Result{}, err
		}
	}

	var res Result
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		// Per viewer, so in the viewer block, which is never cached. The
		// ETag hashes it, so a page whose window has closed revalidates.
		viewer.RenameUntil = renameUntil(a, th.Archived(), window, now)
		// One snapshot: the length the ETag names and the entries and reply
		// counts the page is built from describe the same log. Without it an
		// append landing between the two could be answered with a 304 for the
		// length before it (threadlog.Actor.Stable).
		var (
			snap    snapshot
			sel     lens.Selection
			pageErr error
		)
		th.Actor.Stable(func(logLength int64) {
			if rl != nil {
				snap, sel, pageErr = lensPage(th.Index, logLength, rl, q, size)
			} else {
				snap = snapshot{logLength: logLength, n: th.Index.Len()}
				snap.page, pageErr = PageFor(q, snap.n, size, v.Member(), lastRead)
			}
			if pageErr != nil {
				return
			}
			// From the same snapshot as the page, so a post of v's on it is
			// the one next_post_at counts from.
			// lensPage has already taken a lens page's entries; what
			// follows takes the canonical page's.
			if viewer.NextPostAt, pageErr = nextPostAt(a, th.Index, slow, now); pageErr != nil || rl != nil {
				return
			}
			from := uint32((snap.page-1)*size + 1)
			snap.entries = th.Index.Entries(from, uint32(snap.page*size))
			snap.ordinals = make([]uint32, len(snap.entries))
			for i := range snap.entries {
				snap.ordinals[i] = from + uint32(i)
			}
			snap.replies = th.Index.ReplyCounts(snap.ordinals)
		})
		if pageErr != nil {
			return pageErr
		}

		// The starter is read in the same batch as the page's authors, so
		// the header's author costs the page no statement (TKT-01M29CTY58).
		// The posts are drawn and keyed from the page's authors alone.
		ids := snap.authors()
		onPage := ids[row.AuthorID]
		ids[row.AuthorID] = true
		authors, err := s.users(ctx, ids)
		if err != nil {
			return err
		}
		starter := authors[row.AuthorID]
		if !onPage {
			delete(authors, row.AuthorID)
		}

		res.Body = dto.ThreadPage{
			Thread: header(id, row, forum, starter, tags, snap.n, snap.logLength, now),
			Page:   dto.Pagination{Number: snap.page, Size: size, TotalPages: totalPages(snap.n, size)},
			Viewer: viewer,
		}
		lensKey := ""
		if rl != nil {
			// Page and total_pages count the lens's posts (docs/07).
			res.Body.Page.TotalPages = sel.TotalPages(size)
			res.Body.Lens = lensBody(rl, sel, snap.page, size)
			lensKey = rl.key
		}
		if res.Body.Posts, err = s.pagePosts(ctx, th, id, snap, authors, q, size, row.LogLengthProjected, a, lensKey); err != nil {
			return err
		}
		return res.finish(id, snap.logLength, ifNoneMatch)
	})
	return res, err
}

// snapshot is one consistent view of a thread's index, taken inside
// threadlog.Actor.Stable: the log length, the post count, some posts' entries
// and their reply counts, all as of that length.
type snapshot struct {
	logLength int64
	n         int
	page      int // the thread page's; zero elsewhere
	// ordinals and entries are parallel, in ordinal order. The page's are
	// contiguous; a post's replies are not.
	ordinals []uint32
	entries  []threadlog.IndexEntry
	replies  map[uint32]int
}

// take fills the snapshot for ordinals, which must be in the index. Call it
// inside Stable.
func (snap *snapshot) take(ix *threadlog.Index, logLength int64, ordinals []uint32) error {
	*snap = snapshot{logLength: logLength, n: ix.Len(), ordinals: ordinals,
		entries: make([]threadlog.IndexEntry, len(ordinals))}
	for i, o := range ordinals {
		e, err := ix.Entry(o)
		if err != nil {
			return err
		}
		snap.entries[i] = e
	}
	snap.replies = ix.ReplyCounts(ordinals)
	return nil
}

// authors are the snapshot's posts' authors, for users().
func (snap snapshot) authors() map[int64]bool {
	ids := map[int64]bool{}
	for _, e := range snap.entries {
		ids[int64(e.Author)] = true
	}
	return ids
}

// finish sets the ETag over the body and applies If-None-Match.
func (r *Served[T]) finish(id threadid.ID, logLength int64, ifNoneMatch string) error {
	var err error
	if r.ETag, err = etag(id, logLength, r.Body); err != nil {
		return err
	}
	if matches(ifNoneMatch, r.ETag) {
		var zero T
		r.NotModified, r.Body = true, zero
	}
	return nil
}

// PageFor picks the page: a permalink's, the asked-for one, the one holding
// the first unread post for a returning member (docs/06 "Landing on
// return"), or the first.
func PageFor(q Query, n, size int, member bool, lastRead int64) (int, error) {
	total := totalPages(n, size)
	switch {
	case q.Ordinal > 0:
		if int(q.Ordinal) > n {
			return 0, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: ordinal %d past the last post %d", q.Ordinal, n))
		}
		return int(q.Ordinal-1)/size + 1, nil
	case q.Page > 0:
		if q.Page > total {
			return 0, problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: page %d past the last page %d", q.Page, total))
		}
		return q.Page, nil
	case member && lastRead > 0:
		// The page holding last_read_ordinal + 1, which is past the end
		// when everything is read: then the last page.
		return min(int(lastRead)/size+1, total), nil
	default:
		return 1, nil
	}
}

// totalPages is at least one: an empty thread still has a first page.
func totalPages(n, size int) int {
	return max(1, (n+size-1)/size)
}

// etag is `"<id>-<log_length>-r<renderer>-<digest>"`, the digest a hash of
// the page as it will be written (docs/07, extended in TKT-01M29CA9AX).
//
// The prefix names the thread's version and the pipeline that drew it, which
// is what a person reading a log wants. The digest is what makes it correct:
// a page depends on the log, and also on the projection, the users and forums
// tables, the viewer's own state, permissions and the query, and a list of
// those kept beside the code falls behind it (reviews 391 to 393 each found
// one missing). Two different bodies therefore never share a tag. The cost
// is that a 304 still assembles the page; the render cache makes that cheap,
// and what a 304 saves is the transfer.
func etag(id threadid.ID, logLength int64, body any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("threads: hashing a response for %s: %w", id, err)
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf(`"%s-%d-r%d-%s"`, id, logLength, render.Version, hex.EncodeToString(sum[:8])), nil
}

// matches applies If-None-Match: a list of entity tags, weak ones compared
// weakly as RFC 9110 requires for GET, or "*".
func matches(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == tag {
			return true
		}
	}
	return false
}

// header is a thread page's header. Pinned is set only while the pin is
// active at now: a pin whose pinned_until has passed is no pin, the rule the
// thread list applies (views.activePin), so an expiry needs no job to clear
// it (TKT-01M29CKRGS). The projection row keeps the expired values until the
// next pin record. The header is never cached, so the clock applies on every
// request, and the starter is read from the users row on every request, so a
// rename or an anonymization shows on the next view.
func header(id threadid.ID, row dbq.Thread, forum dbq.Forum, starter dto.Author, tags []string, n int, logLength int64, now time.Time) dto.ThreadHeader {
	h := dto.ThreadHeader{
		ID:     id.String(),
		Slug:   row.Slug,
		Title:  row.Title,
		Forum:  dto.ForumRef{Slug: forum.Slug, Name: forum.Name},
		Author: starter,
		// From the index, not the projection, so it agrees with LogLength.
		PostCount: n,
		LogLength: logLength,
		Locked:    row.Locked != 0,
		Tags:      append([]string{}, tags...),
		CreatedAt: dto.Millis(row.CreatedAt),
	}
	if pinActive(row, now) {
		h.Pinned = &dto.Pinned{Scope: row.PinnedScope.String}
		if row.PinnedUntil.Valid {
			until := dto.Millis(row.PinnedUntil.Int64)
			h.Pinned.Until = &until
		}
	}
	return h
}

// starterOf is the thread's starter, user id, as the header draws it: the
// users row through member.Author, or member.Missing when the database has
// no such row, as the thread list draws its rows' authors.
func starterOf(ctx context.Context, dq *dbq.Queries, id int64) (dto.Author, error) {
	u, err := dq.GetUser(ctx, id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return member.Missing(id), nil
	case err != nil:
		return dto.Author{}, fmt.Errorf("threads: the starter %d: %w", id, err)
	}
	return member.Author(u), nil
}

// posts reads, renders and assembles the page's posts for a, reading the
// revisions the snapshot names rather than whatever the index holds by now.
// A removed post carries its body only where revealRemoved says so.
func (s *Service) posts(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	users map[int64]dto.Author, q Query, a access) ([]dto.Post, error) {
	out, _, err := s.postsDeps(ctx, th, id, snap, users, q, a)
	return out, err
}

// postsDeps is posts, also returning what the HTML was drawn against: every
// post's quotes and handle lookups, for the response cache to check on a hit.
func (s *Service) postsDeps(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	users map[int64]dto.Author, q Query, a access) ([]dto.Post, render.Deps, error) {
	out := []dto.Post{}
	var deps render.Deps
	entries := snap.entries
	// One read per run of consecutive ordinals: the page is a single run, and
	// a post's replies are usually several.
	var read []threadlog.Post
	for i := 0; i < len(entries); {
		j := i + 1
		for j < len(entries) && snap.ordinals[j] == snap.ordinals[j-1]+1 {
			j++
		}
		run, err := th.Reader.ReadEntries(snap.ordinals[i], entries[i:j])
		if err != nil {
			return nil, deps, err
		}
		read = append(read, run...)
		i = j
	}

	// after_log_length keeps the posts whose current revision was written at
	// or after it: new posts and edits. A Delete or Restore moves no entry's
	// offset, so it does not show here; the client refetches the page on a
	// moderation event instead (settled in grooming).
	var posts []threadlog.Post
	for i, p := range read {
		if q.HasAfter && int64(entries[i].Offset) < q.After {
			continue
		}
		if p.Tombstone && revealRemoved(a, entries[i]) {
			var err error
			if p, err = th.Reader.HiddenEntry(p.Ordinal, entries[i]); err != nil {
				return nil, deps, err
			}
		}
		posts = append(posts, p)
	}

	handles := s.handleMemo(ctx)
	rctx := render.Context{Posts: postResolver(th)}
	attachments, err := s.attachmentsOf(ctx, id, posts)
	if err != nil {
		return nil, deps, err
	}

	for i, p := range posts {
		d := dto.Post{
			Ordinal:         int(p.Ordinal),
			Revision:        int(p.Revision),
			Author:          users[int64(p.Author)],
			CreatedAt:       dto.Millis(p.CreatedMS),
			ReplyCount:      snap.replies[p.Ordinal],
			Attachments:     []dto.Attachment{},
			Deleted:         p.Flags&(threadlog.EntryDeleted|threadlog.EntryHiddenByMod|threadlog.EntryRedacted) != 0,
			MachineAuthored: p.Flags.Has(threadlog.EntryMachineAuthored),
			Redacted:        p.Flags.Has(threadlog.EntryRedacted),
		}
		// A hard-redacted post is deleted above, and removal names who took
		// it down: the moderator's, as an admin redacted it, or the
		// author's if they had deleted it first (TKT-01M29CXJZ9). Redacted
		// says it can never come back (TKT-01M3R1WPNS).
		if r := removal(p.Flags); r != "" {
			d.Removal = &r
		}
		// The edited flag, not the revision count: a post whose only edits
		// are grace edits has more than one revision and is not shown as
		// edited. When the flag is set the current revision is a visible
		// edit, so its time is the last visible edit's, as post_meta's
		// edited_at is (threadlog postFlagEffect, TKT-01M29CKRHS). The flag
		// comes from the log alone, so the cached posts depend on no setting.
		if p.Flags.Has(threadlog.EntryEdited) && p.RevisionMS != 0 {
			at := dto.Millis(p.RevisionMS)
			d.EditedAt = &at
			// Read from the current revision's frame, which the log length
			// already keys, so the cached page needs no editor row
			// (settled in grooming for TKT-01M29CRJ5J).
			d.EditedByModerator = p.EditorID != p.Author
		}
		if p.ReplyTo != 0 {
			r := int(p.ReplyTo)
			d.ReplyTo = &r
			d.ReplyToSuppressed = p.ReplyTo == p.Ordinal-1
		}
		d.Attachments = append(d.Attachments, attachments[i]...)
		// A removed post reaches here with its source only when revealRemoved
		// let HiddenEntry read it, and that read cleared Tombstone.
		if !p.Tombstone {
			var (
				used render.Deps
				err  error
			)
			if d.HTML, used, err = s.htmlDeps(id, p, rctx, handles); err != nil {
				return nil, deps, err
			}
			deps.Quotes = append(deps.Quotes, used.Quotes...)
			deps.Handles = append(deps.Handles, used.Handles...)
		}
		out = append(out, d)
	}
	return out, deps, nil
}

// html renders one post through the render cache.
//
// A cached rendering is reused only while what it was drawn against still
// holds: every post it quotes is in the state it was rendered against (a
// quote of a post deleted since is a tombstone), and every handle it looked up
// resolves as it did (a handle nobody had is text, and a link once somebody
// has it; a mention follows its member through a handle change, mentions.go).
// The key names this post's revision and neither of those.
//
// A render or a check during which a handle lookup failed is an error: the
// mention would be drawn as unresolved when it may not be, and that is
// neither cached nor served (review 392).
func (s *Service) html(id threadid.ID, p threadlog.Post, rctx render.Context, handles *handleMemo) (string, error) {
	html, _, err := s.htmlDeps(id, p, rctx, handles)
	return html, err
}

// htmlDeps is html, also returning what the rendering was drawn against.
// The post's mentions resolve as of its current revision's time, which the
// key's revision fixes (mentions.go).
func (s *Service) htmlDeps(id threadid.ID, p threadlog.Post, rctx render.Context, handles *handleMemo) (string, render.Deps, error) {
	k := render.Key{Thread: id, Ordinal: p.Ordinal, Revision: uint32(p.Revision)}
	sc := mentionScope{since: p.RevisionMS}
	if html, deps, ok := s.Render.GetDeps(k); ok && quotesCurrent(deps.Quotes, rctx.Posts) && handles.current(deps.Handles) {
		if handles.err != nil {
			return "", render.Deps{}, fmt.Errorf("threads: checking %s#%d: %w", id, p.Ordinal, handles.err)
		}
		return html, deps, nil
	}
	var used []render.HandleLookup
	rctx.Handles = render.HandleFunc(func(h string) (int64, string, bool) {
		l := handles.resolveAt(h, sc)
		used = append(used, l)
		return l.ID, l.Canonical, l.Found
	})
	r, err := render.RenderWith(p.Source, rctx)
	if err == nil {
		err = handles.err
	}
	if err != nil {
		return "", render.Deps{}, fmt.Errorf("threads: rendering %s#%d: %w", id, p.Ordinal, err)
	}
	deps := render.Deps{Quotes: r.Quotes, Handles: used}
	s.Render.PutDeps(k, r.HTML, deps)
	return r.HTML, deps, nil
}

// postResolver answers the renderer's quote lookups from th's index: a post
// exists when the index has its ordinal, and is deleted when it is a
// tombstone. The read path and the preview share it, so a quote cannot render
// one way on the page and another in the composer.
func postResolver(th *threadcache.Thread) render.PostResolver {
	return render.ResolverFunc(func(o uint32) (bool, bool) {
		tomb, err := th.Index.Tombstoned(o)
		return err == nil, err == nil && tomb
	})
}

func quotesCurrent(quotes []render.Quote, posts render.PostResolver) bool {
	for _, q := range quotes {
		if exists, deleted := posts.ResolvePost(q.Ordinal); !exists || deleted != q.Deleted {
			return false
		}
	}
	return true
}

// users loads the page's authors in one query. An author the database does
// not have (a projection rebuilt against an older users table, docs/04) is
// drawn as a deleted member (member.Missing) rather than failing the page.
func (s *Service) users(ctx context.Context, ids map[int64]bool) (map[int64]dto.Author, error) {
	list := make([]int64, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	out := make(map[int64]dto.Author, len(ids))
	err := s.DB.Read(ctx, func(dq *dbq.Queries, tx *sql.Tx) error {
		us, err := dq.UsersByIDs(ctx, list)
		for _, u := range us {
			out[u.ID] = member.Author(u)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	member.FillMissing(out, list...)
	return out, nil
}
