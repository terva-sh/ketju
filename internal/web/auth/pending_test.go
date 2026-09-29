package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
)

// These tests pin docs/09's approval-bypass row, the one Gitea shipped: "one
// function creates sessions; it checks status = active; test the callback
// path explicitly" (TKT-01M3AFHGJD). session.IssueUserWith is that function
// and its own tests hold it; these drive every route that could lead to a
// session, through the real middleware chain, and then ask the API whether
// the browser came out a member.

// withAPI adds the JSON API to w's server, so a test can ask /api/v1 who the
// browser is after a sign-in, through the same session middleware.
func (w *world) withAPI() {
	w.t.Helper()
	p := perm.New(w.db)
	a := api.New(api.Deps{Users: &users.Service{DB: w.db, Perm: p}, Threads: &threads.Service{DB: w.db, Perm: p},
		Forums: &forums.Service{DB: w.db, Perm: p}, Perm: p, Logger: w.handler.Logger})
	w.srv = web.New(web.Groups{Auth: New(w.handler), API: a}, web.Options{Logger: w.handler.Logger, SiteURL: site,
		Session: session.Middleware(w.store, w.handler.Logger), RateLimit: func(h http.Handler) http.Handler { return h }})
}

// setStatus changes a user's status and nothing else: no session is revoked,
// as if the code that suspends had forgotten to (docs/05 says suspending
// deletes sessions; the viewer must not depend on it).
func (w *world) setStatus(id int64, status string) {
	w.t.Helper()
	if err := w.db.Write(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(w.t.Context(), "UPDATE users SET status = ? WHERE id = ?", status, id)
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
}

// lastLogin is when the identity for subject last signed in, if ever.
func (w *world) lastLogin(subject string) sql.NullInt64 {
	w.t.Helper()
	var last sql.NullInt64
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), "SELECT last_login_at FROM identities WHERE subject = ?", subject).Scan(&last)
	}); err != nil {
		w.t.Fatal(err)
	}
	return last
}

// me is GET /api/v1/me with these cookies: who the API says the browser is.
func (w *world) me(cookies ...*http.Cookie) *dto.Author {
	w.t.Helper()
	resp := w.do("GET", "/api/v1/me", cookies...)
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("/me answered %d: %s", resp.StatusCode, body(resp))
	}
	var m dto.Me
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		w.t.Fatal(err)
	}
	return m.User
}

// draft is GET /api/v1/drafts/{id} for a thread nobody wrote: a member-only
// endpoint. A member gets past the member check to 404; anyone else is
// refused before it looks for the thread.
func (w *world) draft(cookies ...*http.Cookie) int {
	w.t.Helper()
	id, err := threadid.NewGenerator().New()
	if err != nil {
		w.t.Fatal(err)
	}
	return w.do("GET", "/api/v1/drafts/"+id.String(), cookies...).StatusCode
}

// notAMember checks the API treats the browser holding these cookies as
// nobody: /me names no user and a member endpoint refuses.
func (w *world) notAMember(cookies ...*http.Cookie) {
	w.t.Helper()
	if u := w.me(cookies...); u != nil {
		w.t.Errorf("/me shows %s", u.Handle)
	}
	if code := w.draft(cookies...); code != http.StatusUnauthorized {
		w.t.Errorf("a member endpoint answered %d, want 401", code)
	}
}

// The callback, for each status that is not active: the unavailable page,
// no cookie, no row, no recorded sign-in, and no member afterwards.
func TestTheCallbackLetsNoInactiveUserIn(t *testing.T) {
	// The control: the same probes pass for an active member, so a failure
	// below is the status and not the harness.
	t.Run("active", func(t *testing.T) {
		w := newWorld(t)
		w.withAPI()
		w.user("alice", "active", "user-1")
		s := cookie(w.signIn(""), session.CookieName)
		if u := w.me(s); u == nil || u.Handle != "alice" {
			t.Fatalf("/me shows %+v, want alice", u)
		}
		if code := w.draft(s); code != http.StatusNotFound {
			t.Errorf("a member endpoint answered %d, want 404", code)
		}
	})
	for _, status := range []string{"pending", "suspended", "deactivated"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			w.user("u-"+status, status, "user-1")
			resp := w.signIn("/t/abc/x")
			w.refused(resp, http.StatusForbidden, "Account unavailable")
			if loc := resp.Header.Get("Location"); loc != "" {
				t.Errorf("sent on to %q", loc)
			}
			if w.lastLogin("user-1").Valid {
				t.Error("the refused sign-in was recorded on the identity")
			}
			// Whatever the response set, the browser carries on with it.
			w.notAMember(resp.Cookies()...)
			if n := w.sessions(); n != 0 {
				t.Errorf("%d sessions after the API was asked", n)
			}
		})
	}
}

// A member signed in, then made inactive: the session row outlives the
// status change until something revokes it, and it must carry no member
// rights on the very next request, nor let a fresh sign-in through.
func TestASessionOutlivingItsStatusIsNoMember(t *testing.T) {
	for _, status := range []string{"suspended", "deactivated", "pending"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			id := w.user("alice", "active", "user-1")
			s := cookie(w.signIn(""), session.CookieName)
			if u := w.me(s); u == nil {
				t.Fatal("not signed in to begin with")
			}

			w.setStatus(id, status)
			w.notAMember(s)

			// Signing in again, from the browser holding that session, is
			// refused, and neither issues a second session nor ends the one
			// it would have replaced: the refusal changes nothing.
			flow, authURL := w.begin("")
			resp := w.do("GET", w.back(authURL), flow, s)
			if resp.StatusCode != http.StatusForbidden || !strings.Contains(body(resp), "<h1>Account unavailable</h1>") {
				t.Errorf("signing in again answered %d", resp.StatusCode)
			}
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Error("signing in again set a session cookie")
			}
			if n := w.sessions(); n != 1 {
				t.Errorf("%d sessions, want only the one from before", n)
			}
			w.notAMember(s)

			// The login page issues nothing either.
			w.do("GET", "/auth/signin", s)
			if n := w.sessions(); n != 1 {
				t.Errorf("the login page left %d sessions", n)
			}

			// Suspension proper ends the sessions (docs/05); the cookie is
			// then dead and cleared, and still no member.
			if err := w.store.RevokeUser(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			if resp := w.do("GET", "/api/v1/me", s); !cleared(resp, session.CookieName) {
				t.Error("a revoked session's cookie was not cleared")
			}
			w.notAMember(s)
		})
	}
}

// Registration writes an active user, and it is the other way in that ends
// in a session. A pending user reaches it two ways, and both stop short.
func TestRegistrationLetsNoPendingUserIn(t *testing.T) {
	// A pending user's own sign-in is refused at the callback; it never
	// reaches the handle screen, though registration is open.
	t.Run("signing in", func(t *testing.T) {
		w := newWorld(t)
		w.withAPI()
		w.open()
		w.user("pat", "pending", "user-1")
		resp := w.signIn("")
		w.refused(resp, http.StatusForbidden, "Account unavailable")
		if cookie(resp, RegistrationCookie) != nil {
			t.Error("a pending user was sent to register")
		}
	})
	// A registration begun for an identity nobody held, which a pending user
	// has come to hold by the time the handle is submitted (approval mode
	// writing its applicant, say). Registering it again is refused, and it
	// is not signed in as the pending user.
	for _, status := range []string{"pending", "suspended", "deactivated"} {
		t.Run("submitting, identity now "+status, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			w.open()
			reg := w.registering("")
			w.user("pat", status, "user-1")
			resp := w.submit("carol", reg)
			if resp.StatusCode != http.StatusConflict || !strings.Contains(body(resp), "<h1>Already registered</h1>") {
				t.Errorf("answered %d", resp.StatusCode)
			}
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Error("the refused registration set a session cookie")
			}
			if n := w.sessions(); n != 0 {
				t.Errorf("the refused registration wrote %d sessions", n)
			}
			w.notAMember(resp.Cookies()...)
		})
	}
}

// The login page is a way to a provider, never a session: whoever asks, it
// sets no session cookie and writes no row.
func TestTheLoginPageIssuesNoSession(t *testing.T) {
	w := newWorld(t)
	w.withAPI()
	w.user("alice", "active", "user-1")
	alice := cookie(w.signIn(""), session.CookieName)
	// pat signed in while active and is pending now, with the session left
	// behind (review 1231).
	pat := w.user("pat", "active", "user-2")
	w.idp.Subject = "user-2"
	patSession := cookie(w.signIn(""), session.CookieName)
	w.setStatus(pat, "pending")
	n := w.sessions()
	if n != 2 {
		t.Fatalf("%d sessions to begin with, want alice's and pat's", n)
	}
	for name, cookies := range map[string][]*http.Cookie{
		"anonymous":        nil,
		"a member":         {alice},
		"a pending user's": {patSession},
		"a stale cookie":   {{Name: session.CookieName, Value: "not-a-session"}},
		"a flow in flight": {cookie(w.do("GET", "/auth/fake/start"), FlowCookie)},
	} {
		for _, path := range []string{"/auth/signin", signInAt("/t/abc/x")} {
			resp := w.do("GET", path, cookies...)
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Errorf("%s at %s: the login page set a session cookie", name, path)
			}
			if got := w.sessions(); got != n {
				t.Errorf("%s at %s: %d sessions, want %d", name, path, got, n)
			}
		}
	}
	w.notAMember(patSession)
}
