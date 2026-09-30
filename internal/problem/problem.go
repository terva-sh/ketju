// Package problem is the API's error vocabulary: one catalogue of stable
// codes, one table mapping each to a status and a title, and one way to write
// them.
//
// It exists because the alternative is error strings scattered through
// handlers. docs/07 promises clients a stable `type` they can switch on, and a
// promise kept in fifty call sites is a promise that breaks the first time
// somebody writes "not_found" where the rest of the code writes "not-found".
// Here, a code that is not in the catalogue does not compile into a response.
//
// # Nothing from an error reaches the client
//
// The domain errors this maps from say things like "threadlog: checksum
// mismatch" and, once the database lands, will say things like "UNIQUE
// constraint failed: identities.provider_key". That text names the schema to
// anyone who asks, and means nothing to a member. So no function here reads
// err.Error() into a response body. The error goes to the log, where it is
// useful; the client gets the catalogue's fixed title for the mapped code.
// That is a structural guarantee rather than a rule to remember: WriteError
// takes the error and the logger together, and the only text it can put in the
// body is a constant from this file.
package problem

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// ContentType is the media type RFC 9457 defines for a problem detail.
const ContentType = "application/problem+json"

// Code is a stable, machine-readable error code. It is a defined type rather
// than a string so that a handler cannot invent one at a call site: the only
// values that exist are the constants below, and Write rejects any code that
// is not in the catalogue.
//
// RFC 9457 types the `type` member as a URI reference, and each of these is a
// valid relative one. docs/07 asks for "a stable type code the client can
// switch on", and a short relative reference is both that and conformant --
// clients compare it as an opaque string, which is what they would do with an
// absolute URL anyway.
type Code string

// The catalogue. These strings are API surface: a client switches on them, and
// the future HTML client turns them into flash messages. Renaming one is a
// breaking change, which is the reason they are gathered here where that is
// obvious rather than spread across handlers where it would not be.
const (
	// CodeBadRequest is a malformed request: unparseable JSON, a parameter
	// that is not a number, a thread id that is not base32.
	CodeBadRequest Code = "bad-request"

	// CodeValidationFailed is a well-formed request that asks for something
	// the domain refuses -- an empty post body, a title over the limit. It is
	// 422 rather than 400 so a client can tell "I sent nonsense" from "you
	// sent something I understood and will not do".
	CodeValidationFailed Code = "validation-failed"

	// CodeUnauthenticated means no viewer. The client's response is to send
	// the reader to a provider, so it must be distinguishable from forbidden.
	CodeUnauthenticated Code = "unauthenticated"

	// CodeForbidden means there is a viewer and they may not do this. The
	// client's response is to say so, not to offer a sign-in.
	CodeForbidden Code = "forbidden"

	// CodeEditWindowClosed and CodeDeleteWindowClosed are forbidden, named:
	// the post is the viewer's, and the site's window for editing it, or for
	// deleting and restoring it, closed (docs/08 "Primitives",
	// TKT-01M29CKRHS). A client says which window, and does not offer the
	// action again.
	CodeEditWindowClosed   Code = "edit-window-closed"
	CodeDeleteWindowClosed Code = "delete-window-closed"

	// CodeNotFound covers both "no such route" and "no such object". They are
	// deliberately the same code: telling an unauthorized reader which thread
	// ids exist is a disclosure, and the cheapest way not to do that is to
	// have one answer.
	CodeNotFound Code = "not-found"

	// CodeMethodNotAllowed is the route with the wrong verb.
	CodeMethodNotAllowed Code = "method-not-allowed"

	// CodeConflict is a request that lost a race or met the object's state:
	// posting to a closed thread, editing a post another write moved.
	CodeConflict Code = "conflict"

	// CodeGone is an object that existed and was deleted. Distinct from
	// not-found because a permalink to a deleted post should say what
	// happened rather than imply it never existed.
	CodeGone Code = "gone"

	// CodeUnsupportedMediaType is a mutating request whose Content-Type is not
	// JSON. TKT-01M29C800X makes this refusal part of the CSRF posture: the
	// API accepting only JSON is what makes a cross-origin form post unable to
	// reach it at all.
	CodeUnsupportedMediaType Code = "unsupported-media-type"

	// CodePayloadTooLarge is a body over its limit -- 64 KB for a post source,
	// the configurable attachment cap for an upload (docs/07).
	CodePayloadTooLarge Code = "payload-too-large"

	// CodeRateLimited is the token bucket saying no. It travels with
	// Retry-After; see WriteRetryAfter.
	CodeRateLimited Code = "rate-limited"

	// CodeNotImplemented is an endpoint this build does not serve. It is not
	// 404: during Phase 1 most handler groups do not exist, and "not built
	// yet" is a different fact from "no such endpoint" to both a developer and
	// a monitoring check.
	CodeNotImplemented Code = "not-implemented"

	// CodeUnavailable is a dependency that is down or draining -- the thread
	// cache closed, the event bus shut down. Retryable, unlike CodeInternal.
	CodeUnavailable Code = "unavailable"

	// CodeInternal is the bug bucket. Anything unmapped becomes this, so a new
	// domain error is reported as a server fault rather than leaking its text
	// while nobody has decided what it means.
	CodeInternal Code = "internal"
)

// entry is what the catalogue stores for a code.
type entry struct {
	// Status is the HTTP status. This table is the single place a code becomes
	// a status, which is the whole of AC2: a handler names what went wrong and
	// never picks a number.
	Status int

	// Title is fixed text. It never interpolates anything from the request: an
	// error body is reflected straight back to whoever sent it, and echoing
	// input is how a helpful message becomes a way to probe the server.
	Title string
}

var catalogue = map[Code]entry{
	CodeBadRequest:           {http.StatusBadRequest, "The request could not be understood."},
	CodeValidationFailed:     {http.StatusUnprocessableEntity, "The request was understood but is not valid."},
	CodeUnauthenticated:      {http.StatusUnauthorized, "You are not signed in."},
	CodeForbidden:            {http.StatusForbidden, "You may not do that."},
	CodeEditWindowClosed:     {http.StatusForbidden, "The time for editing this post has passed."},
	CodeDeleteWindowClosed:   {http.StatusForbidden, "The time for deleting or restoring this post has passed."},
	CodeNotFound:             {http.StatusNotFound, "No such endpoint or object."},
	CodeMethodNotAllowed:     {http.StatusMethodNotAllowed, "That method is not allowed here."},
	CodeConflict:             {http.StatusConflict, "That conflicts with the current state."},
	CodeGone:                 {http.StatusGone, "That has been deleted."},
	CodeUnsupportedMediaType: {http.StatusUnsupportedMediaType, "This endpoint accepts JSON only."},
	CodePayloadTooLarge:      {http.StatusRequestEntityTooLarge, "That is larger than the limit."},
	CodeRateLimited:          {http.StatusTooManyRequests, "Too many requests. Try again shortly."},
	CodeNotImplemented:       {http.StatusNotImplemented, "This server does not implement that yet."},
	CodeUnavailable:          {http.StatusServiceUnavailable, "That is temporarily unavailable."},
	CodeInternal:             {http.StatusInternalServerError, "The server failed to handle this request."},
}

// Codes returns every code in the catalogue, sorted. Used by the golden test
// and by the documentation generator, both of which need the catalogue to be
// enumerable rather than merely present.
func Codes() []Code {
	out := make([]Code, 0, len(catalogue))
	for c := range catalogue {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Status returns the HTTP status for a code, and whether the code is in the
// catalogue.
func Status(c Code) (int, bool) {
	e, ok := catalogue[c]
	return e.Status, ok
}

// Title returns the fixed title for a code, and whether the code is in the
// catalogue.
func Title(c Code) (string, bool) {
	e, ok := catalogue[c]
	return e.Title, ok
}

// Problem is the wire shape, RFC 9457 §3.1.
type Problem struct {
	// Type is the stable code. Clients switch on it.
	Type Code `json:"type"`

	// Title is the catalogue's fixed text for Type.
	Title string `json:"title"`

	// Status repeats the HTTP status, per RFC 9457, so a body that has been
	// copied out of its response is still self-describing.
	Status int `json:"status"`

	// Errors lists what was wrong, field by field, on a validation-failed
	// body whose error says so: a form's answers or a form schema
	// (TKT-01M29CP2KE). It is an RFC 9457 extension member. Each entry is a
	// field key from a stored schema or a path of property names and
	// indices, with a code from a closed list, so it echoes nothing the
	// request sent. It is absent from every other body.
	Errors []FieldError `json:"errors,omitempty"`

	// AvailableAt is when what was refused becomes possible, in Unix ms,
	// on a body whose error says so (AvailableAt): a member's second
	// handle change inside the cooldown is conflict with it
	// (TKT-01M3NRP3W9). It is an RFC 9457 extension member, a time the
	// server computed rather than anything the request sent, and absent
	// from every other body.
	AvailableAt *int64 `json:"available_at,omitempty"`
}

// FieldError is one entry in a problem's errors.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Write sends the problem for a code.
//
// An unknown code becomes CodeInternal rather than a panic or a blank body: a
// typo in an error path should not take down the process, and it must not
// produce a response with an empty type that a client cannot switch on.
func Write(w http.ResponseWriter, r *http.Request, c Code) {
	write(w, r, c, nil, nil)
}

// write is Write with a problem's field errors and its available_at. Only a
// validation-failed body carries field errors.
func write(w http.ResponseWriter, r *http.Request, c Code, errs []FieldError, availableAt *int64) {
	e, ok := catalogue[c]
	if !ok {
		c = CodeInternal
		e = catalogue[CodeInternal]
	}

	h := w.Header()
	h.Set("Content-Type", ContentType)
	// An error response must never be cached: a 404 stored by an intermediary
	// outlives the deploy that added the route.
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)

	// A HEAD response carries the headers and no body. Encoding one anyway
	// would be discarded by net/http, but the Content-Length it implies is
	// worth not lying about.
	if r != nil && r.Method == http.MethodHead {
		return
	}
	if c != CodeValidationFailed {
		errs = nil
	}
	_ = json.NewEncoder(w).Encode(Problem{Type: c, Title: e.Title, Status: e.Status, Errors: errs,
		AvailableAt: availableAt})
}

// WriteRetryAfter writes a problem with a Retry-After header.
//
// docs/07 documents 429 with Retry-After as part of the API's error contract,
// so the catalogue owns it rather than the limiter that will call it
// (TKT-01M29C800X). Delta-seconds rather than a date, per RFC 9110 §10.2.3,
// because it needs no clock agreement between server and client.
func WriteRetryAfter(w http.ResponseWriter, r *http.Request, c Code, after time.Duration) {
	if secs := int(after.Round(time.Second) / time.Second); secs > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	Write(w, r, c)
}

// WriteError maps an error to its code, writes the problem, and logs the error
// itself.
//
// Taking the logger is what makes the no-leak guarantee structural. The error
// is the useful thing and it goes somewhere useful; what reaches the client is
// a constant from the catalogue. There is deliberately no variant that takes
// an error and no logger, because that variant's obvious implementation is to
// put the error in the body.
func WriteError(ctx context.Context, w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	c := FromError(err)

	if log != nil && err != nil {
		// CodeInternal means nothing claimed this error, which is a gap in the
		// mapping table rather than ordinary traffic, so it is logged louder.
		lvl := slog.LevelWarn
		if c == CodeInternal {
			lvl = slog.LevelError
		}
		log.Log(ctx, lvl, "request failed",
			"code", string(c),
			"error", err,
		)
	}
	if after, ok := RetryAfterOf(err); ok {
		WriteRetryAfter(w, r, c, after)
		return
	}
	var at *int64
	if ms, ok := AvailableAtOf(err); ok {
		at = &ms
	}
	write(w, r, c, FieldErrorsOf(err), at)
}
