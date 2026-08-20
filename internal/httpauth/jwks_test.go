package httpauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func rsaJWK(t *testing.T) (jwk, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating rsa key: %v", err)
	}

	return jwk{
		Kty: "RSA",
		Kid: "rsa-1",
		N:   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}, key
}

func ecJWK(t *testing.T) (jwk, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ec key: %v", err)
	}

	// Uncompressed SEC1 point: 0x04 || X (32 bytes) || Y (32 bytes) for P-256.
	// Read through this rather than key.PublicKey.X/Y directly, which
	// crypto/ecdsa now discourages touching for anything but the value it
	// already holds.
	point, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("encoding ec public key: %v", err)
	}

	return jwk{
		Kty: "EC",
		Kid: "ec-1",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(point[1:33]),
		Y:   base64.RawURLEncoding.EncodeToString(point[33:65]),
	}, key
}

// fakeJWKS is a JWKS endpoint that counts how many times it was asked to
// serve its key set.
type fakeJWKS struct {
	server   *httptest.Server
	requests *int
}

// jwksServer serves set as JSON.
func jwksServer(t *testing.T, set jwkSet) fakeJWKS {
	t.Helper()

	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set) //nolint:errcheck // test server, nothing to react to
	}))
	t.Cleanup(server.Close)

	return fakeJWKS{server: server, requests: &count}
}

func TestKeySetParsesRSAAndVerifiesASignedToken(t *testing.T) {
	t.Parallel()

	jk, priv := rsaJWK(t)
	fake := jwksServer(t, jwkSet{Keys: []jwk{jk}})

	keys := newKeySet(fake.server.URL, fake.server.Client())
	if err := keys.prime(t.Context()); err != nil {
		t.Fatalf("prime() error = %v, want nil", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{})
	token.Header["kid"] = "rsa-1"
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	if _, verifyErr := jwt.Parse(signed, func(_ *jwt.Token) (any, error) {
		return keys.key(context.Background(), "rsa-1")
	}); verifyErr != nil {
		t.Errorf("verifying rsa-signed token against the cached key: %v", verifyErr)
	}
}

func TestKeySetParsesECAndVerifiesASignedToken(t *testing.T) {
	t.Parallel()

	jk, priv := ecJWK(t)
	fake := jwksServer(t, jwkSet{Keys: []jwk{jk}})

	keys := newKeySet(fake.server.URL, fake.server.Client())
	if err := keys.prime(t.Context()); err != nil {
		t.Fatalf("prime() error = %v, want nil", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{})
	token.Header["kid"] = "ec-1"
	signed, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	if _, verifyErr := jwt.Parse(signed, func(_ *jwt.Token) (any, error) {
		return keys.key(context.Background(), "ec-1")
	}); verifyErr != nil {
		t.Errorf("verifying ec-signed token against the cached key: %v", verifyErr)
	}
}

func TestKeySetSkipsUnsupportedKeysWithoutFailingTheWholeSet(t *testing.T) {
	t.Parallel()

	rsaKey, _ := rsaJWK(t)
	fake := jwksServer(t, jwkSet{Keys: []jwk{
		{Kty: "oct", Kid: "symmetric-1"},         // unsupported kty
		{Kty: "EC", Kid: "p521-1", Crv: "P-521"}, // unsupported curve
		rsaKey,
	}})

	keys := newKeySet(fake.server.URL, fake.server.Client())
	if err := keys.prime(t.Context()); err != nil {
		t.Fatalf("prime() error = %v, want nil", err)
	}

	if _, err := keys.key(t.Context(), "rsa-1"); err != nil {
		t.Errorf("key(%q) error = %v, want nil", "rsa-1", err)
	}
	if _, err := keys.key(t.Context(), "symmetric-1"); err == nil {
		t.Errorf("key(%q) error = nil, want an error", "symmetric-1")
	}
}

func TestKeySetCacheHitAvoidsASecondFetch(t *testing.T) {
	t.Parallel()

	jk, _ := rsaJWK(t)
	fake := jwksServer(t, jwkSet{Keys: []jwk{jk}})

	keys := newKeySet(fake.server.URL, fake.server.Client())
	if err := keys.prime(t.Context()); err != nil {
		t.Fatalf("prime() error = %v, want nil", err)
	}
	if *fake.requests != 1 {
		t.Fatalf("requests after priming fetch = %d, want 1", *fake.requests)
	}

	if _, err := keys.key(t.Context(), "rsa-1"); err != nil {
		t.Fatalf("key() error = %v, want nil", err)
	}
	if *fake.requests != 1 {
		t.Errorf("requests after a cache hit = %d, want 1", *fake.requests)
	}
}

func TestKeySetRefetchesOnceOnAnUnknownKID(t *testing.T) {
	t.Parallel()

	jk, _ := rsaJWK(t)
	fake := jwksServer(t, jwkSet{Keys: []jwk{jk}})

	keys := newKeySet(fake.server.URL, fake.server.Client())
	if err := keys.prime(t.Context()); err != nil {
		t.Fatalf("prime() error = %v, want nil", err)
	}
	if *fake.requests != 1 {
		t.Fatalf("requests after priming fetch = %d, want 1", *fake.requests)
	}

	// Unknown kid: triggers exactly one refetch (the key still won't be
	// found, since the server always answers the same set).
	if _, err := keys.key(t.Context(), "does-not-exist"); err == nil {
		t.Fatal("key() error = nil, want an error for an unknown kid")
	}
	if *fake.requests != 2 {
		t.Errorf("requests after one unknown-kid lookup = %d, want 2", *fake.requests)
	}

	// A second unknown-kid lookup right after the first must not trigger a
	// second refetch: jwksMinRefetchInterval has not elapsed.
	if _, err := keys.key(t.Context(), "still-does-not-exist"); err == nil {
		t.Fatal("key() error = nil, want an error for an unknown kid")
	}
	if *fake.requests != 2 {
		t.Errorf("requests after a debounced unknown-kid lookup = %d, want 2", *fake.requests)
	}
}

func TestKeySetFetchRejectsMalformedJWKS(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json")) //nolint:errcheck // test server, nothing to react to
	}))
	t.Cleanup(server.Close)

	keys := newKeySet(server.URL, server.Client())
	if err := keys.prime(t.Context()); err == nil {
		t.Error("prime() error = nil, want an error for malformed jwks")
	}
}
