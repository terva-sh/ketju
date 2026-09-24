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

// renderRequest is the body of POST /api/v1/render (docs/07).
type renderRequest struct {
	Source   *string `json:"source"`
	ThreadID *string `json:"thread_id"`
	ReplyTo  *uint32 `json:"reply_to"`
}

// render is POST /api/v1/render: the composer preview. Who may ask and what
// it renders against are the threads service's to decide (Preview); this
// parses the body and writes the answer.
//
// The body limit and the per-address write limiter are the chain's, applied
// to every mutating API route before this runs (internal/web), so nothing
// here repeats them.
//
// The answer is the viewer's own and is about a body no URL names, so no
// cache keeps it.
func (h *handlers) render(w http.ResponseWriter, r *http.Request) {
	req, err := renderBody(r.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Preview(r.Context(), domain.ViewerFrom(r.Context()), req)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// renderBody parses the request strictly: an unknown field, a missing
// source, trailing data after the object and a malformed thread id are all
// the client's mistake and say so, rather than rendering something other
// than what was meant. A body cut off by the chain's MaxBytesReader keeps
// its *http.MaxBytesError, which problem maps to payload-too-large.
func renderBody(body io.Reader) (threads.PreviewRequest, error) {
	var (
		in  renderRequest
		out threads.PreviewRequest
	)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return out, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the render body: %w", err))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("more than one JSON value")
		}
		return out, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the render body: %w", err))
	}
	if in.Source == nil {
		return out, problem.Coded(problem.CodeBadRequest, errors.New("api: the render body has no source"))
	}
	out.Source, out.ReplyTo = *in.Source, in.ReplyTo
	if in.ThreadID != nil {
		id, err := threadid.Parse(*in.ThreadID)
		if err != nil {
			return out, err
		}
		out.Thread = &id
	}
	return out, nil
}
