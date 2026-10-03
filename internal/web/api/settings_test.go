package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/applications"
	formsvc "github.com/terva-sh/ketju/internal/service/forms"
	"github.com/terva-sh/ketju/internal/service/forums"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/web"
)

// These are TKT-01M29CCC0X's: a member's own settings, sessions and
// sign-ins, through the real chain, the real session middleware and the
// bearer middleware in front of it, as serve composes them.

// settingsWorld is the thread world served through the chain, with alice
// holding two identities, github and club, and bob one.
type settingsWorld struct {
	*world
	now      time.Time
	sessions *session.Store
	h        http.Handler
	// ident is each identity's id, by "handle/provider".
	ident map[string]int64
}

// Firefox and iPhone are the User-Agent headers the sign-ins send.
const (
	firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"
	iphone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1"
)

func newSettingsWorld(t *testing.T) *settingsWorld {
	t.Helper()
	w := &settingsWorld{world: newWorld(t), now: time.UnixMilli(1_757_500_000_000), ident: map[string]int64{}}
	clock := func() time.Time { return w.now }
	w.sessions = &session.Store{DB: w.db, Now: clock}
	perms := w.svc.Perm
	api := New(Deps{Threads: w.svc, Users: &users.Service{DB: w.db, Perm: perms, Now: clock},
		Forums: &forums.Service{DB: w.db, Perm: perms}, Forms: &formsvc.Service{DB: w.db},
		Applications: &applications.Service{DB: w.db}, Perm: perms, Now: clock, Sessions: w.sessions,
		Providers: []identity.ProviderName{{Key: "github", Name: "GitHub"}, {Key: "club", Name: "The Club"}}})
	pass := func(h http.Handler) http.Handler { return h }
	w.h = web.New(web.Groups{API: api}, web.Options{RateLimit: pass,
		Session: agentcred.Middleware(&agentcred.Store{DB: w.db, Now: clock}, session.Middleware(w.sessions, nil), nil)})
	for _, id := range []struct{ handle, provider string }{{"alice", "github"}, {"alice", "club"}, {"bob", "github"}} {
		w.write(func(q *dbq.Queries) error {
			n, err := q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{
				UserID: sql.NullInt64{Int64: w.users[id.handle], Valid: true}, ProviderKey: id.provider,
				Subject: id.handle + "-" + id.provider, Issuer: "https://" + id.provider + ".example",
				Email: sql.NullString{String: id.handle + "@" + id.provider + ".example", Valid: true}, CreatedAt: 1, UpdatedAt: 1})
			w.ident[id.handle+"/"+id.provider] = n
			return err
		})
	}
	return w
}

// signIn signs handle in through their provider identity, as the callback
// does, from a browser sending ua.
func (w *settingsWorld) signIn(handle, provider, ua string) string {
	w.t.Helper()
	tok, _, err := w.sessions.IssueUser(w.t.Context(), w.ident[handle+"/"+provider],
		session.Meta{IP: "192.0.2.10", UserAgent: ua, Provider: provider})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

// do sends a request with the session cookie token, or, for "bearer:"
// followed by a token, an agent's credential; "" is anonymous.
func (w *settingsWorld) do(token, method, path, body string) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if bearer, ok := strings.CutPrefix(token, "bearer:"); ok {
		r.Header.Set("Authorization", "Bearer "+bearer)
	} else if token != "" {
		r.AddCookie(sessiontest.Cookie(token))
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// signedIn reports whether token's next /me names a user.
func (w *settingsWorld) signedIn(token string) bool {
	w.t.Helper()
	return decodeMe(w.t, w.do(token, http.MethodGet, "/api/v1/me", "").Body.Bytes()).User != nil
}

// sessionRows is the session rows with this user_id, or every row for 0.
func (w *settingsWorld) sessionRows(userID int64) int {
	w.t.Helper()
	q, args := `SELECT count(*) FROM sessions`, []any{}
	if userID != 0 {
		q, args = q+` WHERE user_id = ?`, []any{userID}
	}
	var n int
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), q, args...).Scan(&n)
	}); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// cleared reports whether rec tells the browser to drop the session
// cookie.
func cookieCleared(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName && c.MaxAge < 0 && c.Value == "" {
			return true
		}
	}
	return false
}

func (w *settingsWorld) list(token string) dto.SessionList {
	w.t.Helper()
	return decodeAs[dto.SessionList](w.t, w.do(token, http.MethodGet, "/api/v1/settings/sessions", ""))
}

// idOf is the listed id of token's session.
func (w *settingsWorld) idOf(token string) string {
	w.t.Helper()
	s, err := w.sessions.Lookup(w.t.Context(), token)
	if err != nil {
		w.t.Fatal(err)
	}
	return s.ID()
}

// TestSessionListIsOwnLiveSessionsOnly: the list is the viewer's live
// sessions: an expired row nobody has swept is not listed, nor are bob's.
// Exactly one is current, the request's own, and device is the label of
// its User-Agent.
func TestSessionListIsOwnLiveSessionsOnly(t *testing.T) {
	w := newSettingsWorld(t)
	stale := sessiontest.SignIn(t, w.db, w.users["alice"], w.now.Add(-session.IdleExpiry-time.Hour))
	phone := w.signIn("alice", "club", iphone)
	w.now = w.now.Add(time.Minute)
	laptop := w.signIn("alice", "github", firefox)
	w.signIn("bob", "github", firefox)

	got := w.list(laptop).Sessions
	if len(got) != 2 {
		t.Fatalf("listed %+v; want alice's two live sessions", got)
	}
	want := []dto.SessionInfo{
		{ID: w.idOf(laptop), Current: true, Device: "Firefox on Linux", UserAgent: firefox, IP: "192.0.2.10",
			Provider: "github", CreatedAt: dto.Millis(w.now.UnixMilli()), LastUsedAt: dto.Millis(w.now.UnixMilli())},
		{ID: w.idOf(phone), Device: "Safari on iPhone", UserAgent: iphone, IP: "192.0.2.10", Provider: "club",
			CreatedAt: dto.Millis(w.now.Add(-time.Minute).UnixMilli()), LastUsedAt: dto.Millis(w.now.Add(-time.Minute).UnixMilli())},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("session %d is %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, s := range got {
		if strings.Contains(laptop, s.ID) || strings.Contains(phone, s.ID) || strings.Contains(stale, s.ID) {
			t.Errorf("the listed id %q is part of a cookie", s.ID)
		}
	}
	// From the phone, the phone is the current one.
	if l := w.list(phone).Sessions; len(l) != 2 || l[0].Current || !l[1].Current {
		t.Errorf("from the phone: %+v", l)
	}
}

// TestRevokeOwnEndsTheSessionThroughTheAPI: DELETE /settings/sessions/{id}
// of another of the viewer's sessions deletes its row, so that cookie's
// next request is anonymous; the request's own session works and its
// cookie is not cleared.
func TestRevokeOwnEndsTheSessionThroughTheAPI(t *testing.T) {
	w := newSettingsWorld(t)
	laptop, phone := w.signIn("alice", "github", firefox), w.signIn("alice", "club", iphone)
	rec := w.do(laptop, http.MethodDelete, "/api/v1/settings/sessions/"+w.idOf(phone), "")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Cache-Control") != "no-store" || cookieCleared(rec) {
		t.Fatalf("%d, Cache-Control %q, cleared %v: %s", rec.Code, rec.Header().Get("Cache-Control"), cookieCleared(rec), rec.Body)
	}
	if w.signedIn(phone) {
		t.Error("the ended session's cookie still signs in")
	}
	if !w.signedIn(laptop) {
		t.Error("the asking session ended")
	}
	if n := w.sessionRows(w.users["alice"]); n != 1 {
		t.Errorf("alice has %d session rows, want 1", n)
	}
}

// TestRevokeOwnRefusesAnotherMembersSessionThroughTheAPI: alice sending
// bob's listed id gets not-found, the same answer as an id nobody has, and
// bob's session stays live.
func TestRevokeOwnRefusesAnotherMembersSessionThroughTheAPI(t *testing.T) {
	w := newSettingsWorld(t)
	alice, bob := w.signIn("alice", "github", firefox), w.signIn("bob", "github", firefox)
	var bodies []string
	for _, id := range []string{w.idOf(bob), "0123456789abcdef0123456789abcdef"} {
		rec := w.do(alice, http.MethodDelete, "/api/v1/settings/sessions/"+id, "")
		if rec.Code != http.StatusNotFound || problemType(t, rec) != "not-found" || cookieCleared(rec) {
			t.Errorf("DELETE %s: %d %s", id, rec.Code, rec.Body)
		}
		bodies = append(bodies, rec.Body.String())
	}
	if bodies[0] != bodies[1] {
		t.Errorf("bob's id and a made-up one answer differently:\n%s\n%s", bodies[0], bodies[1])
	}
	if !w.signedIn(bob) || !w.signedIn(alice) {
		t.Error("a refused revocation ended a session")
	}
}

// TestRevokeCurrentSessionClearsTheCookie: ending the request's own
// session singly answers 204 with a clearing Set-Cookie, and the old
// cookie's next request is anonymous.
func TestRevokeCurrentSessionClearsTheCookie(t *testing.T) {
	w := newSettingsWorld(t)
	laptop, phone := w.signIn("alice", "github", firefox), w.signIn("alice", "club", iphone)
	rec := w.do(laptop, http.MethodDelete, "/api/v1/settings/sessions/"+w.idOf(laptop), "")
	if rec.Code != http.StatusNoContent || !cookieCleared(rec) {
		t.Fatalf("%d, cleared %v: %s", rec.Code, cookieCleared(rec), rec.Body)
	}
	if w.signedIn(laptop) {
		t.Error("the old cookie still signs in")
	}
	if !w.signedIn(phone) {
		t.Error("the other session ended")
	}
}

// TestSignOutEverywhereEndsEverySessionIncludingThisOne: alice has three
// sessions and a stale pre-user row on one of her identities, and bob one.
// DELETE /settings/sessions deletes all four of alice's rows, clears the
// cookie, and leaves bob signed in.
func TestSignOutEverywhereEndsEverySessionIncludingThisOne(t *testing.T) {
	w := newSettingsWorld(t)
	var toks []string
	for _, p := range []string{"github", "club", "github"} {
		toks = append(toks, w.signIn("alice", p, firefox))
	}
	// A pre-user row from before alice's club identity was approved: it no
	// longer resolves, and stays until something deletes it.
	w.write(func(q *dbq.Queries) error {
		return q.CreateSession(t.Context(), dbq.CreateSessionParams{IDHash: [32]byte{7}, Kind: "pre_user",
			IdentityID: sql.NullInt64{Int64: w.ident["alice/club"], Valid: true}, CreatedAt: w.now.UnixMilli(),
			ExpiresAt: w.now.Add(time.Hour).UnixMilli(), LastUsedAt: w.now.UnixMilli()})
	})
	bob := w.signIn("bob", "github", firefox)
	if n := w.sessionRows(0); n != 5 {
		t.Fatalf("%d session rows before, want 5", n)
	}

	rec := w.do(toks[0], http.MethodDelete, "/api/v1/settings/sessions", "")
	if rec.Code != http.StatusNoContent || !cookieCleared(rec) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d, cleared %v: %s", rec.Code, cookieCleared(rec), rec.Body)
	}
	if n := w.sessionRows(0); n != 1 {
		t.Errorf("%d session rows after, want bob's alone", n)
	}
	for i, tok := range toks {
		if w.signedIn(tok) {
			t.Errorf("alice's session %d still signs in", i)
		}
	}
	if !w.signedIn(bob) {
		t.Error("bob was signed out")
	}
}

// TestSettingsRoutesRefuseAgentsAndAnonymous: an agent's credential is
// forbidden on the session and identity routes and may read and change
// its settings; anonymous is unauthenticated everywhere, and a pre-user
// forbidden. Nothing is written by a refusal.
func TestSettingsRoutesRefuseAgentsAndAnonymous(t *testing.T) {
	w := newSettingsWorld(t)
	var agent string
	w.write(func(q *dbq.Queries) error {
		if err := q.CreateAgent(t.Context(), dbq.CreateAgentParams{UserID: w.users["scribe"], CreatedAt: 1}); err != nil {
			return err
		}
		out, err := agentcred.Issue(t.Context(), q, w.now, audit.By{}, w.users["scribe"])
		agent = "bearer:" + out.Token
		return err
	})
	var applicant int64
	w.write(func(q *dbq.Queries) error {
		var err error
		applicant, err = q.CreateIdentity(t.Context(), dbq.CreateIdentityParams{ProviderKey: "github", Subject: "applicant",
			Issuer: "https://github.example", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	pre := sessiontest.PreUser(t, w.db, applicant, w.now)
	alice := w.signIn("alice", "github", firefox)
	before := w.sessionRows(0)

	ownOnly := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/settings/sessions"},
		{http.MethodDelete, "/api/v1/settings/sessions"},
		{http.MethodDelete, "/api/v1/settings/sessions/" + w.idOf(alice)},
		{http.MethodGet, "/api/v1/settings/identities"},
		{http.MethodDelete, "/api/v1/settings/identities/" + strconv.FormatInt(w.ident["alice/club"], 10)},
		// TKT-01M3V6XVVG: an agent's name is the admin's to give.
		{http.MethodGet, "/api/v1/settings/profile"},
		{http.MethodPatch, "/api/v1/settings/profile"},
	}
	settings := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings", ""},
		{http.MethodPatch, "/api/v1/settings", `{"page_size": 20}`},
		{http.MethodPatch, "/api/v1/settings", `{"shortcuts": false}`},
	}
	for _, r := range ownOnly {
		if rec := w.do(agent, r.method, r.path, ""); rec.Code != http.StatusForbidden || problemType(t, rec) != "forbidden" {
			t.Errorf("%s %s as an agent: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
		settings = append(settings, struct{ method, path, body string }{r.method, r.path, ""})
	}
	for _, r := range settings {
		if rec := w.do("", r.method, r.path, r.body); rec.Code != http.StatusUnauthorized || problemType(t, rec) != "unauthenticated" {
			t.Errorf("%s %s anonymous: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
		if rec := w.do(pre, r.method, r.path, r.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a pre-user: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
	}
	if n := w.sessionRows(0); n != before {
		t.Errorf("%d session rows, were %d: a refusal wrote", n, before)
	}
	if _, ok := w.identityByID(w.ident["alice/club"]); !ok {
		t.Error("a refusal removed an identity")
	}

	// An agent reads and changes its own settings.
	if s := decodeAs[dto.Settings](t, w.do(agent, http.MethodGet, "/api/v1/settings", "")); s.PageSize != 25 {
		t.Errorf("the agent's settings: %+v", s)
	}
	if s := decodeAs[dto.Settings](t, w.do(agent, http.MethodPatch, "/api/v1/settings", `{"page_size": 20}`)); s.PageSize != 20 {
		t.Errorf("the agent's settings after PATCH: %+v", s)
	}
}

func (w *settingsWorld) identityByID(id int64) (int64, bool) {
	w.t.Helper()
	var user sql.NullInt64
	err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT user_id FROM identities WHERE id = ?`, id).Scan(&user)
	})
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return user.Int64, true
}

// TestIdentitiesThroughTheAPI: the list marks the identity this session
// came through; removing it signs this browser out; the last configured
// identity is conflict; another member's identity, and an id that is not
// a number, are not-found.
func TestIdentitiesThroughTheAPI(t *testing.T) {
	w := newSettingsWorld(t)
	viaGithub, viaClub := w.signIn("alice", "github", firefox), w.signIn("alice", "club", iphone)
	path := func(key string) string {
		return "/api/v1/settings/identities/" + strconv.FormatInt(w.ident[key], 10)
	}

	l := decodeAs[dto.IdentityList](t, w.do(viaGithub, http.MethodGet, "/api/v1/settings/identities", ""))
	if len(l.Identities) != 2 || !l.Identities[0].Current || l.Identities[1].Current ||
		l.Identities[0].ProviderName != "GitHub" || !l.Identities[0].Removable || !l.Identities[1].Removable {
		t.Fatalf("listed %+v", l.Identities)
	}

	// bob's, and an id that names nothing.
	for _, p := range []string{path("bob/github"), "/api/v1/settings/identities/abc", "/api/v1/settings/identities/0"} {
		if rec := w.do(viaGithub, http.MethodDelete, p, ""); rec.Code != http.StatusNotFound || problemType(t, rec) != "not-found" {
			t.Errorf("DELETE %s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if user, ok := w.identityByID(w.ident["bob/github"]); !ok || user != w.users["bob"] {
		t.Error("bob's identity changed")
	}

	// Removing the identity this session came through signs it out.
	rec := w.do(viaGithub, http.MethodDelete, path("alice/github"), "")
	if r := decodeAs[dto.IdentityRemoved](t, rec); !r.SignedOut || !cookieCleared(rec) {
		t.Fatalf("removing the current identity: %+v, cleared %v", r, cookieCleared(rec))
	}
	if w.signedIn(viaGithub) {
		t.Error("the session through the removed identity still signs in")
	}
	if !w.signedIn(viaClub) {
		t.Error("the session through the kept identity ended")
	}

	// The last one is not removable, and removing it is conflict.
	l = decodeAs[dto.IdentityList](t, w.do(viaClub, http.MethodGet, "/api/v1/settings/identities", ""))
	if len(l.Identities) != 1 || l.Identities[0].Removable || !l.Identities[0].Current {
		t.Fatalf("listed %+v", l.Identities)
	}
	if rec := w.do(viaClub, http.MethodDelete, path("alice/club"), ""); rec.Code != http.StatusConflict ||
		problemType(t, rec) != "conflict" || cookieCleared(rec) {
		t.Errorf("removing the last identity: %d %s", rec.Code, rec.Body)
	}
	if !w.signedIn(viaClub) {
		t.Error("a refused removal signed the member out")
	}
}

// settingsFieldErrors is a validation-failed body's errors, as "field:code".
func settingsFieldErrors(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var p struct {
		Errors []struct{ Field, Code string } `json:"errors"`
	}
	if rec.Code != http.StatusUnprocessableEntity || problemType(t, rec) != "validation-failed" {
		t.Fatalf("status %d, not validation-failed: %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range p.Errors {
		out = append(out, e.Field+":"+e.Code)
	}
	return out
}

// prefsOf is alice's stored timezone and prefs_json.
func (w *settingsWorld) prefsOf(handle string) (string, string) {
	w.t.Helper()
	var tz sql.NullString
	var prefs string
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT timezone, prefs_json FROM users WHERE id = ?`,
			w.users[handle]).Scan(&tz, &prefs)
	}); err != nil {
		w.t.Fatal(err)
	}
	return tz.String, prefs
}

// TestUpdateSettingsValidates: each bad value is its field error, several
// are reported together, an unknown key is bad-request, and nothing is
// written.
func TestUpdateSettingsValidates(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	tz0, prefs0 := w.prefsOf("alice")
	for _, c := range []struct {
		body string
		want string
	}{
		{`{"timezone": "Mars/Olympus_Mons"}`, "timezone:not_an_option"},
		{`{"timezone": ""}`, "timezone:not_an_option"},
		{`{"timezone": "Local"}`, "timezone:not_an_option"},
		{`{"timezone": 3}`, "timezone:wrong_type"},
		{`{"page_size": 9}`, "page_size:out_of_range"},
		{`{"page_size": 51}`, "page_size:out_of_range"},
		{`{"page_size": "30"}`, "page_size:wrong_type"},
		{`{"page_size": 30.5}`, "page_size:wrong_type"},
		{`{"page_size": null}`, "page_size:wrong_type"},
		{`{"theme": "sepia"}`, "theme:not_an_option"},
		{`{"theme": "Dark"}`, "theme:not_an_option"},
		{`{"theme": true}`, "theme:wrong_type"},
		{`{"theme": null}`, "theme:wrong_type"},
		{`{"timezone": null}`, "timezone:wrong_type"},
		{`{"shortcuts": "no"}`, "shortcuts:wrong_type"},
		{`{"shortcuts": "false"}`, "shortcuts:wrong_type"},
		{`{"shortcuts": 0}`, "shortcuts:wrong_type"},
		{`{"shortcuts": null}`, "shortcuts:wrong_type"},
		{`{"shortcuts": 0, "theme": "dark"}`, "shortcuts:wrong_type"},
		{`{"timezone": "Europe/Helsinki", "page_size": 51, "theme": "sepia"}`, "page_size:out_of_range theme:not_an_option"},
	} {
		got := strings.Join(settingsFieldErrors(t, w.do(alice, http.MethodPatch, "/api/v1/settings", c.body)), " ")
		if got != c.want {
			t.Errorf("PATCH %s: %s, want %s", c.body, got, c.want)
		}
	}
	for _, body := range []string{`{"locale": "fi"}`, `{"page_size": 20, "colour": "red"}`, `[]`, `{"page_size": 20} {}`} {
		if rec := w.do(alice, http.MethodPatch, "/api/v1/settings", body); rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if tz, prefs := w.prefsOf("alice"); tz != tz0 || prefs != prefs0 {
		t.Errorf("a refused PATCH wrote: %q %q, was %q %q", tz, prefs, tz0, prefs0)
	}
}

// TestUpdateSettingsKeepsOtherPrefs: a prefs_json key the patch does not
// name survives it, and so does a setting the patch does not name.
func TestUpdateSettingsKeepsOtherPrefs(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	w.exec(`UPDATE users SET timezone = 'Asia/Tokyo', prefs_json = '{"page_size": 20, "later": {"a": 1}}' WHERE id = ?`,
		w.users["alice"])
	s := decodeAs[dto.Settings](t, w.do(alice, http.MethodPatch, "/api/v1/settings", `{"theme": "dark"}`))
	if s != (dto.Settings{Timezone: "Asia/Tokyo", Locale: "en", PageSize: 20, Theme: "dark", Shortcuts: true}) {
		t.Errorf("answered %+v", s)
	}
	tz, prefs := w.prefsOf("alice")
	var got map[string]any
	if err := json.Unmarshal([]byte(prefs), &got); err != nil {
		t.Fatal(err)
	}
	if tz != "Asia/Tokyo" || fmt.Sprint(got) != "map[later:map[a:1] page_size:20 theme:dark]" {
		t.Errorf("stored %q %s", tz, prefs)
	}

	// A shortcuts write keeps page_size and theme, and a theme write then
	// keeps shortcuts (TKT-01M3V9DWTN).
	s = decodeAs[dto.Settings](t, w.do(alice, http.MethodPatch, "/api/v1/settings", `{"shortcuts": false}`))
	if s != (dto.Settings{Timezone: "Asia/Tokyo", Locale: "en", PageSize: 20, Theme: "dark", Shortcuts: false}) {
		t.Errorf("after shortcuts: answered %+v", s)
	}
	s = decodeAs[dto.Settings](t, w.do(alice, http.MethodPatch, "/api/v1/settings", `{"theme": "light"}`))
	if s != (dto.Settings{Timezone: "Asia/Tokyo", Locale: "en", PageSize: 20, Theme: "light", Shortcuts: false}) {
		t.Errorf("after theme: answered %+v", s)
	}
	_, prefs = w.prefsOf("alice")
	got = nil
	if err := json.Unmarshal([]byte(prefs), &got); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != "map[later:map[a:1] page_size:20 shortcuts:false theme:light]" {
		t.Errorf("stored %s", prefs)
	}
}

// TestUpdateSettingsRepairsMalformedPrefs: a stored prefs_json that is not
// a JSON object does not stop a PATCH, which stores an object holding only
// the patched key (Terva reviews 1704 and 1734).
func TestUpdateSettingsRepairsMalformedPrefs(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	for _, bad := range []string{``, `not json`, `[1]`, `null`, `"text"`, `{"page_size": `} {
		w.exec(`UPDATE users SET prefs_json = ? WHERE id = ?`, bad, w.users["alice"])
		rec := w.do(alice, http.MethodPatch, "/api/v1/settings", `{"page_size": 30}`)
		if s := decodeAs[dto.Settings](t, rec); s.PageSize != 30 || s.Theme != "system" {
			t.Errorf("over %q: answered %+v", bad, s)
		}
		if _, prefs := w.prefsOf("alice"); prefs != `{"page_size":30}` {
			t.Errorf("over %q: stored %s", bad, prefs)
		}
	}
}

// TestSettingsApplyWithoutReLogin: after a PATCH the same cookie's next
// /me shows the new values, and its next thread page is the new size; the
// same holds for an agent's credential.
func TestSettingsApplyWithoutReLogin(t *testing.T) {
	w := newSettingsWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 30 {
		w.post(id, "carol", fmt.Sprintf("post %d", i+2), 0)
	}
	alice := w.signIn("alice", "github", firefox)
	thread := "/api/v1/threads/" + id.String() + "?page=1"
	if p := page(t, w.do(alice, http.MethodGet, thread, "")); p.Page.Size != 25 || len(p.Posts) != 25 {
		t.Fatalf("before: size %d, %d posts", p.Page.Size, len(p.Posts))
	}
	body := `{"timezone": "Europe/Helsinki", "page_size": 10, "theme": "light"}`
	if rec := w.do(alice, http.MethodPatch, "/api/v1/settings", body); rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	me := decodeMe(t, w.do(alice, http.MethodGet, "/api/v1/me", "").Body.Bytes())
	if me.Settings != (dto.Settings{Timezone: "Europe/Helsinki", Locale: "en", PageSize: 10, Theme: "light", Shortcuts: true}) {
		t.Errorf("/me after: %+v", me.Settings)
	}
	if s := decodeAs[dto.Settings](t, w.do(alice, http.MethodGet, "/api/v1/settings", "")); s != me.Settings {
		t.Errorf("GET /settings %+v, /me %+v", s, me.Settings)
	}
	if p := page(t, w.do(alice, http.MethodGet, thread, "")); p.Page.Size != 10 || len(p.Posts) != 10 || p.Page.TotalPages != 3 {
		t.Errorf("after: size %d, %d posts, %d pages", p.Page.Size, len(p.Posts), p.Page.TotalPages)
	}
	// A permalink lands on the page that holds it at the member's size.
	if p := page(t, w.do(alice, http.MethodGet, "/api/v1/threads/"+id.String()+"?ordinal=25", "")); p.Page.Number != 3 {
		t.Errorf("ordinal 25 at 10 a page is on page %d", p.Page.Number)
	}
	// Anonymous is still 25.
	if p := page(t, w.do("", http.MethodGet, thread, "")); p.Page.Size != 25 {
		t.Errorf("anonymous: size %d", p.Page.Size)
	}

	// Turning the shortcuts off shows on the next /me (TKT-01M3V9DWTN), and
	// anonymous still has them on.
	rec := w.do(alice, http.MethodPatch, "/api/v1/settings", `{"shortcuts": false}`)
	if s := decodeAs[dto.Settings](t, rec); rec.Code != http.StatusOK || s.Shortcuts {
		t.Fatalf("PATCH shortcuts: %d %+v", rec.Code, s)
	}
	if me := decodeMe(t, w.do(alice, http.MethodGet, "/api/v1/me", "").Body.Bytes()); me.Settings.Shortcuts {
		t.Errorf("/me after turning the shortcuts off: %+v", me.Settings)
	}
	if me := decodeMe(t, w.do("", http.MethodGet, "/api/v1/me", "").Body.Bytes()); !me.Settings.Shortcuts {
		t.Errorf("anonymous /me: %+v", me.Settings)
	}
}

// TestPageSizeIsInTheCacheKey is the response-cache invariant: two members
// of the same viewer class with page sizes 10 and 50 read the same thread
// through the response cache, each gets their own page, and neither is
// served the other's entry.
func TestPageSizeIsInTheCacheKey(t *testing.T) {
	w := newSettingsWorld(t)
	id, _ := w.thread(1, "Long")
	for i := range 59 {
		w.post(id, "bob", fmt.Sprintf("post %d", i+2), 0)
	}
	w.exec(`UPDATE users SET prefs_json = '{"page_size": 10}' WHERE id = ?`, w.users["alice"])
	w.exec(`UPDATE users SET prefs_json = '{"page_size": 50}' WHERE id = ?`, w.users["carol"])
	// carol and alice are both plain members, one viewer class: only the page
	// size tells their pages apart.
	alice, carol := w.signIn("alice", "github", firefox), sessiontest.SignIn(t, w.db, w.users["carol"], w.now)
	path := "/api/v1/threads/" + id.String() + "?page=1"
	for round := range 2 {
		for _, c := range []struct {
			who   string
			token string
			size  int
		}{{"alice", alice, 10}, {"carol", carol, 50}} {
			before := w.svc.Responses.Stats()
			p := page(t, w.do(c.token, http.MethodGet, path, ""))
			after := w.svc.Responses.Stats()
			if p.Page.Size != c.size || len(p.Posts) != c.size || p.Posts[c.size-1].Ordinal != c.size {
				t.Errorf("round %d, %s: size %d, %d posts", round, c.who, p.Page.Size, len(p.Posts))
			}
			// The first round fills an entry for each size; the second is a
			// hit on each member's own.
			if hit := after.Hits > before.Hits; hit != (round == 1) {
				t.Errorf("round %d, %s: hit %v (stats %+v then %+v)", round, c.who, hit, before, after)
			}
		}
	}
}

// TestProfileSettingsThroughTheAPI (TKT-01M3V6XVVG): a PATCH answers the
// profile as stored, GET /settings/profile reads it back, and the profile
// route then serves the new display name and the bio rendered, a link and a
// mention in it. The mention notifies nobody. A key the body may not name
// is bad-request, and a refused value is validation-failed with its field.
func TestProfileSettingsThroughTheAPI(t *testing.T) {
	w := newSettingsWorld(t)
	alice := w.signIn("alice", "github", firefox)
	bob := w.signIn("bob", "github", firefox)
	notes := func() int {
		t.Helper()
		var n int
		if err := w.db.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
			return tx.QueryRowContext(t.Context(), `SELECT count(*) FROM notifications`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := notes()

	const bio = "Ask @carol about the [book list](https://example.org/books)."
	got := decodeAs[dto.ProfileSettings](t, w.do(alice, http.MethodPatch, "/api/v1/settings/profile",
		`{"display_name": "  Alice Liddell ", "bio": "`+bio+`"}`))
	if got.DisplayName != "Alice Liddell" || got.Bio != bio {
		t.Fatalf("the PATCH answered %+v", got)
	}
	got = decodeAs[dto.ProfileSettings](t, w.do(alice, http.MethodGet, "/api/v1/settings/profile", ""))
	if got.DisplayName != "Alice Liddell" || got.Bio != bio {
		t.Errorf("GET /settings/profile: %+v", got)
	}

	p := decodeAs[dto.Profile](t, w.do(bob, http.MethodGet, "/api/v1/users/alice", ""))
	if p.User.DisplayName != "Alice Liddell" {
		t.Errorf("the profile's user: %+v", p.User)
	}
	if !strings.Contains(p.BioHTML, `href="https://example.org/books"`) || !strings.Contains(p.BioHTML, `/u/carol`) {
		t.Errorf("the profile's bio_html: %s", p.BioHTML)
	}
	if n := notes(); n != before {
		t.Errorf("%d notifications, were %d: a mention in a bio notified", n, before)
	}

	if rec := w.do(alice, http.MethodPatch, "/api/v1/settings/profile", `{"avatar": "x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown key: %d %s", rec.Code, rec.Body)
	}
	rec := w.do(alice, http.MethodPatch, "/api/v1/settings/profile", `{"display_name": 3}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"field":"display_name","code":"wrong_type"`) {
		t.Errorf("a number for a name: %d %s", rec.Code, rec.Body)
	}
	got = decodeAs[dto.ProfileSettings](t, w.do(alice, http.MethodGet, "/api/v1/settings/profile", ""))
	if got.DisplayName != "Alice Liddell" {
		t.Errorf("a refusal wrote: %+v", got)
	}
}
