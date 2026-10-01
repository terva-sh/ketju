package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/web"
)

// Agents on the API (TKT-01M29P9RSW, docs/07 "Agents on the API", docs/08
// "Rate limits").
//
// Every write a bearer credential makes is charged to its agent's token
// bucket: agents.write_burst writes at once, refilling one every
// agents.write_refill_seconds, both overridable per agent. Over it the
// answer is 429 with Retry-After, and the handler does not run, so nothing
// is appended. Reads are not charged, and nor are the writes that only
// record the agent's own viewer state: the read position, drafts, and
// watches and mutes (TKT-01M29CRJ29). Charging those would throttle an
// agent for reading. A member
// signed in with a cookie is never charged here; docs/08 leaves humans
// unlimited but for the per-address ceiling, which still applies to agents.
//
// The bucket wraps each route as build registers it, rather than sitting in
// the middleware chain, for two reasons: a stage the server has to be
// wired with is a stage a caller can leave out, and whether a request is
// exempt is then a property of the route it reached, not of a path parsed a
// second time.

// agentCharge is the per-agent limiter's key: the credential's agent, at
// the credential's rate, for a request a bearer authenticated.
func agentCharge(r *http.Request) (string, web.Rate, bool) {
	c, ok := agentcred.From(r.Context())
	if !ok {
		return "", web.Rate{}, false
	}
	return c.Key(), web.Rate{Burst: c.Rate.Burst, Every: c.Rate.Refill}, true
}

// unchargedPaths are the route paths whose writes an agent's bucket does
// not pay for.
var unchargedPaths = []string{
	"/api/v1/threads/{id}/read",
	"/api/v1/drafts/{thread_id}",
	"/api/v1/forums/{slug}/draft",
	"/api/v1/threads/{id}/watch",
	"/api/v1/forums/{slug}/watch",
}

// charged is fn behind the agent's bucket, unless method does not write or
// path is one of unchargedPaths.
func (h *handlers) charged(method, path string, fn http.HandlerFunc) http.HandlerFunc {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return fn
	}
	for _, p := range unchargedPaths {
		if p == path {
			return fn
		}
	}
	return h.agentLimit(fn).ServeHTTP
}

// registerAgents registers the admin routes that provision agents. Each is
// an admin's, and each answer is no-store: the credential routes carry the
// only copy of a token there will be.
func (h *handlers) registerAgents(mux *http.ServeMux) {
	h.adminRoute(mux, "POST /api/v1/admin/users", adminGate, h.adminCreateUser)
	h.adminRoute(mux, "POST /api/v1/admin/users/{handle}/credential", adminGate, h.adminIssueCredential)
	h.adminRoute(mux, "DELETE /api/v1/admin/users/{handle}/credential", adminGate, h.adminRevokeCredential)
	h.adminRoute(mux, "PUT /api/v1/admin/users/{handle}/agent-limits", adminGate, h.adminAgentLimits)
}

// adminCreateUser is POST /api/v1/admin/users with {kind, handle,
// display_name, issue_credential}, answered 201. Only an agent is created
// this way: a human arrives by signing in, so any other kind is
// validation-failed.
func (h *handlers) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind            string `json:"kind"`
		Handle          string `json:"handle"`
		DisplayName     string `json:"display_name"`
		IssueCredential bool   `json:"issue_credential"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	if in.Kind != domain.KindAgent {
		h.fail(w, r, problem.Coded(problem.CodeValidationFailed,
			errors.New("api: only an agent is created here; a person signs in")))
		return
	}
	res, err := h.users.CreateAgent(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), users.NewAgent{
		Handle: in.Handle, DisplayName: in.DisplayName, IssueCredential: in.IssueCredential})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", "/api/v1/admin/users/"+url.PathEscape(res.User.User.User.Handle))
	writeJSON(w, http.StatusCreated, res)
}

// adminIssueCredential is POST /api/v1/admin/users/{handle}/credential,
// answered 201 with the token.
func (h *handlers) adminIssueCredential(w http.ResponseWriter, r *http.Request) {
	res, err := h.users.IssueCredential(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), r.PathValue("handle"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, res)
}

// adminRevokeCredential is DELETE /api/v1/admin/users/{handle}/credential,
// answered 204.
func (h *handlers) adminRevokeCredential(w http.ResponseWriter, r *http.Request) {
	if err := h.users.RevokeCredential(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), r.PathValue("handle")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// adminAgentLimits is PUT /api/v1/admin/users/{handle}/agent-limits with
// {burst, refill_seconds}; null is the site default. Both keys are
// required, so leaving one out is not read as resetting it.
func (h *handlers) adminAgentLimits(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Burst         json.RawMessage `json:"burst"`
		RefillSeconds json.RawMessage `json:"refill_seconds"`
	}
	if err := decodeStrict(r.Body, &in); err != nil {
		h.fail(w, r, err)
		return
	}
	burst, err := optionalInt("burst", in.Burst)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	refill, err := optionalInt("refill_seconds", in.RefillSeconds)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.users.SetAgentLimits(r.Context(), domain.ViewerFrom(r.Context()), web.ClientIP(r), r.PathValue("handle"),
		burst, refill)
	answer(h, w, r, res, err)
}

// optionalInt reads a required key that is a JSON integer or null. Absent
// is bad-request; null is nil.
func optionalInt(name string, raw json.RawMessage) (*int, error) {
	if raw == nil {
		return nil, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the limits have no %s", name))
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: %s is not a whole number or null", name))
	}
	return &n, nil
}
