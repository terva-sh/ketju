package auth

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/web"
)

// Approval mode's applicant paths (TKT-01M29CKRG9). The world's default
// registration.mode is approval, and its provider does not auto-approve.

// meFull is GET /api/v1/me with these cookies, whole.
func (w *world) meFull(cookies ...*http.Cookie) dto.Me {
	w.t.Helper()
	resp := w.do("GET", "/api/v1/me", cookies...)
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("/me answered %d: %s", resp.StatusCode, body(resp))
	}
	var m dto.Me
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		w.t.Fatal(err)
	}
	return m
}

// applicantCookie checks resp is a callback that placed the browser in a
// pre-user session and sent it to location, and returns the cookie.
func (w *world) applicantCookie(resp *http.Response, location string) *http.Cookie {
	w.t.Helper()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != location {
		w.t.Fatalf("the callback answered %d to %q, want 303 to %s: %s", resp.StatusCode,
			resp.Header.Get("Location"), location, body(resp))
	}
	c := cookie(resp, session.CookieName)
	if c == nil || c.Value == "" {
		w.t.Fatal("the callback set no session cookie")
	}
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		w.t.Errorf("session cookie attributes %+v", c)
	}
	s, err := w.store.Lookup(w.t.Context(), c.Value)
	if err != nil || s.Kind != session.KindPreUser || s.User != nil || s.Provider != "fake" {
		w.t.Fatalf("the cookie names %+v (%v), want a pre-user session from fake", s, err)
	}
	return c
}

// TestApprovalModeGivesAnApplicantAPreUserSession is AC4: someone new, from
// a provider without auto_approve, gets an identity with no user and a
// pre-user session, written together, and 303 to /apply whatever return_to
// said. Signing in again with the same identity is 303 to /apply/status.
// Nothing makes a user.
func TestApprovalModeGivesAnApplicantAPreUserSession(t *testing.T) {
	w := newWorld(t)
	w.withAPI()
	w.idp.Subject = "ada"
	first := w.applicantCookie(w.signIn("/f/general"), ApplyPath)

	if n := w.count("SELECT count(*) FROM identities WHERE provider_key = 'fake' AND subject = 'ada' AND user_id IS NULL"); n != 1 {
		t.Errorf("%d identities with no user for the subject, want 1", n)
	}
	if n := w.count("SELECT count(*) FROM users"); n != 0 {
		t.Errorf("%d users, want none", n)
	}
	if !w.lastLogin("ada").Valid {
		t.Error("applying was not recorded as the identity's first sign-in")
	}
	me := w.meFull(first)
	if me.User != nil || me.PreUser == nil || me.PreUser.Provider != "fake" {
		t.Errorf("/me as the applicant is %+v, want no user and pre_user from fake", me)
	}
	// The session reaches nothing else: not a member endpoint, not the
	// handle screen.
	if code := w.draft(first); code != http.StatusForbidden {
		t.Errorf("a member endpoint answered %d, want 403", code)
	}
	if resp := w.do("GET", RegisterPath, first); resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != web.ApplyStatusPath {
		t.Errorf("the handle screen answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// Coming back, from a browser that kept nothing.
	again := w.applicantCookie(w.signIn("/f/general"), web.ApplyStatusPath)
	if again.Value == first.Value {
		t.Error("the second sign-in reused the first session's id")
	}
	if n := w.count("SELECT count(*) FROM identities"); n != 1 {
		t.Errorf("%d identities after signing in again, want the one", n)
	}
	if n := w.count("SELECT count(*) FROM users"); n != 0 {
		t.Errorf("%d users, want none", n)
	}
	if n := w.count("SELECT count(*) FROM sessions WHERE kind = 'user'"); n != 0 {
		t.Errorf("%d member sessions, want none", n)
	}
}

// TestAnApplicantSignInEndsTheSessionItReplaces: both applicant paths end
// the session the browser held, as every sign-in does (fixation).
func TestAnApplicantSignInEndsTheSessionItReplaces(t *testing.T) {
	for _, returning := range []bool{false, true} {
		name := map[bool]string{false: "someone new", true: "an applicant coming back"}[returning]
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			// The browser holds another applicant's session.
			w.idp.Subject = "other"
			old := w.applicantCookie(w.signIn(""), ApplyPath)
			w.idp.Subject = "user-1"
			want := ApplyPath
			if returning {
				w.applicantCookie(w.signIn(""), ApplyPath)
				want = web.ApplyStatusPath
			}
			before := w.sessions()

			flow, authURL := w.begin("")
			w.applicantCookie(w.do("GET", w.back(authURL), flow, old), want)
			if _, err := w.store.Lookup(t.Context(), old.Value); err == nil {
				t.Error("the session the sign-in replaced still works")
			}
			if n := w.sessions(); n != before {
				t.Errorf("%d sessions, want %d: one issued, one ended", n, before)
			}
		})
	}
}

// An applicant sign-in that cannot end the session it replaces leaves no new
// session behind (review 511).
func TestAnApplicantSignInThatCannotEndTheOldSessionFails(t *testing.T) {
	for _, returning := range []bool{false, true} {
		t.Run(map[bool]string{false: "someone new", true: "coming back"}[returning], func(t *testing.T) {
			w := newWorld(t)
			w.idp.Subject = "other"
			old := w.applicantCookie(w.signIn(""), ApplyPath)
			w.idp.Subject = "user-1"
			if returning {
				w.applicantCookie(w.signIn(""), ApplyPath)
			}
			before := w.sessions()
			w.handler.Login.(*identity.Service).Sessions = failingRevoke{w.store, old.Value}

			flow, authURL := w.begin("")
			resp := w.do("GET", w.back(authURL), flow, old)
			if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("Location") != "" {
				t.Errorf("answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
			}
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Error("a failed sign-in set a session cookie")
			}
			if n := w.sessions(); n != before {
				t.Errorf("%d sessions, want %d: the new one ended", n, before)
			}
			if returning {
				return
			}
			// Someone new: the failure wrote no identity, so the retry,
			// with the old session ended, is still an application and goes
			// to the form, not to a status page for an application that
			// never began (review 1397).
			if n := w.count("SELECT count(*) FROM identities WHERE subject = 'user-1'"); n != 0 {
				t.Errorf("the failed application left %d identities behind", n)
			}
			w.handler.Login.(*identity.Service).Sessions = w.store
			flow, authURL = w.begin("")
			w.applicantCookie(w.do("GET", w.back(authURL), flow, old), ApplyPath)
		})
	}
}

// A pre-user holding a registration cookie, once registration is open,
// cannot use the handle screen: they sign out first. The gate refuses them
// before the handler runs, so no account is made.
func TestAPreUserCannotRegister(t *testing.T) {
	w := newWorld(t)
	applicant := w.applicantCookie(w.signIn(""), ApplyPath)
	w.open()
	w.idp.Subject = "someone-else"
	reg := cookie(w.signIn(""), RegistrationCookie)
	if reg == nil || reg.Value == "" {
		t.Fatal("the callback set no registration cookie")
	}
	resp := w.submit("carol", reg, applicant)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("the handle screen answered %d to a pre-user, want 403", resp.StatusCode)
	}
	if n := w.count("SELECT count(*) FROM users"); n != 0 {
		t.Errorf("%d users, want none", n)
	}
	if _, err := w.store.Lookup(t.Context(), applicant.Value); err != nil {
		t.Errorf("the refusal ended the applicant's session: %v", err)
	}
}

// Sign-out is on the list: an applicant can leave.
func TestAnApplicantCanSignOut(t *testing.T) {
	w := newWorld(t)
	applicant := w.applicantCookie(w.signIn(""), ApplyPath)
	resp := w.do("POST", "/auth/logout", applicant)
	if resp.StatusCode != http.StatusSeeOther || !cleared(resp, session.CookieName) {
		t.Errorf("sign-out answered %d", resp.StatusCode)
	}
	if n := w.sessions(); n != 0 {
		t.Errorf("%d sessions after signing out", n)
	}
}
