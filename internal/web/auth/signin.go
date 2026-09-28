package auth

import (
	"net/http"
	"net/url"

	"github.com/terva-sh/ketju/internal/domain"
)

// signIn is the login page: where the client's "Sign in" goes (docs/05
// "the login page only shows configured providers", TKT-01M3MFS6KM).
//
// It shows only the providers that are configured and working, each a link
// to its start. With exactly one there is no choice to make, so it goes
// straight there; with none it says sign-in is not available, which is how
// this forum is set up rather than an error. A reader already signed in has
// nothing to do here and goes back to where they were.
//
// return_to is checked once here, with the rule start uses, so every
// address this page hands on is a path on this site; start checks it again.
func (h *Handler) signIn(w http.ResponseWriter, r *http.Request) {
	returnTo := SafeReturnPath(r.URL.Query().Get("return_to"))
	w.Header().Set("Cache-Control", "no-store")
	if domain.ViewerFrom(r.Context()).UserID != 0 {
		http.Redirect(w, r, returnTo, http.StatusFound)
		return
	}
	var choices []signInChoice
	if h.Providers != nil {
		for _, p := range h.Providers.Enabled() {
			choices = append(choices, signInChoice{Name: p.Name(), Start: startURL(p.Key(), returnTo)})
		}
	}
	switch len(choices) {
	case 0:
		writePage(w, http.StatusOK, pageSignInUnavailable)
	case 1:
		http.Redirect(w, r, choices[0].Start, http.StatusFound)
	default:
		writeSignInPage(w, choices)
	}
}

// startURL is provider key's start, carrying returnTo when it says more
// than the default.
func startURL(key, returnTo string) string {
	u := "/auth/" + url.PathEscape(key) + "/start"
	if returnTo != "/" {
		u += "?" + url.Values{"return_to": {returnTo}}.Encode()
	}
	return u
}
