package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/applications"
	"github.com/terva-sh/ketju/internal/web"
)

// The application routes (docs/07 "Resources (v1)"): an approval-mode
// applicant reads the form, submits it, reads its status, and withdraws it.
// A pre-user session reaches them through web.PreUserRoutes, and
// internal/service/applications refuses anyone else: anonymous is
// unauthenticated and a member forbidden. Every answer is no-store: it is
// the applicant's own and changes with every decision.
//
// The two writes are limited per identity by web.KeyedLimit, after session
// resolution, on top of the per-address stage every mutating API request
// meets: a script holding one pre-user session is one caller however many
// addresses it sends from.

// applicantKey keys the per-identity stage: the identity a pre-user session
// is bound to. Anyone else has no key and passes, to be refused by the
// service; members get no per-session limit (docs/08).
func applicantKey(r *http.Request) (string, bool) {
	v := domain.ViewerFrom(r.Context())
	if !v.PreUser || v.IdentityID == 0 {
		return "", false
	}
	return "identity:" + strconv.FormatInt(v.IdentityID, 10), true
}

// limitApplicant wraps a handler in the per-identity stage the two
// application writes share.
func (h *handlers) limitApplicant(next http.HandlerFunc) http.HandlerFunc {
	return h.applyLimit(next).ServeHTTP
}

// applicationForm is GET /api/v1/apply/form.
func (h *handlers) applicationForm(w http.ResponseWriter, r *http.Request) {
	out, err := h.applications.Form(r.Context(), domain.ViewerFrom(r.Context()))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// submitApplication is POST /api/v1/apply, taking {version, answers}. It
// answers 201 with the application's status.
func (h *handlers) submitApplication(w http.ResponseWriter, r *http.Request) {
	// Who may ask first, then what was asked: an anonymous or member
	// request is 401 or 403 whatever its body (review 1425).
	if err := applications.Applicant(domain.ViewerFrom(r.Context())); err != nil {
		h.fail(w, r, err)
		return
	}
	var in struct {
		Version *int                       `json:"version"`
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Version == nil || in.Answers == nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, errors.New("api: version and answers are required")))
		return
	}
	if *in.Version < 1 {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: version %d is not a form version", *in.Version)))
		return
	}
	out, err := h.applications.Submit(r.Context(), domain.ViewerFrom(r.Context()), *in.Version, in.Answers)
	h.writeNoStore(w, r, http.StatusCreated, out, err)
}

// applicationStatus is GET /api/v1/apply/status.
func (h *handlers) applicationStatus(w http.ResponseWriter, r *http.Request) {
	out, err := h.applications.Status(r.Context(), domain.ViewerFrom(r.Context()))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// withdrawApplication is POST /api/v1/apply/withdraw, with an empty body or
// {}. It answers 200 with the status that follows.
//
// The body is read whole, rather than judged by Content-Length, because a
// chunked request has no length and may still be empty (review 1425). The
// body limit in the middleware chain bounds the read.
func (h *handlers) withdrawApplication(w http.ResponseWriter, r *http.Request) {
	if err := applications.Applicant(domain.ViewerFrom(r.Context())); err != nil {
		h.fail(w, r, err)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the request body: %w", err)))
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		var in struct{}
		if err := decodeStrict(bytes.NewReader(body), &in); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	out, err := h.applications.Withdraw(r.Context(), domain.ViewerFrom(r.Context()))
	h.writeNoStore(w, r, http.StatusOK, out, err)
}

// writeNoStore writes v, or the error, as no-store: a refusal of the
// applicant's is as much theirs as an answer.
func (h *handlers) writeNoStore(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, status, v)
}

// applyRoutes are the application routes, for routes.
func (h *handlers) applyRoutes() []web.Route {
	return []web.Route{
		{Method: http.MethodGet, Path: "/api/v1/apply/form", Handler: h.applicationForm},
		{Method: http.MethodPost, Path: "/api/v1/apply", Handler: h.limitApplicant(h.submitApplication)},
		{Method: http.MethodGet, Path: "/api/v1/apply/status", Handler: h.applicationStatus},
		{Method: http.MethodPost, Path: "/api/v1/apply/withdraw", Handler: h.limitApplicant(h.withdrawApplication)},
	}
}
