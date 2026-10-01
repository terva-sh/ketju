package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// replyRequest is the body of POST /api/v1/threads/{id}/posts (docs/07).
type replyRequest struct {
	Source      *string  `json:"source"`
	ReplyTo     *uint32  `json:"reply_to"`
	Attachments []string `json:"attachments"`
}

// threadRequest is the body of POST /api/v1/forums/{slug}/threads (docs/07).
type threadRequest struct {
	Title       *string  `json:"title"`
	Source      *string  `json:"source"`
	Tags        []string `json:"tags"`
	Attachments []string `json:"attachments"`
}

// reply is POST /api/v1/threads/{id}/posts and createThread is POST
// /api/v1/forums/{slug}/threads: the two ways a member writes. The rules are
// the threads service's (Reply, CreateThread); these ask whether the viewer
// may write at all first, as render does, then parse and answer.
//
// The body limit and the per-address write limiter are the chain's, as for
// render. The answer is 201, is the viewer's own, and is not cached.
func (h *handlers) reply(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayWrite(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in replyRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Source == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the post body has no source")))
		return
	}
	res, err := h.threads.Reply(r.Context(), v, id, threads.ReplyRequest{
		Source: *in.Source, ReplyTo: in.ReplyTo, Attachments: in.Attachments,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", fmt.Sprintf("/api/v1/threads/%s/posts/%d", id, res.Ordinal))
	writeJSON(w, http.StatusCreated, res)
}

func (h *handlers) createThread(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayWrite(v); err != nil {
		h.fail(w, r, err)
		return
	}
	var in threadRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Title == nil || in.Source == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the thread body needs a title and a source")))
		return
	}
	res, err := h.threads.CreateThread(r.Context(), v, r.PathValue("slug"), threads.CreateThreadRequest{
		Title: *in.Title, Source: *in.Source, Tags: in.Tags, Attachments: in.Attachments,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/api/v1/threads/"+res.Thread.ID)
	writeJSON(w, http.StatusCreated, res)
}

// decodeStrict reads one JSON object into v, refusing an unknown field and
// anything after the object, for the reason renderBody gives. A body cut off
// by the chain's MaxBytesReader keeps its *http.MaxBytesError.
func decodeStrict(body io.Reader, v any) error {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the request body: %w", err))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("more than one JSON value")
		}
		return problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the request body: %w", err))
	}
	return nil
}
