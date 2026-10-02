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
// The title is the site name, which /me carries (TKT-01M29CXK2H), so it
// costs no statement of its own and follows an admin's change from the next
// page. So does the name iOS gives the page on a home screen
// (TKT-01M29CGBYY). The description stays the shell's default, and a
// thread's own title is the thread page's (TKT-01M29CEE6V).
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
		return web.Page{Title: me.SiteName, SiteName: me.SiteName, Bootstrap: me}, nil
	}
}
