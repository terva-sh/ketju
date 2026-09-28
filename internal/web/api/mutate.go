package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// editRequest is the body of PATCH /api/v1/threads/{id}/posts/{ordinal}
// (docs/07).
type editRequest struct {
	Source *string `json:"source"`
	Reason string  `json:"reason"`
}

// reasonRequest is the optional body of DELETE /api/v1/threads/{id}/posts/
// {ordinal} and of POST …/restore.
type reasonRequest struct {
	Reason string `json:"reason"`
}

// editPost, deletePost and restorePost change an existing post, each by
// appending one record (TKT-01M29CA9BP). The rules are the threads
// service's; these ask whether the viewer may write at all first, as reply
// does, then parse and answer 200 with the post as the viewer now reads it.
// Like reply, the answer is the viewer's own and is not cached.
func (h *handlers) editPost(w http.ResponseWriter, r *http.Request) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayWrite(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in editRequest
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Source == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the edit has no source")))
		return
	}
	res, err := h.threads.Edit(r.Context(), v, id, ordinal, threads.EditRequest{Source: *in.Source, Reason: in.Reason})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

func (h *handlers) deletePost(w http.ResponseWriter, r *http.Request) {
	h.setDeleted(w, r, h.threads.Delete)
}

func (h *handlers) restorePost(w http.ResponseWriter, r *http.Request) {
	h.setDeleted(w, r, h.threads.Restore)
}

func (h *handlers) setDeleted(w http.ResponseWriter, r *http.Request,
	do func(context.Context, domain.Viewer, threadid.ID, uint32, string) (dto.CreatedPost, error)) {
	v := domain.ViewerFrom(r.Context())
	if err := h.threads.MayWrite(v); err != nil {
		h.fail(w, r, err)
		return
	}
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	in, err := optionalReason(r.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := do(r.Context(), v, id, ordinal, in.Reason)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// maxReasonBody bounds an optional {reason} body, well under the chain's
// post-sized limit: a reason is a line, and nothing here needs more.
const maxReasonBody = 4 << 10

// optionalReason reads a {reason} body, or none at all: a DELETE usually has
// no body, and a client should not have to send "{}" to get one.
func optionalReason(body io.Reader) (reasonRequest, error) {
	var in reasonRequest
	err := optionalJSON(body, maxReasonBody, &in)
	return in, err
}

// optionalJSON reads one JSON object into v, or leaves v alone when the body
// is empty or only whitespace.
//
// It streams rather than buffering the body first, and stops at max bytes,
// so a large or endless body costs no more than that (review 1119 on PR
// #95). Leading whitespace is skipped to tell an empty body from a JSON one;
// decodeStrict then reads the rest.
func optionalJSON(body io.Reader, max int64, v any) error {
	lr := &io.LimitedReader{R: body, N: max + 1}
	br := bufio.NewReader(lr)
	for {
		c, err := br.ReadByte()
		if errors.Is(err, io.EOF) {
			if lr.N <= 0 {
				return problem.Coded(problem.CodePayloadTooLarge, errors.New("api: the request body is too large"))
			}
			return nil
		}
		if err != nil {
			return problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the request body: %w", err))
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
	}
	if err := br.UnreadByte(); err != nil {
		return err
	}
	err := decodeStrict(br, v)
	if lr.N <= 0 {
		return problem.Coded(problem.CodePayloadTooLarge, errors.New("api: the request body is too large"))
	}
	return err
}
