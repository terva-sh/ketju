// Package oidctest is an in-process OpenID provider for tests of the whole
// sign-in flow (TKT-01M29C7ZZ9): the authorization endpoint a browser is
// sent to, the token endpoint the callback exchanges a code at, and the key
// set the ID token is checked against. It needs no network beyond loopback.
//
// It is a separate package from internal/auth/oidc's own fake, which tests
// the relying party's key handling and exposes every field an attacker
// could control; this one is the honest IdP a login meets, with a knob for
// each way a token can be wrong. Like that one it really signs, so a relying
// party that skipped verification would fail against it.
//
// It is for tests, and nothing but a test may import it, with one exception:
// the e2e build of ketju serve (cmd/ketju/e2e.go, build tag e2e), which the
// Playwright smoke suite signs in through (TKT-01M29CGBY1). That build is
// never released. For it, Users puts a sign-in page in front of the
// approval, so a browser can choose who signs in.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/terva-sh/ketju/internal/config"
)

// ClientID and ClientSecret are what Provider configures, and what the token
// endpoint requires.
const (
	ClientID     = "ketju"
	ClientSecret = "s3cret"
)

// IdP is the fake provider. Set its fields before the login they should
// affect; they are read when the token is minted.
type IdP struct {
	// URL is the issuer.
	URL string

	// Subject is who signs in: the sub of every token minted. Defaults to
	// "user-1".
	Subject string
	// Claims are added to the ID token, and override the standard ones.
	Claims map[string]any
	// Audience replaces the client id as aud, for a token meant for someone
	// else.
	Audience any
	// Expiry is how long a minted token lives. Negative mints one already
	// expired.
	Expiry time.Duration
	// Nonce, when set, is minted in place of the nonce the login sent: a token
	// from another login, replayed.
	Nonce string

	// Users, when set, are the subjects a browser may sign in as: the
	// authorization endpoint answers with a page linking each, and the one
	// followed is the token's sub. Unset, it approves at once as Subject.
	Users []string

	srv *httptest.Server
	key *rsa.PrivateKey

	mu     sync.Mutex
	grants map[string]grant
	// Tokens counts the token requests that succeeded.
	tokens int
}

type grant struct {
	nonce, challenge, redirect, subject string
}

// New starts an IdP for the test's lifetime.
func New(t testing.TB) *IdP {
	t.Helper()
	f, err := Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

// Start starts an IdP on loopback, for a caller that is not a test: the e2e
// build of ketju serve. Close stops it.
func Start() (*IdP, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	f := &IdP{key: key, grants: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                f.URL,
			"authorization_endpoint":                f.URL + "/authorize",
			"token_endpoint":                        f.URL + "/token",
			"jwks_uri":                              f.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"code_challenge_methods_supported":      []string{"S256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("GET /authorize", f.authorize)
	mux.HandleFunc("POST /token", f.token)
	f.srv = httptest.NewTLSServer(mux)
	f.URL = f.srv.URL
	return f, nil
}

// Close stops the IdP now, rather than when the test ends: an issuer that
// has gone away.
func (f *IdP) Close() { f.srv.Close() }

// Client trusts this IdP's certificate and nothing else, for the relying
// party's discovery and exchange.
func (f *IdP) Client() *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
}

// Provider is the config block for this IdP under key.
func (f *IdP) Provider(key string) config.Provider {
	return config.Provider{Key: key, Name: key, Issuer: f.URL, ClientID: ClientID,
		ClientSecret: config.NewSecret(ClientSecret)}
}

// Tokens is how many codes have been exchanged.
func (f *IdP) Tokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens
}

// Authorize plays the browser at the IdP: it follows authURL, the redirect a
// sign-in's start answered with, as a user who approves, and returns the
// callback URL the IdP sends them back to.
func (f *IdP) Authorize(t testing.TB, authURL string) string {
	t.Helper()
	c := f.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the IdP answered the authorization request with %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

// authorize approves: at once as Subject, or, with Users set, as the user
// the browser chose on the sign-in page it answers first.
func (f *IdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != ClientID || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("redirect_uri") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	subject := f.Subject
	if len(f.Users) > 0 {
		subject = q.Get("login_as")
		if !slices.Contains(f.Users, subject) {
			f.signInPage(w, q)
			return
		}
	}
	code := random()
	f.mu.Lock()
	f.grants[code] = grant{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), subject: subject}
	f.mu.Unlock()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

// signInPage is the IdP's login screen: one link per user, each the
// authorization request again with login_as set.
func (f *IdP) signInPage(w http.ResponseWriter, q url.Values) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<!doctype html><title>Fake IdP</title><h1>Sign in as</h1><ul>")
	for _, u := range f.Users {
		v := url.Values{}
		for k, vs := range q {
			v[k] = vs
		}
		v.Set("login_as", u)
		fmt.Fprintf(w, `<li><a href="/authorize?%s">%s</a></li>`, html.EscapeString(v.Encode()), html.EscapeString(u))
	}
	fmt.Fprint(w, "</ul>")
}

// token exchanges a code once, for the client that holds the PKCE verifier
// the authorization request committed to.
func (f *IdP) token(w http.ResponseWriter, r *http.Request) {
	id, secret, _ := r.BasicAuth()
	if err := r.ParseForm(); err != nil || id != ClientID || secret != ClientSecret {
		oauthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	f.mu.Lock()
	g, ok := f.grants[r.PostForm.Get("code")]
	delete(f.grants, r.PostForm.Get("code"))
	f.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirect ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		oauthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	nonce := g.nonce
	if f.Nonce != "" {
		nonce = f.Nonce
	}
	f.mu.Lock()
	f.tokens++
	f.mu.Unlock()
	tok, err := f.mint(g.subject, nonce)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": tok})
}

// Mint is an ID token for Subject carrying nonce, signed by this IdP.
func (f *IdP) Mint(t testing.TB, nonce string) string {
	t.Helper()
	tok, err := f.mint(f.Subject, nonce)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (f *IdP) mint(sub, nonce string) (string, error) {
	if sub == "" {
		sub = "user-1"
	}
	var aud any = ClientID
	if f.Audience != nil {
		aud = f.Audience
	}
	life := time.Hour
	if f.Expiry != 0 {
		life = f.Expiry
	}
	now := time.Now()
	claims := map[string]any{"iss": f.URL, "sub": sub, "aud": aud, "nonce": nonce,
		"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(life).Unix()}
	for k, v := range f.Claims {
		claims[k] = v
	}
	payload, _ := json.Marshal(claims)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		return "", err
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

func random() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
