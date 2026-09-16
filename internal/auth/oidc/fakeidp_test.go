package oidc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/terva-sh/ketju/internal/config"
)

// fakeIdP is a minimal OpenID provider that really signs.
//
// The signing is the point. A fixture that returned an unsigned or fixed string
// would let every test below pass against a relying party that had skipped
// signature verification entirely, which is exactly the defect the library
// exists to prevent. Everything an attacker would control -- the signing key,
// the key id, the advertised algorithms, the issuer in the metadata -- is a
// field here so that a test can be the attacker.
type fakeIdP struct {
	srv *httptest.Server

	issuer    string
	issuerOut string // what the discovery document claims; set for mix-up tests

	key      *rsa.PrivateKey
	signWith *rsa.PrivateKey // defaults to key; set to forge

	// ecKey, when set, replaces the RSA key entirely: the key set publishes it
	// and tokens are signed ES256. Some IdPs are configured this way and
	// advertise no RSA algorithm at all.
	ecKey    *ecdsa.PrivateKey
	keyID    string // kid in the JWKS and in minted tokens; defaults to k1
	tokenKid string // kid written into the token only; defaults to keyID

	claims   map[string]any
	audience any
	algsOut  []string // advertised in discovery; defaults to RS256

	// Failure injection.
	discoveryStatus int
	jwksStatus      int

	// Counters. Every acceptance criterion in this ticket is a statement about
	// how many times one of these moved.
	discoveryHits atomic.Int64
	jwksHits      atomic.Int64
	tokenHits     atomic.Int64

	lastForm url.Values
	// lastAuth is the Authorization header the token request carried. oauth2
	// prefers HTTP Basic for client authentication, so the secret is here
	// rather than in the form, and a test that only looked at the form would
	// conclude the client had authenticated as nobody.
	lastAuth string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	f := &fakeIdP{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		f.discoveryHits.Add(1)
		if f.discoveryStatus != 0 {
			w.WriteHeader(f.discoveryStatus)
			return
		}
		iss := f.issuerOut
		if iss == "" {
			iss = f.issuer
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss,
			"authorization_endpoint":                f.issuer + "/authorize",
			"token_endpoint":                        f.issuer + "/token",
			"jwks_uri":                              f.issuer + "/jwks",
			"id_token_signing_alg_values_supported": f.algs(),
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		f.jwksHits.Add(1)
		if f.jwksStatus != 0 {
			w.WriteHeader(f.jwksStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		jwk := jose.JSONWebKey{Key: &f.key.PublicKey, KeyID: f.kid(), Algorithm: "RS256", Use: "sig"}
		if f.ecKey != nil {
			jwk = jose.JSONWebKey{Key: &f.ecKey.PublicKey, KeyID: f.kid(), Algorithm: "ES256", Use: "sig"}
		}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenHits.Add(1)
		_ = r.ParseForm()
		f.lastForm = r.PostForm
		f.lastAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at",
			"token_type":   "Bearer",
			"id_token":     f.mintIDToken(t),
		})
	})

	f.srv = httptest.NewTLSServer(mux)
	f.issuer = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) kid() string {
	if f.keyID == "" {
		return "k1"
	}
	return f.keyID
}

func (f *fakeIdP) algs() []string {
	if len(f.algsOut) == 0 {
		return []string{"RS256"}
	}
	return f.algsOut
}

// rotate replaces the signing key and key id, as a real IdP does on rotation.
func (f *fakeIdP) rotate(t *testing.T, newKid string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	f.key = key
	f.signWith = nil
	f.keyID = newKid
	f.tokenKid = ""
}

func (f *fakeIdP) payload(extra map[string]any) []byte {
	aud := f.audience
	if aud == nil {
		aud = "ketju"
	}
	p := map[string]any{
		"iss": f.issuerFor(), "sub": "user-123", "aud": aud,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}
	for k, v := range f.claims {
		p[k] = v
	}
	for k, v := range extra {
		p[k] = v
	}
	b, _ := json.Marshal(p)
	return b
}

func (f *fakeIdP) issuerFor() string {
	if f.issuerOut != "" {
		return f.issuerOut
	}
	return f.issuer
}

func (f *fakeIdP) mintIDToken(t *testing.T) string {
	t.Helper()
	if f.ecKey != nil {
		return f.mintSigned(t, jose.ES256, f.ecKey, f.tokenKeyID())
	}
	return f.mintWith(t, f.signWithKey(), f.tokenKeyID(), nil)
}

// useEC switches the IdP to an elliptic-curve key and makes discovery advertise
// ES256 alone, which is a real configuration and not a contrived one.
func (f *fakeIdP) useEC(t *testing.T) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ec keygen: %v", err)
	}
	f.ecKey = k
	f.algsOut = []string{"ES256"}
}

func (f *fakeIdP) signWithKey() *rsa.PrivateKey {
	if f.signWith != nil {
		return f.signWith
	}
	return f.key
}

func (f *fakeIdP) tokenKeyID() string {
	if f.tokenKid != "" {
		return f.tokenKid
	}
	return f.kid()
}

// mintWith signs a token with an explicit key and key id, which is how the
// forgery and rate-limit tests produce tokens the IdP would never issue.
func (f *fakeIdP) mintWith(t *testing.T, key *rsa.PrivateKey, kid string, extra map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	obj, err := signer.Sign(f.payload(extra))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return raw
}

// mintSigned signs the current claim set with an explicit algorithm and key.
func (f *fakeIdP) mintSigned(t *testing.T, alg jose.SignatureAlgorithm, key any, kid string) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	if err != nil {
		t.Fatalf("%s signer: %v", alg, err)
	}
	obj, err := signer.Sign(f.payload(nil))
	if err != nil {
		t.Fatalf("%s sign: %v", alg, err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("%s serialize: %v", alg, err)
	}
	return raw
}

// mintHMAC signs with a symmetric key, which is the key-confusion shape: a
// token naming HS256 against a key set of public keys.
func (f *fakeIdP) mintHMAC(t *testing.T, secret []byte) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: secret},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", f.kid()),
	)
	if err != nil {
		t.Fatalf("hmac signer: %v", err)
	}
	obj, err := signer.Sign(f.payload(nil))
	if err != nil {
		t.Fatalf("hmac sign: %v", err)
	}
	raw, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("hmac serialize: %v", err)
	}
	return raw
}

// providerConfig is the config.toml block that would name this IdP.
func (f *fakeIdP) providerConfig(key string) config.Provider {
	return config.Provider{
		Key:          key,
		Name:         key,
		Issuer:       f.issuer,
		ClientID:     "ketju",
		ClientSecret: config.NewSecret("s3cret"),
	}
}
