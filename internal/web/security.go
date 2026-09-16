package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// Security header values, fixed by docs/07.
const (
	// ReferrerPolicy sends the origin to other sites and the full path to our
	// own. A thread title in a Referer header is a disclosure to whoever the
	// reader clicks through to.
	ReferrerPolicy = "strict-origin-when-cross-origin"

	// ContentTypeOptions stops a browser from guessing a type we did not send.
	// It is what makes an uploaded file served as application/octet-stream
	// stay inert rather than being sniffed into HTML and executed.
	ContentTypeOptions = "nosniff"
)

// nonceBytes is the size of a CSP nonce before encoding.
//
// The CSP specification requires at least 128 bits of entropy, and the nonce's
// whole job is to be unguessable: an attacker who can predict it can inject a
// script tag that the policy will then welcome.
const nonceBytes = 16

// nonceKey types the context value so nothing else can collide with it.
type nonceKey struct{}

// NonceFrom returns this response's CSP nonce.
//
// The SPA shell calls it to put the nonce on its script tag. Unlike the
// viewer in internal/logging, a plain context value is right here: the nonce
// is created by securityHeaders on the way *in* and read by a handler further
// down, and a context value travels inward. The viewer needed a pointer slot
// precisely because it goes the other way -- set deep in the chain, read by
// the logger wrapped around the outside.
func NonceFrom(ctx context.Context) (string, bool) {
	n, ok := ctx.Value(nonceKey{}).(string)
	return n, ok
}

// newNonce returns a fresh base64 nonce.
//
// URL-safe base64 without padding, which the CSP grammar accepts and which
// needs no escaping in an HTML attribute. rand.Read does not fail on any
// supported platform -- since Go 1.24 it panics rather than returning an
// error, because a system that cannot produce randomness cannot serve a
// request safely either.
func newNonce() string {
	b := make([]byte, nonceBytes)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// securityHeaders sets the headers docs/07 requires and puts a per-response
// CSP nonce in the request context.
//
// It sets them on the way in rather than on the way out, which is what makes
// them survive everything downstream: a panic recovered into a 500, a 413 from
// the body limit, a 404 from the router. Headers written on the way out would
// arrive after the status was already sent and be silently dropped, and the
// responses that most need a restrictive policy are exactly the error ones a
// handler did not write on purpose.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := newNonce()

		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy(nonce))
		h.Set("Referrer-Policy", ReferrerPolicy)
		h.Set("X-Content-Type-Options", ContentTypeOptions)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nonceKey{}, nonce)))
	})
}

// contentSecurityPolicy builds the policy for one response.
//
// Every directive here is a decision, and the ones that are not in docs/07 are
// the ones that close the gaps 'self' leaves open:
//
//   - default-src 'self' is the no-third-party-origins rule. Everything the
//     client needs is served from this binary, so anything reaching elsewhere
//     is either a mistake or an injection.
//   - script-src and style-src take the nonce and do not carry
//     'unsafe-inline'. This is the whole point of the ticket: a strict policy
//     is cheap on day one and painful later, because retrofitting means
//     finding every inline handler written in the meantime.
//   - img-src adds data: because the client inlines small images; docs/07
//     names this exception.
//   - frame-ancestors 'none' makes the forum unframeable, which is what stops
//     a clickjacking overlay from borrowing a member's session.
//   - base-uri 'self' stops an injected <base> from re-pointing every relative
//     URL on the page, which would route the client's own fetches off-site.
//   - form-action 'self' matters for the non-JS client docs/07 plans: without
//     it, an injected form can post a member's input to another origin.
//   - object-src 'none' removes the plugin surface entirely.
//
// X-Frame-Options is deliberately absent. frame-ancestors supersedes it in
// every browser this forum targets, and a second header saying the same thing
// is a second place to keep in sync.
func contentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"img-src 'self' data:",
		"script-src 'self' 'nonce-" + nonce + "'",
		"style-src 'self' 'nonce-" + nonce + "'",
		"connect-src 'self'",
	}, "; ")
}
