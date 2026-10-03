// Package blobs serves GET and HEAD /blobs/{hash}, an attachment's bytes
// (docs/07 "Attachments", TKT-01M29CXK0K), /blobs/{hash}/thumb, an image
// attachment's thumbnail (TKT-01M29CYS9W), and /avatars/{hash}, a member's
// avatar (TKT-01M3YSQ4CR), for web.Groups.Blobs.
//
// Who may read a blob is internal/service/blobs's question (Authorize);
// this package asks it on every request, conditional ones included, and
// writes the headers that keep a stored file from acting as a page of this
// origin.
package blobs

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	blobsvc "github.com/terva-sh/ketju/internal/service/blobs"
)

// Policy is a blob response's Content-Security-Policy. It replaces the page
// policy securityHeaders set on the way in: a blob loads nothing and runs
// nothing, and sandbox gives a document opened from it an opaque origin.
const Policy = "default-src 'none'; sandbox"

// New returns the /blobs/ handler.
func New(svc *blobsvc.Service, log *slog.Logger) http.Handler {
	if svc == nil {
		panic("blobs.New: the blob service is required")
	}
	if log == nil {
		log = slog.Default()
	}
	h := &handler{svc: svc, log: log}
	mux := http.NewServeMux()
	// GET also matches HEAD.
	mux.HandleFunc("GET /blobs/{hash}", h.serve)
	mux.HandleFunc("GET /blobs/{hash}/thumb", h.thumb)
	mux.HandleFunc("GET /avatars/{hash}", h.avatar)
	mux.HandleFunc("/avatars/", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotFound)
	})
	mux.HandleFunc("/blobs/", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeNotFound)
	})
	return mux
}

type handler struct {
	svc *blobsvc.Service
	log *slog.Logger
}

// serve answers a blob to a viewer who may read a post naming it, and
// not-found to everyone else, in the same bytes as for a hash nothing has.
//
// Every answer, a refusal included, is private, no-cache and varies by
// cookie: the browser may keep the bytes, but asks again on every use, and
// Authorize runs before http.ServeContent compares If-None-Match, so a
// reader who has lost access, or signed out, gets not-found rather than a
// 304 (docs/07 "Attachments").
func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Cookie")

	hash := r.PathValue("hash")
	b, err := h.svc.Authorize(r.Context(), domain.ViewerFrom(r.Context()), hash)
	if errors.Is(err, blobsvc.ErrNotFound) {
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	if err != nil {
		problem.WriteError(r.Context(), w, r, h.log, err)
		return
	}
	path, err := h.svc.Path(hash)
	if err != nil {
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		// A row with no file: the bytes are gone, which an operator must
		// hear about, and to the reader it is a blob that is not there.
		h.log.ErrorContext(r.Context(), "blobs: a blob's row has no file", "hash", hash)
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	if err != nil {
		problem.WriteError(r.Context(), w, r, h.log, err)
		return
	}
	defer f.Close()

	disposition := "attachment"
	if strings.HasPrefix(b.Mime, "image/") {
		disposition = "inline"
	}
	content(w, r, f, b.Mime, `"`+hash+`"`, disposition)
}

// content writes a served file's headers and the file, by
// http.ServeContent, which answers Range, HEAD and If-None-Match. The
// caller has already authorized the request.
func content(w http.ResponseWriter, r *http.Request, f *os.File, mime, etag, disposition string) {
	hd := w.Header()
	hd.Set("Content-Type", mime)
	hd.Set("ETag", etag)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", Policy)
	hd.Set("Cross-Origin-Resource-Policy", "same-origin")
	hd.Set("Content-Disposition", disposition)
	// No modification time: the hash is the validator, and the bytes under
	// it never change.
	http.ServeContent(w, r, "", time.Time{}, f)
}

// thumb answers an image blob's thumbnail to a viewer the blob would be
// served to, made on this request if it is missing, with the blob's
// headers and "<hash>.thumb" as its ETag. Authorize runs inside Thumb,
// before any 304, as for the blob. Everyone else, a blob that is not an
// image, and an image that could not be thumbnailed get not-found in the
// same bytes as for a hash nothing has; the blob itself is still served.
func (h *handler) thumb(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Cookie")

	hash := r.PathValue("hash")
	f, ctype, err := h.svc.Thumb(r.Context(), domain.ViewerFrom(r.Context()), hash)
	if errors.Is(err, blobsvc.ErrNotFound) {
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	if err != nil {
		problem.WriteError(r.Context(), w, r, h.log, err)
		return
	}
	defer f.Close()
	content(w, r, f, ctype, `"`+hash+`.thumb"`, "inline")
}

// avatar answers an avatar to a signed-in member, and not-found to
// everyone else, in the same bytes as for a hash nothing has: an anonymous
// reader, a pre-user, a suspended account, a hash that is not an account's
// avatar now, and an attachment (docs/05 "Display name and bio", an
// exception to docs/07 "Attachments"). The headers are an attachment's,
// and Authorize runs before any 304, so a reader who signed out gets
// not-found rather than the bytes their browser kept.
func (h *handler) avatar(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Cookie")

	hash := r.PathValue("hash")
	b, err := h.svc.AuthorizeAvatar(r.Context(), domain.ViewerFrom(r.Context()), hash)
	if errors.Is(err, blobsvc.ErrNotFound) {
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	if err != nil {
		problem.WriteError(r.Context(), w, r, h.log, err)
		return
	}
	path, err := h.svc.Path(hash)
	if err != nil {
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		h.log.ErrorContext(r.Context(), "blobs: an avatar's row has no file", "hash", hash)
		problem.Write(w, r, problem.CodeNotFound)
		return
	}
	if err != nil {
		problem.WriteError(r.Context(), w, r, h.log, err)
		return
	}
	defer f.Close()
	content(w, r, f, b.Mime, `"`+hash+`"`, "inline")
}
