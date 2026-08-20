package httpauth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// errUnknownKID means a token's key id was not found in the jwks, even after
// a refetch.
var errUnknownKID = errors.New("token key id not found in the jwks")

// errUnsupportedKeyType means a jwk's kty (or, for an EC key, its crv) is not
// one this server verifies against.
var errUnsupportedKeyType = errors.New("unsupported json web key type")

// errUnexpectedStatus means the jwks endpoint answered with something other
// than 200.
var errUnexpectedStatus = errors.New("unexpected status fetching jwks")

// jwksMinRefetchInterval bounds how often an unknown kid triggers a refetch.
// Without it, a caller sending a token with a bogus kid could make this
// server hammer the authorization server on every request.
const jwksMinRefetchInterval = 30 * time.Second

// jwk mirrors the RFC 7517 fields this server understands. Only RSA and EC
// (P-256) keys are parsed; every other kty is skipped rather than failing the
// whole set, so one unsupported key in a JWKS (e.g. an encryption key with
// "use":"enc") does not take down every signing key next to it.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	// RSA
	N string `json:"n,omitempty"`
	E string `json:"e,omitempty"`
	// EC
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// keySet caches an authorization server's signing keys by kid.
//
// It is lazy, not periodic: a background refresh goroutine would need its own
// stop channel wired into the server's shutdown path for a benefit that
// lazy-refetch-on-unknown-kid already provides, since an authorization server
// publishes a rotated key in its JWKS before it starts signing with it. One
// mutex guards both the map and the fetch itself, which serialises concurrent
// refetches into a single HTTP round trip rather than a thundering herd --
// acceptable contention for a check that runs once per request.
type keySet struct {
	// lastRefetch is when an unknown kid last triggered a fetch, the zero
	// value until the first one does. It is deliberately distinct from
	// priming: New's own fetch never sets it, so the very first unknown kid
	// a caller presents always gets one real refetch attempt regardless of
	// how recently the process started.
	lastRefetch time.Time
	httpClient  *http.Client
	keys        map[string]crypto.PublicKey
	url         string
	mu          sync.Mutex
}

func newKeySet(url string, httpClient *http.Client) *keySet {
	return &keySet{url: url, httpClient: httpClient}
}

// key returns the public key for kid, fetching (or refetching, subject to
// jwksMinRefetchInterval) if it is not already cached.
func (k *keySet) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if pub, ok := k.keys[kid]; ok {
		return pub, nil
	}
	if !k.lastRefetch.IsZero() && time.Since(k.lastRefetch) < jwksMinRefetchInterval {
		return nil, fmt.Errorf("%q: %w", kid, errUnknownKID)
	}
	k.lastRefetch = time.Now()

	if err := k.fetch(ctx); err != nil {
		return nil, fmt.Errorf("refetching jwks: %w", err)
	}
	if pub, ok := k.keys[kid]; ok {
		return pub, nil
	}

	return nil, fmt.Errorf("%q: %w", kid, errUnknownKID)
}

// prime performs the first fetch, used once by New to fail fast if the
// authorization server is unreachable or its jwks is malformed.
func (k *keySet) prime(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.fetch(ctx)
}

// fetch replaces the cached key set. Callers hold k.mu.
func (k *keySet) fetch(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, http.NoBody)
	if err != nil {
		return fmt.Errorf("building jwks request: %w", err)
	}

	resp, err := k.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", k.url, err)
	}
	// The body is consumed below; a failure to close it is not actionable.
	defer resp.Body.Close() //nolint:errcheck // see above

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: %d: %w", k.url, resp.StatusCode, errUnexpectedStatus)
	}

	var set jwkSet
	if decodeErr := json.NewDecoder(resp.Body).Decode(&set); decodeErr != nil {
		return fmt.Errorf("decoding jwks from %s: %w", k.url, decodeErr)
	}

	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, candidate := range set.Keys {
		if candidate.Kid == "" {
			continue // unusable: nothing to index it by
		}
		if pub, parseErr := parseKey(&candidate); parseErr == nil {
			keys[candidate.Kid] = pub
		}
	}

	k.keys = keys

	return nil
}

func parseKey(k *jwk) (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAKey(k)
	case "EC":
		return parseECKey(k)
	default:
		return nil, fmt.Errorf("%q: %w", k.Kty, errUnsupportedKeyType)
	}
}

func parseRSAKey(k *jwk) (*rsa.PublicKey, error) {
	n, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decoding rsa modulus: %w", err)
	}
	e, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decoding rsa exponent: %w", err)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(n),
		E: int(new(big.Int).SetBytes(e).Int64()),
	}, nil
}

// parseECKey supports P-256 only, because ES256 is the only EC algorithm this
// server accepts (see claims.go's allowedAlgs) -- a key for a curve it will
// never be asked to verify against is not worth the extra branches.
func parseECKey(k *jwk) (*ecdsa.PublicKey, error) {
	if k.Crv != "P-256" {
		return nil, fmt.Errorf("%q: %w", k.Crv, errUnsupportedKeyType)
	}
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("decoding ec x: %w", err)
	}
	y, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decoding ec y: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}
