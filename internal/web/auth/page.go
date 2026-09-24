package auth

import (
	"html/template"
	"net/http"
)

// page is the whole of what a sign-in shows when it does not end in a
// redirect. The callback is the server's, not the client's, so these are
// served here, plain: the page policy allows no inline style or script, and
// a sign-in that has failed should not depend on the client loading to say
// so. Each says what happened and offers the way on.
var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Ketju</title>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
<p><a href="{{.Next}}">{{.NextLabel}}</a></p>
</main>
</body>
</html>
`))

type pageData struct {
	Title, Message, Next, NextLabel string
}

// Pages, by what went wrong.
var (
	// A sign-in that failed for a reason the reader can do nothing about but
	// try again: an expired or missing login cookie, a state that does not
	// match, a token that does not verify. Which of those it was is logged,
	// not shown: the difference is useful to an operator and to nobody
	// probing the endpoint.
	pageFailed = pageData{"Sign-in failed", "The sign-in could not be completed. It may have taken too long, " +
		"or been started in another tab. Please try again.", "/", "Go to the forum"}
	// The IdP answered the authorization request with an error, most often
	// because the reader declined.
	pageCancelled = pageData{"Sign-in cancelled", "The identity provider did not complete the sign-in.",
		"/", "Go to the forum"}
	// The provider is not configured, or is disabled.
	pageNoProvider = pageData{"Sign-in unavailable", "This sign-in provider is not available.",
		"/", "Go to the forum"}
	// allowed_email_domains refused the address.
	pageNotAllowed = pageData{"Sign-in not allowed", "This forum does not accept sign-ins from that account's email address.",
		"/", "Go to the forum"}
	// A suspended or deactivated account, and a pending one. The reason and
	// any end date are Phase 2 moderation columns; until then the page is
	// the same for all three (settled in grooming for TKT-01M29C7ZZ9).
	pageUnavailable = pageData{"Account unavailable", "This account cannot sign in at the moment. " +
		"If you think this is a mistake, contact the forum's moderators.", "/", "Go to the forum"}
	// The branches whose tickets have not landed: registration
	// (TKT-01M29C8005), linking (TKT-01M29CKRES) and approval-mode
	// applicants (TKT-01M29CKRG9).
	pageRegistration = pageData{"Registration is not open yet", "There is no account for this sign-in, " +
		"and this forum cannot create one yet.", "/", "Go to the forum"}
	pageLinking = pageData{"Linking is not available yet", "This sign-in is not linked to your account, " +
		"and linking another identity is not available yet.", "/", "Go to the forum"}
	pageApplicant = pageData{"Application pending", "This sign-in belongs to an application to join, " +
		"which this forum cannot review yet.", "/", "Go to the forum"}
)

func writePage(w http.ResponseWriter, status int, p pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, p)
}
