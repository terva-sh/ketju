package api

import (
	"net/http"

	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/web"
)

// ShellPage is the page function for web.Shell.SetPage: the shell inlines
// the viewer's /me as its bootstrap block (docs/07 "Embedding"), so the
// client's first paint needs no round trip. It costs what GET /me costs.
//
// The title and description stay the shell's defaults. The site name and a
// thread's own title are for whoever wires the server together
// (TKT-01M29CCBYX) and the thread page (TKT-01M29CEE6V).
//
// A nil service panics here, at wiring, rather than on the first page.
func ShellPage(u *users.Service) func(*http.Request) (web.Page, error) {
	if u == nil {
		panic("api.ShellPage: the users service is required")
	}
	return func(r *http.Request) (web.Page, error) {
		me, err := u.Me(r.Context())
		if err != nil {
			return web.Page{}, err
		}
		return web.Page{Title: "ketju", Bootstrap: me}, nil
	}
}
