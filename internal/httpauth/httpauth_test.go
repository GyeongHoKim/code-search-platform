package httpauth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/httpauth"
)

const testAudience = "https://mcp.example.com/mcp"

// reached reports whether the guarded handler ran, and answers 204 when it did.
func reached(ran *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*ran = true
		w.WriteHeader(http.StatusNoContent)
	})
}

// idp is a fake authorization server: discovery plus a JWKS endpoint, backed
// by one RSA key pair a test can sign tokens with.
type idp struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string
}

func newIDP(t *testing.T) *idp {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating rsa key: %v", err)
	}

	i := &idp{key: key, kid: "test-key"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck // test server, nothing to react to
			"issuer":                           i.server.URL,
			"authorization_endpoint":           i.server.URL + "/authorize",
			"token_endpoint":                   i.server.URL + "/token",
			"jwks_uri":                         i.server.URL + "/keys",
			"response_types_supported":         []string{"code"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck // test server, nothing to react to
			"keys": []map[string]string{{
				"kty": "RSA",
				"kid": i.kid,
				"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})

	i.server = httptest.NewServer(mux)
	t.Cleanup(i.server.Close)

	return i
}

// token mints a signed JWT. override replaces individual default claims (a
// valid iss/aud/exp/sub for i and testAudience); pass nil to accept every
// default.
func (i *idp) token(t *testing.T, override jwt.MapClaims) string {
	t.Helper()

	claims := jwt.MapClaims{
		"iss": i.server.URL,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "test-user",
	}
	maps.Copy(claims, override)

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = i.kid

	signed, err := tok.SignedString(i.key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	return signed
}

func newGuard(t *testing.T, i *idp) *httpauth.Guard {
	t.Helper()

	guard, err := httpauth.New(t.Context(), httpauth.Config{
		IssuerURL: i.server.URL,
		Audience:  testAudience,
	})
	if err != nil {
		t.Fatalf("httpauth.New() error = %v, want nil", err)
	}

	return guard
}

func TestGuardAcceptsAValidToken(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i)

	var ran bool
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+i.token(t, nil))
	rec := httptest.NewRecorder()

	guard.RequireToken(reached(&ran)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if !ran {
		t.Error("the guarded handler did not run")
	}
}

// Authentik's issuer is "https://host/application/o/<slug>/" and its tokens
// carry that trailing slash in iss, while config normalises IssuerURL by
// trimming it. The comparison must not turn that into a rejection.
func TestGuardAcceptsAnIssuerThatDiffersOnlyByATrailingSlash(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i)

	var ran bool
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+i.token(t, jwt.MapClaims{"iss": i.server.URL + "/"}))
	rec := httptest.NewRecorder()

	guard.RequireToken(reached(&ran)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if !ran {
		t.Error("the guarded handler did not run")
	}
}

func TestGuardRejects(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i)

	// none-alg confusion attempt: sign with HS256, keyed on the audience --
	// a value an attacker who only knows the resource identifier could
	// guess -- and reuse a real kid to look legitimate.
	confused := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": i.server.URL,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	confused.Header["kid"] = i.kid
	confusedSigned, err := confused.SignedString([]byte(testAudience))
	if err != nil {
		t.Fatalf("signing confused token: %v", err)
	}

	cases := map[string]string{ //nolint:gosec // test fixtures, not real credentials
		"no authorization header": "",
		"the wrong scheme":        "Token " + i.token(t, nil),
		"a malformed token":       "Bearer not-a-jwt",
		"wrong issuer":            "Bearer " + i.token(t, jwt.MapClaims{"iss": "https://someone-else.example.com"}),
		"wrong audience":          "Bearer " + i.token(t, jwt.MapClaims{"aud": "https://someone-else.example.com/mcp"}),
		"expired":                 "Bearer " + i.token(t, jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}),
		"unknown kid": "Bearer " + func() string {
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"iss": i.server.URL, "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix(),
			})
			tok.Header["kid"] = "does-not-exist"
			signed, signErr := tok.SignedString(i.key)
			if signErr != nil {
				t.Fatalf("signing unknown-kid token: %v", signErr)
			}

			return signed
		}(),
		"disallowed algorithm (alg confusion)": "Bearer " + confusedSigned,
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var ran bool
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()

			guard.RequireToken(reached(&ran)).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if ran {
				t.Error("the guarded handler ran")
			}
		})
	}
}

func TestGuardRejectionCarriesTheResourceMetadataURL(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	rec := httptest.NewRecorder()

	guard.RequireToken(reached(new(bool))).ServeHTTP(rec, req)

	const want = `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, want) {
		t.Errorf("WWW-Authenticate = %q, want it to contain %q", got, want)
	}
}

func TestGuardServesProtectedResourceMetadata(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.well-known/oauth-protected-resource", http.NoBody)
	rec := httptest.NewRecorder()

	guard.ProtectedResourceMetadataHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var metadata struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&metadata); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if metadata.Resource != testAudience {
		t.Errorf("Resource = %q, want %q", metadata.Resource, testAudience)
	}
	if len(metadata.AuthorizationServers) != 1 || metadata.AuthorizationServers[0] != i.server.URL {
		t.Errorf("AuthorizationServers = %v, want [%q]", metadata.AuthorizationServers, i.server.URL)
	}
}

func TestGuardReportsAJWKSOutageAsAServerError(t *testing.T) {
	t.Parallel()

	i := newIDP(t)
	guard := newGuard(t, i) // primes the key cache while the idp is still up

	i.server.Close() // now the idp cannot answer a refetch

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": i.server.URL, "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "does-not-exist" // forces a refetch attempt
	signed, err := tok.SignedString(i.key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()

	var ran bool
	guard.RequireToken(reached(&ran)).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if ran {
		t.Error("the guarded handler ran")
	}
}
