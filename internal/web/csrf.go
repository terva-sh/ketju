package web

import (
	"mime"
	"net/http"
	"strings"

	"github.com/terva-sh/ketju/internal/problem"
)

// The CSRF posture, in one place.
//
// docs/07 asks for no token plumbing in the SPA, and this is what replaces it.
// Three independent things have to hold, and the point of listing them
// together is that each one alone is not enough:
//
//  1. The API accepts JSON only on mutating routes. An HTML form can send
//     exactly three content types -- application/x-www-form-urlencoded,
//     multipart/form-data and text/plain -- and none of them is JSON. A form
//     on another origin therefore cannot produce a request the API will even
//     look at, which is the property that makes a token unnecessary.
//
//  2. http.CrossOriginProtection rejects non-safe cross-origin browser
//     requests using Sec-Fetch-Site, or Origin against Host. This is what
//     covers the two places rule 1 cannot: the multipart upload route, which
//     must accept a form content type, and the form posts the non-JS client
//     of docs/07 will eventually make.
//
//  3. Session cookies are SameSite=Lax, so the browser does not attach them to
//     a cross-site POST in the first place. That belongs to the session ticket
//     (TKT-01M29C5Y3E); it is named here because this posture assumes it, and
//     a future reader deciding to relax it should find out from this comment
//     rather than from an incident.
//
// Enabling all of this before any form post exists is deliberate. docs/07
// counts it among the constraints that keep the second client cheap: it costs
// nothing today and is awkward to retrofit once handlers exist, because
// retrofitting means auditing every handler written in the meantime.

// jsonContentType is the only body type a mutating API route accepts.
const jsonContentType = "application/json"

// CrossOrigin returns the CSRF middleware for Options.CSRF.
//
// siteURL, when set, is added as a trusted origin. A deployment behind a proxy
// sees Host as the internal name while the browser sends the public origin, so
// without this every write from a real browser would be refused.
func CrossOrigin(siteURL string) func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	if siteURL != "" {
		// An unparseable site_url is config's problem to report, not a reason
		// to fail closed on every request here.
		_ = protection.AddTrustedOrigin(strings.TrimSuffix(siteURL, "/"))
	}
	// Without this the rejection is net/http's plain-text 403, which a JSON
	// client reports as a parse error rather than as the refusal it is.
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, r, problem.CodeForbidden)
	}))

	return func(next http.Handler) http.Handler {
		// Rule 1 runs outside rule 2 so that a cross-origin form post to an
		// API route is refused as the wrong content type, which is the more
		// specific of the two true statements about it.
		return requireJSON(protection.Handler(next))
	}
}

// requireJSON refuses a mutating API request whose body is not JSON.
//
// This is AC1, and it is the load-bearing half of the posture: it is what lets
// the SPA carry no token at all.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !needsJSON(r) {
			next.ServeHTTP(w, r)
			return
		}

		ct := r.Header.Get("Content-Type")
		if ct == "" {
			// No content type and no body is a legitimate DELETE. No content
			// type *with* a body is a request whose parse would be a guess,
			// and guessing is what nosniff exists to stop elsewhere.
			if r.ContentLength == 0 {
				next.ServeHTTP(w, r)
				return
			}
			problem.Write(w, r, problem.CodeUnsupportedMediaType)
			return
		}

		// Parsed rather than compared, so "application/json; charset=utf-8" is
		// accepted and "application/jsonp" is not. A prefix match would accept
		// both.
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mt, jsonContentType) {
			problem.Write(w, r, problem.CodeUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// needsJSON reports whether a request must carry a JSON body.
//
// The upload route is exempt because docs/07 makes it multipart, which is
// precisely a content type a form can send. That exemption is the reason rule
// 2 above is not optional: CrossOriginProtection is the only thing standing
// between a cross-origin form and the upload endpoint. PUT /settings/avatar
// is multipart for the same reason (TKT-01M3YSQ4CR), and is exempt for PUT
// only; a form cannot send a PUT at all, and CrossOriginProtection covers it
// as it does the upload.
func needsJSON(r *http.Request) bool {
	if !isMutating(r.Method) || !isAPIPath(r.URL.Path) {
		return false
	}
	p := r.URL.Path
	if p == AvatarPath && r.Method == http.MethodPut {
		return false
	}
	return p != UploadPath && !strings.HasPrefix(p, UploadPath+"/")
}
