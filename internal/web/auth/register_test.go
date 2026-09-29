package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/auth/session/sessiontest"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/settings"
)

// open sets registration.mode to "open".
func (w *world) open() {
	w.t.Helper()
	if err := settings.Set(w.t.Context(), w.db, settings.RegistrationMode.Name, json.RawMessage(`"open"`), 0, time.Now()); err != nil {
		w.t.Fatal(err)
	}
}

// submit posts a handle to the handle screen as the form does.
func (w *world) submit(handle string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	r := httptest.NewRequest("POST", site+RegisterPath, strings.NewReader(url.Values{"handle": {handle}}.Encode()))
	r.RemoteAddr = "192.0.2.9:4000"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", site)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	rec := httptest.NewRecorder()
	w.srv.ServeHTTP(rec, r)
	return rec.Result()
}

// registering signs in as someone new and returns the registration cookie.
func (w *world) registering(returnTo string) *http.Cookie {
	w.t.Helper()
	resp := w.signIn(returnTo)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != RegisterPath {
		w.t.Fatalf("the callback answered %d to %q, want the handle screen", resp.StatusCode, resp.Header.Get("Location"))
	}
	reg := cookie(resp, RegistrationCookie)
	if reg == nil || reg.Value == "" {
		w.t.Fatal("the callback set no registration cookie")
	}
	if !reg.Secure || !reg.HttpOnly || reg.SameSite != http.SameSiteLaxMode || reg.Path != "/" ||
		reg.MaxAge != int(RegistrationTTL/time.Second) {
		w.t.Errorf("registration cookie attributes %+v", reg)
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		w.t.Error("the callback signed someone in before they chose a handle")
	}
	if n := w.sessions(); n != 0 {
		w.t.Errorf("the callback wrote %d sessions", n)
	}
	return reg
}

func cleared(resp *http.Response, name string) bool {
	c := cookie(resp, name)
	return c != nil && c.Value == "" && c.MaxAge < 0
}

// The whole of open registration: the callback, the handle screen with its
// prefill, the submit, signed in where the sign-in was going.
func TestOpenRegistration(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.idp.Claims = map[string]any{"preferred_username": "Carol.D", "name": "Carol D", "email": "carol@example.org"}
	reg := w.registering("/t/abc/a-thread")

	form := w.do("GET", RegisterPath, reg)
	b := body(form)
	if form.StatusCode != http.StatusOK || !strings.Contains(b, "<h1>Choose a handle</h1>") ||
		!strings.Contains(b, `name="handle" type="text" value="carol_d"`) {
		t.Fatalf("the handle screen answered %d:\n%s", form.StatusCode, b)
	}
	if form.Header.Get("Cache-Control") != "no-store" {
		t.Error("the handle screen may be cached")
	}

	resp := w.submit("Carol", reg)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/t/abc/a-thread" {
		t.Fatalf("the submit answered %d to %q:\n%s", resp.StatusCode, resp.Header.Get("Location"), body(resp))
	}
	if h := signedInAs(t, w, resp); h != "carol" {
		t.Errorf("signed in as %s", h)
	}
	if !cleared(resp, RegistrationCookie) {
		t.Error("the registration cookie was not cleared")
	}

	// Signing in again finds the account.
	if h := signedInAs(t, w, w.signIn("")); h != "carol" {
		t.Errorf("the next sign-in is %s", h)
	}
}

// AC1 at the screen: a refused handle shows the form again with what was
// typed and why, and keeps the registration going.
func TestTheHandleScreenRefuses(t *testing.T) {
	cases := []struct {
		handle string
		status int
		says   string
	}{
		{"x", http.StatusUnprocessableEntity, "That handle is not allowed."},
		// A trailing '-' is refused for a new handle (TKT-01M3ANX1NF).
		{"bob-", http.StatusUnprocessableEntity, "That handle is not allowed."},
		{"Admin", http.StatusUnprocessableEntity, "That handle is reserved."},
		{"ALICE", http.StatusConflict, "That handle is taken."},
	}
	for _, c := range cases {
		t.Run(c.handle, func(t *testing.T) {
			w := newWorld(t)
			w.open()
			w.user("alice", "active", "someone-else")
			reg := w.registering("")
			resp := w.submit(c.handle, reg)
			b := body(resp)
			if resp.StatusCode != c.status || !strings.Contains(b, c.says) || !strings.Contains(b, `role="alert"`) ||
				!strings.Contains(b, `value="`+c.handle+`"`) {
				t.Fatalf("answered %d:\n%s", resp.StatusCode, b)
			}
			if cookie(resp, RegistrationCookie) != nil {
				t.Error("a refused handle touched the registration cookie")
			}
			if n := w.sessions(); n != 0 {
				t.Errorf("a refused handle wrote %d sessions", n)
			}
			// And the registration goes on with another handle.
			if h := signedInAs(t, w, w.submit("carol", reg)); h != "carol" {
				t.Errorf("signed in as %s", h)
			}
		})
	}
}

// What is typed is shown back escaped.
func TestTheHandleScreenEscapes(t *testing.T) {
	w := newWorld(t)
	w.open()
	resp := w.submit(`"><script>x</script>`, w.registering(""))
	if b := body(resp); strings.Contains(b, "<script>") {
		t.Errorf("the typed handle was not escaped:\n%s", b)
	}
}

// The handle screen with no registration, an expired one, or a cookie
// sealed for something else, says to sign in again and writes nothing.
func TestTheHandleScreenNeedsARegistration(t *testing.T) {
	w := newWorld(t)
	w.open()
	flow, _ := w.begin("")
	reg := w.registering("")
	w.now = w.now.Add(RegistrationTTL + time.Second)
	for name, c := range map[string]*http.Cookie{
		"none":          nil,
		"expired":       reg,
		"a flow cookie": {Name: RegistrationCookie, Value: flow.Value},
		"garbage":       {Name: RegistrationCookie, Value: "garbage"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, resp := range []*http.Response{w.do("GET", RegisterPath, c), w.submit("carol", c)} {
				w.refused(resp, http.StatusBadRequest, "Registration expired")
				if !cleared(resp, RegistrationCookie) {
					t.Error("the registration cookie was not cleared")
				}
			}
		})
	}
}

// A second submit, from another tab or a double click, finds the account
// made and says so.
func TestTheHandleScreenSubmittedTwice(t *testing.T) {
	w := newWorld(t)
	w.open()
	reg := w.registering("")
	signedInAs(t, w, w.submit("carol", reg))
	resp := w.submit("carol2", reg)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body(resp), "<h1>Already registered</h1>") {
		t.Fatalf("answered %d", resp.StatusCode)
	}
	if !cleared(resp, RegistrationCookie) {
		t.Error("the registration cookie was not cleared")
	}
}

// Closed while someone was choosing: the submit says so and writes nothing.
func TestRegistrationClosedMidway(t *testing.T) {
	w := newWorld(t)
	w.open()
	reg := w.registering("")
	if err := settings.Set(t.Context(), w.db, settings.RegistrationMode.Name, json.RawMessage(`"approval"`), 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	w.refused(w.submit("carol", reg), http.StatusForbidden, "Registration is not open")
}

// A member who signed in, in another tab, since the callback does not make
// a second account by submitting the form.
func TestAMemberDoesNotRegisterASecondAccount(t *testing.T) {
	w := newWorld(t)
	w.open()
	reg := w.registering("")
	w.user("bob", "active", "bob-sub")
	w.idp.Subject = "bob-sub"
	bob := cookie(w.signIn(""), session.CookieName)
	resp := w.submit("carol", reg, bob)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(body(resp), "Linking is not available yet") {
		t.Fatalf("answered %d", resp.StatusCode)
	}
	if _, err := w.store.Lookup(t.Context(), bob.Value); err != nil {
		t.Errorf("bob's session was ended: %v", err)
	}
}

// A browser that holds a session that is not a member's, such as one whose
// user was suspended after it was issued, comes out of registration with
// only the new session: it never keeps a session id it did not issue. (An
// approval-mode applicant's pre-user session never reaches the handle
// screen: the pre-user gate refuses it, TestAPreUserCannotRegister.)
func TestRegistrationEndsTheSessionItReplaces(t *testing.T) {
	w := newWorld(t)
	w.open()
	reg := w.registering("")
	old := sessiontest.SignIn(t, w.db, w.user("sam", "suspended", "sam-sub"), time.Now())
	resp := w.submit("carol", reg, &http.Cookie{Name: session.CookieName, Value: old})
	if h := signedInAs(t, w, resp); h != "carol" {
		t.Fatalf("signed in as %s", h)
	}
	if _, err := w.store.Lookup(t.Context(), old); err == nil {
		t.Error("the session the registration replaced still works")
	}
	if n := w.sessions(); n != 1 {
		t.Errorf("%d sessions, want the new one", n)
	}
}

// A form on another site cannot submit the handle screen.
func TestTheHandleScreenRefusesCrossSite(t *testing.T) {
	w := newWorld(t)
	w.open()
	reg := w.registering("")
	r := httptest.NewRequest("POST", site+RegisterPath, strings.NewReader("handle=carol"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(&http.Cookie{Name: reg.Name, Value: reg.Value})
	rec := httptest.NewRecorder()
	w.srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || w.sessions() != 0 {
		t.Errorf("a cross-site submit answered %d and wrote %d sessions", rec.Code, w.sessions())
	}
}

// A config-named admin who registers is an admin at once (TKT-01M29C800G).
func TestRegistrationAppliesTheBootstrap(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.bootstrap(config.Bootstrap{AdminSubjects: []string{"fake:user-1"}})
	signedInAs(t, w, w.submit("boss", w.registering("")))
	if role, n := w.role("boss"); role != "admin" || n != 1 {
		t.Errorf("role %s, %d promotions; want admin, 1", role, n)
	}
}

// Claims too large for a cookie shed what the registration can do without,
// so the browser keeps the cookie: the name and preferred_username first,
// then the return_to (review 1146).
func TestLargeClaimsStillFitTheCookie(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.idp.Claims = map[string]any{"name": strings.Repeat("N", 5000), "preferred_username": strings.Repeat("u", 5000)}
	reg := w.registering("/t/abc/" + strings.Repeat("r", 5000))
	if len(reg.Value) > MaxRegistrationCookie {
		t.Fatalf("the registration cookie is %d bytes", len(reg.Value))
	}
	if b := body(w.do("GET", RegisterPath, reg)); !strings.Contains(b, `name="handle" type="text" value=""`) {
		t.Errorf("the prefill survived shedding preferred_username:\n%s", b)
	}
	resp := w.submit("carol", reg)
	if h := signedInAs(t, w, resp); h != "carol" {
		t.Fatalf("signed in as %s", h)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("went to %q, want / once the return_to was shed", loc)
	}
	s, _ := w.store.Lookup(t.Context(), cookie(resp, session.CookieName).Value)
	if s.User.DisplayName != "carol" {
		t.Errorf("display name %q, want the handle", s.User.DisplayName)
	}
}

// A long name alone is shed while the return_to is kept.
func TestALongNameKeepsTheReturnTo(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.idp.Claims = map[string]any{"name": strings.Repeat("N", 5000)}
	resp := w.submit("carol", w.registering("/t/abc/a-thread"))
	if loc := resp.Header.Get("Location"); loc != "/t/abc/a-thread" {
		t.Errorf("went to %q", loc)
	}
}

// Claims the account needs that are still too large are refused at the
// callback, not sent to a browser that would drop them.
func TestClaimsThatCannotFitAreRefused(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.idp.Claims = map[string]any{"email": strings.Repeat("e", 5000) + "@example.org"}
	resp := w.signIn("")
	w.refused(resp, http.StatusBadRequest, "Sign-in failed")
	if c := cookie(resp, RegistrationCookie); c != nil && c.Value != "" {
		t.Error("an oversized registration cookie was set")
	}
}
