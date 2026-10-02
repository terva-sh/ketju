package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/threads"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// Handle changes through the API (TKT-01M3NRP3W9, docs/05 "Changing a
// handle", docs/07).

// shell is what the client shell stands in for in these tests.
const shell = "the client shell"

// newRenameWorld is the admin world with every service on the world's
// clock, so a test can move past a reservation's end, as one process's
// services all read the same clock in serve.go.
func newRenameWorld(t *testing.T) *adminWorld {
	t.Helper()
	w := newAdminWorld(t)
	clock := func() time.Time { return w.now }
	w.h, w.mux = build(Deps{Users: &users.Service{DB: w.db, Perm: w.perm, Now: clock},
		Threads: &threads.Service{DB: w.db, Perm: w.perm, Now: clock}, Forums: &forums.Service{DB: w.db, Perm: w.perm},
		Forms: &formsvc.Service{DB: w.db}, Applications: &applications.Service{DB: w.db, Now: clock}, Perm: w.perm})
	store := &session.Store{DB: w.db, Now: clock}
	pass := func(h http.Handler) http.Handler { return h }
	w.handler = web.New(web.Groups{API: w.mux}, web.Options{Session: session.Middleware(store, nil), RateLimit: pass, CSRF: pass})
	return w
}

// later moves the world's clock on by d and signs everyone in again, since
// their sessions would not outlive a jump of months.
func (w *adminWorld) later(d time.Duration) {
	w.t.Helper()
	w.now = w.now.Add(d)
	for _, u := range adminUsers {
		w.tokens[u.handle] = sessiontest.SignIn(w.t, w.db, u.id, w.now)
	}
}

// withShell is the admin world's server with the shell behind
// ProfileRedirects, as serve.go wires it.
func (w *adminWorld) withShell() http.Handler {
	store := &session.Store{DB: w.db, Now: func() time.Time { return w.now }}
	pass := func(h http.Handler) http.Handler { return h }
	page := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "no-store")
		rw.Write([]byte(shell))
	})
	u := &users.Service{DB: w.db, Perm: w.perm, Now: func() time.Time { return w.now }}
	return web.New(web.Groups{API: w.mux, SPA: ProfileRedirects(u, page)},
		web.Options{Session: session.Middleware(store, nil), RateLimit: pass, CSRF: pass})
}

// get is a GET of path through h as viewer.
func (w *adminWorld) getThrough(h http.Handler, viewer, path string) *httptest.ResponseRecorder {
	w.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tok := w.tokens[viewer]; tok != "" {
		req.AddCookie(sessiontest.Cookie(tok))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// rename renames the account handle names to to, as the admin.
func (w *adminWorld) rename(handle, to string) dto.HandleChange {
	w.t.Helper()
	return decodeAs[dto.HandleChange](w.t, w.call("admin", "POST", "/api/v1/admin/users/"+handle+"/rename",
		fmt.Sprintf(`{"handle":%q}`, to)))
}

// fieldCodeOf is the handle field's code in a validation-failed answer.
func fieldCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct {
		Type   string `json:"type"`
		Errors []struct {
			Field, Code string
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, not validation-failed: %s", rec.Code, rec.Body)
	}
	if len(p.Errors) != 1 || p.Errors[0].Field != "handle" {
		t.Fatalf("errors %+v", p.Errors)
	}
	return p.Errors[0].Code
}

// redirects asserts rec is a 302 to loc with no-store.
func redirects(t *testing.T, what string, rec *httptest.ResponseRecorder, loc string) {
	t.Helper()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != loc || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("%s: %d, Location %q, Cache-Control %q; want 302 to %q with no-store", what, rec.Code,
			rec.Header().Get("Location"), rec.Header().Get("Cache-Control"), loc)
	}
}

// TestRenameRoutes is AC1 through the routes: the member route is the
// viewer's own, refused to a stranger and a pre-user, and a second change
// inside 30 days is conflict with available_at; the admin route is an
// admin's alone, and neither waits for nor starts the member's cooldown.
func TestRenameRoutes(t *testing.T) {
	w := newRenameWorld(t)
	for viewer, want := range map[string]int{"anonymous": http.StatusUnauthorized, "pre-user": http.StatusForbidden} {
		if rec := w.call(viewer, "PUT", "/api/v1/settings/handle", `{"handle":"zed"}`); rec.Code != want {
			t.Errorf("%s: %d, want %d", viewer, rec.Code, want)
		}
	}
	// A stranger is refused as one whatever the body.
	if rec := w.call("anonymous", "PUT", "/api/v1/settings/handle", `{nonsense`); rec.Code != http.StatusUnauthorized {
		t.Errorf("a stranger's malformed body: %d", rec.Code)
	}

	first := w.now
	res := decodeAs[dto.HandleChange](t, w.call("member", "PUT", "/api/v1/settings/handle", `{"handle":"Membre"}`))
	next := dto.Millis(first.Add(identity.RenameCooldown).UnixMilli())
	if res.Handle != "membre" || res.Previous != "member" || res.NextChangeAt == nil || *res.NextChangeAt != next {
		t.Fatalf("the member's change: %+v", res)
	}
	if me := w.meOf("member"); me.User == nil || me.User.Handle != "membre" {
		t.Errorf("GET /me after the change: %+v", me.User)
	}

	w.later(24 * time.Hour)
	rec := w.call("member", "PUT", "/api/v1/settings/handle", `{"handle":"member2"}`)
	var p struct {
		Type        string `json:"type"`
		AvailableAt *int64 `json:"available_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusConflict || p.AvailableAt == nil ||
		*p.AvailableAt != int64(next) {
		t.Fatalf("a second change a day later: %d %s", rec.Code, rec.Body)
	}
	if code := fieldCodeOf(t, w.call("moderator", "PUT", "/api/v1/settings/handle", `{"handle":"ADMIN"}`)); code != "reserved" {
		t.Errorf("admin as a handle: %q", code)
	}

	for _, viewer := range []string{"member", "scoped", "moderator"} {
		if rec := w.call(viewer, "POST", "/api/v1/admin/users/membre/rename", `{"handle":"zed"}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s renaming: %d", viewer, rec.Code)
		}
	}
	res = w.rename("membre", "member3")
	if res.Handle != "member3" || res.NextChangeAt == nil || *res.NextChangeAt != next {
		t.Errorf("the admin's rename: %+v", res)
	}
	res = w.rename("moderator", "moderatrix")
	if res.NextChangeAt != nil {
		t.Errorf("an admin's rename started a cooldown: %+v", res)
	}
	if rec := w.call("moderator", "PUT", "/api/v1/settings/handle", `{"handle":"mod"}`); rec.Code != http.StatusOK {
		t.Errorf("the moderator changing their own after the admin's rename: %d %s", rec.Code, rec.Body)
	}
	if n := w.auditCount(identity.ActionRenamed); n != 4 {
		t.Errorf("%d user.renamed rows, want 4", n)
	}
}

// TestHandleStateRoute is TKT-01M3V6XTYG criterion 1: GET /settings/handle
// answers the viewer's handle and when they may next change it, behind the
// PUT's gates and with no-store. next_change_at is null before any change,
// the date after the member's own, the available_at a PUT inside the
// cooldown is refused with, unmoved by an admin's rename, and null again
// once the 30 days are over.
func TestHandleStateRoute(t *testing.T) {
	w := newRenameWorld(t)
	const path = "/api/v1/settings/handle"
	for viewer, want := range map[string]int{"anonymous": http.StatusUnauthorized, "pre-user": http.StatusForbidden} {
		if rec := w.call(viewer, "GET", path, ""); rec.Code != want {
			t.Errorf("%s: %d, want %d", viewer, rec.Code, want)
		}
	}
	state := func(viewer string) dto.HandleState {
		t.Helper()
		// decodeAs holds the answer to no-store and to the type's fields.
		return decodeAs[dto.HandleState](t, w.call(viewer, "GET", path, ""))
	}
	if s := state("member"); s.Handle != "member" || s.NextChangeAt != nil {
		t.Fatalf("before any change: %+v", s)
	}

	first := w.now
	decodeAs[dto.HandleChange](t, w.call("member", "PUT", path, `{"handle":"membre"}`))
	next := dto.Millis(first.Add(identity.RenameCooldown).UnixMilli())
	if s := state("member"); s.Handle != "membre" || s.NextChangeAt == nil || *s.NextChangeAt != next {
		t.Fatalf("after the member's own change: %+v, want next_change_at %d", s, next)
	}

	w.later(24 * time.Hour)
	rec := w.call("member", "PUT", path, `{"handle":"member2"}`)
	var p struct {
		AvailableAt *int64 `json:"available_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusConflict || p.AvailableAt == nil {
		t.Fatalf("a change inside the cooldown: %d %s", rec.Code, rec.Body)
	}
	if s := state("member"); s.NextChangeAt == nil || int64(*s.NextChangeAt) != *p.AvailableAt {
		t.Errorf("next_change_at %v, the refusal's available_at %d", s.NextChangeAt, *p.AvailableAt)
	}

	w.rename("membre", "member3")
	if s := state("member"); s.Handle != "member3" || s.NextChangeAt == nil || *s.NextChangeAt != next {
		t.Errorf("after an admin's rename: %+v, want next_change_at %d", s, next)
	}
	w.rename("moderator", "moderatrix")
	if s := state("moderator"); s.Handle != "moderatrix" || s.NextChangeAt != nil {
		t.Errorf("an admin's rename started a cooldown: %+v", s)
	}

	w.later(identity.RenameCooldown)
	if s := state("member"); s.NextChangeAt != nil {
		t.Errorf("30 days on: %+v", s)
	}
}

// TestReleasedHandleRedirects is AC3 through the API: for 90 days a member
// asking for the old handle's profile or history is sent to the account's
// handle now, query kept, with a temporary uncached redirect, a chain of
// renames included; a stranger is refused with no Location; a pending
// account's old handle is not-found to a member and redirects staff; and
// once the reservation ends the old handle is not-found and free.
func TestReleasedHandleRedirects(t *testing.T) {
	w := newRenameWorld(t)
	w.exec(`INSERT INTO users (id, handle, display_name, status, role, kind, prefs_json, created_at, updated_at)
		VALUES (6, 'pend', 'pend', 'pending', 'member', 'human', '{}', 1, 1)`)
	w.rename("member", "bob")
	w.rename("pend", "pending2")

	redirects(t, "the profile", w.call("scoped", "GET", "/api/v1/users/member", ""), "/api/v1/users/bob")
	redirects(t, "the history", w.call("scoped", "GET", "/api/v1/users/Member/posts?cursor=x", ""),
		"/api/v1/users/bob/posts?cursor=x")
	w.later(time.Hour)
	w.rename("bob", "carol")
	for _, old := range []string{"member", "bob"} {
		redirects(t, old+" in the chain", w.call("scoped", "GET", "/api/v1/users/"+old, ""), "/api/v1/users/carol")
	}
	if rec := w.call("scoped", "GET", "/api/v1/users/carol", ""); rec.Code != http.StatusOK {
		t.Errorf("the new handle: %d", rec.Code)
	}

	for _, viewer := range []string{"anonymous", "suspended", "pre-user"} {
		rec := w.call(viewer, "GET", "/api/v1/users/member", "")
		if rec.Code/100 != 4 || rec.Header().Get("Location") != "" {
			t.Errorf("%s: %d, Location %q", viewer, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := w.call("scoped", "GET", "/api/v1/users/pend", ""); rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
		t.Errorf("a pending account's old handle to a member: %d", rec.Code)
	}
	redirects(t, "a pending account's old handle to staff", w.call("moderator", "GET", "/api/v1/users/pend", ""),
		"/api/v1/users/pending2")

	w.later(domain.HandleReservation)
	for _, old := range []string{"member", "bob", "member/posts"} {
		if rec := w.call("scoped", "GET", "/api/v1/users/"+old, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s after the reservation: %d", old, rec.Code)
		}
	}
	w.rename("scoped", "member")
	if rec := w.call("moderator", "GET", "/api/v1/users/member", ""); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"handle":"member"`) {
		t.Errorf("the handle's new holder: %d %s", rec.Code, rec.Body)
	}
}

// TestShellRedirectsReleasedProfiles is AC3 for the client's own paths: a
// member opening /u/{old} or /u/{old}/posts is sent to /u/{new}, query
// kept, with no-store; a stranger gets the shell, as for any profile, and
// so does every other path.
func TestShellRedirectsReleasedProfiles(t *testing.T) {
	w := newRenameWorld(t)
	h := w.withShell()
	w.rename("member", "bob")
	redirects(t, "/u/member", w.getThrough(h, "scoped", "/u/member"), "/u/bob")
	redirects(t, "/u/member/posts", w.getThrough(h, "scoped", "/u/member/posts?cursor=x"), "/u/bob/posts?cursor=x")
	for _, c := range []struct{ viewer, path string }{
		{"anonymous", "/u/member"},
		{"anonymous", "/u/member/posts"},
		{"scoped", "/u/bob"},
		{"scoped", "/u/member/threads"},
		{"scoped", "/u/nobody"},
		{"scoped", "/u/"},
		{"scoped", "/settings"},
	} {
		rec := w.getThrough(h, c.viewer, c.path)
		if rec.Code != http.StatusOK || rec.Body.String() != shell || rec.Header().Get("Location") != "" {
			t.Errorf("%s as %s: %d %q", c.path, c.viewer, rec.Code, rec.Body)
		}
	}
}

// TestReleasedHandleIsTakenEverywhere is AC2 across every path that gives
// out a handle: after a rename, the old handle is taken for registration,
// the auto_approve skip, another member's rename, application approval and
// agent creation, and once the 90 days are over registration takes it.
func TestReleasedHandleIsTakenEverywhere(t *testing.T) {
	w := newRenameWorld(t)
	if err := settings.Set(t.Context(), w.db, settings.RegistrationMode.Name, json.RawMessage(`"open"`), 0, w.now); err != nil {
		t.Fatal(err)
	}
	reg := &identity.Service{DB: w.db, Sessions: &session.Store{DB: w.db, Now: func() time.Time { return w.now }},
		Perm: perm.New(w.db), Now: func() time.Time { return w.now },
		Policy: identity.PolicyFrom([]config.Provider{{Key: "club", AutoApprove: true}, {Key: "fake"}})}
	claims := func(provider, subject string) identity.Claims {
		return identity.Claims{Provider: provider, Issuer: "https://idp.example", Subject: subject,
			PreferredUsername: "member"}
	}
	w.rename("member", "bob")

	for _, h := range []string{"member", "MEMBER"} {
		if _, err := reg.Register(t.Context(), claims("fake", "new-1"), h, session.Meta{Provider: "fake"}, ""); !errors.Is(err, identity.ErrHandleTaken) {
			t.Errorf("registering %s: %v", h, err)
		}
	}
	if _, why, err := reg.AutoRegister(t.Context(), claims("club", "new-2"), session.Meta{Provider: "club"}, ""); err != nil || why != identity.SkipTaken {
		t.Errorf("the auto_approve skip: %v, %v", why, err)
	}
	if code := fieldCodeOf(t, w.call("moderator", "PUT", "/api/v1/settings/handle", `{"handle":"member"}`)); code != "taken" {
		t.Errorf("another member's rename: %q", code)
	}
	app := w.application("pending", 1)
	if code := fieldCodeOf(t, w.call("admin", "POST", fmt.Sprintf("/api/v1/admin/applications/%d/approve", app),
		`{"handle":"member"}`)); code != "taken" {
		t.Errorf("approval: %q", code)
	}
	if rec := w.call("admin", "POST", "/api/v1/admin/users", `{"kind":"agent","handle":"member"}`); rec.Code != http.StatusConflict {
		t.Errorf("creating an agent: %d %s", rec.Code, rec.Body)
	}
	if n := w.countRows(`SELECT count(*) FROM users WHERE handle = 'member'`); n != 0 {
		t.Fatalf("%d accounts took the handle", n)
	}

	w.later(domain.HandleReservation)
	if _, err := reg.Register(t.Context(), claims("fake", "new-1"), "member", session.Meta{Provider: "fake"}, ""); err != nil {
		t.Fatalf("registering once the reservation ended: %v", err)
	}
}

// TestAnonymizeReleasesWithoutRedirect is AC8: anonymizing an account that
// renamed reserves its handle with a row naming nobody, and its earlier
// releases stop naming it, so no old handle redirects to deleted-user-<id>,
// on the API or the client's paths, and each stays taken for its 90 days.
func TestAnonymizeReleasesWithoutRedirect(t *testing.T) {
	w := newRenameWorld(t)
	h := w.withShell()
	w.rename("member", "bob")
	if rec := w.call("admin", "POST", "/api/v1/admin/users/bob/anonymize", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("anonymizing: %d %s", rec.Code, rec.Body)
	}
	for _, old := range []string{"member", "bob"} {
		var named int
		var until int64
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(user_id), max(reserved_until) FROM handle_releases
				WHERE handle = ?`, old).Scan(&named, &until)
		}); err != nil {
			t.Fatal(err)
		}
		if named != 0 || until <= w.now.UnixMilli() {
			t.Errorf("%s: %d releases name a user, reserved until %d", old, named, until)
		}
		if rec := w.call("moderator", "GET", "/api/v1/users/"+old, ""); rec.Code != http.StatusNotFound ||
			rec.Header().Get("Location") != "" {
			t.Errorf("GET /users/%s: %d, Location %q", old, rec.Code, rec.Header().Get("Location"))
		}
		if rec := w.getThrough(h, "moderator", "/u/"+old); rec.Code != http.StatusOK || rec.Body.String() != shell {
			t.Errorf("/u/%s: %d %q", old, rec.Code, rec.Body)
		}
		if code := fieldCodeOf(t, w.call("moderator", "PUT", "/api/v1/settings/handle",
			fmt.Sprintf(`{"handle":%q}`, old))); code != "taken" {
			t.Errorf("taking %s: %q", old, code)
		}
	}
}
