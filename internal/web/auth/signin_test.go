package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/auth/seal"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/service/identity"
	"github.com/terva-sh/ketju/internal/web"
)

// signInAt is the login page's address with return_to set to returnTo, or
// without one for "".
func signInAt(returnTo string) string {
	if returnTo == "" {
		return "/auth/signin"
	}
	return "/auth/signin?return_to=" + url.QueryEscape(returnTo)
}

// withProviders swaps w's providers for these, all at w's in-process IdP,
// and the sign-in's policy for theirs, as serve builds both from one
// configuration.
func (w *world) withProviders(pcs ...config.Provider) {
	w.t.Helper()
	reg, err := oidc.New(w.t.Context(), &config.Config{SiteURL: site, OIDC: config.OIDC{Providers: pcs}},
		oidc.Options{HTTPClient: w.idp.Client(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { reg.Close() })
	w.handler.Providers = reg
	w.handler.Login.(*identity.Service).Policy = identity.PolicyFrom(pcs)
}

// hostileReturnTo are return_to values that name somewhere other than this
// site, each spelled as a browser or a normalising proxy would read it.
var hostileReturnTo = []string{
	"https://evil.example/phish",
	"//evil.example/phish",
	"/\\evil.example",
	"/%2F%2Fevil.example",
	"/%5Cevil.example",
	"/\t/evil.example",
	"javascript:alert(1)",
	"evil.example",
}

// AC2: with one provider there is nothing to choose, so the page is a
// redirect to its start, and the sign-in it starts returns where the
// reader was.
func TestTheLoginPageSkipsToTheOnlyProvider(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")

	for returnTo, want := range map[string]string{
		"":                      "/auth/fake/start",
		"/":                     "/auth/fake/start",
		"/t/abc/a-thread?p=2#x": "/auth/fake/start?return_to=%2Ft%2Fabc%2Fa-thread%3Fp%3D2%23x",
	} {
		resp := w.do("GET", signInAt(returnTo))
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
			t.Errorf("return_to %q: %d to %q, want 302 to %q", returnTo, resp.StatusCode, resp.Header.Get("Location"), want)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("return_to %q: Cache-Control %q", returnTo, cc)
		}
	}

	// Followed through: start, the IdP, the callback, and back.
	start := w.do("GET", signInAt("/t/abc/a-thread?page=2")).Header.Get("Location")
	resp := w.do("GET", start)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start answered %d", resp.StatusCode)
	}
	resp = w.do("GET", w.back(resp.Header.Get("Location")), cookie(resp, FlowCookie))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/t/abc/a-thread?page=2" {
		t.Fatalf("callback answered %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h := signedInAs(t, w, resp); h != "alice" {
		t.Errorf("signed in as %s", h)
	}
}

// AC1: with several, the page lists each by name, in config order, as a
// link to its start carrying return_to, through the server's own headers.
func TestTheLoginPageListsTheProviders(t *testing.T) {
	w := newWorld(t)
	club, other := w.idp.Provider("club"), w.idp.Provider("other")
	club.Name = "Club SSO"
	other.Name = `<b>"Other" & co</b>`
	w.withProviders(club, other)

	resp := w.do("GET", signInAt("/u/alice"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' 'nonce-") {
		t.Errorf("the page is served without the site's policy: %q", csp)
	}
	b := body(resp)
	want := []string{
		`<a href="/auth/club/start?return_to=%2Fu%2Falice">Sign in with Club SSO</a>`,
		`<a href="/auth/other/start?return_to=%2Fu%2Falice">Sign in with &lt;b&gt;&#34;Other&#34; &amp; co&lt;/b&gt;</a>`,
	}
	at := -1
	for _, s := range want {
		i := strings.Index(b, s)
		if i < 0 {
			t.Fatalf("the page lacks %s:\n%s", s, b)
		}
		if i < at {
			t.Errorf("the providers are not in config order:\n%s", b)
		}
		at = i
	}
	if strings.Contains(b, "<b>") {
		t.Errorf("a provider's name was not escaped:\n%s", b)
	}
}

// providersNone is a registry whose every provider failed discovery.
type providersNone struct{}

func (providersNone) Lookup(string) (*oidc.Provider, bool) { return nil, false }
func (providersNone) Enabled() []*oidc.Provider            { return nil }

// AC3: with no provider configured, or none working, the page says
// sign-in is not available, as a page rather than an error.
func TestTheLoginPageWithNoProvider(t *testing.T) {
	sealer, _ := seal.New(make([]byte, seal.KeySize))
	for name, p := range map[string]Providers{"none configured": nil, "none enabled": providersNone{}} {
		t.Run(name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := &Handler{Providers: p, Sealer: sealer, Logger: log}
			srv := web.New(web.Groups{Auth: New(h)}, web.Options{Logger: log, SiteURL: site,
				RateLimit: func(h http.Handler) http.Handler { return h }})
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest("GET", site+signInAt("/u/alice"), nil))
			rec := w.Result()
			if rec.StatusCode != http.StatusOK {
				t.Errorf("status %d", rec.StatusCode)
			}
			b := body(rec)
			if !strings.Contains(b, "<h1>Sign-in is not available</h1>") || strings.Contains(b, "/start") {
				t.Errorf("the page:\n%s", b)
			}
		})
	}
}

// AC4: a reader already signed in is sent back where they came from.
func TestTheLoginPageSendsASignedInReaderBack(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	s := cookie(w.signIn(""), session.CookieName)
	if s == nil {
		t.Fatal("no session")
	}
	for returnTo, want := range map[string]string{"": "/", "/u/alice?tab=posts": "/u/alice?tab=posts",
		"//evil.example/": "/", "https://evil.example/": "/"} {
		resp := w.do("GET", signInAt(returnTo), s)
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
			t.Errorf("return_to %q: %d to %q, want 302 to %q", returnTo, resp.StatusCode, resp.Header.Get("Location"), want)
		}
	}
}

// AC5: a return_to naming anywhere off this site is dropped, from the
// redirect and from every link, and the sign-in it starts returns home.
func TestTheLoginPageDropsAHostileReturnTo(t *testing.T) {
	w := newWorld(t)
	w.user("alice", "active", "user-1")
	for _, raw := range hostileReturnTo {
		resp := w.do("GET", signInAt(raw))
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusFound || loc != "/auth/fake/start" {
			t.Errorf("one provider, return_to %q: %d to %q", raw, resp.StatusCode, loc)
		}
	}
	resp := w.do("GET", w.do("GET", signInAt(hostileReturnTo[0])).Header.Get("Location"))
	resp = w.do("GET", w.back(resp.Header.Get("Location")), cookie(resp, FlowCookie))
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("the sign-in ended at %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	w.withProviders(w.idp.Provider("club"), w.idp.Provider("other"))
	href := regexp.MustCompile(`href="([^"]*)"`)
	for _, raw := range hostileReturnTo {
		b := body(w.do("GET", signInAt(raw)))
		var got []string
		for _, m := range href.FindAllStringSubmatch(b, -1) {
			got = append(got, m[1])
		}
		if strings.Join(got, " ") != "/auth/club/start /auth/other/start /" {
			t.Errorf("several providers, return_to %q: links %q", raw, got)
		}
	}
}
