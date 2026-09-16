package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
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
	shell, err := NewShell(DefaultShell, log)
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
	shell, err := NewShell(DefaultShell, log)
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
	shell, err := NewShell(DefaultShell, log)
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
	shell, err := NewShell(DefaultShell, log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	h := New(Groups{SPA: shell}, Options{Logger: log})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	doc, err := html.Parse(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("the shell is not parseable HTML: %v", err)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			var body strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					body.WriteString(c.Data)
				}
			}
			if strings.TrimSpace(body.String()) != "" {
				t.Errorf("<%s> has an inline body, which the policy blocks:\n%s", n.Data, body.String())
			}
			if n.Data == "script" && attr(n, "nonce") == "" {
				t.Errorf("<script> has no nonce, so the policy would block it")
			}
		}
		// An inline event handler is inline script by another name.
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					t.Errorf("<%s> carries the inline handler %s, which the policy blocks", n.Data, a.Key)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

// TestTheShellIsNotCached: the shell carries a per-response nonce, so a cached
// copy hands a stale nonce to the next reader and the policy blocks every
// script on the page.
func TestTheShellIsNotCached(t *testing.T) {
	log, _ := testLogger()
	shell, _ := NewShell(DefaultShell, log)
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
	shell, _ := NewShell(DefaultShell, log)
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
	shell, _ := NewShell(DefaultShell, log)

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
