package auth

import (
	"database/sql"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/agentcred"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// These are TKT-01M29CKRES's: account linking through the explicit "link a
// sign-in" action, and nothing else.

// fakeAndOther gives w two providers at its IdP, "fake" and "other".
// configure adjusts "other".
func (w *world) fakeAndOther(configure ...func(*config.Provider)) {
	w.t.Helper()
	fake, other := w.idp.Provider("fake"), w.idp.Provider("other")
	fake.Name, other.Name = "Fake", "Other"
	for _, c := range configure {
		c(&other)
	}
	w.withProviders(fake, other)
}

// postForm sends a same-origin form POST to target, as a form on this site
// does.
func (w *world) postForm(target string, form url.Values, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	r := httptest.NewRequest(http.MethodPost, site+target, strings.NewReader(form.Encode()))
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

var continueLink = regexp.MustCompile(`<a href="([^"]+)">Continue to`)

// startLinking posts the link page's form for provider key with returnTo,
// as the browser holding cookies, and returns the flow cookie and the IdP
// address the continue page sends the browser to.
func (w *world) startLinking(key, returnTo string, cookies ...*http.Cookie) (*http.Cookie, string) {
	w.t.Helper()
	resp := w.postForm("/auth/"+key+"/link", url.Values{"return_to": {returnTo}}, cookies...)
	b := body(resp)
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("the link start answered %d:\n%s", resp.StatusCode, b)
	}
	flow := cookie(resp, FlowCookie)
	if flow == nil || flow.Value == "" {
		w.t.Fatal("the link start set no flow cookie")
	}
	m := continueLink.FindStringSubmatch(b)
	if m == nil {
		w.t.Fatalf("the continue page has no link on:\n%s", b)
	}
	authURL := html.UnescapeString(m[1])
	if !strings.Contains(b, `<meta http-equiv="refresh" content="0; url=`+m[1]+`">`) {
		w.t.Errorf("the continue page does not send the browser on by itself:\n%s", b)
	}
	return flow, authURL
}

// linkWith is the whole link: the start, the IdP, the callback, as the
// browser holding cookies.
func (w *world) linkWith(key, returnTo string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	flow, authURL := w.startLinking(key, returnTo, cookies...)
	return w.finishAt(key, flow, authURL, cookies...)
}

// finishAt is the IdP and the callback of a sign-in whose start set flow.
func (w *world) finishAt(key string, flow *http.Cookie, authURL string, cookies ...*http.Cookie) *http.Response {
	w.t.Helper()
	cb := w.idp.Authorize(w.t, authURL)
	prefix := site + "/auth/" + key + "/callback"
	if !strings.HasPrefix(cb, prefix) {
		w.t.Fatalf("the IdP sent the browser to %s", cb)
	}
	return w.do("GET", strings.TrimPrefix(cb, site), append([]*http.Cookie{flow}, cookies...)...)
}

// identities is every identities row, as id:user:provider:subject:last
// login set, which a refused link must leave as it was.
func (w *world) identities() string {
	w.t.Helper()
	var s sql.NullString
	if err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), `SELECT group_concat(id || ':' || coalesce(user_id, '-') || ':' ||
			provider_key || ':' || subject || ':' || (last_login_at IS NOT NULL), ',') FROM identities`).Scan(&s)
	}); err != nil {
		w.t.Fatal(err)
	}
	return s.String
}

// owner is the user id the identity at provider for subject belongs to: 0
// for none, -1 when there is no such identity.
func (w *world) owner(provider, subject string) int64 {
	w.t.Helper()
	var u sql.NullInt64
	err := w.db.Read(w.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(w.t.Context(), "SELECT user_id FROM identities WHERE provider_key = ? AND subject = ?",
			provider, subject).Scan(&u)
	})
	if err == sql.ErrNoRows {
		return -1
	}
	if err != nil {
		w.t.Fatal(err)
	}
	return u.Int64
}

// signedInAlice is alice, active, at "fake" as user-1, and her session.
func (w *world) signedInAlice() (int64, *http.Cookie) {
	w.t.Helper()
	alice := w.user("alice", "active", "user-1")
	w.idp.Subject = "user-1"
	c := cookie(w.signInWith("fake"), session.CookieName)
	if c == nil || c.Value == "" {
		w.t.Fatal("alice did not sign in")
	}
	return alice, c
}

// stillSignedIn checks the session c holds works, as handle.
func (w *world) stillSignedIn(c *http.Cookie, handle string) {
	w.t.Helper()
	s, err := w.store.Lookup(w.t.Context(), c.Value)
	if err != nil || s.User == nil || s.User.Handle != handle {
		w.t.Errorf("the session is not %s's any more: %+v, %v", handle, s.User, err)
	}
}

// AC2, AC4, AC5, AC6 and AC7: alice links a second provider's identity. It
// is attached to her, with one audit row, no new user and no new session,
// and nothing recorded as a sign-in; her session is untouched. Signing in
// with it afterwards is a sign-in as alice, through the callback and
// session.IssueUser. With alice pending, suspended or deactivated after the
// link, that sign-in is "Account unavailable" with no session row: linking
// is no second way to a member session.
func TestLinkingThenSigningIn(t *testing.T) {
	for _, status := range []string{"active", "pending", "suspended", "deactivated"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t)
			w.fakeAndOther()
			alice, aliceCookie := w.signedInAlice()
			users, sessions := w.count("SELECT count(*) FROM users"), w.sessions()

			w.idp.Subject = "other-sub"
			resp := w.linkWith("other", "/t/abc/x", aliceCookie)
			if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/t/abc/x" {
				t.Fatalf("the link answered %d to %q:\n%s", resp.StatusCode, resp.Header.Get("Location"), body(resp))
			}
			if c := cookie(resp, session.CookieName); c != nil {
				t.Errorf("the link touched the session cookie: %+v", c)
			}
			if c := cookie(resp, FlowCookie); c == nil || c.MaxAge >= 0 {
				t.Errorf("the flow cookie was not cleared: %+v", c)
			}
			if got := w.owner("other", "other-sub"); got != alice {
				t.Fatalf("the identity belongs to %d, want alice (%d)", got, alice)
			}
			if w.count("SELECT count(*) FROM users") != users || w.sessions() != sessions {
				t.Errorf("the link wrote a user or a session: %s", w.rows())
			}
			if w.lastLogin("other-sub").Valid {
				t.Error("the link was recorded as a sign-in")
			}
			if n := w.count(`SELECT count(*) FROM audit_log WHERE action = 'identity.linked' AND actor_id = ?
				AND target_type = 'user' AND target_id = CAST(? AS TEXT) AND details_json ->> 'provider' = 'other'
				AND details_json ->> 'identity' = (SELECT id FROM identities WHERE subject = 'other-sub')
				AND ip = '192.0.2.9'`, alice, alice); n != 1 {
				t.Errorf("%d identity.linked rows naming the link, want 1", n)
			}
			w.stillSignedIn(aliceCookie, "alice")

			// Signed out, then in again with the linked identity.
			if resp := w.post("/auth/logout", aliceCookie); resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("sign-out answered %d", resp.StatusCode)
			}
			if status != "active" {
				w.setStatus(alice, status)
				resp := w.signInWith("other")
				w.refused(resp, http.StatusForbidden, "Account unavailable")
				if w.lastLogin("other-sub").Valid {
					t.Error("a refused sign-in was recorded")
				}
				return
			}
			resp = w.signInWith("other")
			if h := signedInAs(t, w, resp); h != "alice" {
				t.Errorf("the linked identity signed in as %s", h)
			}
			if !w.lastLogin("other-sub").Valid {
				t.Error("the first sign-in with the linked identity was not recorded")
			}
		})
	}
}

// AC3: signed out, a second provider asserting the same verified email as
// a member takes the registration path, and never signs in as that member.
// The two identities stay two accounts' until one is linked.
func TestTwoProvidersWithOneEmailDoNotLink(t *testing.T) {
	w := newWorld(t)
	w.open()
	club, other := w.idp.Provider("club"), w.idp.Provider("other")
	w.withProviders(club, other)
	w.idp.Claims = map[string]any{"email": "a@example.org", "email_verified": true}

	w.idp.Subject = "alice-club"
	resp := w.signInWith("club")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != RegisterPath {
		t.Fatalf("the first sign-in answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h := signedInAs(t, w, w.submit("alice", cookie(resp, RegistrationCookie))); h != "alice" {
		t.Fatalf("registered as %s", h)
	}
	alice := w.owner("club", "alice-club")

	w.idp.Subject = "alice-other"
	resp = w.signInWith("other")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != RegisterPath {
		t.Fatalf("the same email at another provider answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if c := cookie(resp, session.CookieName); c != nil && c.Value != "" {
		t.Fatal("the same email at another provider was signed in")
	}
	if got := w.owner("other", "alice-other"); got != -1 {
		t.Errorf("the second identity was written, to user %d", got)
	}
	if h := signedInAs(t, w, w.submit("alice2", cookie(resp, RegistrationCookie))); h != "alice2" {
		t.Errorf("the second identity registered as %s", h)
	}
	if got := w.owner("other", "alice-other"); got == alice || got <= 0 {
		t.Errorf("the second identity belongs to %d; alice is %d", got, alice)
	}
}

// AC1 and AC6: each link that may not happen answers with its page, and
// changes no identities row, no user and no session.
func TestLinkRefusals(t *testing.T) {
	var bob *http.Cookie
	for _, c := range []struct {
		name   string
		status int
		says   string
		setup  func(w *world) // before alice starts
		// run links as alice, whose session is aliceCookie, and returns the
		// callback's answer.
		run func(w *world, aliceCookie *http.Cookie) *http.Response
	}{
		{"an identity another member has", http.StatusConflict, "already belongs to another account",
			func(w *world) { w.user("bob", "active", "bob-sub") },
			func(w *world, a *http.Cookie) *http.Response {
				w.idp.Subject = "bob-sub"
				return w.linkWith("fake", "", a)
			}},
		{"an applicant's identity", http.StatusConflict, "belongs to an application to join",
			func(w *world) {
				w.db.Write(w.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
					_, err := q.CreateIdentity(w.t.Context(), dbq.CreateIdentityParams{ProviderKey: "other", Subject: "applicant",
						Issuer: w.idp.URL, CreatedAt: 1, UpdatedAt: 1})
					return err
				})
			},
			func(w *world, a *http.Cookie) *http.Response {
				w.idp.Subject = "applicant"
				return w.linkWith("other", "", a)
			}},
		{"her own identity", http.StatusOK, "already linked to your account", nil,
			func(w *world, a *http.Cookie) *http.Response {
				w.idp.Subject = "user-1"
				return w.linkWith("fake", "", a)
			}},
		{"a link another member started", http.StatusConflict, "no longer signed in as",
			func(w *world) {
				w.user("bob", "active", "bob-sub")
				w.idp.Subject = "bob-sub"
				bob = cookie(w.signInWith("fake"), session.CookieName)
			},
			func(w *world, a *http.Cookie) *http.Response {
				// bob starts a link in this browser, then alice is signed in
				// when it comes back.
				flow, authURL := w.startLinking("other", "", bob)
				w.idp.Subject = "someone-new"
				return w.finishAt("other", flow, authURL, a)
			}},
		{"signed out during the link", http.StatusConflict, "no longer signed in as", nil,
			func(w *world, a *http.Cookie) *http.Response {
				flow, authURL := w.startLinking("other", "", a)
				w.idp.Subject = "someone-new"
				return w.finishAt("other", flow, authURL)
			}},
		{"suspended during the link", http.StatusConflict, "no longer signed in as", nil,
			func(w *world, a *http.Cookie) *http.Response {
				flow, authURL := w.startLinking("other", "", a)
				w.setStatus(w.owner("fake", "user-1"), "suspended")
				w.idp.Subject = "someone-new"
				return w.finishAt("other", flow, authURL, a)
			}},
		{"an email the gate refuses", http.StatusForbidden, "does not accept sign-ins from that account",
			nil,
			func(w *world, a *http.Cookie) *http.Response {
				w.idp.Subject = "someone-new"
				w.idp.Claims = map[string]any{"email": "eve@evil.example", "email_verified": true}
				return w.linkWith("other", "", a)
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.withAPI()
			w.fakeAndOther(func(p *config.Provider) { p.AllowedEmailDomains = []string{"example.org"} })
			w.idp.Claims = map[string]any{"email": "someone@example.org", "email_verified": true}
			if c.setup != nil {
				c.setup(w)
			}
			_, aliceCookie := w.signedInAlice()
			before, rows := w.identities(), w.rows()
			resp := c.run(w, aliceCookie)
			b := body(resp)
			if resp.StatusCode != c.status || !strings.Contains(b, c.says) {
				t.Errorf("answered %d, want %d saying %q:\n%s", resp.StatusCode, c.status, c.says, b)
			}
			if loc := resp.Header.Get("Location"); loc != "" {
				t.Errorf("sent on to %q", loc)
			}
			for _, name := range []string{session.CookieName, RegistrationCookie} {
				if ck := cookie(resp, name); ck != nil {
					t.Errorf("the refusal set %s: %+v", name, ck)
				}
			}
			if after := w.identities(); after != before {
				t.Errorf("identities changed:\n  %s\nwas\n  %s", after, before)
			}
			if after := w.rows(); after != rows {
				t.Errorf("the refusal left %s, before %s", after, rows)
			}
			if n := w.count("SELECT count(*) FROM audit_log WHERE action = 'identity.linked'"); n != 0 {
				t.Errorf("%d identity.linked rows", n)
			}
		})
	}
}

// AC6: without the link action, an identity nobody has, arriving at a
// browser a member is signed in on, is refused with 409 and writes
// nothing. The member stays signed in as themselves.
func TestNoLinkWithoutIntent(t *testing.T) {
	for _, mode := range []string{"approval", "open"} {
		t.Run(mode, func(t *testing.T) {
			w := newWorld(t)
			w.fakeAndOther()
			if mode == "open" {
				w.open()
			}
			_, aliceCookie := w.signedInAlice()
			before, rows := w.identities(), w.rows()

			w.idp.Subject = "someone-new"
			resp := w.signInWith("other", aliceCookie)
			if b := body(resp); resp.StatusCode != http.StatusConflict || !strings.Contains(b, "<h1>Sign-in not linked</h1>") ||
				!strings.Contains(b, `href="`+LinkPath+`"`) {
				t.Errorf("answered %d:\n%s", resp.StatusCode, b)
			}
			for _, name := range []string{session.CookieName, RegistrationCookie} {
				if ck := cookie(resp, name); ck != nil {
					t.Errorf("the refusal set %s: %+v", name, ck)
				}
			}
			if after := w.identities(); after != before {
				t.Errorf("identities changed:\n  %s\nwas\n  %s", after, before)
			}
			if after := w.rows(); after != rows {
				t.Errorf("the refusal left %s, before %s", after, rows)
			}
			w.stillSignedIn(aliceCookie, "alice")
		})
	}
}

// Only an active member signed in with a session may start a link, and
// only from this site. The link page sends anyone else to sign in.
func TestTheLinkStartNeedsAMember(t *testing.T) {
	w := newWorld(t)
	w.fakeAndOther()
	alice, aliceCookie := w.signedInAlice()

	t.Run("signed out", func(t *testing.T) {
		resp := w.postForm("/auth/fake/link", nil)
		if b := body(resp); resp.StatusCode != http.StatusForbidden || !strings.Contains(b, "<h1>Linking not allowed</h1>") {
			t.Errorf("answered %d:\n%s", resp.StatusCode, b)
		}
		if cookie(resp, FlowCookie) != nil {
			t.Error("a refused start set a flow cookie")
		}
	})
	t.Run("cross-site", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, site+"/auth/fake/link", nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.AddCookie(aliceCookie)
		rec := httptest.NewRecorder()
		w.srv.ServeHTTP(rec, r)
		if rec.Code != http.StatusForbidden || cookie(rec.Result(), FlowCookie) != nil {
			t.Errorf("a cross-site start answered %d, cookies %v", rec.Code, rec.Result().Cookies())
		}
	})
	t.Run("GET", func(t *testing.T) {
		if resp := w.do("GET", "/auth/fake/link", aliceCookie); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET answered %d", resp.StatusCode)
		}
	})
	t.Run("an unknown provider", func(t *testing.T) {
		resp := w.postForm("/auth/nope/link", nil, aliceCookie)
		if resp.StatusCode != http.StatusNotFound || cookie(resp, FlowCookie) != nil {
			t.Errorf("answered %d", resp.StatusCode)
		}
	})
	t.Run("an agent credential", func(t *testing.T) {
		v := domain.Viewer{UserID: alice, Active: true, Role: domain.RoleMember}
		ctx := agentcred.With(domain.WithViewer(t.Context(), v), agentcred.Credential{UserID: alice})
		r := httptest.NewRequest(http.MethodPost, site+"/auth/fake/link", nil).WithContext(ctx)
		r.SetPathValue("provider", "fake")
		rec := httptest.NewRecorder()
		w.handler.startLink(rec, r)
		if rec.Code != http.StatusForbidden || cookie(rec.Result(), FlowCookie) != nil {
			t.Errorf("an agent's start answered %d", rec.Code)
		}
		rec = httptest.NewRecorder()
		w.handler.linkPage(rec, httptest.NewRequest(http.MethodGet, site+LinkPath, nil).WithContext(ctx))
		if rec.Code != http.StatusSeeOther {
			t.Errorf("an agent's link page answered %d", rec.Code)
		}
	})
	t.Run("the start seals the member and return_to", func(t *testing.T) {
		flow, _ := w.startLinking("other", "/t/abc/x?page=2", aliceCookie)
		var f Flow
		if err := w.sealer.Open(flowPurpose, flow.Value, FlowTTL, &f); err != nil {
			t.Fatal(err)
		}
		if f.LinkUser != alice || f.Provider != "other" || f.ReturnTo != "/t/abc/x?page=2" {
			t.Errorf("the flow is %+v", f)
		}
		flow, _ = w.startLinking("other", "https://evil.example/", aliceCookie)
		if err := w.sealer.Open(flowPurpose, flow.Value, FlowTTL, &f); err != nil || f.ReturnTo != "/" {
			t.Errorf("a return_to off this site was sealed as %q (%v)", f.ReturnTo, err)
		}
	})
	t.Run("a plain start seals no member", func(t *testing.T) {
		resp := w.do("GET", "/auth/other/start", aliceCookie)
		var f Flow
		if err := w.sealer.Open(flowPurpose, cookie(resp, FlowCookie).Value, FlowTTL, &f); err != nil || f.LinkUser != 0 {
			t.Errorf("a plain sign-in's flow is %+v (%v)", f, err)
		}
	})

	t.Run("the link page, signed out", func(t *testing.T) {
		resp := w.do("GET", LinkPath+"?return_to=/t/abc/x")
		want := "/auth/signin?return_to=" + url.QueryEscape(LinkPath+"?return_to="+url.QueryEscape("/t/abc/x"))
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
			t.Errorf("answered %d to %q, want 303 to %q", resp.StatusCode, resp.Header.Get("Location"), want)
		}
	})
	t.Run("the link page", func(t *testing.T) {
		resp := w.do("GET", LinkPath+"?return_to=/t/abc/x", aliceCookie)
		b := body(resp)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("answered %d, Cache-Control %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
		for _, want := range []string{`<form method="post" action="/auth/fake/link">`, `<button type="submit">Link Fake</button>`,
			`<form method="post" action="/auth/other/link">`, `<button type="submit">Link Other</button>`,
			`<input type="hidden" name="return_to" value="/t/abc/x">`} {
			if !strings.Contains(b, want) {
				t.Errorf("the page has no %s:\n%s", want, b)
			}
		}
	})
	t.Run("the link page, suspended", func(t *testing.T) {
		w.setStatus(alice, "suspended")
		defer w.setStatus(alice, "active")
		if resp := w.do("GET", LinkPath, aliceCookie); resp.StatusCode != http.StatusSeeOther {
			t.Errorf("answered %d", resp.StatusCode)
		}
		if resp := w.postForm("/auth/fake/link", nil, aliceCookie); resp.StatusCode != http.StatusForbidden {
			t.Errorf("the start answered %d", resp.StatusCode)
		}
	})
}
