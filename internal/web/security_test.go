package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/web/spa"
)

// documentedHeaders is the set docs/07 requires on every response.
var documentedHeaders = map[string]string{
	"Referrer-Policy":        ReferrerPolicy,
	"X-Content-Type-Options": ContentTypeOptions,
}

// TestEveryResponseCarriesTheDocumentedHeaders is AC1.
//
// The cases are chosen to be the responses nobody wrote on purpose: a 404 from
// the router, a 501 from an unbuilt group, a 413 from the body limit, a 500
// from a panic. Those are the ones a header applied by a handler would miss,
// and they are the ones that most need a restrictive policy -- a 500 that
// reflects part of a request is worth a great deal more to an attacker when
// the page it lands on can run script.
func TestEveryResponseCarriesTheDocumentedHeaders(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}

	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == APIPrefix+"boom" {
				panic("kaboom")
			}
			w.WriteHeader(http.StatusOK)
		}),
		SPA: shell,
	}, Options{Logger: log, Limits: Limits{Body: 64}})

	cases := []struct {
		name string
		req  *http.Request
		want int
	}{
		{"an ordinary API 200", httptest.NewRequest(http.MethodGet, APIPrefix+"me", nil), 200},
		{"the SPA shell", httptest.NewRequest(http.MethodGet, "/threads/abc", nil), 200},
		{"a router 404", httptest.NewRequest(http.MethodGet, "/api/v2/threads", nil), 404},
		{"an unbuilt group 501", httptest.NewRequest(http.MethodGet, "/blobs/x", nil), 501},
		{"a panic 500", httptest.NewRequest(http.MethodGet, APIPrefix+"boom", nil), 500},
		{"a body limit 413", httptest.NewRequest(http.MethodPost, APIPrefix+"threads",
			strings.NewReader(strings.Repeat("x", 4096))), 413},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			for k, v := range documentedHeaders {
				if got := rec.Header().Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
			csp := rec.Header().Get("Content-Security-Policy")
			if csp == "" {
				t.Fatal("no Content-Security-Policy")
			}
			for _, directive := range []string{
				"default-src 'self'",
				"frame-ancestors 'none'",
				"img-src 'self' data:",
				"base-uri 'self'",
				"form-action 'self'",
				"object-src 'none'",
			} {
				if !strings.Contains(csp, directive) {
					t.Errorf("CSP is missing %q:\n%s", directive, csp)
				}
			}
		})
	}
}

// TestThePolicyNeverAllowsInlineScript is the directive the whole ticket turns
// on. 'unsafe-inline' would make the nonce decorative: the browser stops
// checking nonces the moment the policy says inline script is fine anyway.
func TestThePolicyNeverAllowsInlineScript(t *testing.T) {
	csp := contentSecurityPolicy("abc123")
	for _, forbidden := range []string{"unsafe-inline", "unsafe-eval", "*"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("the policy contains %q, which defeats the nonce:\n%s", forbidden, csp)
		}
	}
	if !strings.Contains(csp, "script-src 'self' 'nonce-abc123'") {
		t.Errorf("script-src does not carry the nonce:\n%s", csp)
	}
	if !strings.Contains(csp, "style-src 'self' 'nonce-abc123'") {
		t.Errorf("style-src does not carry the nonce:\n%s", csp)
	}
}

// TestTheNonceIsPerResponse is AC2's first half. A nonce reused across
// responses is not a nonce: an attacker who reads one page learns the value
// that will be accepted on the next.
func TestTheNonceIsPerResponse(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	h := New(Groups{SPA: shell}, Options{Logger: log})

	seen := map[string]bool{}
	const runs = 50
	for range runs {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		n := nonceFromCSP(t, rec.Header().Get("Content-Security-Policy"))
		if n == "" {
			t.Fatal("no nonce in the policy")
		}
		if seen[n] {
			t.Fatalf("nonce %q was reused across responses", n)
		}
		seen[n] = true
	}
	if len(seen) != runs {
		t.Errorf("%d distinct nonces over %d responses", len(seen), runs)
	}
}

// TestTheShellInjectsTheSameNonceThePolicyNames is AC2's second half, and the
// failure it catches is silent in the worst way: a shell whose nonce does not
// match the header renders a blank page, and the only clue is a console
// message the server never sees.
func TestTheShellInjectsTheSameNonceThePolicyNames(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	h := New(Groups{SPA: shell}, Options{Logger: log})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/abc", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	policyNonce := nonceFromCSP(t, rec.Header().Get("Content-Security-Policy"))

	scripts := scriptNonces(t, rec.Body.String())
	if len(scripts) == 0 {
		t.Fatal("the shell has no script tag, so this test is not measuring anything")
	}
	for _, got := range scripts {
		if got != policyNonce {
			t.Errorf("script nonce %q does not match the policy's %q; every script on the page would be blocked",
				got, policyNonce)
		}
	}
}

// TestTheShellHasNoInlineScriptOrStyle is as much of AC3 as can be checked
// without the client.
//
// The ticket asks for "no inline script or style is needed by the client,
// verified with the CSP in report-only off". The client does not exist yet --
// TKT-01M29CCC02 scaffolds it -- so the browser half of that check cannot be
// run here. What is checkable now is the mechanism this ticket owns and the
// shell it ships: every script and style element is external, carries the
// nonce, and has no inline body. If that were not true, the client would
// inherit a shell that already needed 'unsafe-inline'.
func TestTheShellHasNoInlineScriptOrStyle(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	h := New(Groups{SPA: shell}, Options{Logger: log})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for _, p := range inlineScriptProblems(t, rec.Body.String()) {
		t.Error(p)
	}
}

// inlineScriptProblems lists what in a page the policy would block: an inline
// script or style body, a script without a nonce, and an on* attribute.
func inlineScriptProblems(t *testing.T, page string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("the shell is not parseable HTML: %v", err)
	}
	var problems []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			var body strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					body.WriteString(c.Data)
				}
			}
			// A JSON data block is never executed, so its body is not inline
			// script (settled in grooming for TKT-01M29CCC02). Any other type
			// with a body is.
			data := n.Data == "script" && attr(n, "type") == "application/json"
			if strings.TrimSpace(body.String()) != "" && !data {
				problems = append(problems, fmt.Sprintf("<%s> has an inline body, which the policy blocks:\n%s", n.Data, body.String()))
			}
			if n.Data == "script" && attr(n, "nonce") == "" {
				problems = append(problems, "<script> has no nonce, so the policy would block it")
			}
		}
		// An inline event handler is inline script by another name.
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					problems = append(problems, fmt.Sprintf("<%s> carries the inline handler %s, which the policy blocks", n.Data, a.Key))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return problems
}

// TestTheInlineCheckIsNarrow: the check above exempts a JSON data block and
// nothing else, so it still catches each thing it exists to catch. Without
// this, loosening the exemption would pass: the built shell has nothing for
// a loosened check to miss.
func TestTheInlineCheckIsNarrow(t *testing.T) {
	for page, want := range map[string]int{
		`<script type="application/json" nonce="n">{"a":1}</script>`: 0,
		`<script type="module" nonce="n" src="/a.js"></script>`:      0,
		`<script nonce="n">alert(1)</script>`:                        1,
		`<script type="text/javascript" nonce="n">alert(1)</script>`: 1,
		`<script type="module" nonce="n">import "/a.js"</script>`:    1,
		`<script type="application/ld+json" nonce="n">{}</script>`:   1,
		`<style nonce="n">body{}</style>`:                            1,
		`<script src="/a.js"></script>`:                              1,
		`<script type="application/json">{"a":1}</script>`:           1,
		`<div onclick="x()"></div>`:                                  1,
	} {
		if got := inlineScriptProblems(t, page); len(got) != want {
			t.Errorf("%s: %d problems, want %d: %v", page, len(got), want, got)
		}
	}
}

// TestTheShellIsNotCached: the shell carries a per-response nonce, so a cached
// copy hands a stale nonce to the next reader and the policy blocks every
// script on the page.
func TestTheShellIsNotCached(t *testing.T) {
	log, _ := testLogger()
	shell, _ := NewShell(builtShell(t), log)
	h := New(Groups{SPA: shell}, Options{Logger: log})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

// TestTheShellRefusesAWriteMethod: a POST to an application route is a client
// bug, and answering it with 200 and a page would hide it.
func TestTheShellRefusesAWriteMethod(t *testing.T) {
	log, _ := testLogger()
	shell, _ := NewShell(builtShell(t), log)
	h := New(Groups{SPA: shell}, Options{Logger: log})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/threads/abc", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// TestTheShellWithoutANonceFailsLoudly. Serving a page whose scripts the
// policy will block is worse than an error: the reader gets a blank screen and
// the cause is two packages away.
func TestTheShellWithoutANonceFailsLoudly(t *testing.T) {
	log, buf := testLogger()
	shell, _ := NewShell(builtShell(t), log)

	// Called directly, with no securityHeaders in front of it.
	rec := httptest.NewRecorder()
	shell.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "nonce") {
		t.Errorf("the misconfiguration was not logged:\n%s", buf.String())
	}
}

func TestNewShellRejectsABrokenTemplate(t *testing.T) {
	if _, err := NewShell("{{.Nonce", nil); err == nil {
		t.Error("a malformed template was accepted, so the failure would arrive at the first request")
	}
}

// nonceFromCSP pulls the nonce out of a policy string.
func nonceFromCSP(t *testing.T, csp string) string {
	t.Helper()
	const marker = "'nonce-"
	i := strings.Index(csp, marker)
	if i < 0 {
		return ""
	}
	rest := csp[i+len(marker):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("unterminated nonce in policy: %s", csp)
	}
	return rest[:j]
}

// scriptNonces returns the nonce attribute of every script element.
func scriptNonces(t *testing.T, body string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			out = append(out, attr(n, "nonce"))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// builtShell is the real shell: the client's built index.html.
func builtShell(t *testing.T) string {
	t.Helper()
	s, err := spa.Shell()
	if err != nil {
		t.Fatalf("the built shell: %v", err)
	}
	return s
}

// TestTheShellCarriesTheBootstrapAndMeta is AC3 of TKT-01M29CCC02: the built
// index.html renders as a Go template with the nonce on every script and
// link, the page's title and description, and the viewer as JSON in the data
// block, escaped so that no value can close the element.
func TestTheShellCarriesTheBootstrapAndMeta(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatal(err)
	}
	hostile := "</script><script>alert(1)</script>"
	want := dto.Me{User: &dto.Author{Handle: "alice", DisplayName: hostile, Role: "member", Kind: "human"},
		Notifications: 2, Settings: dto.Settings{PageSize: 25}}
	shell.SetPage(func(*http.Request) (Page, error) {
		return Page{Title: "Why we left Slack & more", Description: `a "quoted" thread`, Bootstrap: want}, nil
	})
	h := New(Groups{SPA: shell}, Options{Logger: log})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/t/3yymw47g00001/why", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	if strings.Contains(body, "{{") || strings.Contains(body, hostile) {
		t.Fatalf("the shell left a placeholder or an unescaped value:\n%s", body)
	}

	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	nonce := nonceFromCSP(t, rec.Header().Get("Content-Security-Policy"))
	var (
		bootstrap, title, description string
		scripts, links                int
	)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "script" && attr(n, "id") == "bootstrap":
				if n.FirstChild != nil {
					bootstrap = n.FirstChild.Data
				}
				fallthrough
			case n.Data == "script", n.Data == "link" && attr(n, "rel") == "stylesheet":
				if attr(n, "nonce") != nonce {
					t.Errorf("<%s> carries nonce %q, the policy names %q", n.Data, attr(n, "nonce"), nonce)
				}
				if n.Data == "script" {
					scripts++
				} else {
					links++
				}
			case n.Data == "title" && n.FirstChild != nil:
				title = n.FirstChild.Data
			case n.Data == "meta" && attr(n, "name") == "description":
				description = attr(n, "content")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if scripts < 2 || links < 1 {
		t.Errorf("%d scripts and %d stylesheets; want the module, the bootstrap block and the stylesheet", scripts, links)
	}
	if title != "Why we left Slack & more" || description != `a "quoted" thread` {
		t.Errorf("title %q, description %q", title, description)
	}
	var got dto.Me
	if err := json.Unmarshal([]byte(bootstrap), &got); err != nil {
		t.Fatalf("the bootstrap block is not JSON: %v\n%s", err, bootstrap)
	}
	if got.User == nil || got.User.DisplayName != hostile || got.Notifications != 2 {
		t.Errorf("the bootstrap block decodes to %+v", got)
	}
}

// TestTheShellRefusesAFailedPage: a page that cannot be built is a 500, not
// a shell with a guessed viewer in it.
func TestTheShellRefusesAFailedPage(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatal(err)
	}
	shell.SetPage(func(*http.Request) (Page, error) { return Page{}, errors.New("session store down") })
	rec := httptest.NewRecorder()
	New(Groups{SPA: shell}, Options{Logger: log}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

// TestShellHasInstallTags is AC3 of TKT-01M29CGBYY: the shell links the
// manifest and the two hashed icons, colours the browser's chrome for both
// themes, and carries the tags iOS and Android read when the page is added
// to a home screen, with the site name escaped, and the status bar left at
// default so nothing is drawn beneath it. None of it is script, so the
// policy and the inline check above are unchanged by it.
func TestShellHasInstallTags(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatal(err)
	}
	hostile := `Club" onload="alert(1)`
	shell.SetPage(func(*http.Request) (Page, error) {
		return Page{Title: "A thread", SiteName: hostile}, nil
	})
	h := New(Groups{SPA: shell}, Options{Logger: log})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	links := map[string]string{}
	metas := map[string][]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "link":
				links[attr(n, "rel")] = attr(n, "href")
			case "meta":
				if name := attr(n, "name"); name != "" {
					v := attr(n, "content")
					if m := attr(n, "media"); m != "" {
						v = m + " " + v
					}
					metas[name] = append(metas[name], v)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if links["manifest"] != "/manifest.webmanifest" {
		t.Errorf("the manifest link is %q", links["manifest"])
	}
	for rel, ext := range map[string]string{"icon": ".svg", "apple-touch-icon": ".png"} {
		if !strings.HasPrefix(links[rel], "/assets/icon") || !strings.HasSuffix(links[rel], ext) {
			t.Errorf("the %s link is %q, want a hashed %s under /assets", rel, links[rel], ext)
		}
	}
	for name, want := range map[string][]string{
		"theme-color":                           {"(prefers-color-scheme: light) #F6F7FA", "(prefers-color-scheme: dark) #12161E"},
		"mobile-web-app-capable":                {"yes"},
		"apple-mobile-web-app-capable":          {"yes"},
		"apple-mobile-web-app-status-bar-style": {"default"},
		"apple-mobile-web-app-title":            {hostile},
	} {
		if got := metas[name]; strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("meta %s is %q, want %q", name, got, want)
		}
	}
	if strings.Contains(body, hostile) {
		t.Error("the site name reached the shell unescaped")
	}
	for _, p := range inlineScriptProblems(t, body) {
		t.Error(p)
	}
}
