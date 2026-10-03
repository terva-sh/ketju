package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// MaxTitle is the longest thread title, in characters (settled in grooming
// for TKT-01M29CA9B9).
const MaxTitle = 200

// ReplyRequest is a new post in an existing thread.
type ReplyRequest struct {
	Source string
	// ReplyTo is the post being answered, nil for none.
	ReplyTo *uint32
	// Attachments are blob hashes the author uploaded (Attachments.Check).
	Attachments []string
}

// CreateThreadRequest is a new thread and its first post.
type CreateThreadRequest struct {
	Title  string
	Source string
	// Tags are as the request sent them, nil or empty for none. CreateThread
	// trims and folds each, keeps a repeat once and sorts them, and refuses
	// the thread for one that breaks docs/04 `thread_tags`, or for a reserved
	// tag from a viewer who neither moderates the forum nor is an agent
	// account (docs/08 "Thread tags", TKT-01M3B12ZZ4).
	Tags []string
	// Attachments are the first post's, as for a reply.
	Attachments []string
	// From is the post a reply as a new thread continues, nil for an
	// ordinary new thread (TKT-01M29CYSC0).
	From *ContinueFrom
}

// ContinueFrom names the post a new thread continues: its thread's base32
// id and its ordinal.
type ContinueFrom struct {
	Thread  string
	Ordinal uint32
}

// Attachments is what posting needs of the blob store
// (internal/service/blobs, TKT-01M29CXK0K). It is an interface because
// blobs imports this package, for ForumOf and CanSeePostBody, and this one
// must not import it back.
//
// docs/03 "Blob garbage collection" sets the order: Check refuses before
// anything is appended, AddRefs raises each blob's refcount just before the
// Post is appended, and DropRefs takes the references back when that append
// fails.
type Attachments interface {
	// Check answers the hashes as the Post will record them, each once in
	// the order sent, or why the attach is refused: too many, a blob the
	// poster did not upload, one quarantined or unknown, or a poster
	// without upload in forumID.
	Check(ctx context.Context, v domain.Viewer, forumID int64, hashes []string) ([]string, error)
	// AddRefs raises each blob's refcount in one transaction, and refuses
	// the lot if a blob has been collected since Check.
	AddRefs(ctx context.Context, hashes []string) error
	// DropRefs lowers each blob's refcount again.
	DropRefs(ctx context.Context, hashes []string) error
	// Recheck asks Check's per-blob rules again of hashes, as Check
	// answered them, through q, a write transaction of the caller's. A
	// draft holds no reference, so it asks this in the write that saves it,
	// and a blob the collector has deleted since Check refuses the save
	// rather than leaving the draft a hash with no bytes.
	Recheck(ctx context.Context, q *dbq.Queries, userID int64, hashes []string) error
}

// checkAttachments is Attachments.Check, or a refusal of any attachment when
// the service has no blob store, as before one existed.
func (s *Service) checkAttachments(ctx context.Context, v domain.Viewer, forumID int64, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	if s.Attachments == nil {
		return nil, problem.Coded(problem.CodeValidationFailed, errors.New("threads: this server takes no attachments"))
	}
	return s.Attachments.Check(ctx, v, forumID, hashes)
}

// appendWithRefs raises the references of hashes, then appends a through
// the Writer. When the append fails the references are dropped again, and a
// failure there is logged with the hashes: the refcount is then one high,
// which keeps a blob longer and loses nothing.
func (s *Service) appendWithRefs(th *threadcache.Thread, hashes []string, a threadlog.Append) (threadlog.Result, error) {
	if len(hashes) == 0 {
		return s.Writer.Append(th, a)
	}
	// The references are bookkeeping for what the log says, so they are
	// written whatever the client does meanwhile.
	ctx := context.Background()
	if err := s.Attachments.AddRefs(ctx, hashes); err != nil {
		return threadlog.Result{}, err
	}
	res, err := s.Writer.Append(th, a)
	if err != nil {
		if derr := s.Attachments.DropRefs(ctx, hashes); derr != nil {
			s.logger().Error("threads: a post's append failed and its blob references could not be dropped",
				"thread", th.ID.String(), "hashes", hashes, "err", derr)
		}
	}
	return res, err
}

// MayWrite reports whether v may post at all: a member (member.Require). The
// handlers ask this before reading the body, as for POST /render, so a
// stranger is refused as one whatever they sent. Whether v may post *here*
// is Reply's and CreateThread's question.
func (s *Service) MayWrite(v domain.Viewer) error {
	return member.Require(v, "post")
}

// Reply is POST /threads/{id}/posts (docs/07), following docs/02 "Create a
// post": v may reply, the thread is open to v, slow mode has passed, the
// source renders; then the post is appended, durable, and published, and
// the answer is the post as GET /threads/{id}/posts/{ordinal} serves it.
//
// Every refusal comes before the append, so a refused post leaves the log as
// it was. The checks read the thread's state before the append rather than
// under the actor, so a reply racing a lock or another of v's own posts can
// land; docs/08 asks no more of a lock than that.
func (s *Service) Reply(ctx context.Context, v domain.Viewer, id threadid.ID, req ReplyRequest) (dto.CreatedPost, error) {
	var out dto.CreatedPost
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	a, err := s.access(ctx, v, id)
	if err != nil {
		return out, err
	}
	if err := readOnlyForum(a); err != nil {
		return out, err
	}
	if !a.canReply {
		return out, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: %s is not open to this viewer's replies", id))
	}
	if err := checkPost(req.Source); err != nil {
		return out, err
	}
	attachments, err := s.checkAttachments(ctx, v, a.row.ForumID, req.Attachments)
	if err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	slow, err := s.effectiveSlowMode(ctx, id)
	if err != nil {
		return out, err
	}

	var res threadlog.Result
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		switch {
		case th.Archived():
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is archived", id))
		case a.row.Locked != 0 && !a.canModerate:
			return problem.Coded(problem.CodeConflict, fmt.Errorf("threads: %s is locked", id))
		}
		now := s.now()
		if !a.canModerate {
			if err := slowMode(th.Index, v.UserID, slow, now); err != nil {
				return err
			}
		}
		r, err := s.renderSource(ctx, th, req.Source, req.ReplyTo)
		if err != nil {
			return err
		}
		res, err = s.appendWithRefs(th, attachments, threadlog.Append{
			Type: threadlog.TypePost, Flags: flags, Author: uint64(v.UserID),
			Body: postBody(req.Source, r, attachments), TimeMS: now.UnixMilli(),
		})
		return err
	})
	if err != nil {
		return out, err
	}
	// A reply with attachments is projected before the answer, as a new
	// thread is, because serving a blob reads post_attachments: the client
	// shows the post's files at once (TKT-01M29CXK0K). The bus delivers the
	// same event to the projection, which is the retry when this fails.
	if len(attachments) > 0 && s.Project != nil {
		if err := s.Project(ctx, id, res.LogLength); err != nil {
			return out, fmt.Errorf("threads: projecting %s's new post %d: %w", id, res.Ordinal, err)
		}
	}
	p, err := s.Post(ctx, v, id, res.Ordinal, "")
	if err != nil {
		return out, fmt.Errorf("threads: reading back post %d of %s: %w", res.Ordinal, id, err)
	}
	return dto.CreatedPost{Post: p.Body, Ordinal: res.Ordinal, Revision: res.Revision, LogLength: res.LogLength}, nil
}

// CreateThread is POST /forums/{slug}/threads (docs/07): a ThreadCreated and
// the first Post, two appends, then the thread page's header and first post.
//
// The checks all come before the thread's files exist. A crash between the
// two appends leaves a thread with no posts, which access serves as
// not-found (settled in grooming: no new record type for it).
//
// The thread is projected before the answer, through Project, so the client
// can open it at once: access finds no thread without its projection row.
// The bus delivers the same events to the projection writer, and projection
// is idempotent, so that is the retry when projecting here fails.
func (s *Service) CreateThread(ctx context.Context, v domain.Viewer, forumSlug string, req CreateThreadRequest) (dto.CreatedThread, error) {
	var out dto.CreatedThread
	if err := s.MayWrite(v); err != nil {
		return out, err
	}
	forum, err := s.forumForThread(ctx, v, forumSlug)
	if err != nil {
		return out, err
	}
	title, err := checkTitle(req.Title)
	if err != nil {
		return out, err
	}
	// The account's kind is read before the tags, which need it for the
	// reserved rule, and the records carry the same flags.
	flags, err := s.authorFlags(ctx, v)
	if err != nil {
		return out, err
	}
	tags, err := s.checkTags(ctx, v, forum.ID, flags, req.Tags)
	if err != nil {
		return out, err
	}
	if err := checkPost(req.Source); err != nil {
		return out, err
	}
	attachments, err := s.checkAttachments(ctx, v, forum.ID, req.Attachments)
	if err != nil {
		return out, err
	}
	var continues *threadlog.PostRef
	if req.From != nil {
		if continues, err = s.continuesFrom(ctx, v, *req.From); err != nil {
			return out, err
		}
	}
	// No thread to quote from or reply to yet: this is for the render's
	// errors and mentions. The post is drawn against its thread when read.
	r, err := s.renderSource(ctx, nil, req.Source, nil)
	if err != nil {
		return out, err
	}

	id, err := s.IDs.New()
	if err != nil {
		return out, err
	}
	now := s.now().UnixMilli()
	th, err := s.Threads.Create(id, uint64(forum.ID), now)
	if err != nil {
		return out, err
	}
	res, err := func() (threadlog.Result, error) {
		defer s.Threads.Release(th)
		if _, err := s.Writer.Append(th, threadlog.Append{
			Type: threadlog.TypeThreadCreated, Flags: flags, Author: uint64(v.UserID), TimeMS: now,
			Body: &threadlog.ThreadCreatedBody{Title: title, Slug: Slug(title), ForumID: uint64(forum.ID), Tags: tags,
				Continues: continues},
		}); err != nil {
			return threadlog.Result{}, err
		}
		return s.appendWithRefs(th, attachments, threadlog.Append{
			Type: threadlog.TypePost, Flags: flags, Author: uint64(v.UserID), TimeMS: now,
			Body: postBody(req.Source, r, attachments),
		})
	}()
	if err != nil {
		return out, err
	}
	if s.Project != nil {
		if err := s.Project(ctx, id, res.LogLength); err != nil {
			return out, fmt.Errorf("threads: projecting new thread %s: %w", id, err)
		}
	}

	page, err := s.View(ctx, v, id, Query{Ordinal: res.Ordinal}, "")
	if err != nil {
		return out, fmt.Errorf("threads: reading back new thread %s: %w", id, err)
	}
	for _, p := range page.Body.Posts {
		if uint32(p.Ordinal) == res.Ordinal {
			out.Thread = page.Body.Thread
			out.Post = dto.CreatedPost{Post: p, Ordinal: res.Ordinal, Revision: res.Revision, LogLength: res.LogLength}
			return out, nil
		}
	}
	return out, fmt.Errorf("threads: new thread %s's page has no post %d", id, res.Ordinal)
}

// continuesFrom is the post a reply as a new thread continues, as its
// ThreadCreated records it (TKT-01M29CYSC0): one v may read, shown as itself.
// A thread id that is not one is validation-failed. A thread v may not read,
// one that does not exist, a post it does not hold and a post not shown as
// itself (removed, hidden, pending review, redacted or moved) are all
// not-found, so the request never confirms a thread or a post v cannot see.
// A locked or archived thread is not refused: the action writes only the
// new thread's log (docs/06 "Reply as a new thread", run decision of
// 2026-10-02).
func (s *Service) continuesFrom(ctx context.Context, v domain.Viewer, from ContinueFrom) (*threadlog.PostRef, error) {
	id, err := threadid.Parse(from.Thread)
	if err != nil {
		return nil, problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: the post continued from: %w", err))
	}
	gone := func(why error) error {
		return problem.Coded(problem.CodeNotFound, fmt.Errorf("threads: %s/%d is not a post this viewer can continue: %w", id, from.Ordinal, why))
	}
	if from.Ordinal == 0 {
		return nil, gone(errors.New("ordinal 0"))
	}
	if _, err := s.access(ctx, v, id); err != nil {
		if refusal(err) && problem.FromError(err) != problem.CodeUnavailable {
			return nil, gone(err)
		}
		return nil, err
	}
	err = s.Threads.With(id, func(th *threadcache.Thread) error {
		e, err := th.Index.Entry(from.Ordinal)
		if err != nil {
			return gone(err)
		}
		if !e.Flags.Visible() {
			return gone(fmt.Errorf("flags %#x", uint16(e.Flags)))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &threadlog.PostRef{ThreadID: uint64(id), Ordinal: from.Ordinal}, nil
}

// canCreateThread is whether a's viewer may start a thread in the thread's
// forum, as forumForThread decides it: a member, the forum not read-only to
// them, and create_thread there. The thread page offers "reply as a new
// thread" on it (TKT-01M29CYSC0).
func (s *Service) canCreateThread(ctx context.Context, a access) (bool, error) {
	if !a.viewer.Member() || readOnlyForum(a) != nil {
		return false, nil
	}
	return s.Perm.Can(ctx, a.viewer, a.row.ForumID, domain.PermCreateThread)
}

// forumForThread finds the forum v is starting a thread in, answering the
// way access does for a thread: a forum hidden from v is not-found, one v
// cannot read is unauthenticated or forbidden. Then an archived forum is
// conflict to everyone but an admin, read from the row just loaded rather
// than from the decision cache, as readOnlyForum reads it; it comes before
// create_thread, which perm denies there, so the refusal says why. Last, v
// needs create_thread.
func (s *Service) forumForThread(ctx context.Context, v domain.Viewer, slug string) (dbq.Forum, error) {
	forum, err := s.readForum(ctx, v, slug)
	if err != nil {
		return forum, err
	}
	if forum.Archived != 0 && !(v.Member() && v.Role == domain.RoleAdmin) {
		return forum, problem.Coded(problem.CodeConflict, fmt.Errorf("threads: forum %q is archived", slug))
	}
	can, err := s.Perm.Can(ctx, v, forum.ID, domain.PermCreateThread)
	if err != nil {
		return forum, err
	}
	if !can {
		return forum, problem.Coded(problem.CodeForbidden, fmt.Errorf("threads: this viewer may not start threads in %q", slug))
	}
	return forum, nil
}

// CheckTitle is checkTitle for a title written outside the service: ketju
// redact's --title, which replaces a redacted title (TKT-01M29CXJZ9).
func CheckTitle(title string) (string, error) { return checkTitle(title) }

// checkTitle returns the title as it will be stored, trimmed, or why it is
// refused: holding a control character anywhere, which no title needs and a
// log would keep forever, then empty or over MaxTitle characters once
// trimmed. Control characters are looked for before trimming, so "Topic\n"
// is refused as docs/07 says rather than quietly stored as "Topic" (review
// on PR #79).
func checkTitle(title string) (string, error) {
	if strings.ContainsFunc(title, unicode.IsControl) {
		return "", problem.Coded(problem.CodeValidationFailed, errors.New("threads: the title holds a control character"))
	}
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", problem.Coded(problem.CodeValidationFailed, errors.New("threads: the title is empty"))
	case utf8.RuneCountInString(title) > MaxTitle:
		return "", problem.Coded(problem.CodeValidationFailed, fmt.Errorf("threads: the title is over %d characters", MaxTitle))
	}
	return title, nil
}

// checkPost refuses a post with nothing to show. Attachments are
// checkAttachments'.
func checkPost(source string) error {
	if strings.TrimSpace(source) == "" {
		return problem.Coded(problem.CodeValidationFailed, errors.New("threads: the post is empty"))
	}
	return nil
}

// postBody is what the log records of a post: its source, the facts the
// render validated, and the attachments Attachments.Check passed. Mentions
// are user ids in document order, each once, as the render reports them.
func postBody(source string, r render.Result, attachments []string) *threadlog.PostBody {
	b := &threadlog.PostBody{Source: source, Mentions: []uint64{}, Attachments: attachments}
	if r.ReplyTo != nil {
		b.ReplyTo = *r.ReplyTo
	}
	for _, m := range r.Mentions {
		b.Mentions = append(b.Mentions, uint64(m.UserID))
	}
	return b
}

// authorFlags marks a record machine-authored when v is an agent account
// (docs/03 frame flags). The viewer does not carry the account's kind, so it
// is read here.
func (s *Service) authorFlags(ctx context.Context, v domain.Viewer) (threadlog.FrameFlags, error) {
	var u dbq.User
	err := s.DB.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		u, err = q.GetUser(ctx, v.UserID)
		return err
	})
	if err != nil {
		return 0, err
	}
	if u.Kind == domain.KindAgent {
		return threadlog.FrameFlagMachineAuthored, nil
	}
	return 0, nil
}

// slowMode refuses a post that comes less than seconds after the author's
// newest post in the thread, saying how long is left (docs/08 "Primitives").
// Zero seconds is off. seconds is the thread's effective slow mode
// (effectiveSlowMode).
func slowMode(ix *threadlog.Index, author int64, seconds int, now time.Time) error {
	wait, err := slowModeWait(ix, author, seconds, now)
	if err != nil || wait <= 0 {
		return err
	}
	// Retry-After is whole seconds; rounding up means trying again when it
	// says to is never early. A wait within a second of the longest
	// Duration, which a slow mode default too large for one gives
	// (secondsDuration), would overflow into a negative wait and lose the
	// header, so it is truncated instead: at most a second early out of
	// 292 years.
	if wait > time.Duration(math.MaxInt64)-(time.Second-1) {
		wait = wait.Truncate(time.Second)
	} else {
		wait = (wait + time.Second - 1).Truncate(time.Second)
	}
	return problem.RetryAfter(problem.CodeRateLimited, wait,
		fmt.Errorf("threads: slow mode: %s until this author may post again", wait))
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
