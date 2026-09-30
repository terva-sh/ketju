package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/audit"
	"github.com/terva-sh/ketju/internal/auth/oidc/oidctest"
	"github.com/terva-sh/ketju/internal/auth/session"
	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/web/auth"
)

// The helpers the end-to-end tests over the real serve share
// (TKT-01M3NTSCMN): an actor who carries their own cookies, a sign-in
// through the fake provider, and staff seeded before serve starts.

// actor is one person, or one agent, talking to a running serve. Cookies
// are kept by name and added to each request by hand, because
// net/http/cookiejar withholds a Secure cookie, such as
// __Host-ketju_session, from the plain-HTTP loopback listener.
type actor struct {
	name    string
	r       *running
	cookies map[string]*http.Cookie
	// bearer, when set, is sent as Authorization: Bearer.
	bearer string
	// bodies is every answer this actor was given, for a test that checks
	// that something never reached them.
	bodies []string
}

func newActor(r *running, name string) *actor {
	return &actor{name: name, r: r, cookies: map[string]*http.Cookie{}}
}

// noRedirects answers each redirect itself, so a test sees the 303 a
// callback or a page gives.
var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// answer is a response read whole.
type answer struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
	body    []byte
}

func (a answer) String() string { return string(a.body) }

// decode reads the body as JSON into v.
func (a answer) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(a.body, v); err != nil {
		t.Fatalf("decoding %s: %v", a.body, err)
	}
}

// do sends method path, with body as JSON when it is not nil, and keeps
// every cookie the answer sets, dropping one the server clears.
func (a *actor) do(t *testing.T, method, path string, body any) answer {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+a.r.addr+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+a.bearer)
	}
	for _, c := range a.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	resp, err := noRedirects.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	a.keep(resp.Cookies())
	a.bodies = append(a.bodies, string(b))
	return answer{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies(), body: b}
}

// keep records cookies as a browser would: a cleared one is dropped.
func (a *actor) keep(cs []*http.Cookie) {
	for _, c := range cs {
		if c.MaxAge < 0 || c.Value == "" {
			delete(a.cookies, c.Name)
			continue
		}
		a.cookies[c.Name] = c
	}
}

// session is the actor's session cookie's value, or "".
func (a *actor) session() string {
	if c := a.cookies[session.CookieName]; c != nil {
		return c.Value
	}
	return ""
}

// holding is a new actor with only the session cookie value, for a test
// that goes on using a cookie the actor has since replaced.
func holding(r *running, name, value string) *actor {
	a := newActor(r, name)
	a.cookies[session.CookieName] = &http.Cookie{Name: session.CookieName, Value: value}
	return a
}

// meAnswer is what the tests read of GET /api/v1/me.
type meAnswer struct {
	User *struct {
		Handle string `json:"handle"`
		Role   string `json:"role"`
		Kind   string `json:"kind"`
	} `json:"user"`
	PreUser *struct {
		Provider string `json:"provider"`
	} `json:"pre_user"`
	Notifications int `json:"notifications"`
}

// me is the actor's GET /api/v1/me.
func (a *actor) me(t *testing.T) meAnswer {
	t.Helper()
	res := a.do(t, http.MethodGet, "/api/v1/me", nil)
	if res.status != http.StatusOK {
		t.Fatalf("%s: /me answered %d:\n%s", a.name, res.status, res)
	}
	var m meAnswer
	res.decode(t, &m)
	return m
}

// signIn takes a through the whole sign-in as subject: the provider's
// start, the fake provider's approval, and the callback with the flow
// cookie. It answers the callback's answer, whose cookies a now holds.
//
// A sign-in is two /auth/* requests, and /auth/* allows a burst of ten per
// address (docs/08), which every actor here shares on loopback: a test
// signs in at most five times on one server.
func signIn(t *testing.T, a *actor, idp *oidctest.IdP, subject string) answer {
	t.Helper()
	start := a.do(t, http.MethodGet, "/auth/fake/start", nil)
	authURL := start.header.Get("Location")
	if start.status != http.StatusFound || !strings.HasPrefix(authURL, idp.URL+"/authorize?") {
		t.Fatalf("%s: start answered %d to %q:\n%s", a.name, start.status, authURL, start)
	}
	if a.cookies[auth.FlowCookie] == nil {
		t.Fatalf("%s: start set no flow cookie", a.name)
	}
	// Users is set, so the provider signs in whoever login_as names, as its
	// sign-in page's links do.
	cb, err := url.Parse(idp.Authorize(t, authURL+"&login_as="+url.QueryEscape(subject)))
	if err != nil {
		t.Fatal(err)
	}
	return a.do(t, http.MethodGet, cb.RequestURI(), nil)
}

// seeded is a user written before serve starts.
type seeded struct{ subject, handle, role string }

// seedPeople writes each of people, with an identity at the fake provider,
// into the data directory before serve starts, as cmd/ketju/e2e.go seeds
// its members. Each is made a member, and a role above that goes through
// perm, so the change is audited as every role change is. It also writes the
// public forum general.
func seedPeople(t *testing.T, data, issuer string, people ...seeded) {
	t.Helper()
	ctx := context.Background()
	for _, sub := range []string{threadid.ThreadsDir, "db"} {
		if err := os.MkdirAll(filepath.Join(data, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(ctx, db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	ids := map[string]int64{}
	if err := d.Write(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for _, p := range people {
			id, err := q.CreateUser(ctx, dbq.CreateUserParams{Handle: p.handle, DisplayName: p.handle,
				Status: domain.StatusActive, Role: domain.RoleMember, Kind: domain.KindHuman, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			ids[p.handle] = id
			if _, err := q.CreateIdentity(ctx, dbq.CreateIdentityParams{UserID: sql.NullInt64{Int64: id, Valid: true},
				ProviderKey: e2eProviderKey, Subject: p.subject, Issuer: issuer, CreatedAt: now, UpdatedAt: now}); err != nil {
				return err
			}
		}
		_, err := q.CreateForum(ctx, dbq.CreateForumParams{Slug: "general", Name: "General",
			Description: "Anything", Visibility: domain.VisibilityPublic, CreatedAt: now, UpdatedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e := perm.New(d)
	for _, p := range people {
		if p.role == domain.RoleMember {
			continue
		}
		if err := e.ChangeRole(ctx, audit.By{}, ids[p.handle], p.role); err != nil {
			t.Fatalf("making %s %s: %v", p.handle, p.role, err)
		}
	}
}

// e2eProviderKey is the provider key providerBlock configures.
const e2eProviderKey = "fake"
