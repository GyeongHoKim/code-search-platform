// Package httpauth guards the http transport by verifying that a request's
// bearer token was issued, for this server, by the configured OAuth 2.1
// authorization server.
//
// It is a leaf: it takes the issuer and audience as arguments rather than
// reading configuration, so it can be tested without an environment. New does
// one network round trip -- discovering (or being told) where to fetch
// signing keys, and fetching them once to prime the cache -- so a
// misconfigured or unreachable authorization server fails the process at
// startup rather than 500ing every request that ever reaches it.
package httpauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// errNoJWKSURI means the authorization server's metadata did not advertise
// where to fetch its signing keys.
var errNoJWKSURI = errors.New("authorization server metadata has no jwks_uri")

// defaultHTTPTimeout bounds discovery and JWKS requests when Config.HTTPClient
// is unset.
const defaultHTTPTimeout = 10 * time.Second

// Config is what New needs to build a Guard.
type Config struct {
	// HTTPClient is used for discovery and JWKS fetches. Nil uses a client
	// bounded by defaultHTTPTimeout.
	HTTPClient *http.Client
	// IssuerURL is the OAuth 2.1 authorization server whose tokens this Guard
	// accepts.
	IssuerURL string
	// Audience is this server's own resource identifier: the value a token's
	// aud claim must contain, and the "resource" this Guard advertises at
	// /.well-known/oauth-protected-resource (RFC 9728).
	Audience string
	// JWKSURL overrides authorization-server-metadata discovery of the
	// signing-key endpoint. Empty means discover it from IssuerURL.
	JWKSURL string
	// ClockSkew tolerates a small difference between this server's clock and
	// the authorization server's when checking a token's expiration.
	ClockSkew time.Duration
}

// Guard verifies bearer tokens and answers this server's own RFC 9728
// protected resource metadata.
type Guard struct {
	middleware func(http.Handler) http.Handler
	metadata   http.Handler
}

// RequireToken wraps next so that only requests carrying a token this Guard
// accepts reach it.
func (g *Guard) RequireToken(next http.Handler) http.Handler {
	return g.middleware(next)
}

// ProtectedResourceMetadataHandler serves RFC 9728 metadata describing this
// resource server, meant for the /.well-known/oauth-protected-resource path.
func (g *Guard) ProtectedResourceMetadataHandler() http.Handler {
	return g.metadata
}

// New discovers cfg.IssuerURL's signing keys (or fetches them directly from
// cfg.JWKSURL, if set), primes the key cache with one fetch, and returns a
// Guard that verifies tokens against them.
func New(ctx context.Context, cfg Config) (*Guard, error) {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		meta, err := auth.GetAuthServerMetadata(ctx, cfg.IssuerURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("discovering %s: %w", cfg.IssuerURL, err)
		}
		if meta.JWKSURI == "" {
			return nil, fmt.Errorf("%s: %w", cfg.IssuerURL, errNoJWKSURI)
		}
		jwksURL = meta.JWKSURI
	}

	keys := newKeySet(jwksURL, httpClient)
	if err := keys.prime(ctx); err != nil {
		return nil, fmt.Errorf("fetching signing keys from %s: %w", jwksURL, err)
	}

	resourceMetadataURL, err := wellKnownURL(cfg.Audience)
	if err != nil {
		return nil, fmt.Errorf("building resource metadata url: %w", err)
	}

	middleware := auth.RequireBearerToken(
		verifyJWT(keys, cfg.IssuerURL, cfg.Audience, cfg.ClockSkew),
		&auth.RequireBearerTokenOptions{
			ResourceMetadataURL: resourceMetadataURL,
			ClockSkew:           cfg.ClockSkew,
		},
	)
	metadata := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.Audience,
		AuthorizationServers:   []string{cfg.IssuerURL},
		BearerMethodsSupported: []string{"header"},
	})

	return &Guard{middleware: middleware, metadata: metadata}, nil
}

// wellKnownURL builds the /.well-known/oauth-protected-resource URL this
// server answers at, from its own resource identifier. This server exposes
// exactly one resource per instance, so the metadata lives at the fixed
// well-known path rather than one keyed by the resource's own path segment --
// RFC 9728 §3.1 permits either.
func wellKnownURL(audience string) (string, error) {
	parsed, err := url.Parse(audience)
	if err != nil {
		return "", fmt.Errorf("%q: %w", audience, err)
	}
	parsed.Path, parsed.RawQuery, parsed.Fragment = "/.well-known/oauth-protected-resource", "", ""

	return parsed.String(), nil
}
