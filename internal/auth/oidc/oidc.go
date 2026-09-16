// Package oidc is ketju's OpenID Connect relying party: the registry that turns
// the provider blocks in config.toml into discovered, ready-to-use providers.
//
// The protocol is not ours. Discovery, the JWKS fetch and its rotation, ID
// token signature verification, and the authorization-code exchange all come
// from github.com/coreos/go-oidc and golang.org/x/oauth2. What lives here is
// the part that is genuinely ketju's: which providers exist, what happens when
// one of them is broken at startup, how a local group is derived from an IdP
// group, and the refusal to talk to a plaintext issuer.
//
// That split follows the same decision reached in terva (terva-sh/terva,
// packages/agent/oidc), where an earlier hand-rolled flow was removed in favour
// of these two libraries. The reasoning transfers exactly: a hand-rolled relying
// party can lean on TLS in place of signature verification and stay spec-legal,
// but only while every token arrives by direct exchange, which leaves the
// project one feature away from needing crypto it chose not to write.
//
// Three rules hold here:
//
//   - A provider that fails discovery is disabled, not fatal. One broken IdP
//     must not take the forum offline for everyone who uses a different one.
//   - An issuer must be https. There is no escape hatch, because discovery over
//     plaintext is unauthenticated and a signature check against keys an
//     attacker supplied proves nothing.
//   - The accepted signing algorithms are a fixed asymmetric list and are never
//     read from the discovery document.
//
// The first two are each held down by a test that fails when the rule is
// removed. The third is not, and the test comments say so rather than implying
// a defence that is not there: see TestAnEllipticCurveProviderWorks for what
// the pinned list actually buys and what refuses key confusion instead.
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/terva-sh/ketju/internal/config"
)

// DefaultRefreshInterval is how often a provider's discovery document and key
// set are re-read even when nothing has failed. Key rotation normally announces
// itself through an unknown key id, which refreshes immediately; this is the
// backstop for the rotation that changes an endpoint rather than a key.
const DefaultRefreshInterval = 24 * time.Hour

// signingAlgs is every algorithm an ID token may be signed with. All are
// public-key schemes, so a valid signature can only come from whoever holds the
// IdP's private key and nothing ketju holds could forge one.
//
// Being precise about what this is for, because the obvious answer is wrong.
// It is not what stops key confusion -- a token naming HS256 against a key set
// of public keys. go-oidc already drops HS256 and none from the algorithms it
// reads out of a discovery document, and go-jose refuses an HMAC algorithm
// against a public key beneath that. Deleting this line does not make the
// confusion test fail.
//
// What it does is make the accepted set depend on neither the IdP nor a library
// default. gooidc.NewVerifier with an empty list accepts RS256 alone, so a
// provider configured with an elliptic-curve key would have every token
// refused; that is an ordinary deployment, and the failure would be a login
// that never works rather than anything that looks like a security event.
// Pinning the list also means a change in what the library filters cannot
// quietly widen what ketju accepts.
var signingAlgs = []string{
	gooidc.RS256, gooidc.RS384, gooidc.RS512,
	gooidc.ES256, gooidc.ES384, gooidc.ES512,
	gooidc.PS256, gooidc.PS384, gooidc.PS512,
	gooidc.EdDSA,
}

// Options configures a Registry. The zero value is usable.
type Options struct {
	// Logger receives the disabled-provider lines and the refresh results.
	// Defaults to slog.Default().
	Logger *slog.Logger

	// HTTPClient is used for discovery, the JWKS fetch and the token exchange.
	// A deployment behind a private CA passes one carrying its pool; a test
	// passes httptest's. Defaults to http.DefaultClient.
	HTTPClient *http.Client

	// RefreshInterval overrides DefaultRefreshInterval.
	RefreshInterval time.Duration

	// Now exists so the key-set rate limit can be tested without sleeping.
	Now func() time.Time
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return http.DefaultClient
}

func (o Options) interval() time.Duration {
	if o.RefreshInterval > 0 {
		return o.RefreshInterval
	}
	return DefaultRefreshInterval
}

func (o Options) now() func() time.Time {
	if o.Now != nil {
		return o.Now
	}
	return time.Now
}

// Disabled records a provider that could not be discovered, so that the reason
// survives past the log line and can be shown to an admin.
type Disabled struct {
	Key  string
	Name string
	Err  error

	// cfg is kept so that Refresh can retry a provider it never managed to
	// build, which is the difference between an IdP that was down for one
	// deploy and one that needs a restart to come back.
	cfg config.Provider
}

// Registry holds the configured providers, discovered.
//
// Providers are an allowlist by construction: only issuers named in config.toml
// are ever contacted, and the login page is built from Enabled. There is no
// path by which a URL arriving in a request becomes an issuer.
type Registry struct {
	log      *slog.Logger
	hc       *http.Client
	interval time.Duration
	now      func() time.Time
	siteURL  string

	mu       sync.RWMutex
	order    []string
	byKey    map[string]*Provider
	disabled []Disabled

	stop   chan struct{}
	closed atomic.Bool
	wg     sync.WaitGroup
}

// New discovers every configured provider and returns a registry of the ones
// that answered.
//
// It returns an error only when no provider could be discovered at all, because
// a forum whose every login method is broken cannot serve anyone and should say
// so at startup rather than present an empty login page. A partial failure is
// not an error: the working providers are returned and the broken ones are in
// Disabled.
func New(ctx context.Context, cfg *config.Config, opts Options) (*Registry, error) {
	if cfg == nil {
		return nil, errors.New("oidc: nil config")
	}
	r := &Registry{
		log:      opts.logger(),
		hc:       opts.client(),
		interval: opts.interval(),
		now:      opts.now(),
		siteURL:  strings.TrimRight(cfg.SiteURL, "/"),
		byKey:    map[string]*Provider{},
		stop:     make(chan struct{}),
	}
	if len(cfg.OIDC.Providers) == 0 {
		return nil, errors.New("oidc: no providers are configured; nobody can sign in")
	}

	for _, pc := range cfg.OIDC.Providers {
		p, err := r.discover(ctx, pc)
		if err != nil {
			// Loud, and with the key in it, because the operator's next move is
			// to find that block in config.toml. The server still starts.
			r.log.Error("oidc provider disabled: discovery failed",
				"provider", pc.Key, "issuer", pc.Issuer, "err", err)
			r.disabled = append(r.disabled, Disabled{Key: pc.Key, Name: pc.Name, Err: err, cfg: pc})
			continue
		}
		if pc.InsecureSkipIssuerCheck {
			// This is on in production more often than anyone intends, so it
			// says so every single startup.
			r.log.Warn("oidc provider has the issuer check disabled",
				"provider", pc.Key, "issuer", pc.Issuer,
				"why", "the id_token issuer is not compared against the configured one")
		}
		r.order = append(r.order, pc.Key)
		r.byKey[pc.Key] = p
	}

	if len(r.byKey) == 0 {
		return nil, fmt.Errorf("oidc: every configured provider failed discovery (%s)",
			strings.Join(r.disabledKeys(), ", "))
	}
	return r, nil
}

// Start runs the periodic refresh until Close. It is separate from New so that
// a command which only wants to validate configuration, such as ketju config
// check, can discover without leaving a goroutine behind.
func (r *Registry) Start() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				r.Refresh(ctx)
				cancel()
			}
		}
	}()
}

// Close stops the refresh loop. It is safe to call more than once.
func (r *Registry) Close() error {
	if r.closed.Swap(true) {
		return nil
	}
	close(r.stop)
	r.wg.Wait()
	return nil
}

// Refresh re-runs discovery for every configured provider, including the ones
// currently disabled.
//
// A provider that recovers is re-enabled, which is the whole reason disabled
// providers are kept rather than dropped: an IdP that was down during a deploy
// should come back on its own rather than need a restart. A provider that fails
// here keeps its previous working state, because a transient failure must not
// remove a working login method.
func (r *Registry) Refresh(ctx context.Context) {
	r.mu.RLock()
	cfgs := make([]config.Provider, 0, len(r.byKey)+len(r.disabled))
	for _, key := range r.order {
		cfgs = append(cfgs, r.byKey[key].cfg)
	}
	stale := append([]Disabled(nil), r.disabled...)
	r.mu.RUnlock()

	for _, pc := range cfgs {
		p, err := r.discover(ctx, pc)
		if err != nil {
			r.log.Warn("oidc refresh failed; keeping the previous discovery",
				"provider", pc.Key, "err", err)
			continue
		}
		r.mu.Lock()
		if cur, ok := r.byKey[pc.Key]; ok {
			cur.swap(p)
		}
		r.mu.Unlock()
	}

	for _, d := range stale {
		pc, ok := r.configFor(d.Key)
		if !ok {
			continue
		}
		p, err := r.discover(ctx, pc)
		if err != nil {
			continue
		}
		r.log.Info("oidc provider recovered and is enabled again", "provider", pc.Key)
		r.mu.Lock()
		r.order = append(r.order, pc.Key)
		r.byKey[pc.Key] = p
		r.disabled = removeDisabled(r.disabled, pc.Key)
		r.mu.Unlock()
	}
}

// Enabled returns the working providers in the order they appear in
// config.toml, which is the order the login page draws them.
func (r *Registry) Enabled() []*Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Provider, 0, len(r.order))
	for _, key := range r.order {
		out = append(out, r.byKey[key])
	}
	return out
}

// Lookup finds a provider by the key in its config block, which is also the
// value stored in identities.provider_key.
func (r *Registry) Lookup(key string) (*Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byKey[key]
	return p, ok
}

// Disabled returns the providers that failed discovery, for the admin surface
// and for healthz.
func (r *Registry) Disabled() []Disabled {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Disabled(nil), r.disabled...)
}

func (r *Registry) disabledKeys() []string {
	out := make([]string, 0, len(r.disabled))
	for _, d := range r.disabled {
		out = append(out, d.Key)
	}
	return out
}

func (r *Registry) configFor(key string) (config.Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.byKey[key]; ok {
		return p.cfg, true
	}
	for _, d := range r.disabled {
		if d.Key == key {
			return d.cfg, d.cfg.Key != ""
		}
	}
	return config.Provider{}, false
}

func removeDisabled(in []Disabled, key string) []Disabled {
	out := in[:0]
	for _, d := range in {
		if d.Key != key {
			out = append(out, d)
		}
	}
	return out
}

// RedirectURL is where the IdP sends the browser back to.
func (r *Registry) RedirectURL(key string) string {
	return r.siteURL + "/auth/" + key + "/callback"
}

// discover fetches one provider's metadata and builds everything derived from
// it. Every failure path here ends in a disabled provider rather than a crash.
func (r *Registry) discover(ctx context.Context, pc config.Provider) (*Provider, error) {
	if err := requireSecure(pc.Issuer); err != nil {
		return nil, err
	}
	ctx = gooidc.ClientContext(ctx, r.hc)
	if pc.InsecureSkipIssuerCheck {
		// The Azure quirk in docs/09: the discovery document names an issuer
		// that differs from the URL it was fetched from, because the tenant id
		// is substituted into it. This tells go-oidc to accept the document
		// anyway; the verifier below is told separately not to compare issuers.
		ctx = gooidc.InsecureIssuerURLContext(ctx, pc.Issuer)
	}

	gp, err := gooidc.NewProvider(ctx, pc.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}

	ep := gp.Endpoint()
	for _, u := range []string{ep.AuthURL, ep.TokenURL} {
		if err := requireSecure(u); err != nil {
			return nil, fmt.Errorf("discovery named an insecure endpoint: %w", err)
		}
	}

	var meta struct {
		Issuer  string `json:"issuer"`
		JWKSURL string `json:"jwks_uri"`
	}
	if err := gp.Claims(&meta); err != nil {
		return nil, fmt.Errorf("reading the discovery document: %w", err)
	}
	if err := requireSecure(meta.JWKSURL); err != nil {
		return nil, fmt.Errorf("discovery named an insecure jwks_uri: %w", err)
	}

	// The key set is fetched here rather than left for the first login. That is
	// the acceptance criterion, and it is also what turns "this IdP is
	// unreachable" from an error the first person to log in discovers into one
	// the operator sees in the startup log.
	keys, err := newBoundedKeySet(ctx, meta.JWKSURL, r.hc, r.now)
	if err != nil {
		return nil, fmt.Errorf("jwks: %w", err)
	}

	secret, err := clientSecret(pc)
	if err != nil {
		return nil, err
	}

	// The issuer tokens will actually carry is the one the discovery document
	// names, which is not always the URL it was fetched from -- that difference
	// is the whole reason insecure_skip_issuer_check exists.
	issuer := meta.Issuer
	if issuer == "" {
		issuer = pc.Issuer
	}

	return &Provider{
		cfg:  pc,
		keys: keys,
		verifier: gooidc.NewVerifier(issuer, keys, &gooidc.Config{
			ClientID:             pc.ClientID,
			SupportedSigningAlgs: signingAlgs,
			SkipIssuerCheck:      pc.InsecureSkipIssuerCheck,
		}),
		oauth: &oauth2.Config{
			ClientID:     pc.ClientID,
			ClientSecret: secret,
			Endpoint:     ep,
			RedirectURL:  r.RedirectURL(pc.Key),
			Scopes:       scopes(pc.Scopes),
		},
	}, nil
}

// clientSecret is the one place a configured secret is unwrapped. Keeping the
// unwrap here, and the oauth2.Config unexported, is what stops the secret from
// reaching the handler package at all: the flow calls AuthCodeURL and Exchange
// and never holds the value.
func clientSecret(pc config.Provider) (string, error) {
	if pc.ClientSecret.IsZero() {
		// A public client authenticates with PKCE alone, which is a legitimate
		// configuration. Validation in internal/config decides whether this
		// provider was allowed to omit it.
		return "", nil
	}
	return pc.ClientSecret.Value(), nil
}

// requireSecure refuses a plaintext endpoint.
//
// There is no per-provider escape hatch for this, unlike the issuer check. The
// two look similar and are not: skipping the issuer comparison accepts a
// document whose contents we still fetched over TLS, whereas fetching that
// document over plaintext means whoever can rewrite the response chooses the
// JWKS, and then every signature check passes against keys they minted.
func requireSecure(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("unparseable url %q: %w", raw, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	return fmt.Errorf("%q is not https: discovery and the key fetch would be unauthenticated, "+
		"so verifying a signature against the keys it returned would prove nothing", raw)
}

// scopes guarantees openid is present exactly once and preserves the operator's
// order otherwise.
func scopes(want []string) []string {
	if len(want) == 0 {
		want = strings.Fields(config.DefaultProviderScopes)
	}
	out := []string{gooidc.ScopeOpenID}
	seen := map[string]bool{gooidc.ScopeOpenID: true}
	for _, s := range want {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Provider is one discovered IdP.
type Provider struct {
	cfg config.Provider

	// state is swapped wholesale on refresh so that a login in flight keeps the
	// verifier and endpoints it started with.
	state atomic.Pointer[providerState]

	// The fields below are only written by discover before the Provider is
	// published, and by swap through the atomic above.
	keys     *boundedKeySet
	verifier *gooidc.IDTokenVerifier
	oauth    *oauth2.Config
}

type providerState struct {
	keys     *boundedKeySet
	verifier *gooidc.IDTokenVerifier
	oauth    *oauth2.Config
}

func (p *Provider) current() *providerState {
	if s := p.state.Load(); s != nil {
		return s
	}
	return &providerState{keys: p.keys, verifier: p.verifier, oauth: p.oauth}
}

func (p *Provider) swap(fresh *Provider) {
	p.state.Store(&providerState{
		keys:     fresh.keys,
		verifier: fresh.verifier,
		oauth:    fresh.oauth,
	})
}

// Key is the stable identifier stored in identities.provider_key.
func (p *Provider) Key() string { return p.cfg.Key }

// Name is what the login button says.
func (p *Provider) Name() string {
	if p.cfg.Name != "" {
		return p.cfg.Name
	}
	return p.cfg.Key
}

// AutoApprove reports whether this provider bypasses approval-mode
// registration.
func (p *Provider) AutoApprove() bool { return p.cfg.AutoApprove }

// AuthCodeURL is where the browser goes to log in. The caller owns state, nonce
// and the PKCE verifier, because only it knows which browser started this.
func (p *Provider) AuthCodeURL(state, nonce, pkceVerifier string) string {
	return p.current().oauth.AuthCodeURL(state,
		oauth2.S256ChallengeOption(pkceVerifier),
		gooidc.Nonce(nonce),
	)
}

// Exchange trades an authorization code for a verified identity.
//
// Signature, issuer, audience and expiry are checked by the library against the
// IdP's published keys. The nonce is checked here because only this process
// knows what it sent.
func (p *Provider) Exchange(ctx context.Context, hc *http.Client, code, nonce, pkceVerifier string) (*Identity, error) {
	st := p.current()
	if hc != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, hc)
	}
	tok, err := st.oauth.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errors.New("oidc: the token response carried no id_token; " +
			"is the openid scope granted for this client?")
	}
	return p.Verify(ctx, raw, nonce)
}

// Verify checks an ID token and returns the identity it asserts.
//
// It is exported separately from Exchange because the key-rotation behaviour is
// a property of verification alone, and testing it should not require a token
// endpoint.
func (p *Provider) Verify(ctx context.Context, rawIDToken, nonce string) (*Identity, error) {
	st := p.current()
	idt, err := st.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: id_token: %w", err)
	}
	// The nonce ties this token to the login attempt that started in this
	// browser. Without it, a token minted for a different session could be
	// replayed into this callback. The library cannot check it, because it does
	// not know what was sent.
	if nonce != "" && idt.Nonce != nonce {
		return nil, errors.New("oidc: the id_token nonce does not match this login attempt")
	}
	if strings.TrimSpace(idt.Subject) == "" {
		return nil, errors.New("oidc: the id_token has no sub")
	}

	var claims map[string]json.RawMessage
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: id_token claims: %w", err)
	}
	rawClaims, _ := json.Marshal(claims)

	id := &Identity{
		Provider:          p.cfg.Key,
		Issuer:            idt.Issuer,
		Subject:           idt.Subject,
		Email:             stringClaim(claims, "email"),
		EmailVerified:     boolClaim(claims, "email_verified"),
		Name:              stringClaim(claims, "name"),
		PreferredUsername: stringClaim(claims, "preferred_username"),
		// Google's hd claim names the Workspace domain the account belongs to.
		// It is recorded rather than acted on here: a domain gate that trusts
		// hd is a different decision from one that trusts the email domain, and
		// it belongs with the provider-gating ticket.
		HostedDomain: stringClaim(claims, "hd"),
		Groups:       groupsFrom(claims, p.groupsClaim()),
		RawClaims:    rawClaims,
	}
	return id, nil
}

func (p *Provider) groupsClaim() string {
	if c := strings.TrimSpace(p.cfg.GroupsClaim); c != "" {
		return c
	}
	return "groups"
}

// LocalGroups maps an identity's IdP groups onto local group names through the
// configured group_map.
//
// An unmapped group yields nothing rather than a default. Authenticating
// establishes who someone is and says nothing about what this forum meant to
// grant them, and an operator who writes a group map has already expressed that
// distinction by writing one.
func (p *Provider) LocalGroups(id *Identity) []string {
	if id == nil || len(p.cfg.GroupMap) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range id.Groups {
		local, ok := p.cfg.GroupMap[g]
		if !ok || local == "" || seen[local] {
			continue
		}
		seen[local] = true
		out = append(out, local)
	}
	return out
}

// EmailAllowed applies the provider's allowed_email_domains gate.
//
// An unset list allows everything, which is what an operator who did not write
// one meant. A set list with no matching domain refuses, and an identity with
// no email refuses too: a gate on a value that is absent is not a gate.
func (p *Provider) EmailAllowed(email string) bool {
	if len(p.cfg.AllowedEmailDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	got := strings.ToLower(email[at+1:])
	for _, want := range p.cfg.AllowedEmailDomains {
		if got == strings.ToLower(strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

// Identity is what a verified ID token asserts.
type Identity struct {
	// Provider is the config key, stored as identities.provider_key.
	Provider string
	// Issuer is the iss claim, stored alongside the subject.
	Issuer string
	// Subject is the IdP's stable identifier for this person. Everything
	// downstream keys on this and never on email or handle: both are mutable in
	// every IdP, so keying on them means a rename either strands an account or,
	// worse, lets a recycled address inherit someone else's.
	Subject string

	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
	HostedDomain      string
	Groups            []string

	// RawClaims is the claim set as received, for identities.raw_claims_json.
	RawClaims json.RawMessage
}

func stringClaim(claims map[string]json.RawMessage, name string) string {
	raw, ok := claims[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func boolClaim(claims map[string]json.RawMessage, name string) bool {
	raw, ok := claims[name]
	if !ok {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	// Some IdPs send email_verified as the string "true".
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s == "true"
	}
	return false
}

// groupsFrom reads the configured groups claim, tolerating the two shapes IdPs
// actually emit: a list of strings, or a single string.
func groupsFrom(claims map[string]json.RawMessage, claim string) []string {
	raw, ok := claims[claim]
	if !ok {
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil && one != "" {
		return []string{one}
	}
	return nil
}
