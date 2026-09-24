// Package api is the JSON API under /api/v1 (docs/07 "Resources (v1)").
//
// Handlers here parse a request, call a service, and write a dto value or a
// problem. They hold no rules of their own: what a thread page contains is
// internal/service/threads's to decide, so the HTML client can ask the same
// question and get the same answer.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// Deps are the services the handlers call.
type Deps struct {
	Threads *threads.Service
	Logger  *slog.Logger
}

// New returns the /api/v1 handler, for web.Groups.API. It receives full
// paths, so routes are registered as docs/07 writes them.
func New(d Deps) http.Handler {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	h := &handlers{threads: d.Threads, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/threads/{id}", h.thread)
	mux.HandleFunc("GET /api/v1/threads/{id}/posts/{ordinal}", h.post)
	mux.HandleFunc("GET /api/v1/threads/{id}/posts/{ordinal}/replies", h.replies)
	mux.HandleFunc("GET /api/v1/threads/{id}/posts/{ordinal}/history", h.history)
	mux.Handle("/api/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotFound)
	}))
	return mux
}

type handlers struct {
	threads *threads.Service
	log     *slog.Logger
}

// thread is GET /api/v1/threads/{id}?page=&ordinal=&lens=&after_log_length=
// (docs/07; ordinal is the permalink parameter, added in TKT-01M29CA9AX).
func (h *handlers) thread(w http.ResponseWriter, r *http.Request) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	q, err := threadQuery(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	res, err := h.threads.View(r.Context(), domain.ViewerFrom(r.Context()), id, q, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

// post, replies and history are GET /api/v1/threads/{id}/posts/{ordinal} and
// its /replies and /history (docs/07).
func (h *handlers) post(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Post(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

func (h *handlers) replies(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.Replies(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	id, ordinal, err := postPath(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.threads.History(r.Context(), domain.ViewerFrom(r.Context()), id, ordinal, r.Header.Get("If-None-Match"))
	respond(h, w, r, res, err)
}

// respond writes a served body, a 304, or the error.
//
// Vary: Cookie because every body here depends on who is asking, and
// no-cache because every use must revalidate: the ETag makes that a 304 when
// nothing moved, and without it a browser would reuse a response that the
// next post made stale.
func respond[T any](h *handlers, w http.ResponseWriter, r *http.Request, res threads.Served[T], err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	hd := w.Header()
	hd.Set("ETag", res.ETag)
	hd.Set("Vary", "Cookie")
	hd.Set("Cache-Control", "private, no-cache")
	if res.NotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, res.Body)
}

// postPath parses {id} and {ordinal}.
func postPath(r *http.Request) (threadid.ID, uint32, error) {
	id, err := threadid.Parse(r.PathValue("id"))
	if err != nil {
		return 0, 0, err
	}
	s := r.PathValue("ordinal")
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n < 1 {
		return 0, 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: ordinal %q is not a positive integer", s))
	}
	return id, uint32(n), nil
}

// threadQuery parses the page parameters. A malformed one is a client bug
// and says so, rather than being read as absent; that includes a parameter
// given with no value, such as ?page=, which URL.Query().Get cannot tell from
// one not given at all (review 394).
func threadQuery(r *http.Request) (threads.Query, error) {
	v := r.URL.Query()
	var q threads.Query
	positive := func(name string) (int64, error) {
		if !v.Has(name) {
			return 0, nil
		}
		s := v.Get(name)
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil || n < 1 {
			return 0, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: %s=%q is not a positive integer", name, s))
		}
		return n, nil
	}
	page, err := positive("page")
	if err != nil {
		return q, err
	}
	ordinal, err := positive("ordinal")
	if err != nil {
		return q, err
	}
	q.Page, q.Ordinal, q.Lens = int(page), uint32(ordinal), v.Get("lens")
	if v.Has("after_log_length") {
		s := v.Get("after_log_length")
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			return q, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: after_log_length=%q is not a length", s))
		}
		q.After, q.HasAfter = n, true
	}
	return q, nil
}

func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	problem.WriteError(r.Context(), w, r, h.log, err)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		// A dto value that will not marshal is a programming error, and the
		// status line has not gone out yet, so it can still be a 500. The
		// ETag set for the page it failed to write must not go with it.
		w.Header().Del("ETag")
		problem.Write(w, nil, problem.CodeInternal)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}
