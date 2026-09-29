package auth

import (
	"html/template"
	"net/http"

	"github.com/terva-sh/ketju/internal/service/identity"
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
	// allowed_email_domains is set, and the provider did not say the
	// address is verified. The IdP already told the reader both facts, so
	// saying which refusal it was discloses nothing new.
	pageUnverified = pageData{"Sign-in not allowed", "This forum accepts sign-ins from this provider " +
		"only with a verified email address.", "/", "Go to the forum"}
	// A pending account, and the fallback for any account that is not
	// active. A suspended or deactivated one gets writeUnavailable's page.
	pageUnavailable = pageData{"Account unavailable", "This account cannot sign in at the moment. " +
		"If you think this is a mistake, contact the forum's moderators.", "/", "Go to the forum"}
	// The account was reactivated, but the sign-in after it did not finish.
	pageReactivatedSignInAgain = pageData{"Account reactivated", "Your account is active again, " +
		"but signing you in did not finish. Please sign in again.", "/auth/signin", "Sign in"}
	// The reactivation cookie is missing or past ReactivateTTL.
	pageReactivateExpired = pageData{"Reactivation expired", "Reactivating took too long, " +
		"or no sign-in is waiting for it. Please sign in again.", "/", "Go to the forum"}
	// Someone new who cannot register: registration closed, for this
	// provider, between the callback and the account being written. Signing
	// in again reaches the application form instead (beginApplication).
	pageRegistration = pageData{"Registration is not open", "There is no account for this sign-in, " +
		"and registration closed while it was being created. Please sign in again.", "/auth/signin", "Sign in"}
	// The registration cookie is missing or past RegistrationTTL: the handle
	// screen was opened without a sign-in, or left too long.
	pageRegistrationExpired = pageData{"Registration expired", "Choosing a handle took too long, " +
		"or no sign-in is waiting for one. Please sign in again.", "/", "Go to the forum"}
	// The identity was registered after the handle screen was shown: the
	// form submitted twice, or from two tabs.
	pageAlreadyRegistered = pageData{"Already registered", "This sign-in already has an account. " +
		"Please sign in again to use it.", "/", "Go to the forum"}
	// A member signed in, in another tab, while the handle screen was open.
	// A member adds an identity by linking it, never by registering a
	// second account.
	pageRegisteringSignedIn = pageData{"Already signed in", "You are signed in, so this sign-in cannot " +
		"create a new account. To add it to your account, link it.", LinkPath, "Link a sign-in"}
)

// The answers to a link (link.go), and to a sign-in that would have been
// one, by LinkRefusal. Each writes nothing.
var (
	// An identity nobody has, from a browser a member is signed in on, with
	// no link started (docs/05 "Login flow" step 3).
	pageLinkNoIntent = pageData{"Sign-in not linked", "You are signed in, and this sign-in is not linked " +
		"to your account. To add it to your account, use Link a sign-in. To use another account, " +
		"sign out first.", LinkPath, "Link a sign-in"}
	// The link was started by a member this browser is no longer signed in
	// as.
	pageLinkNotYou = pageData{"Sign-in not linked", "The link was started by an account this browser " +
		"is no longer signed in as, so nothing was linked. Sign in and start again.", "/", "Go to the forum"}
	pageLinkOtherUser = pageData{"Sign-in not linked", "This sign-in already belongs to another account, " +
		"so it was not linked to yours.", "/", "Go to the forum"}
	pageLinkApplicant = pageData{"Sign-in not linked", "This sign-in belongs to an application to join, " +
		"so it was not linked to your account.", "/", "Go to the forum"}
	// Linking an identity that is already the member's changes nothing.
	pageLinkAlready = pageData{"Already linked", "This sign-in is already linked to your account.",
		"/", "Go to the forum"}
	// The account may not link: it is not an active member, or it is an
	// agent, which never uses OIDC.
	pageLinkNotAllowed = pageData{"Linking not allowed", "This account cannot link a sign-in.",
		"/", "Go to the forum"}
	// Nobody is signed in, or an agent credential made the request.
	pageLinkNeedsMember = pageData{"Linking not allowed", "Sign in to link another sign-in to your account.",
		"/auth/signin", "Sign in"}
	// The link page with nothing to offer.
	pageLinkUnavailable = pageData{"Linking is not available", "This forum has no sign-in provider to link.",
		"/", "Go to the forum"}
)

// linkRefusedPages are the page and status for each LinkRefusal.
var linkRefusedPages = map[identity.LinkRefusal]struct {
	status int
	page   pageData
}{
	identity.LinkNoIntent:  {http.StatusConflict, pageLinkNoIntent},
	identity.LinkNotYou:    {http.StatusConflict, pageLinkNotYou},
	identity.LinkOtherUser: {http.StatusConflict, pageLinkOtherUser},
	identity.LinkApplicant: {http.StatusConflict, pageLinkApplicant},
	identity.LinkAlready:   {http.StatusOK, pageLinkAlready},
}

// writeLinkRefused answers a refused link. A refusal it does not know is
// the generic failure.
func writeLinkRefused(w http.ResponseWriter, why identity.LinkRefusal) {
	p, ok := linkRefusedPages[why]
	if !ok {
		writePage(w, http.StatusInternalServerError, pageFailed)
		return
	}
	writePage(w, p.status, p.page)
}

// linkTmpl is the "link a sign-in" page: one form per enabled provider,
// each posting to that provider's link start, which the cross-origin check
// holds to this site.
var linkTmpl = template.Must(template.New("link").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Link a sign-in · Ketju</title>
</head>
<body>
<main>
<h1>Link a sign-in</h1>
<p>Add another way to sign in to your account. Choose a provider, and sign in there with the account to link.</p>
<ul>
{{range .Choices}}<li><form method="post" action="{{.Action}}">
<input type="hidden" name="return_to" value="{{$.ReturnTo}}">
<button type="submit">Link {{.Name}}</button>
</form></li>
{{end}}</ul>
<p><a href="{{.ReturnTo}}">Go back</a></p>
</main>
</body>
</html>
`))

type linkChoice struct {
	Name, Action string
}

type linkData struct {
	Choices  []linkChoice
	ReturnTo string
}

func writeLinkPage(w http.ResponseWriter, d linkData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = linkTmpl.Execute(w, d)
}

// continueTmpl is the answer to a link's start: on to the IdP. A redirect
// would be the natural answer, but the page policy's form-action 'self'
// covers where a form's submission is redirected, and a browser enforcing
// it would stop a 303 to the IdP. So the page sends the browser on with an
// immediate refresh, which no policy directive governs, and offers the
// same address as a link in case it does not.
var continueTmpl = template.Must(template.New("continue").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="0; url={{.URL}}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Continue to {{.Name}} · Ketju</title>
</head>
<body>
<main>
<h1>Continue to {{.Name}}</h1>
<p>Sign in at {{.Name}} with the account to link.</p>
<p><a href="{{.URL}}">Continue to {{.Name}}</a></p>
</main>
</body>
</html>
`))

type continueData struct {
	Name, URL string
}

func writeContinuePage(w http.ResponseWriter, d continueData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = continueTmpl.Execute(w, d)
}

func writePage(w http.ResponseWriter, status int, p pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, p)
}

// unavailableTmpl is the "account unavailable" page for a suspended or
// deactivated account (docs/05 "Login flow"). A suspension shows its reason
// and end: there is no shadow banning (docs/08). A deactivated account is
// offered the Reactivate button, a form posting to /auth/reactivate, which
// the sealed reactivation cookie set with this page stands behind.
// html/template escapes the reason, which a moderator wrote.
var unavailableTmpl = template.Must(template.New("unavailable").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Account unavailable · Ketju</title>
</head>
<body>
<main>
<h1>Account unavailable</h1>
{{if .Suspended}}<p>This account is suspended.</p>
{{if .Reason}}<p>Reason: {{.Reason}}</p>
{{end}}{{if .Until}}<p>Until: {{.Until}}</p>
{{else}}<p>No end date is set.</p>
{{end}}<p>If you think this is a mistake, contact the forum's moderators.</p>
{{else}}<p>This account is deactivated.</p>
{{if .Reactivate}}<form method="post" action="{{.Action}}">
<p><button type="submit">Reactivate my account</button></p>
</form>
{{else}}<p>Please sign in again to reactivate it.</p>
{{end}}{{end}}<p><a href="/">Go to the forum</a></p>
</main>
</body>
</html>
`))

type unavailableData struct {
	Suspended bool
	Reason    string
	// Until is the suspension's end as a reader sees it, or empty.
	Until string
	// Reactivate offers the button; Action is where it posts.
	Reactivate bool
	Action     string
}

func writeUnavailable(w http.ResponseWriter, d unavailableData) {
	d.Action = ReactivatePath
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_ = unavailableTmpl.Execute(w, d)
}

// handleRule is the handle rule in words, from domain.ValidateHandle.
const handleRule = "Use 3 to 30 characters: letters, digits, _ and -, starting with a letter or digit and not ending in -."

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
{{if .Why}}<p id="handle-why">{{.Why}}</p>
{{end}}<form method="post" action="/auth/register">
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
	// Why says why an auto_approve provider's preferred_username was not
	// used as the handle, above the field.
	Why string
}

func writeRegisterPage(w http.ResponseWriter, status int, d registerData) {
	d.Rule = handleRule
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = registerTmpl.Execute(w, d)
}

// pageSignInUnavailable is the login page with nothing to offer: no
// provider is configured, or none is enabled. (Startup refuses a registry
// whose every provider failed discovery, and a working provider is never
// disabled later, so the second is a guard.) That is how the forum stands,
// not a failure of this request, so it is a 200.
var pageSignInUnavailable = pageData{"Sign-in is not available", "This forum has no way to sign in at the moment.",
	"/", "Go to the forum"}

// signInTmpl is the login page when there is a choice: one link per
// provider, in config order, each to that provider's start (signin.go).
var signInTmpl = template.Must(template.New("signin").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in · Ketju</title>
</head>
<body>
<main>
<h1>Sign in</h1>
<p>Choose how to sign in.</p>
<ul>
{{range .}}<li><a href="{{.Start}}">Sign in with {{.Name}}</a></li>
{{end}}</ul>
<p><a href="/">Go to the forum</a></p>
</main>
</body>
</html>
`))

type signInChoice struct {
	Name, Start string
}

func writeSignInPage(w http.ResponseWriter, choices []signInChoice) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = signInTmpl.Execute(w, choices)
}
