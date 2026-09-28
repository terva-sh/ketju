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
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/service/posts"
	"github.com/terva-sh/ketju/internal/service/respcache"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// DefaultPageSize is docs/06's decided default. The per-user setting (10 to
// 50) arrives with the settings endpoint; until then every page is this size.
const DefaultPageSize = 25

// Viewer classes (docs/07): what a response's cacheable part depends on.
// Moderators see tombstone contents; members and anonymous viewers do not.
const (
	ClassAnon      = "anon"
	ClassMember    = "member"
	ClassModerator = "moderator"
)

// Service reads threads.
type Service struct {
	DB      *db.DB
	Perm    *perm.Evaluator
	Threads *threadcache.Cache
	Render  *render.Cache
	// Responses caches a thread page's posts per viewer class (docs/07). Nil
	// caches nothing.
	Responses *respcache.Cache

	// Quarantined reports a thread whose log startup catch-up found shorter
	// than its projection (projection.Writer.Quarantined). Every endpoint
	// answers it 503 until an operator repairs it, and every other thread
	// keeps serving (docs/03 "Durability and recovery"). Nil quarantines
	// nothing.
	Quarantined func(threadid.ID) bool

	// Writer appends and publishes every post and thread a request writes.
	Writer *posts.Writer
	// IDs names new threads.
	IDs *threadid.Generator
	// Project projects a thread up to a log length before CreateThread
	// answers (projection.Writer.ProjectThread). Nil leaves it to the bus.
	Project func(ctx context.Context, id threadid.ID, logLength int64) error
	// Now is the clock posts are stamped with and slow mode measures by. Nil
	// is time.Now.
	Now func() time.Time

	// UserPostsPage is the posts on one page of a user's history; zero is
	// UserPostsPageSize. Tests set it to page through a few posts.
	UserPostsPage int

	// trace, when set, is told which path Replies answered from. Tests use it
	// to prove both paths ran.
	trace func(path string)
}

// Query is what a thread page request asks for. Zero values mean "not given".
type Query struct {
	// Page is one-based.
	Page int
	// Ordinal is a permalink: the page holding it is served. It wins over
	// Page when both are given.
	Ordinal uint32
	// Lens names a filter. Only the identity lens exists yet (TKT-01M29CRJ1P
	// adds the rest), so any value is refused.
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
	if q.Lens != "" {
		return Result{}, problem.Coded(problem.CodeBadRequest, fmt.Errorf("threads: unknown lens %q", q.Lens))
	}
	size := q.PageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return Result{}, err
	}
	v, row, class, canModerate := a.viewer, a.row, a.class, a.canModerate

	var (
		forum    dbq.Forum
		tags     []string
		lastRead int64
		watch    string
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
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		watch, err = dq.ThreadWatchLevel(ctx, dbq.ThreadWatchLevelParams{
			UserID: v.UserID, ThreadID: sql.NullInt64{Int64: int64(id), Valid: true}})
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		return err
	})
	if err != nil {
		return Result{}, err
	}

	viewer := dto.ThreadViewer{
		LastReadOrdinal: int(lastRead),
		Watch:           watch,
		CanReply:        a.canReply && (row.Locked == 0 || canModerate),
		CanModerate:     canModerate,
	}

	var res Result
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		// One snapshot: the length the ETag names and the entries and reply
		// counts the page is built from describe the same log. Without it an
		// append landing between the two could be answered with a 304 for the
		// length before it (threadlog.Actor.Stable).
		var (
			snap    snapshot
			pageErr error
		)
		th.Actor.Stable(func(logLength int64) {
			snap = snapshot{logLength: logLength, n: th.Index.Len()}
			snap.page, pageErr = PageFor(q, snap.n, size, v.Member(), lastRead)
			if pageErr != nil {
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

		authors, err := s.users(ctx, snap.authors())
		if err != nil {
			return err
		}

		res.Body = dto.ThreadPage{
			Thread: header(id, row, forum, tags, snap.n, snap.logLength),
			Page:   dto.Pagination{Number: snap.page, Size: size, TotalPages: totalPages(snap.n, size)},
			Viewer: viewer,
		}
		if res.Body.Posts, err = s.pagePosts(ctx, th, id, snap, authors, q, size, row.LogLengthProjected, class); err != nil {
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

func header(id threadid.ID, row dbq.Thread, forum dbq.Forum, tags []string, n int, logLength int64) dto.ThreadHeader {
	h := dto.ThreadHeader{
		ID:    id.String(),
		Slug:  row.Slug,
		Title: row.Title,
		Forum: dto.ForumRef{Slug: forum.Slug, Name: forum.Name},
		// From the index, not the projection, so it agrees with LogLength.
		PostCount: n,
		LogLength: logLength,
		Locked:    row.Locked != 0,
		Tags:      append([]string{}, tags...),
		CreatedAt: dto.Millis(row.CreatedAt),
	}
	if row.PinnedScope.Valid {
		h.Pinned = &dto.Pinned{Scope: row.PinnedScope.String}
		if row.PinnedUntil.Valid {
			until := dto.Millis(row.PinnedUntil.Int64)
			h.Pinned.Until = &until
		}
	}
	return h
}

// posts reads, renders and assembles the page's posts, reading the
// revisions the snapshot names rather than whatever the index holds by now.
func (s *Service) posts(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	users map[int64]dto.Author, q Query, moderator bool) ([]dto.Post, error) {
	out, _, err := s.postsDeps(ctx, th, id, snap, users, q, moderator)
	return out, err
}

// postsDeps is posts, also returning what the HTML was drawn against: every
// post's quotes and handle lookups, for the response cache to check on a hit.
func (s *Service) postsDeps(ctx context.Context, th *threadcache.Thread, id threadid.ID, snap snapshot,
	users map[int64]dto.Author, q Query, moderator bool) ([]dto.Post, render.Deps, error) {
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
		if p.Tombstone && moderator {
			var err error
			if p, err = th.Reader.HiddenEntry(p.Ordinal, entries[i]); err != nil {
				return nil, deps, err
			}
		}
		posts = append(posts, p)
	}

	handles := s.handleMemo(ctx)
	rctx := render.Context{Posts: postResolver(th)}

	for _, p := range posts {
		d := dto.Post{
			Ordinal:         int(p.Ordinal),
			Revision:        int(p.Revision),
			Author:          users[int64(p.Author)],
			CreatedAt:       dto.Millis(p.CreatedMS),
			ReplyCount:      snap.replies[p.Ordinal],
			Attachments:     []dto.Attachment{},
			Deleted:         p.Flags.Has(threadlog.EntryDeleted) || p.Flags.Has(threadlog.EntryHiddenByMod),
			MachineAuthored: p.Flags.Has(threadlog.EntryMachineAuthored),
		}
		if p.Revision > 1 && p.RevisionMS != 0 {
			at := dto.Millis(p.RevisionMS)
			d.EditedAt = &at
		}
		if p.ReplyTo != 0 {
			r := int(p.ReplyTo)
			d.ReplyTo = &r
			d.ReplyToSuppressed = p.ReplyTo == p.Ordinal-1
		}
		// Blob metadata arrives with the blobs table (TKT-01M29BYYJ0); until
		// then an attachment is its hash.
		for _, h := range p.Attachments {
			d.Attachments = append(d.Attachments, dto.Attachment{Hash: h})
		}
		// A tombstone reaches here with its source only for a moderator.
		if !p.Tombstone || moderator {
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
// has it). The key names this post's revision and neither of those.
//
// A render or a check during which a handle lookup failed is an error: the
// mention would be drawn as unresolved when it may not be, and that is
// neither cached nor served (review 392).
func (s *Service) html(id threadid.ID, p threadlog.Post, rctx render.Context, handles *handleMemo) (string, error) {
	html, _, err := s.htmlDeps(id, p, rctx, handles)
	return html, err
}

// htmlDeps is html, also returning what the rendering was drawn against.
func (s *Service) htmlDeps(id threadid.ID, p threadlog.Post, rctx render.Context, handles *handleMemo) (string, render.Deps, error) {
	k := render.Key{Thread: id, Ordinal: p.Ordinal, Revision: uint32(p.Revision)}
	if html, deps, ok := s.Render.GetDeps(k); ok && quotesCurrent(deps.Quotes, rctx.Posts) && handles.current(deps.Handles) {
		if handles.err != nil {
			return "", render.Deps{}, fmt.Errorf("threads: checking %s#%d: %w", id, p.Ordinal, handles.err)
		}
		return html, deps, nil
	}
	var used []render.HandleLookup
	rctx.Handles = render.HandleFunc(func(h string) (int64, string, bool) {
		l := handles.lookup(h)
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
			out[u.ID] = dto.Author{Handle: u.Handle, DisplayName: u.DisplayName,
				Avatar: u.AvatarBlob.String, Role: u.Role, Kind: u.Kind}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	member.FillMissing(out, list...)
	return out, nil
}

// handleMemo resolves mentions for one request: each handle once however
// many posts mention it, with the first failure kept, because the renderer's
// resolver cannot return one.
type handleMemo struct {
	s    *Service
	ctx  context.Context
	seen map[string]render.HandleLookup
	err  error
}

func (s *Service) handleMemo(ctx context.Context) *handleMemo {
	return &handleMemo{s: s, ctx: ctx, seen: map[string]render.HandleLookup{}}
}

func (m *handleMemo) lookup(h string) render.HandleLookup {
	if l, ok := m.seen[h]; ok {
		return l
	}
	id, canonical, ok, err := m.s.handle(m.ctx, h)
	if err != nil {
		if m.err == nil {
			m.err = err
		}
		// Not remembered: a failure is not an answer.
		return render.HandleLookup{Typed: h}
	}
	l := render.HandleLookup{Typed: h, ID: id, Canonical: canonical, Found: ok}
	m.seen[h] = l
	return l
}

// current reports whether every recorded lookup still has the same answer.
func (m *handleMemo) current(lookups []render.HandleLookup) bool {
	for _, l := range lookups {
		if m.lookup(l.Typed) != l {
			return false
		}
	}
	return true
}

// handle resolves a mention for the renderer. No such handle is ok=false
// with no error; anything else is an error, which is not the same answer.
func (s *Service) handle(ctx context.Context, h string) (id int64, canonical string, ok bool, err error) {
	var u dbq.User
	err = s.DB.Read(ctx, func(dq *dbq.Queries, tx *sql.Tx) error {
		var err error
		u, err = dq.GetUserByHandle(ctx, h)
		return err
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, "", false, nil
	case err != nil:
		return 0, "", false, fmt.Errorf("threads: resolving @%s: %w", h, err)
	}
	return u.ID, u.Handle, true, nil
}
