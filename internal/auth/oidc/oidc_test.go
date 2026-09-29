package oidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/config"
)

// clientFor builds a client that trusts exactly the fake issuers given, so that
// a multi-provider test can reach both without turning verification off.
func clientFor(idps ...*fakeIdP) *http.Client {
	pool := x509.NewCertPool()
	for _, f := range idps {
		pool.AddCert(f.srv.Certificate())
	}
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
}

// safeBuf is a bytes.Buffer a slog handler may write to from any goroutine.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func testConfig(providers ...config.Provider) *config.Config {
	return &config.Config{
		SiteURL: "https://forum.example",
		OIDC:    config.OIDC{Providers: providers},
	}
}

// newRegistry builds a registry over the given IdPs, one provider each, keyed
// p0, p1, ... and fails the test if it could not be built.
func newRegistry(t *testing.T, opts Options, idps ...*fakeIdP) *Registry {
	t.Helper()
	provs := make([]config.Provider, 0, len(idps))
	for i, f := range idps {
		provs = append(provs, f.providerConfig(fmt.Sprintf("p%d", i)))
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = clientFor(idps...)
	}
	r, err := New(context.Background(), testConfig(provs...), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// --- Acceptance criterion 1 -------------------------------------------------

// TestDiscoveryAndKeysAreFetchedAtStartup is the first criterion, and it is a
// statement about when the work happens rather than whether it happens.
//
// Leaving the key fetch to the first login means an unreachable IdP is
// discovered by a person trying to sign in, hours after the deploy that broke
// it, and reported as "login is broken" rather than as the startup failure it
// is. So the counters must have moved before anyone has logged in.
func TestDiscoveryAndKeysAreFetchedAtStartup(t *testing.T) {
	a, b := newFakeIdP(t), newFakeIdP(t)
	newRegistry(t, Options{Logger: discard()}, a, b)

	for i, f := range []*fakeIdP{a, b} {
		if got := f.discoveryHits.Load(); got != 1 {
			t.Errorf("provider %d: discovery fetched %d times at startup, want 1", i, got)
		}
		if got := f.jwksHits.Load(); got < 1 {
			t.Errorf("provider %d: the key set was never fetched at startup", i)
		}
	}
}

func TestAnUnreachableKeySetIsAStartupFailure(t *testing.T) {
	f := newFakeIdP(t)
	f.jwksStatus = http.StatusInternalServerError

	_, err := New(context.Background(), testConfig(f.providerConfig("p0")),
		Options{Logger: discard(), HTTPClient: clientFor(f)})
	if err == nil {
		t.Fatal("a provider whose key set is unreachable was accepted")
	}
	if !strings.Contains(err.Error(), "p0") {
		t.Errorf("the error does not name the provider: %v", err)
	}
}

func TestAnEmptyKeySetIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	// An IdP that answers with an empty key set is reachable and useless: no
	// token it ever issues could be verified.
	f.srv.Config.Handler.(*http.ServeMux).HandleFunc("/empty-jwks",
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"keys":[]}`))
		})
	_, err := fetchKeyIDs(context.Background(), f.issuer+"/empty-jwks", clientFor(f))
	if err != nil {
		t.Fatalf("fetchKeyIDs: %v", err)
	}
	ks, err := newBoundedKeySet(context.Background(), f.issuer+"/empty-jwks", clientFor(f), time.Now)
	if err == nil {
		_ = ks
		t.Fatal("an empty key set was accepted")
	}
}

// --- Acceptance criterion 2 -------------------------------------------------

// TestABrokenProviderIsDisabledAndTheOthersStillServe is the second criterion.
//
// The failure this prevents is a forum with three login methods going entirely
// dark because one identity provider is having a bad morning. It is also the
// reason Disabled is a field rather than a log line: an admin needs to be told
// which one and why, after the startup scroll is gone.
func TestABrokenProviderIsDisabledAndTheOthersStillServe(t *testing.T) {
	good, broken := newFakeIdP(t), newFakeIdP(t)
	broken.discoveryStatus = http.StatusBadGateway

	var buf safeBuf
	log := slog.New(slog.NewTextHandler(&buf, nil))

	r, err := New(context.Background(), testConfig(
		broken.providerConfig("broken"),
		good.providerConfig("good"),
	), Options{Logger: log, HTTPClient: clientFor(good, broken)})
	if err != nil {
		t.Fatalf("one broken provider made the whole registry fail: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if n := len(r.Enabled()); n != 1 {
		t.Fatalf("%d providers enabled, want 1", n)
	}
	if r.Enabled()[0].Key() != "good" {
		t.Errorf("the surviving provider is %q, want good", r.Enabled()[0].Key())
	}
	if _, ok := r.Lookup("broken"); ok {
		t.Error("the broken provider is still reachable through Lookup")
	}

	dis := r.Disabled()
	if len(dis) != 1 || dis[0].Key != "broken" {
		t.Fatalf("Disabled = %+v, want one entry for broken", dis)
	}
	if dis[0].Err == nil {
		t.Error("the disabled provider carries no reason")
	}
	if out := buf.String(); !strings.Contains(out, "broken") {
		t.Errorf("the log does not name the disabled provider:\n%s", out)
	}
}

func TestEveryProviderBrokenIsAStartupFailure(t *testing.T) {
	a, b := newFakeIdP(t), newFakeIdP(t)
	a.discoveryStatus = http.StatusBadGateway
	b.discoveryStatus = http.StatusBadGateway

	// One broken provider is survivable; all of them is a forum nobody can sign
	// in to, and it should say so at startup rather than serve an empty login
	// page and wait for a bug report.
	_, err := New(context.Background(), testConfig(
		a.providerConfig("p0"), b.providerConfig("p1"),
	), Options{Logger: discard(), HTTPClient: clientFor(a, b)})
	if err == nil {
		t.Fatal("a registry with no working provider was accepted")
	}
}

func TestARecoveredProviderIsEnabledOnRefresh(t *testing.T) {
	good, flaky := newFakeIdP(t), newFakeIdP(t)
	flaky.discoveryStatus = http.StatusBadGateway

	r, err := New(context.Background(), testConfig(
		good.providerConfig("good"), flaky.providerConfig("flaky"),
	), Options{Logger: discard(), HTTPClient: clientFor(good, flaky)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	flaky.discoveryStatus = 0
	r.Refresh(context.Background())

	if _, ok := r.Lookup("flaky"); !ok {
		t.Error("a provider that came back is still disabled; it would need a restart")
	}
	if n := len(r.Disabled()); n != 0 {
		t.Errorf("%d providers still disabled, want 0", n)
	}
	if n := len(r.Enabled()); n != 2 {
		t.Errorf("%d providers enabled, want 2", n)
	}
}

func TestARefreshFailureKeepsTheWorkingProvider(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard()}, f)

	f.discoveryStatus = http.StatusBadGateway
	r.Refresh(context.Background())

	// A transient failure during a refresh must not remove a login method that
	// was working a second ago.
	if _, ok := r.Lookup("p0"); !ok {
		t.Fatal("a transient refresh failure disabled a working provider")
	}
	p, _ := r.Lookup("p0")
	if p.current().verifier == nil {
		t.Error("the provider lost its verifier")
	}
}

// --- Acceptance criterion 3 -------------------------------------------------

// TestKeyRotationCostsOneRefreshAndThenWorks is the good half of the third
// criterion: a real rotation is invisible to whoever is logging in.
func TestKeyRotationCostsOneRefreshAndThenWorks(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{"nonce": "n1"}
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	hc := clientFor(f)
	if _, err := p.Exchange(context.Background(), hc, "code", "n1", "verifier"); err != nil {
		t.Fatalf("the first login failed: %v", err)
	}

	f.rotate(t, "k2")
	before := f.jwksHits.Load()

	if _, err := p.Exchange(context.Background(), hc, "code", "n1", "verifier"); err != nil {
		t.Fatalf("a login after key rotation failed: %v", err)
	}
	if got := f.jwksHits.Load() - before; got != 1 {
		t.Errorf("rotation cost %d key fetches, want exactly 1", got)
	}

	// And the new key id is now known, so the next login costs nothing.
	after := f.jwksHits.Load()
	if _, err := p.Exchange(context.Background(), hc, "code", "n1", "verifier"); err != nil {
		t.Fatalf("the login after the rotation refresh failed: %v", err)
	}
	if got := f.jwksHits.Load() - after; got != 0 {
		t.Errorf("a login on the rotated key cost %d further key fetches, want 0", got)
	}
}

// TestForgedKeyIDsCannotDriveKeyFetches is the half of the third criterion that
// the library does not give us, and the reason boundedKeySet exists.
//
// go-oidc refetches whenever a token's key id misses the cache, and coalesces
// concurrent refetches. Sequentially it does not bound anything, and the key id
// is a field in a token supplied by whoever called the callback endpoint. So an
// unauthenticated caller with a stream of cheap forged tokens turns ketju into
// the thing hammering someone else's identity provider.
func TestForgedKeyIDsCannotDriveKeyFetches(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	before := f.jwksHits.Load()
	const attempts = 50
	for i := range attempts {
		tok := f.mintWith(t, attacker, fmt.Sprintf("invented-%d", i), nil)
		if _, err := p.Verify(context.Background(), tok, ""); err == nil {
			t.Fatalf("a token signed by a key the IdP never published was accepted (attempt %d)", i)
		}
	}
	fetches := f.jwksHits.Load() - before
	if fetches > 1 {
		t.Errorf("%d forged key ids provoked %d key fetches; one caller can point this at the IdP",
			attempts, fetches)
	}
}

func TestTheRateLimitDoesNotDelayARealRotation(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{"nonce": "n1"}

	now := time.Now()
	clock := func() time.Time { return now }
	r := newRegistry(t, Options{Logger: discard(), Now: clock}, f)
	p, _ := r.Lookup("p0")
	hc := clientFor(f)

	// Spend the budget on a forgery, the way an attacker would just before a
	// real rotation lands.
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(context.Background(), f.mintWith(t, attacker, "invented", nil), ""); err == nil {
		t.Fatal("a forged token was accepted")
	}

	f.rotate(t, "k2")
	now = now.Add(MinKeyRefresh + time.Second)

	if _, err := p.Exchange(context.Background(), hc, "code", "n1", "verifier"); err != nil {
		t.Fatalf("a real rotation was still blocked after the interval elapsed: %v", err)
	}
}

func TestConcurrentVerificationIsSafe(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{"nonce": "n1"}
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	tok := f.mintIDToken(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := p.Verify(context.Background(), tok, "n1"); err != nil {
					t.Errorf("Verify: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// --- Acceptance criterion 4 -------------------------------------------------

// TestTheIssuerMismatchEscapeHatchIsPerProviderAndOffByDefault is the fourth
// criterion, and it has to be both halves: that the hatch works, and that it is
// shut unless someone opened it.
//
// The quirk it exists for is real (docs/09 names Azure, whose metadata
// substitutes a tenant id into the issuer). The danger is that a flag added for
// one provider quietly becomes the default for all of them.
func TestTheIssuerMismatchEscapeHatchIsPerProviderAndOffByDefault(t *testing.T) {
	strict, lax := newFakeIdP(t), newFakeIdP(t)
	strict.issuerOut = "https://login.example/tenant-1/v2.0"
	lax.issuerOut = "https://login.example/tenant-1/v2.0"

	laxCfg := lax.providerConfig("lax")
	laxCfg.InsecureSkipIssuerCheck = true

	var buf safeBuf
	r, err := New(context.Background(), testConfig(strict.providerConfig("strict"), laxCfg),
		Options{
			Logger:     slog.New(slog.NewTextHandler(&buf, nil)),
			HTTPClient: clientFor(strict, lax),
		})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	// Both discovered: go-oidc refuses the mismatched document by default, so
	// the strict provider is expected to be disabled rather than to fail later.
	if _, ok := r.Lookup("lax"); !ok {
		t.Fatal("the provider with the escape hatch on was not discovered")
	}
	if _, ok := r.Lookup("strict"); ok {
		t.Error("a provider whose metadata names a different issuer was accepted with the check on")
	}

	if out := buf.String(); !strings.Contains(out, "issuer check") {
		t.Errorf("turning the issuer check off produced no warning:\n%s", out)
	}

	// The hatch is per provider: the strict one is disabled, so nothing about
	// the lax block loosened it.
	dis := r.Disabled()
	if len(dis) != 1 || dis[0].Key != "strict" {
		t.Errorf("Disabled = %+v, want one entry for strict", dis)
	}
}

// --- An issuer is used exactly as written (TKT-01M3MAS08R) ------------------

// TestAnIssuerEndingInASlashWorksEndToEnd is the Authentik shape: the
// per-application issuer ends in a slash, and so do the discovery document's
// issuer and every token's iss claim. Configured exactly as the provider names
// it, the provider discovers and a login verifies. Nothing between config and
// go-oidc may trim the slash, or discovery would refuse the document.
func TestAnIssuerEndingInASlashWorksEndToEnd(t *testing.T) {
	f := newFakeIdPAt(t, "/application/o/ketju/")
	if !strings.HasSuffix(f.issuer, "/application/o/ketju/") {
		t.Fatalf("fixture issuer = %q; the test needs one ending in a slash", f.issuer)
	}
	f.claims = map[string]any{"nonce": "n1"}

	r := newRegistry(t, Options{Logger: discard()}, f)
	p, ok := r.Lookup("p0")
	if !ok {
		t.Fatalf("the slash-ended issuer was not discovered; disabled: %+v", r.Disabled())
	}
	if n := f.discoveryHits.Load(); n != 1 {
		t.Errorf("discovery fetched %d times, want 1", n)
	}

	id, err := p.Exchange(context.Background(), clientFor(f), "code", "n1", "the-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Issuer != f.issuer {
		t.Errorf("Issuer = %q, want %q exactly", id.Issuer, f.issuer)
	}
}

// TestAnIssuerThatDiffersFromDiscoveryFailsAtStartupNamingBoth covers a wrong
// guess about the slash, in either direction. The provider is refused at
// discovery, before anyone tries to log in, and the startup log names the
// configured and the discovered issuer, because telling "…/ketju" from
// "…/ketju/" is the whole of the operator's fix.
func TestAnIssuerThatDiffersFromDiscoveryFailsAtStartupNamingBoth(t *testing.T) {
	cases := []struct {
		name string
		path string
		out  func(configured string) string
	}{
		{"configured with a slash, discovered without", "/application/o/ketju/",
			func(c string) string { return strings.TrimSuffix(c, "/") }},
		{"configured without a slash, discovered with", "/application/o/ketju",
			func(c string) string { return c + "/" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdPAt(t, tc.path)
			configured := f.issuer
			discovered := tc.out(configured)
			f.issuerOut = discovered

			var buf safeBuf
			r, err := New(context.Background(), testConfig(f.providerConfig("authentik")),
				Options{Logger: slog.New(slog.NewJSONHandler(&buf, nil)), HTTPClient: clientFor(f)})
			if err == nil {
				_ = r.Close()
				t.Fatal("a provider whose discovery document names a different issuer was accepted")
			}
			if f.discoveryHits.Load() != 1 {
				t.Errorf("discovery fetched %d times, want 1", f.discoveryHits.Load())
			}

			// Decode the log line rather than grepping the raw output: one value
			// is a prefix of the other, and the handler escapes the quotes that
			// tell them apart.
			type logLine struct {
				Msg      string `json:"msg"`
				Provider string `json:"provider"`
				Err      string `json:"err"`
			}
			var line logLine
			found := false
			for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				var l logLine
				if err := json.Unmarshal([]byte(raw), &l); err != nil {
					t.Fatalf("log line %q: %v", raw, err)
				}
				if l.Msg == "oidc provider disabled: discovery failed" {
					line, found = l, true
					break
				}
			}
			if !found {
				t.Fatalf("no discovery failure was logged:\n%s", buf.String())
			}
			if line.Provider != "authentik" {
				t.Errorf("the failure names provider %q, want authentik", line.Provider)
			}
			for _, want := range []string{fmt.Sprintf("%q", configured), fmt.Sprintf("%q", discovered)} {
				if !strings.Contains(line.Err, want) {
					t.Errorf("the failure does not name %s:\n%s", want, line.Err)
				}
			}
		})
	}
}

// --- The rules in the package comment ---------------------------------------

func TestAPlaintextIssuerIsRefused(t *testing.T) {
	good, plain := newFakeIdP(t), newFakeIdP(t)
	pc := plain.providerConfig("plain")
	pc.Issuer = strings.Replace(plain.issuer, "https://", "http://", 1)

	r, err := New(context.Background(), testConfig(good.providerConfig("good"), pc),
		Options{Logger: discard(), HTTPClient: clientFor(good, plain)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if _, ok := r.Lookup("plain"); ok {
		t.Fatal("a plaintext issuer was accepted")
	}
	dis := r.Disabled()
	if len(dis) != 1 {
		t.Fatalf("Disabled = %+v, want one entry", dis)
	}
	// The reason has to say why, because "not https" reads like pedantry until
	// you know that the key fetch is what it protects.
	if !strings.Contains(dis[0].Err.Error(), "prove nothing") {
		t.Errorf("the refusal does not explain itself: %v", dis[0].Err)
	}
	// And it never reached the network.
	if n := plain.discoveryHits.Load(); n != 0 {
		t.Errorf("the plaintext issuer was contacted %d times before being refused", n)
	}
}

func TestDiscoveryNamingAPlaintextEndpointIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	// A compromised or careless IdP can advertise a plaintext token endpoint,
	// which would send the client secret and the authorization code in clear.
	mux := f.srv.Config.Handler.(*http.ServeMux)
	mux.HandleFunc("/bad/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":"http://evil.example/token","jwks_uri":%q}`,
			f.issuer+"/bad", f.issuer+"/authorize", f.issuer+"/jwks")
	})
	pc := f.providerConfig("p0")
	pc.Issuer = f.issuer + "/bad"

	_, err := New(context.Background(), testConfig(pc),
		Options{Logger: discard(), HTTPClient: clientFor(f)})
	if err == nil {
		t.Fatal("a discovery document naming a plaintext token endpoint was accepted")
	}
}

// TestAnEllipticCurveProviderWorks is what the pinned algorithm list actually
// buys, and it is worth being precise about, because the obvious claim is wrong.
//
// The obvious claim is that the pin stops key confusion: a token naming HS256
// against a key set of public keys. It does not, because that attack is already
// dead twice over. go-oidc filters HS256 and none out of the algorithms it
// takes from a discovery document, and go-jose refuses an HMAC algorithm
// against a public key beneath that. Removing the pin does not make
// TestASymmetricallySignedTokenIsRefused fail, and a comment claiming otherwise
// would be describing a defence that is not there.
//
// What the pin does is make the accepted set independent of both the IdP and
// the library's default. gooidc.NewVerifier with no list defaults to RS256
// alone, so an identity provider configured with an elliptic-curve key -- an
// ordinary configuration, not an exotic one -- would have every token refused.
// That is the failure this test holds down, and it is a liveness failure rather
// than a security one, which is exactly why it would otherwise go unnoticed
// until somebody deployed against such a provider.
func TestAnEllipticCurveProviderWorks(t *testing.T) {
	f := newFakeIdP(t)
	f.useEC(t)
	f.claims = map[string]any{"nonce": "n1"}

	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	id, err := p.Verify(context.Background(), f.mintIDToken(t), "n1")
	if err != nil {
		t.Fatalf("a provider signing with ES256 was refused: %v", err)
	}
	if id.Subject != "user-123" {
		t.Errorf("Subject = %q", id.Subject)
	}
}

// TestTheSigningAlgorithmListCarriesNoSymmetricEntry locks the property that
// the list is asymmetric-only.
//
// Every entry is a public-key scheme, so a valid signature can only come from
// whoever holds the IdP's private key and nothing ketju holds could forge one.
// Adding an HMAC entry, or "none", would mean the key set's public keys became
// usable as signing secrets, and the layers beneath would be the only thing
// left refusing it.
func TestTheSigningAlgorithmListCarriesNoSymmetricEntry(t *testing.T) {
	for _, alg := range signingAlgs {
		if strings.HasPrefix(alg, "HS") || strings.EqualFold(alg, "none") || alg == "" {
			t.Errorf("signingAlgs contains %q, which is not a public-key scheme", alg)
		}
	}
	if len(signingAlgs) == 0 {
		t.Error("signingAlgs is empty; every token would be refused")
	}
}

// TestASymmetricallySignedTokenIsRefused checks the outcome rather than the
// mechanism: an HS256 token minted against the published public key is not
// accepted. Two layers refuse it before this package's pin is consulted (see
// TestAnEllipticCurveProviderWorks), so this test measures the stack, not the
// pin. It is here because the outcome is what matters and a future upgrade
// could change which layer delivers it.
func TestASymmetricallySignedTokenIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	f.algsOut = []string{"RS256", "HS256"} // the IdP says it is willing
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	pub, err := x509.MarshalPKIXPublicKey(&f.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	// The classic confusion: use the published public key as the HMAC secret.
	tok := f.mintHMAC(t, pub)

	if _, err := p.Verify(context.Background(), tok, ""); err == nil {
		t.Fatal("a token signed with HS256 against the published public key was accepted")
	}
}

func TestATokenSignedByTheWrongKeyIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// Same key id as the real one, so only the signature distinguishes them.
	tok := f.mintWith(t, attacker, f.kid(), nil)

	if _, err := p.Verify(context.Background(), tok, ""); err == nil {
		t.Fatal("a token signed by a key the IdP never published was accepted")
	}
}

func TestANonceFromAnotherLoginIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{"nonce": "somebody-elses-login"}
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	_, err := p.Exchange(context.Background(), clientFor(f), "code", "this-login", "verifier")
	if err == nil {
		t.Fatal("a token minted for a different login attempt was accepted")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Errorf("err = %v, want it to name the nonce", err)
	}
}

func TestAWrongAudienceIsRefused(t *testing.T) {
	f := newFakeIdP(t)
	f.audience = "some-other-client"
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	if _, err := p.Verify(context.Background(), f.mintIDToken(t), ""); err == nil {
		t.Fatal("a token minted for another client was accepted")
	}
}

// --- What the flow ticket will consume --------------------------------------

func TestExchangeYieldsTheIdentity(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{
		"nonce": "n1", "email": "drew@example.com", "email_verified": true,
		"name": "Drew", "preferred_username": "drew",
		"hd": "example.com", "groups": []string{"forum-admins", "everyone"},
	}
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	id, err := p.Exchange(context.Background(), clientFor(f), "code", "n1", "the-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if id.Subject != "user-123" {
		t.Errorf("Subject = %q", id.Subject)
	}
	if id.Email != "drew@example.com" || !id.EmailVerified {
		t.Errorf("email = %q verified = %v", id.Email, id.EmailVerified)
	}
	if id.Name != "Drew" || id.PreferredUsername != "drew" {
		t.Errorf("name = %q preferred_username = %q", id.Name, id.PreferredUsername)
	}
	if id.HostedDomain != "example.com" {
		t.Errorf("hd = %q", id.HostedDomain)
	}
	if id.Provider != "p0" {
		t.Errorf("Provider = %q, want p0; it is what identities.provider_key stores", id.Provider)
	}
	if len(id.RawClaims) == 0 {
		t.Error("RawClaims is empty; identities.raw_claims_json would have nothing to store")
	}
	if f.lastForm.Get("code_verifier") != "the-verifier" {
		t.Error("the PKCE verifier did not reach the token endpoint")
	}
	if !clientAuthenticated(f) {
		t.Error("the token request authenticated as nobody")
	}
}

func TestASingleStringGroupsClaimIsAccepted(t *testing.T) {
	f := newFakeIdP(t)
	// Some IdPs send one group as a bare string rather than a one-element list.
	f.claims = map[string]any{"groups": "admins"}
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	id, err := p.Verify(context.Background(), f.mintIDToken(t), "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(id.Groups) != 1 || id.Groups[0] != "admins" {
		t.Errorf("Groups = %v, want [admins]", id.Groups)
	}
}

// TestTheGroupsClaimIsReadAsAsserted: every value, in order, duplicates
// included. Mapping them onto local groups is identity.Policy.LocalGroups'
// (TestPolicyLocalGroups), which applies group_map.
func TestTheGroupsClaimIsReadAsAsserted(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = map[string]any{"teams": []string{"unmapped", "staff", "staff"}}
	pc := f.providerConfig("p0")
	pc.GroupsClaim = "teams"
	pc.GroupMap = map[string]string{"staff": "moderators"}

	r, err := New(context.Background(), testConfig(pc),
		Options{Logger: discard(), HTTPClient: clientFor(f)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	p, _ := r.Lookup("p0")

	id, err := p.Verify(context.Background(), f.mintIDToken(t), "")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := []string{"unmapped", "staff", "staff"}; !slices.Equal(id.Groups, want) {
		t.Errorf("Groups = %v, want %v", id.Groups, want)
	}
}

// TestGate is the allowed_email_domains gate, applied to the token as the
// IdP signed it, so email_verified is read as Verify reads it
// (TKT-01M29CKRE9 AC2). Each row gives exactly one result.
func TestGate(t *testing.T) {
	f := newFakeIdP(t)
	gated := f.providerConfig("p0")
	gated.AllowedEmailDomains = []string{"Example.COM", "example.org"}

	r, err := New(context.Background(), testConfig(gated),
		Options{Logger: discard(), HTTPClient: clientFor(f)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g, _ := r.Lookup("p0")
	// The same IdP with no list: an operator who did not write a gate did
	// not mean to have one, and nothing requires email_verified.
	o, _ := newRegistry(t, Options{Logger: discard()}, f).Lookup("p0")

	cases := []struct {
		name   string
		p      *Provider
		claims map[string]any
		want   GateResult
	}{
		{"no list, no email", o, map[string]any{}, GateOK},
		{"no list, unverified, another domain", o, map[string]any{"email": "drew@evil.example", "email_verified": false}, GateOK},
		{"no list, verified", o, map[string]any{"email": "drew@example.com", "email_verified": true}, GateOK},

		{"matching and verified", g, map[string]any{"email": "drew@example.com", "email_verified": true}, GateOK},
		{"the list's other domain", g, map[string]any{"email": "drew@example.org", "email_verified": true}, GateOK},
		{"matching in another case", g, map[string]any{"email": "drew@EXAMPLE.com", "email_verified": true}, GateOK},
		{"verified as the string true", g, map[string]any{"email": "drew@example.com", "email_verified": "true"}, GateOK},

		{"matching, email_verified false", g, map[string]any{"email": "drew@example.com", "email_verified": false}, GateUnverified},
		{"matching, email_verified absent", g, map[string]any{"email": "drew@example.com"}, GateUnverified},
		{"matching, email_verified the string false", g, map[string]any{"email": "drew@example.com", "email_verified": "false"}, GateUnverified},
		{"matching, email_verified the string True", g, map[string]any{"email": "drew@example.com", "email_verified": "True"}, GateUnverified},
		{"matching, email_verified 1", g, map[string]any{"email": "drew@example.com", "email_verified": 1}, GateUnverified},
		{"matching, email_verified null", g, map[string]any{"email": "drew@example.com", "email_verified": nil}, GateUnverified},
		{"another domain, unverified", g, map[string]any{"email": "drew@evil.example"}, GateUnverified},

		{"another domain, verified", g, map[string]any{"email": "drew@evil.example", "email_verified": true}, GateDomain},
		{"a subdomain, verified", g, map[string]any{"email": "drew@sub.example.com", "email_verified": true}, GateDomain},
		{"the domain as a prefix, verified", g, map[string]any{"email": "drew@example.com.evil.example", "email_verified": true}, GateDomain},
		// A gate on a value that is absent is not a gate.
		{"no email, verified", g, map[string]any{"email_verified": true}, GateDomain},
		{"no domain, verified", g, map[string]any{"email": "drew", "email_verified": true}, GateDomain},
		{"an empty domain, verified", g, map[string]any{"email": "drew@", "email_verified": true}, GateDomain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := c.p.Verify(context.Background(), f.mintWith(t, f.signWithKey(), f.tokenKeyID(), c.claims), "")
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if got := c.p.Gate(id); got != c.want {
				t.Errorf("Gate = %v, want %v", got, c.want)
			}
		})
	}
}

func TestScopesAlwaysCarryOpenIDExactlyOnce(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{nil, []string{"openid", "email", "profile"}},
		{[]string{"openid"}, []string{"openid"}},
		{[]string{"email", "openid", "email"}, []string{"openid", "email"}},
		{[]string{"groups", ""}, []string{"openid", "groups"}},
	}
	for _, c := range cases {
		got := scopes(c.in)
		if len(got) != len(c.want) {
			t.Errorf("scopes(%v) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("scopes(%v) = %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestEnabledKeepsConfigOrder(t *testing.T) {
	a, b, c := newFakeIdP(t), newFakeIdP(t), newFakeIdP(t)
	r, err := New(context.Background(), testConfig(
		a.providerConfig("zeta"), b.providerConfig("alpha"), c.providerConfig("mid"),
	), Options{Logger: discard(), HTTPClient: clientFor(a, b, c)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	want := []string{"zeta", "alpha", "mid"}
	got := r.Enabled()
	for i := range want {
		if got[i].Key() != want[i] {
			// The login page draws them in this order, so it is the operator's
			// choice and not the map's.
			t.Fatalf("Enabled = %q, want %v", keysOf(got), want)
		}
	}
}

func TestRedirectURLIsDerivedFromSiteURL(t *testing.T) {
	f := newFakeIdP(t)
	cfg := testConfig(f.providerConfig("p0"))
	cfg.SiteURL = "https://forum.example/" // trailing slash
	r, err := New(context.Background(), cfg, Options{Logger: discard(), HTTPClient: clientFor(f)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if got, want := r.RedirectURL("p0"), "https://forum.example/auth/p0/callback"; got != want {
		t.Errorf("RedirectURL = %q, want %q", got, want)
	}
}

func TestAuthCodeURLCarriesPKCEAndNonce(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard()}, f)
	p, _ := r.Lookup("p0")

	raw := p.AuthCodeURL("the-state", "the-nonce", "the-verifier")
	for _, want := range []string{
		"state=the-state", "nonce=the-nonce",
		"code_challenge=", "code_challenge_method=S256",
		"scope=openid", "redirect_uri=",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("AuthCodeURL is missing %q:\n%s", want, raw)
		}
	}
	// The verifier itself must never appear: sending it at authorization would
	// defeat the point of sending its hash.
	if strings.Contains(raw, "the-verifier") {
		t.Error("the PKCE verifier appears in the authorization URL")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard()}, f)
	r.Start()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestTheRefreshLoopRuns(t *testing.T) {
	f := newFakeIdP(t)
	r := newRegistry(t, Options{Logger: discard(), RefreshInterval: 5 * time.Millisecond}, f)
	before := f.discoveryHits.Load()
	r.Start()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.discoveryHits.Load() > before {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Error("the refresh loop never re-read discovery")
}

// clientAuthenticated reports whether the token request proved who it was, by
// either of the two styles oauth2 uses.
func clientAuthenticated(f *fakeIdP) bool {
	if f.lastForm.Get("client_secret") == "s3cret" && f.lastForm.Get("client_id") == "ketju" {
		return true
	}
	user, pass, ok := parseBasic(f.lastAuth)
	return ok && user == "ketju" && pass == "s3cret"
}

func parseBasic(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(header[len(prefix):])
	if err != nil {
		return "", "", false
	}
	// oauth2 form-encodes each half before base64, per RFC 6749 section 2.3.1.
	u, p, found := strings.Cut(string(raw), ":")
	if !found {
		return "", "", false
	}
	du, err1 := url.QueryUnescape(u)
	dp, err2 := url.QueryUnescape(p)
	if err1 != nil || err2 != nil {
		return "", "", false
	}
	return du, dp, true
}

func keysOf(ps []*Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Key())
	}
	return out
}

func discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
