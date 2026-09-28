package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/web/auth"
)

func providerBlock(issuer string) string {
	return fmt.Sprintf("[[oidc.providers]]\nkey = \"fake\"\nname = \"Fake\"\nissuer = %q\nclient_id = %q\nclient_secret = %q\n",
		issuer, oidctest.ClientID, oidctest.ClientSecret)
}

// siteURL is the environment with site_url set, which a provider requires;
// writeConfig's file has no top-level place left for it.
func siteURL(k string) string {
	if k == "KETJU_SITE_URL" {
		return "https://forum.example"
	}
	return ""
}

// serve discovers the configured providers, makes the cookie key, and
// mounts /auth/*, all the way to a callback through the real listener.
func TestServeSignsInThroughAProvider(t *testing.T) {
	idp := oidctest.New(t)
	path, data := writeConfig(t, providerBlock(idp.URL))
	r := start(t, path, serveOpts{idpClient: idp.Client(), getenv: siteURL})

	fi, err := os.Stat(filepath.Join(data, "secrets", "cookie.key"))
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != 32 {
		t.Errorf("cookie key: %v, %v", fi, err)
	}

	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get("http://" + r.addr + "/auth/fake/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	authURL := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(authURL, idp.URL+"/authorize?") {
		t.Fatalf("start answered %d to %q", resp.StatusCode, authURL)
	}
	var flow *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.FlowCookie {
			flow = ck
		}
	}
	if flow == nil {
		t.Fatal("start set no flow cookie")
	}

	cb, _ := url.Parse(idp.Authorize(t, authURL))
	req, _ := http.NewRequest("GET", "http://"+r.addr+cb.RequestURI(), nil)
	req.AddCookie(&http.Cookie{Name: flow.Name, Value: flow.Value})
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// Nobody has this identity yet, and registration.mode defaults to
	// approval, whose applicant path has not landed (TKT-01M29CKRG9):
	// reaching that page is the whole flow working.
	if resp.StatusCode != http.StatusNotImplemented || !strings.Contains(string(b), "Registration is not open yet") {
		t.Errorf("callback answered %d:\n%s", resp.StatusCode, b)
	}
	if idp.Tokens() != 1 {
		t.Errorf("%d code exchanges, want 1", idp.Tokens())
	}

	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.steps(), "close providers") {
		t.Errorf("shutdown did not close the providers: %v", r.steps())
	}
}

// A forum nobody can sign in to says so at startup (docs/05).
func TestServeRefusesToStartWhenEveryProviderFails(t *testing.T) {
	// A provider whose issuer does not answer: its server is gone.
	dead := oidctest.New(t)
	path, _ := writeConfig(t, providerBlock(dead.URL))
	deadClient := dead.Client()
	dead.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := &buffer{}
	var steps []string
	err := serve(ctx, path, serveOpts{idpClient: deadClient, logOut: log, getenv: siteURL, step: func(s string) { steps = append(steps, s) },
		ready: func(string) { t.Error("serve became ready with no working provider") }})
	if err == nil || !strings.Contains(err.Error(), "discovering the identity providers") {
		t.Fatalf("serve = %v\n%s", err, log)
	}
	if slices.Contains(steps, "listener") || !slices.Contains(steps, "release lock") {
		t.Errorf("steps %v", steps)
	}
}
