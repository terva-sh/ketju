package threads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// Attachments are blob hashes. No upload exists yet, so any is refused.
	Attachments []string
}

// CreateThreadRequest is a new thread and its first post.
type CreateThreadRequest struct {
	Title  string
	Source string
	// Tags must be empty: nothing yet says what a tag may be, and a log keeps
	// whatever it is given forever, so none is accepted until something does.
	Tags []string
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
	if err := checkPost(req.Source, req.Attachments); err != nil {
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
		res, err = s.Writer.Append(th, threadlog.Append{
			Type: threadlog.TypePost, Flags: flags, Author: uint64(v.UserID),
			Body: postBody(req.Source, r), TimeMS: now.UnixMilli(),
		})
		return err
	})
	if err != nil {
		return out, err
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
	if len(req.Tags) > 0 {
		return out, problem.Coded(problem.CodeValidationFailed, errors.New("threads: tags are not accepted yet"))
	}
	if err := checkPost(req.Source, nil); err != nil {
		return out, err
	}
	// No thread to quote from or reply to yet: this is for the render's
	// errors and mentions. The post is drawn against its thread when read.
	r, err := s.renderSource(ctx, nil, req.Source, nil)
	if err != nil {
		return out, err
	}
	flags, err := s.authorFlags(ctx, v)
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
			Body: &threadlog.ThreadCreatedBody{Title: title, Slug: Slug(title), ForumID: uint64(forum.ID), Tags: []string{}},
		}); err != nil {
			return threadlog.Result{}, err
		}
		return s.Writer.Append(th, threadlog.Append{
			Type: threadlog.TypePost, Flags: flags, Author: uint64(v.UserID), TimeMS: now,
			Body: postBody(req.Source, r),
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

// checkPost refuses a post with nothing to show, and any attachment, since
// nothing can upload one yet.
func checkPost(source string, attachments []string) error {
	if strings.TrimSpace(source) == "" {
		return problem.Coded(problem.CodeValidationFailed, errors.New("threads: the post is empty"))
	}
	if len(attachments) > 0 {
		return problem.Coded(problem.CodeValidationFailed, errors.New("threads: attachments are not accepted yet"))
	}
	return nil
}

// postBody is what the log records of a post: its source, and the facts the
// render validated. Mentions are user ids in document order, each once, as
// the render reports them.
func postBody(source string, r render.Result) *threadlog.PostBody {
	b := &threadlog.PostBody{Source: source, Mentions: []uint64{}}
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
	// says to is never early.
	wait = (wait + time.Second - 1).Truncate(time.Second)
	return problem.RetryAfter(problem.CodeRateLimited, wait,
		fmt.Errorf("threads: slow mode: %s until this author may post again", wait))
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
