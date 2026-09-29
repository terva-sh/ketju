package auth

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/service/identity"
)

// These are TKT-01M29CKRE9's: several providers at once, the
// allowed_email_domains gate and the email_verified it needs, and
// auto_approve under approval mode. registration.mode is "approval" unless
// a test opens it, as on a fresh install.

// clubAndPublic gives w two providers at its IdP: "club", the organization's
// own SSO with auto_approve, and "public", an ordinary one.
func (w *world) clubAndPublic() {
	w.t.Helper()
	club, public := w.idp.Provider("club"), w.idp.Provider("public")
	club.Name, club.AutoApprove = "Club SSO", true
	public.Name = "Public"
	w.withProviders(club, public)
}

// rows is how many users, identities, sessions and applications exist.
func (w *world) rows() string {
	w.t.Helper()
	return fmt.Sprintf("%d users, %d identities, %d sessions, %d applications",
		w.count("SELECT count(*) FROM users"), w.count("SELECT count(*) FROM identities"),
		w.count("SELECT count(*) FROM sessions"), w.count("SELECT count(*) FROM applications"))
}

// AC1 and AC2: with allowed_email_domains set, a sign-in passes only with a
// verified email in an allowed domain. Each refusal is 403 with its own
// page and writes nothing: under approval mode a sign-in that passed would
// write an identity and a pre-user session, so an empty database is the
// gate's doing.
func TestTheDomainGateNeedsAVerifiedEmail(t *testing.T) {
	gated := func(pc *config.Provider) { pc.AllowedEmailDomains = []string{"example.org"} }
	const notAllowed = "This forum does not accept sign-ins from that account"
	const unverified = "only with a verified email address"
	for _, c := range []struct {
		name   string
		claims map[string]any
		says   string
	}{
		{"another domain, verified", map[string]any{"email": "eve@evil.example", "email_verified": true}, notAllowed},
		{"no email, verified", map[string]any{"email_verified": true}, notAllowed},
		{"the domain, email_verified false", map[string]any{"email": "eve@example.org", "email_verified": false}, unverified},
		{"the domain, email_verified absent", map[string]any{"email": "eve@example.org"}, unverified},
		{"the domain, email_verified the string false", map[string]any{"email": "eve@example.org", "email_verified": "false"}, unverified},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, gated)
			w.withAPI()
			w.idp.Claims = c.claims
			resp := w.signIn("/t/abc/x")
			w.refused(resp, http.StatusForbidden, "Sign-in not allowed")
			if b := body(w.signIn("")); !strings.Contains(b, c.says) {
				t.Errorf("the page does not say %q:\n%s", c.says, b)
			}
			if got := w.rows(); got != "0 users, 0 identities, 0 sessions, 0 applications" {
				t.Errorf("the refused sign-in left %s", got)
			}
			w.notAMember(resp.Cookies()...)
		})
	}

	// The gate is before the identity is looked up, so it holds for a
	// member whose identity exists as for someone new.
	t.Run("a returning member, unverified", func(t *testing.T) {
		w := newWorld(t, gated)
		w.user("alice", "active", "user-1")
		w.idp.Claims = map[string]any{"email": "alice@example.org", "email_verified": false}
		w.refused(w.signIn(""), http.StatusForbidden, "Sign-in not allowed")
		w.idp.Claims = map[string]any{"email": "alice@example.org", "email_verified": true}
		if h := signedInAs(t, w, w.signIn("")); h != "alice" {
			t.Errorf("signed in as %s", h)
		}
	})

	// The control: a verified address in the domain gets through, here to
	// the application form.
	t.Run("the domain, verified", func(t *testing.T) {
		w := newWorld(t, gated)
		w.idp.Claims = map[string]any{"email": "ann@EXAMPLE.org", "email_verified": true}
		if resp := w.signIn(""); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != ApplyPath {
			t.Errorf("answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	// With no list, nothing requires email_verified.
	t.Run("no list, unverified", func(t *testing.T) {
		w := newWorld(t)
		w.idp.Claims = map[string]any{"email": "ann@anywhere.example", "email_verified": false}
		if resp := w.signIn(""); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != ApplyPath {
			t.Errorf("answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})
}

// AC3 and AC4: under approval mode, someone new from the auto_approve
// provider is an active member at the callback, under the handle their
// preferred_username makes, with no application and no handle screen.
// Someone new from the other provider gets no user and no member session,
// only the application flow's pre-user session.
func TestAutoApproveUnderApprovalMode(t *testing.T) {
	w := newWorld(t)
	w.withAPI()
	w.clubAndPublic()

	w.idp.Subject = "ada-sub"
	w.idp.Claims = map[string]any{"preferred_username": "Ada.L", "name": "Ada Lovelace",
		"email": "ada@club.example", "email_verified": true}
	resp := w.signInWith("club")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("the callback answered %d to %q:\n%s", resp.StatusCode, resp.Header.Get("Location"), body(resp))
	}
	if cookie(resp, RegistrationCookie) != nil {
		t.Error("the callback set a registration cookie: the handle screen was not skipped")
	}
	s := cookie(resp, session.CookieName)
	if u := w.me(s); u == nil || u.Handle != "ada_l" || u.DisplayName != "Ada Lovelace" {
		t.Fatalf("/me shows %+v, want ada_l", u)
	}
	if code := w.draft(s); code != http.StatusNotFound {
		t.Errorf("a member endpoint answered %d, want 404", code)
	}
	if n := w.count("SELECT count(*) FROM users WHERE handle = 'ada_l' AND status = 'active' AND role = 'member'"); n != 1 {
		t.Errorf("%d active members ada_l", n)
	}
	if got := w.rows(); got != "1 users, 1 identities, 1 sessions, 0 applications" {
		t.Errorf("after the auto-approved registration: %s", got)
	}
	// Signing in again finds the account.
	if h := signedInAs(t, w, w.signInWith("club")); h != "ada_l" {
		t.Errorf("the next sign-in is %s", h)
	}

	// Someone new from the ordinary provider applies.
	w.idp.Subject = "bob-sub"
	w.idp.Claims = map[string]any{"preferred_username": "bob", "email": "bob@else.example", "email_verified": true}
	resp = w.signInWith("public")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != ApplyPath {
		t.Fatalf("the callback answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// A pre-user session: /me names no user, and a member endpoint is
	// refused by the pre-user gate before the member check.
	if u := w.me(resp.Cookies()...); u != nil {
		t.Errorf("/me shows %s", u.Handle)
	}
	if code := w.draft(resp.Cookies()...); code != http.StatusForbidden {
		t.Errorf("a member endpoint answered %d to a pre-user, want 403", code)
	}
	if n := w.count("SELECT count(*) FROM users"); n != 1 {
		t.Errorf("%d users; the ordinary provider made one", n)
	}
	if n := w.count("SELECT count(*) FROM identities WHERE provider_key = 'public' AND user_id IS NULL"); n != 1 {
		t.Errorf("%d applicant identities at public, want 1", n)
	}
}

// Under open mode the auto_approve provider skips the handle screen too
// (docs/05 "for those providers"), and the other one shows it.
func TestAutoApproveUnderOpenMode(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.clubAndPublic()
	w.idp.Claims = map[string]any{"preferred_username": "Ada.L"}
	w.idp.Subject = "ada-sub"
	if h := signedInAs(t, w, w.signInWith("club")); h != "ada_l" {
		t.Errorf("signed in as %s", h)
	}
	w.idp.Subject = "bob-sub"
	resp := w.signInWith("public")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != RegisterPath {
		t.Errorf("the ordinary provider answered %d to %q, want the handle screen", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// AC4's fallback: when the preferred_username makes no free handle, the
// callback writes nothing and shows the handle screen, prefilled, with a
// line naming the reason. Submitting a free handle then registers, under
// approval mode, which is the POST honouring the provider's policy.
func TestAutoApproveFallsBackToTheHandleScreen(t *testing.T) {
	cases := []struct {
		name    string
		claims  map[string]any
		prefill string
		why     string
	}{
		{"missing", map[string]any{"email": "grace@club.example"}, "grace", "the sign-in did not include one"},
		{"invalid", map[string]any{"preferred_username": "x!", "email": "grace@club.example"}, "x", "it is not a valid handle"},
		{"invalid, nothing left", map[string]any{"preferred_username": "日本語", "email": "grace@club.example"}, "grace", "it is not a valid handle"},
		{"reserved", map[string]any{"preferred_username": "Admin", "email": "grace@club.example"}, "admin", "it is reserved"},
		{"taken", map[string]any{"preferred_username": "Ada_L", "email": "grace@club.example"}, "ada_l", "it is taken"},
		{"taken by a suspended account", map[string]any{"preferred_username": "Sue", "email": "grace@club.example"}, "sue", "it is taken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			w.clubAndPublic()
			ada := w.user("ada_l", "active", "ada-sub")
			w.identityAt(ada, "club", "ada-club")
			sue := w.user("sue", "suspended", "sue-sub")
			w.identityAt(sue, "club", "sue-club")
			before := w.rows()

			w.idp.Subject = "grace-sub"
			w.idp.Claims = c.claims
			resp := w.signInWith("club")
			if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != RegisterPath {
				t.Fatalf("the callback answered %d to %q, want the handle screen:\n%s",
					resp.StatusCode, resp.Header.Get("Location"), body(resp))
			}
			if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
				t.Error("the callback set a session cookie")
			}
			if got := w.rows(); got != before {
				t.Errorf("the callback wrote: %s, before %s", got, before)
			}
			reg := cookie(resp, RegistrationCookie)

			form := w.do("GET", RegisterPath, reg)
			b := body(form)
			if form.StatusCode != http.StatusOK || !strings.Contains(b, `name="handle" type="text" value="`+c.prefill+`"`) {
				t.Errorf("the screen answered %d, want the prefill %q:\n%s", form.StatusCode, c.prefill, b)
			}
			if want := `<p id="handle-why">Your sign-in&#39;s username could not be used as your handle: ` + c.why + `.</p>`; !strings.Contains(b, want) {
				t.Errorf("the screen lacks %s:\n%s", want, b)
			}

			sub := w.submit("grace_h", reg)
			if sub.StatusCode != http.StatusSeeOther {
				t.Fatalf("the submit answered %d:\n%s", sub.StatusCode, body(sub))
			}
			if u := w.me(cookie(sub, session.CookieName)); u == nil || u.Handle != "grace_h" {
				t.Errorf("/me shows %+v, want grace_h", u)
			}
			if n := w.count("SELECT count(*) FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.handle IN ('ada_l', 'sue')"); n != 0 {
				t.Errorf("%d sessions for the handle's holder", n)
			}
		})
	}
}

// A handle screen with no reason sealed shows no reason line: an ordinary
// provider's registration, under open mode.
func TestTheHandleScreenHasNoReasonUnlessOneWasSealed(t *testing.T) {
	w := newWorld(t)
	w.open()
	w.idp.Claims = map[string]any{"preferred_username": "Carol.D"}
	b := body(w.do("GET", RegisterPath, w.registering("")))
	if strings.Contains(b, "handle-why") || !strings.Contains(b, `value="carol_d"`) {
		t.Errorf("the screen:\n%s", b)
	}
}

// AC5: under approval mode, the handle screen's POST refuses a provider
// without auto_approve even with a correctly sealed registration cookie,
// and writes nothing.
func TestRegisterUnderApprovalModeOnlyForAutoApprove(t *testing.T) {
	for _, provider := range []string{"public", "unconfigured"} {
		t.Run(provider, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			w.clubAndPublic()
			sealed, err := w.handler.sealRegistration(pending{ReturnTo: "/",
				Claims: identity.Claims{Provider: provider, Issuer: w.idp.URL, Subject: "mallory-sub"}})
			if err != nil {
				t.Fatal(err)
			}
			reg := &http.Cookie{Name: RegistrationCookie, Value: sealed}
			resp := w.submit("mallory", reg)
			w.refused(resp, http.StatusForbidden, "Registration is not open")
			if got := w.rows(); got != "0 users, 0 identities, 0 sessions, 0 applications" {
				t.Errorf("the refused registration left %s", got)
			}
			w.notAMember(resp.Cookies()...)
		})
	}
}

// TestNoProviderLetsAnInactiveUserIn is AC6, across providers and modes: a
// pending, suspended or deactivated user holding identities at the
// auto_approve provider and at an ordinary one signs in through each. Each
// sign-in is "Account unavailable", writes no session and no user, and
// leaves the browser no member. auto_approve changes only what happens to
// an identity nobody has; it never reaches an account that exists.
func TestNoProviderLetsAnInactiveUserIn(t *testing.T) {
	for _, status := range []string{"pending", "suspended", "deactivated"} {
		for _, mode := range []string{"approval", "open"} {
			for _, provider := range []string{"club", "public"} {
				t.Run(status+"/"+mode+"/"+provider, func(t *testing.T) {
					w := newWorld(t)
					w.withAPI()
					w.clubAndPublic()
					if mode == "open" {
						w.open()
					}
					id := w.user("ada_l", status, "ada-fake")
					w.identityAt(id, "club", "ada-club")
					w.identityAt(id, "public", "ada-public")
					before := w.rows()

					w.idp.Subject = "ada-" + provider
					w.idp.Claims = map[string]any{"preferred_username": "someone_new", "email": "ada@club.example", "email_verified": true}
					resp := w.signInWith(provider)
					w.refused(resp, http.StatusForbidden, "Account unavailable")
					if loc := resp.Header.Get("Location"); loc != "" {
						t.Errorf("sent on to %q", loc)
					}
					if cookie(resp, RegistrationCookie) != nil {
						t.Error("an inactive user was sent to register")
					}
					w.notAMember(resp.Cookies()...)
					if got := w.rows(); got != before {
						t.Errorf("the refused sign-in left %s, before %s", got, before)
					}
					if n := w.count("SELECT count(*) FROM users WHERE handle = 'someone_new'"); n != 0 {
						t.Error("the refused sign-in registered the preferred_username")
					}
				})
			}
		}
	}
}

// AC7: with three providers configured and one failing discovery, the login
// page lists exactly the two that work, by name, in config order, and the
// failed one's start is refused.
func TestTheLoginPageListsOnlyHealthyProviders(t *testing.T) {
	w := newWorld(t)
	club, broken, other := w.idp.Provider("club"), w.idp.Provider("broken"), w.idp.Provider("other")
	club.Name, broken.Name, other.Name = "Club SSO", "Broken IdP", "Other"
	// The IdP serves no discovery document here, so discovery fails.
	broken.Issuer = w.idp.URL + "/broken"
	w.withProviders(club, broken, other)

	resp := w.do("GET", "/auth/signin")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	b := body(resp)
	if n := strings.Count(b, "<li>"); n != 2 {
		t.Errorf("%d providers listed, want 2:\n%s", n, b)
	}
	clubAt := strings.Index(b, `<a href="/auth/club/start">Sign in with Club SSO</a>`)
	otherAt := strings.Index(b, `<a href="/auth/other/start">Sign in with Other</a>`)
	if clubAt < 0 || otherAt < 0 || otherAt < clubAt {
		t.Errorf("the page does not list Club SSO then Other:\n%s", b)
	}
	if strings.Contains(b, "Broken") || strings.Contains(b, "/auth/broken/") {
		t.Errorf("the page lists the provider that failed discovery:\n%s", b)
	}
	if resp := w.do("GET", "/auth/broken/start"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("the failed provider's start answered %d", resp.StatusCode)
	}
}
