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
	// Someone new, when registration.mode is not "open". Approval mode's
	// application (TKT-01M29CKRG9) has not landed, so there is nothing
	// else to offer them yet.
	pageRegistration = pageData{"Registration is not open yet", "There is no account for this sign-in, " +
		"and this forum cannot create one yet.", "/", "Go to the forum"}
	// The registration cookie is missing or past RegistrationTTL: the handle
	// screen was opened without a sign-in, or left too long.
	pageRegistrationExpired = pageData{"Registration expired", "Choosing a handle took too long, " +
		"or no sign-in is waiting for one. Please sign in again.", "/", "Go to the forum"}
	// The identity was registered after the handle screen was shown: the
	// form submitted twice, or from two tabs.
	pageAlreadyRegistered = pageData{"Already registered", "This sign-in already has an account. " +
		"Please sign in again to use it.", "/", "Go to the forum"}
	// The branches whose tickets have not landed: linking (TKT-01M29CKRES)
	// and approval-mode applicants (TKT-01M29CKRG9).
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

// handleRule is the handle rule in words, from domain.ValidateHandle.
const handleRule = "Use 3 to 30 characters: letters, digits, _ and -, starting with a letter or digit."

// registerTmpl is the handle screen: one field, prefilled, and the rule
// beside it (docs/05 "mode = open"). An error, when there is one, is tied to
// the field and announced.
var registerTmpl = template.Must(template.New("register").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Choose a handle · Ketju</title>
</head>
<body>
<main>
<h1>Choose a handle</h1>
<p>Your handle is how others mention you, as @handle. Nobody else on this forum can have it.</p>
<form method="post" action="/auth/register">
<p><label for="handle">Handle</label></p>
<p><input id="handle" name="handle" type="text" value="{{.Handle}}" required minlength="3" maxlength="30"
 autocomplete="username" autocapitalize="none" spellcheck="false"
 aria-describedby="handle-rule{{if .Error}} handle-error{{end}}"{{if .Error}} aria-invalid="true"{{end}} autofocus></p>
<p id="handle-rule">{{.Rule}}</p>
{{if .Error}}<p id="handle-error" role="alert">{{.Error}}</p>
{{end}}<p><button type="submit">Create account</button></p>
</form>
</main>
</body>
</html>
`))

type registerData struct {
	Handle, Error, Rule string
}

func writeRegisterPage(w http.ResponseWriter, status int, d registerData) {
	d.Rule = handleRule
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = registerTmpl.Execute(w, d)
}
