package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

// MinKeyRefresh is the shortest interval between two key fetches provoked by an
// unrecognised key id. See boundedKeySet for why there is a floor at all.
const MinKeyRefresh = time.Minute

// maxJWKS bounds the key document read, so that a compromised or hostile issuer
// cannot make ketju allocate without limit. Real key sets are a few kilobytes.
const maxJWKS = 512 << 10

// boundedKeySet is go-oidc's RemoteKeySet with a floor on how often an
// unrecognised key id may provoke a fetch.
//
// The library already does the hard half correctly: it verifies against cached
// keys, and only when the token's kid matches nothing does it refetch, with
// concurrent refetches coalesced into one request. That satisfies the rotation
// requirement, and it is what makes a key rotation invisible to everyone
// logging in.
//
// What it does not do is bound the *sequential* case, and that is the one an
// attacker controls. Verification runs on a token supplied by whoever called
// the callback endpoint, the kid is a field in that token, and a fresh random
// kid misses the cache every time. So an unauthenticated caller can turn one
// cheap forged token into one JWKS fetch at the IdP, repeatedly, and ketju
// becomes the thing hammering someone else's identity provider.
//
// The fix is to remember which key ids have actually verified something and to
// rate limit only the ones that have not. A real rotation presents an unknown
// kid once, is allowed its fetch, and is remembered; a flood of invented kids
// pays for one fetch a minute between them. Nothing about the legitimate path
// is slowed, because a legitimate token carries a kid that is already known.
type boundedKeySet struct {
	inner      *gooidc.RemoteKeySet
	minRefresh time.Duration
	now        func() time.Time

	mu sync.Mutex
	// known holds the key ids that have verified a token, plus the ones the
	// startup fetch found. Membership means "asking the IdP about this kid
	// would tell us nothing new".
	known       map[string]bool
	lastRefresh time.Time

	// refreshes counts fetches this type permitted, for the tests that assert
	// one refresh rather than one per request.
	refreshes atomic.Uint64
	// suppressed counts the fetches it refused.
	suppressed atomic.Uint64
}

// newBoundedKeySet fetches the key set once, so that an unreachable jwks_uri is
// a startup failure rather than a surprise for whoever logs in first, and seeds
// the known key ids from what it found.
func newBoundedKeySet(ctx context.Context, jwksURL string, hc *http.Client, now func() time.Time) (*boundedKeySet, error) {
	kids, err := fetchKeyIDs(ctx, jwksURL, hc)
	if err != nil {
		return nil, err
	}
	if len(kids) == 0 {
		return nil, errors.New("the key set is empty, so no token from this issuer could ever be verified")
	}
	known := make(map[string]bool, len(kids))
	for _, kid := range kids {
		known[kid] = true
	}
	return &boundedKeySet{
		inner:      gooidc.NewRemoteKeySet(gooidc.ClientContext(ctx, hc), jwksURL),
		minRefresh: MinKeyRefresh,
		now:        now,
		known:      known,
	}, nil
}

// VerifySignature implements gooidc.KeySet.
func (k *boundedKeySet) VerifySignature(ctx context.Context, rawJWT string) ([]byte, error) {
	kid, err := keyIDOf(rawJWT)
	if err != nil {
		return nil, err
	}

	if !k.allow(kid) {
		k.suppressed.Add(1)
		// Deliberately the same shape of failure as a bad signature. A caller
		// learns that the token was not accepted and nothing about the state of
		// the key cache.
		return nil, fmt.Errorf("failed to verify id token signature: unknown key id %q "+
			"and the key set was refreshed less than %s ago", kid, k.minRefresh)
	}

	payload, err := k.inner.VerifySignature(ctx, rawJWT)
	if err != nil {
		return nil, err
	}
	if kid != "" {
		k.mu.Lock()
		k.known[kid] = true
		k.mu.Unlock()
	}
	return payload, nil
}

// allow reports whether this token may reach the remote key set, and records
// the refresh it is about to cause.
func (k *boundedKeySet) allow(kid string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()

	// A kid that has verified before will hit the library's cache, so there is
	// nothing to rate limit. An empty kid is the single-key case: it matches
	// every cached key, so it also cannot force a fetch.
	if kid == "" || k.known[kid] {
		return true
	}
	if !k.lastRefresh.IsZero() && k.now().Sub(k.lastRefresh) < k.minRefresh {
		return false
	}
	k.lastRefresh = k.now()
	k.refreshes.Add(1)
	return true
}

// keyIDOf reads the kid header without verifying anything.
//
// The algorithm list is the same pinned set the verifier uses. Parsing with a
// wider list here would mean an HMAC-shaped token got as far as the key cache
// before being refused, which is a longer path to the same answer.
func keyIDOf(rawJWT string) (string, error) {
	jws, err := jose.ParseSigned(rawJWT, joseAlgs())
	if err != nil {
		return "", fmt.Errorf("oidc: malformed id_token: %w", err)
	}
	if len(jws.Signatures) != 1 {
		// go-oidc refuses multi-signature tokens further in; refusing here
		// keeps the kid unambiguous.
		return "", errors.New("oidc: id_token does not carry exactly one signature")
	}
	return jws.Signatures[0].Header.KeyID, nil
}

func joseAlgs() []jose.SignatureAlgorithm {
	out := make([]jose.SignatureAlgorithm, 0, len(signingAlgs))
	for _, a := range signingAlgs {
		out = append(out, jose.SignatureAlgorithm(a))
	}
	return out
}

// fetchKeyIDs reads the key set once and returns the key ids in it.
func fetchKeyIDs(ctx context.Context, jwksURL string, hc *http.Client) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", jwksURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKS))
	if err != nil {
		return nil, err
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("parsing the key set: %w", err)
	}
	out := make([]string, 0, len(set.Keys))
	for _, key := range set.Keys {
		out = append(out, key.KeyID)
	}
	return out, nil
}
