package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/config"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/httpauth"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/mcpserver"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

const testAudience = "https://mcp.example.com/mcp"

// idle is a Searcher that is never reached: these tests end at the transport.
type idle struct{}

func (idle) Search(context.Context, string, zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	return &zoekt.SearchResult{}, nil
}

func (idle) List(context.Context, string) (*zoekt.RepoList, error) {
	return &zoekt.RepoList{}, nil
}

// bearer sends every request with the given Authorization header, or with none
// when the header is empty.
type bearer struct {
	header string
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.header != "" {
		req.Header.Set("Authorization", b.header)
	}

	return http.DefaultTransport.RoundTrip(req)
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

// token mints a signed JWT with a valid iss/aud/exp for i and testAudience.
func (i *idp) token(t *testing.T) string {
	t.Helper()

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": i.server.URL,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"sub": "test-user",
	})
	tok.Header["kid"] = i.kid

	signed, err := tok.SignedString(i.key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}

	return signed
}

// serveGuarded runs the real handler wiring, backed by a real Guard verifying
// against a fake authorization server, over a test server.
func serveGuarded(t *testing.T) (string, *idp) {
	t.Helper()

	i := newIDP(t)

	guard, err := httpauth.New(t.Context(), httpauth.Config{
		IssuerURL: i.server.URL,
		Audience:  testAudience,
	})
	if err != nil {
		t.Fatalf("httpauth.New() error = %v, want nil", err)
	}

	cfg := &config.Config{
		ZoektURL:     "http://127.0.0.1:6070",
		Transport:    config.TransportHTTP,
		Addr:         "127.0.0.1:0",
		Timeout:      5 * time.Second,
		MaxResults:   50,
		ContextLines: 3,
	}

	server := httptest.NewServer(httpHandler(mcpserver.New(cfg, idle{}), guard))
	t.Cleanup(server.Close)

	return server.URL, i
}

// connectWith opens a session through a client that sends header.
func connectWith(t *testing.T, url, header string) (*mcp.ClientSession, error) {
	t.Helper()

	transport := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearer{header: header}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	return client.Connect(t.Context(), transport, nil)
}

func TestHTTPHandlerRejectsAnUnauthenticatedClient(t *testing.T) {
	t.Parallel()

	url, _ := serveGuarded(t)

	for name, header := range map[string]string{ //nolint:gosec // test fixtures, not real credentials
		"no header":      "",
		"a wrong token":  "Bearer not-a-jwt",
		"a wrong scheme": "Token something",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session, err := connectWith(t, url, header)
			if err == nil {
				t.Error("Connect() error = nil, want a rejection")

				if closeErr := session.Close(); closeErr != nil {
					t.Errorf("closing session: %v", closeErr)
				}
			}
		})
	}
}

func TestHTTPHandlerServesAnAuthenticatedClient(t *testing.T) {
	t.Parallel()

	url, i := serveGuarded(t)

	session, err := connectWith(t, url, "Bearer "+i.token(t))
	if err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("closing session: %v", closeErr)
		}
	})

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v, want nil", err)
	}
	if len(tools.Tools) == 0 {
		t.Error("ListTools() returned no tools")
	}
}
