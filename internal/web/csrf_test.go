package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/problem"
)

// csrfServer builds a server with the CSRF posture on and a handler that
// records whether it was reached.
func csrfServer(t *testing.T, siteURL string) (http.Handler, *bool) {
	t.Helper()
	log, _ := testLogger()
	reached := new(bool)
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*reached = true
			w.WriteHeader(http.StatusOK)
		}),
	}, Options{Logger: log, CSRF: CrossOrigin(siteURL)})
	return h, reached
}

// TestAMutatingAPIRequestMustBeJSON is AC1, and the reason the SPA needs no
// token: an HTML form can send only urlencoded, multipart or text/plain, so a
// form on another origin cannot produce a request the API will look at.
func TestAMutatingAPIRequestMustBeJSON(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		want        int
	}{
		{"a form post is refused", http.MethodPost, APIPrefix + "threads",
			"application/x-www-form-urlencoded", "a=1", http.StatusUnsupportedMediaType},
		{"a multipart post is refused", http.MethodPost, APIPrefix + "threads",
			"multipart/form-data; boundary=x", "x", http.StatusUnsupportedMediaType},
		{"text/plain is refused", http.MethodPost, APIPrefix + "threads",
			"text/plain", "hi", http.StatusUnsupportedMediaType},
		{"json is accepted", http.MethodPost, APIPrefix + "threads",
			"application/json", "{}", http.StatusOK},
		{"json with a charset is accepted", http.MethodPost, APIPrefix + "threads",
			"application/json; charset=utf-8", "{}", http.StatusOK},
		{"jsonp is refused, not prefix-matched", http.MethodPost, APIPrefix + "threads",
			"application/jsonp", "{}", http.StatusUnsupportedMediaType},
		{"an unparseable content type is refused", http.MethodPost, APIPrefix + "threads",
			"application/json;;;", "{}", http.StatusUnsupportedMediaType},
		{"PATCH is mutating too", http.MethodPatch, APIPrefix + "threads/1/posts/1",
			"application/x-www-form-urlencoded", "a=1", http.StatusUnsupportedMediaType},
		{"a bodiless DELETE needs no content type", http.MethodDelete, APIPrefix + "threads/1/posts/1",
			"", "", http.StatusOK},
		{"a body with no content type is refused", http.MethodPost, APIPrefix + "threads",
			"", "{}", http.StatusUnsupportedMediaType},
		{"a GET is not mutating", http.MethodGet, APIPrefix + "threads",
			"application/x-www-form-urlencoded", "", http.StatusOK},
		{"the upload route accepts multipart", http.MethodPost, UploadPath,
			"multipart/form-data; boundary=x", "x", http.StatusOK},
		{"the avatar route accepts a multipart PUT", http.MethodPut, AvatarPath,
			"multipart/form-data; boundary=x", "x", http.StatusOK},
		{"and only a PUT", http.MethodPost, AvatarPath,
			"multipart/form-data; boundary=x", "x", http.StatusUnsupportedMediaType},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := csrfServer(t, "")

			var body *strings.Reader
			if tc.body == "" {
				body = strings.NewReader("")
			} else {
				body = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			} else {
				req.Header.Del("Content-Type")
				req.ContentLength = int64(len(tc.body))
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d\n%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusUnsupportedMediaType {
				if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
					t.Errorf("refusal content type = %q, want a problem response", ct)
				}
			}
		})
	}
}

// TestACrossOriginFormPostIsRefused is AC2.
//
// The upload route is the case that matters: it is exempt from the JSON rule
// because docs/07 makes it multipart, so CrossOriginProtection is the only
// thing standing between a cross-origin form and it.
func TestACrossOriginFormPostIsRefused(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		ct        string
		fetchSite string
		want      int
	}{
		{"cross-site multipart to the upload route", UploadPath,
			"multipart/form-data; boundary=x", "cross-site", http.StatusForbidden},
		{"cross-site JSON to the API", APIPrefix + "threads",
			"application/json", "cross-site", http.StatusForbidden},
		{"same-origin JSON is allowed", APIPrefix + "threads",
			"application/json", "same-origin", http.StatusOK},
		{"a request with no fetch metadata is allowed", APIPrefix + "threads",
			"application/json", "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, reached := csrfServer(t, "")

			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", tc.ct)
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden {
				if *reached {
					t.Error("the handler ran for a cross-origin write")
				}
				if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
					t.Errorf("refusal content type = %q, want a problem response rather than net/http's plain text", ct)
				}
			}
		})
	}
}

// TestACrossOriginRequestByOriginHeaderIsRefused covers the other detection
// path: a browser that sends Origin but not Sec-Fetch-Site.
func TestACrossOriginRequestByOriginHeaderIsRefused(t *testing.T) {
	h, reached := csrfServer(t, "")

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"threads", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if *reached {
		t.Error("the handler ran for a request from another origin")
	}
}

// TestTheConfiguredSiteURLIsTrusted. Behind a proxy the server sees an
// internal Host while the browser sends the public origin, so without this
// every write from a real browser is refused.
func TestTheConfiguredSiteURLIsTrusted(t *testing.T) {
	h, _ := csrfServer(t, "https://forum.example")

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"threads", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://forum.example")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for the configured site origin\n%s", rec.Code, rec.Body.String())
	}
}

// TestSafeMethodsAreNeverRefused. CrossOriginProtection treats GET, HEAD and
// OPTIONS as safe, which is only sound because this server does not change
// state on them.
func TestSafeMethodsAreNeverRefused(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		h, _ := csrfServer(t, "")
		req := httptest.NewRequest(m, APIPrefix+"threads", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("%s was refused as cross-origin; safe methods must pass", m)
		}
	}
}

// TestNoCSRFTokenIsThreadedThroughTheSPA is AC4.
//
// It is a claim about absence, so it is checked as one: the shell carries no
// token, the server sets no CSRF cookie, and no response header offers one. If
// any of those appeared, the SPA would have to start reading it, which is the
// plumbing this posture exists to avoid.
func TestNoCSRFTokenIsThreadedThroughTheSPA(t *testing.T) {
	log, _ := testLogger()
	shell, err := NewShell(builtShell(t), log)
	if err != nil {
		t.Fatalf("parse shell: %v", err)
	}
	h := New(Groups{SPA: shell}, Options{Logger: log, CSRF: CrossOrigin("")})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/threads/abc", nil))

	body := strings.ToLower(rec.Body.String())
	for _, marker := range []string{"csrf", "xsrf", "authenticity_token"} {
		if strings.Contains(body, marker) {
			t.Errorf("the shell mentions %q, so a token is being threaded through the client:\n%s",
				marker, rec.Body.String())
		}
	}
	for _, c := range rec.Result().Cookies() {
		n := strings.ToLower(c.Name)
		if strings.Contains(n, "csrf") || strings.Contains(n, "xsrf") {
			t.Errorf("the server set the cookie %q", c.Name)
		}
	}
	for k := range rec.Header() {
		if strings.Contains(strings.ToLower(k), "csrf") {
			t.Errorf("the server sent the header %q", k)
		}
	}
}

// TestThePostureIsOnByDefault. AC2 says CrossOriginProtection is enabled, and
// a protection that only applies when a caller remembers to pass it is not
// enabled -- the one caller who would have to remember is `ketju serve`, which
// is a different ticket entirely.
func TestThePostureIsOnByDefault(t *testing.T) {
	log, _ := testLogger()
	reached := false
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }),
	}, Options{Logger: log}) // no CSRF, no RateLimit

	// A cross-origin form post: refused by the JSON rule.
	req := httptest.NewRequest(http.MethodPost, APIPrefix+"threads", strings.NewReader("a=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415 with no options set", rec.Code)
	}
	if reached {
		t.Error("the handler ran for a cross-origin form post on a default server")
	}

	// And the multipart upload route, which the JSON rule exempts, is still
	// covered by CrossOriginProtection.
	rec = httptest.NewRecorder()
	up := httptest.NewRequest(http.MethodPost, UploadPath, strings.NewReader("x"))
	up.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	up.Header.Set("Sec-Fetch-Site", "cross-site")
	h.ServeHTTP(rec, up)

	if rec.Code != http.StatusForbidden {
		t.Errorf("upload status = %d, want 403 on a default server", rec.Code)
	}
}

// TestRateLimitingIsOnByDefault, for the same reason.
func TestRateLimitingIsOnByDefault(t *testing.T) {
	log, _ := testLogger()
	h := New(Groups{
		Auth: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}, Options{Logger: log})

	limited := false
	for range authRate.Burst + 10 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/google/start", nil))
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("an auth flood was never limited on a server built with no options")
	}
}

// TestAMiddlewareCanBeDisabledExplicitly. The escape hatch has to exist for
// tests that isolate one layer, and it has to be visible at the call site
// rather than a flag somewhere else.
func TestAMiddlewareCanBeDisabledExplicitly(t *testing.T) {
	log, _ := testLogger()
	passthrough := func(h http.Handler) http.Handler { return h }

	reached := false
	h := New(Groups{
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }),
	}, Options{Logger: log, CSRF: passthrough})

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"threads", strings.NewReader("a=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !reached {
		t.Error("an explicitly disabled CSRF middleware still refused the request")
	}
}

// TestCrossSiteAvatarPutIsRefused: the avatar route takes multipart, which
// the JSON rule exempts, so CrossOriginProtection is what refuses a
// cross-site PUT to it, before the handler runs (TKT-01M3YSQ4CR).
func TestCrossSiteAvatarPutIsRefused(t *testing.T) {
	h, reached := csrfServer(t, "")
	req := httptest.NewRequest(http.MethodPut, AvatarPath, strings.NewReader("x"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if *reached {
		t.Error("the handler ran for a cross-site avatar PUT")
	}
}
