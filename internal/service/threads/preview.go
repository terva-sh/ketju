package threads

import (
	"context"
	"errors"
	"fmt"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/service/member"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// PreviewRequest is what the composer asks to see rendered.
type PreviewRequest struct {
	Source string
	// Thread is the thread the post is being written in, nil for a new
	// thread. With it, quotes resolve against the thread and ReplyTo is
	// checked, as the create path will; without it, quotes are optimistic
	// (render.Context) and ReplyTo cannot be given.
	Thread *threadid.ID
	// ReplyTo is the post being answered, nil for none.
	ReplyTo *uint32
}

// MayPreview reports whether v may preview a post: a member
// (member.Require), because this renders arbitrary input and is not an open
// rendering service. The handler asks this before reading the body, so a
// stranger is refused as one whatever they sent.
func (s *Service) MayPreview(v domain.Viewer) error {
	return member.Require(v, "preview a post")
}

// Preview renders source for v's composer (docs/07 POST /render), with the
// same render the thread page draws a stored post with, so what the preview
// shows is what the post will look like.
//
// A member is required (MayPreview).
//
// With a thread, v must be able to read it, through the same access every
// thread endpoint uses, so a thread hidden from v is not-found here too. Read
// is enough: a preview tells v nothing a reader of the thread cannot see, and
// whether v may reply is the create path's question.
func (s *Service) Preview(ctx context.Context, v domain.Viewer, req PreviewRequest) (dto.RenderResult, error) {
	if err := s.MayPreview(v); err != nil {
		return dto.RenderResult{}, err
	}
	if req.Thread == nil {
		if req.ReplyTo != nil {
			return dto.RenderResult{}, problem.Coded(problem.CodeBadRequest, errors.New("threads: reply_to needs the thread_id it is in"))
		}
		r, err := s.renderSource(ctx, nil, req.Source, nil)
		if err != nil {
			return dto.RenderResult{}, err
		}
		return RenderDTO(r), nil
	}

	id := *req.Thread
	if _, err := s.access(ctx, v, id); err != nil {
		return dto.RenderResult{}, err
	}
	var r render.Result
	err := s.Threads.With(id, func(th *threadcache.Thread) error {
		var err error
		r, err = s.renderSource(ctx, th, req.Source, req.ReplyTo)
		return err
	})
	if err != nil {
		return dto.RenderResult{}, err
	}
	return RenderDTO(r), nil
}

// renderSource renders a post's source as it would be stored in th, or with
// nothing to resolve quotes against when th is nil: quotes resolve through
// postResolver and mentions through the handle lookup, which is exactly the
// context html gives a stored post. The create path (TKT-01M29CA9B9) is meant
// to render through this too, so the three cannot drift.
//
// A mention lookup that failed is an error rather than an unresolved mention,
// for the reason html gives (review 392).
func (s *Service) renderSource(ctx context.Context, th *threadcache.Thread, source string, replyTo *uint32) (render.Result, error) {
	handles := s.handleMemo(ctx)
	rctx := render.Context{
		Handles: render.HandleFunc(func(h string) (int64, string, bool) {
			l := handles.lookup(h)
			return l.ID, l.Canonical, l.Found
		}),
		ReplyTo: replyTo,
	}
	if th != nil {
		rctx.Posts = postResolver(th)
	}
	r, err := render.RenderWith(source, rctx)
	if err == nil {
		err = handles.err
	}
	if err != nil {
		return render.Result{}, fmt.Errorf("threads: rendering a preview: %w", err)
	}
	return r, nil
}
