package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// The thread moderation routes (docs/07 "Moderation and admin",
// TKT-01M29CKRGS). Thread moderation follows the forum grant (docs/08
// "Decided 2026-09-30", TKT-01M3RQP0TG), so each is behind the moderates
// gate, which admits a member who moderates any forum, by role or through a
// group. The threads service then decides the forum at the log length the
// record is appended at: moderate in the thread's own forum, and for a move
// in the destination too (TKT-01M29CKRH9). A global pin stays a moderator's
// or admin's by role. Past the gate the handlers only parse the path and the
// body, so a bad-request says nothing about the thread. Each appends one
// record, answers 200 with the thread's header as its page now serves it,
// and is no-store.
//
// Renaming is not here: an author may rename their own thread, so it is
// PATCH /api/v1/threads/{id} (renameThread), and the service decides who
// may.
func (h *handlers) registerModeration(mux *http.ServeMux) {
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/lock", moderatesGate, h.lockThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/unlock", moderatesGate, h.unlockThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/pin", moderatesGate, h.pinThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/unpin", moderatesGate, h.unpinThread)
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/move", moderatesGate, h.moveThread)
	h.adminRoute(mux, "PUT /api/v1/admin/threads/{id}/tags", moderatesGate, h.retagThread)
	// A split needs moderate in the source's forum and the new thread's,
	// which the service decides (TKT-01M29CYSAM).
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/split", moderatesGate, h.splitPosts)
	// Archiving is an admin's alone (docs/08 "Primitives"), so it keeps the
	// admin gate (TKT-01M29CRJ64).
	h.adminRoute(mux, "POST /api/v1/admin/threads/{id}/archive", adminGate, h.archiveThread)
}

// archiveThread is POST /api/v1/admin/threads/{id}/archive with {reason?},
// or no body, as a lock takes (docs/07 "Archiving threads").
func (h *handlers) archiveThread(w http.ResponseWriter, r *http.Request) {
	h.setLocked(w, r, h.threads.Archive)
}

// retagRequest is the body of PUT …/tags: the thread's whole new tag set,
// read by decodeTags.
type retagRequest struct {
	Tags json.RawMessage `json:"tags"`
}

// retagThread is PUT /api/v1/admin/threads/{id}/tags with {tags}, a
// moderator's retag (docs/07 "Retagging threads", TKT-01M29CYSCP). A body
// without tags, or with tags null, is bad-request: an empty list is how a
// retag removes every tag, so nothing is taken to mean it.
func (h *handlers) retagThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in retagRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Tags == nil || bytes.Equal(in.Tags, []byte("null")) {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the retag has no tags")))
		return
	}
	tags, err := decodeTags(in.Tags)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Retag(r.Context(), domain.ViewerFrom(r.Context()), id, tags)
	h.answerHeader(w, r, res, err)
}

// renameRequest is the body of PATCH /threads/{id}.
type renameRequest struct {
	Title *string `json:"title"`
}

// moveRequest is the body of POST …/move: the destination forum's slug.
type moveRequest struct {
	Forum *string `json:"forum"`
}

// renameThread is PATCH /api/v1/threads/{id} with {title}, for the thread's
// author within threads.rename_window_seconds and for a moderator at any
// time (docs/07, TKT-01M29CKRH9). A stranger is refused as one before the
// body is read, as for an edit.
func (h *handlers) renameThread(w http.ResponseWriter, r *http.Request) {
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
	var in renameRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Title == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the rename has no title")))
		return
	}
	res, err := h.threads.Rename(r.Context(), v, id, *in.Title)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) moveThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in moveRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Forum == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the move has no forum")))
		return
	}
	res, err := h.threads.Move(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Forum)
	h.answerHeader(w, r, res, err)
}

// maxSplitBody bounds a split's body: room for threadlog.MaxMoveOrdinals
// ordinals of the longest a thread can hold, a title and a forum slug.
const maxSplitBody = 128 << 10

// splitRequest is the body of POST …/split: the posts to move, by ordinal,
// and the new thread's title and forum, by slug.
type splitRequest struct {
	Ordinals []uint32 `json:"ordinals"`
	Title    *string  `json:"title"`
	Forum    *string  `json:"forum"`
}

// splitPosts is POST /api/v1/admin/threads/{id}/split with {ordinals, title,
// forum}: a moderator's split of posts into a new thread (docs/07 "Moving
// posts", TKT-01M29CYSAM). Each field is required; the service judges the
// posts, the title as a new thread's, and moderate in both forums. It
// answers 200 with both threads' headers, no-store.
func (h *handlers) splitPosts(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in splitRequest
	if err := optionalJSON(r.Body, maxSplitBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Ordinals == nil || in.Title == nil || in.Forum == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: a split needs ordinals, a title and a forum")))
		return
	}
	res, err := h.threads.Split(r.Context(), domain.ViewerFrom(r.Context()), id, in.Ordinals, *in.Forum, *in.Title)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}

// pinRequest is the body of POST …/pin. Until is Unix ms, null for no end.
type pinRequest struct {
	Scope *string `json:"scope"`
	Until *int64  `json:"until"`
}

// unpinRequest is the body of POST …/unpin.
type unpinRequest struct {
	Scope *string `json:"scope"`
}

func (h *handlers) lockThread(w http.ResponseWriter, r *http.Request) {
	h.setLocked(w, r, h.threads.Lock)
}

func (h *handlers) unlockThread(w http.ResponseWriter, r *http.Request) {
	h.setLocked(w, r, h.threads.Unlock)
}

// setLocked is lock, unlock and archive: an optional {reason} body, as a
// delete takes.
func (h *handlers) setLocked(w http.ResponseWriter, r *http.Request,
	do func(context.Context, domain.Viewer, threadid.ID, string) (dto.ThreadHeader, error)) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	in, err := optionalReason(r.Body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := do(r.Context(), domain.ViewerFrom(r.Context()), id, in.Reason)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) pinThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in pinRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Scope == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the pin has no scope")))
		return
	}
	var until *time.Time
	if in.Until != nil {
		t := time.UnixMilli(*in.Until)
		until = &t
	}
	res, err := h.threads.Pin(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Scope, until)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) unpinThread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var in unpinRequest
	if err := optionalJSON(r.Body, maxReasonBody, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Scope == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: the unpin has no scope")))
		return
	}
	res, err := h.threads.Unpin(r.Context(), domain.ViewerFrom(r.Context()), id, *in.Scope)
	h.answerHeader(w, r, res, err)
}

func (h *handlers) answerHeader(w http.ResponseWriter, r *http.Request, res dto.ThreadHeader, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, res)
}
