// Package config turns the process environment into a validated Config.
//
// The environment is read through an injected lookup function rather than
// os.Getenv directly, so tests describe an environment as a map instead of
// mutating the one the test binary runs in.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Transport selects how the MCP server speaks to its client.
type Transport string

const (
	// TransportStdio serves JSON-RPC on stdin and stdout, for a client that
	// spawns the binary itself.
	TransportStdio Transport = "stdio"
	// TransportHTTP serves Streamable HTTP, for a client reaching a server
	// that already runs somewhere.
	TransportHTTP Transport = "http"
)

// EnvPrefix is prepended to every variable this package reads.
const EnvPrefix = "ZOEKT_MCP_"

// Defaults. Every one of these is safe for a single developer running the
// server locally against a Zoekt on the loopback interface.
const (
	defaultTransport    = TransportStdio
	defaultAddr         = "127.0.0.1:8080"
	defaultTimeout      = 30 * time.Second
	defaultMaxResults   = 50
	defaultContextLines = 3
)

// Ceilings. A model that asks for ten thousand results does not want ten
// thousand results; it wants the first page and has no way to say so.
const (
	maxResultsCeiling   = 500
	contextLinesCeiling = 50
)

// Errors this package returns. Callers match them with errors.Is.
var (
	// ErrMissingZoektURL means the one required variable was not set.
	ErrMissingZoektURL = errors.New("zoekt url is required")
	// ErrInvalidZoektURL means the value was set but is not a usable http(s) URL.
	ErrInvalidZoektURL = errors.New("zoekt url is not a valid http(s) url")
	// ErrUnknownTransport means the transport was neither stdio nor http.
	ErrUnknownTransport = errors.New("unknown transport")
	// ErrNotAnInteger means a numeric variable did not hold a number.
	ErrNotAnInteger = errors.New("value is not an integer")
	// ErrOutOfRange means a numeric variable held a number outside its bounds.
	ErrOutOfRange = errors.New("value is out of range")
	// ErrInvalidDuration means a duration variable did not parse.
	ErrInvalidDuration = errors.New("value is not a duration")
	// ErrMissingAddr means the http transport was selected without a listen address.
	ErrMissingAddr = errors.New("listen address is required for the http transport")
	// ErrMissingOIDCIssuerURL means the http transport was selected without an
	// OAuth 2.1 authorization server to verify bearer tokens against.
	ErrMissingOIDCIssuerURL = errors.New("oidc issuer url is required for the http transport")
	// ErrInvalidOIDCIssuerURL means the value was set but is not a usable
	// http(s) URL.
	ErrInvalidOIDCIssuerURL = errors.New("oidc issuer url is not a valid http(s) url")
	// ErrMissingOIDCAudience means the http transport was selected without this
	// server's own resource identifier.
	ErrMissingOIDCAudience = errors.New("oidc audience is required for the http transport")
	// ErrInvalidOIDCAudience means the value was set but is not a usable
	// http(s) URL.
	ErrInvalidOIDCAudience = errors.New("oidc audience is not a valid http(s) url")
	// ErrInvalidOIDCJWKSURL means the value was set but is not a usable http(s)
	// URL.
	ErrInvalidOIDCJWKSURL = errors.New("oidc jwks url is not a valid http(s) url")
)

// Config is everything the server needs to start, already validated.
type Config struct {
	// ZoektURL is the base URL of a zoekt-webserver started with -rpc.
	ZoektURL string
	// Transport is how this process speaks MCP.
	Transport Transport
	// Addr is the listen address, used only by the http transport.
	Addr string
	// OIDCIssuerURL is the OAuth 2.1 authorization server whose tokens the
	// http transport accepts.
	OIDCIssuerURL string
	// OIDCAudience is this server's own resource identifier: the value a
	// token's aud claim must contain, and the "resource" this server
	// advertises at /.well-known/oauth-protected-resource (RFC 9728).
	OIDCAudience string
	// OIDCJWKSURL overrides authorization-server-metadata discovery of the
	// signing-key endpoint. Empty means discover it from OIDCIssuerURL.
	OIDCJWKSURL string
	// Timeout bounds a single request to Zoekt.
	Timeout time.Duration
	// MaxResults caps how many file matches one search returns.
	MaxResults int
	// ContextLines is how many lines surround each match in a result.
	ContextLines int
}

// Lookup reports the value of an environment variable and whether it was set,
// matching the shape of os.LookupEnv.
type Lookup func(key string) (string, bool)

// Load reads the environment through lookup and returns a validated Config.
func Load(lookup Lookup) (*Config, error) {
	cfg := &Config{
		Transport:    defaultTransport,
		Addr:         defaultAddr,
		Timeout:      defaultTimeout,
		MaxResults:   defaultMaxResults,
		ContextLines: defaultContextLines,
	}

	if err := loadZoektURL(lookup, cfg); err != nil {
		return nil, err
	}
	if err := loadTransport(lookup, cfg); err != nil {
		return nil, err
	}
	// After loadTransport: whether an authorization server is required depends
	// on which transport was selected.
	if err := loadOIDC(lookup, cfg); err != nil {
		return nil, err
	}
	if err := loadNumbers(lookup, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validHTTPURL reports whether raw parses as an absolute http(s) URL.
func validHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func loadZoektURL(lookup Lookup, cfg *Config) error {
	raw, ok := lookup(EnvPrefix + "UPSTREAM_URL")
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return fmt.Errorf("%s: %w", EnvPrefix+"UPSTREAM_URL", ErrMissingZoektURL)
	}
	if !validHTTPURL(raw) {
		return fmt.Errorf("%s=%q: %w", EnvPrefix+"UPSTREAM_URL", raw, ErrInvalidZoektURL)
	}

	cfg.ZoektURL = strings.TrimSuffix(raw, "/")

	return nil
}

// loadOIDC reads the OAuth 2.1 authorization server and this server's own
// resource identifier, used by the http transport to verify bearer tokens.
//
// The http transport is the one that puts this server on a network, and this
// server is the only front door to an index that has no authentication of its
// own. Starting without an issuer and an audience there would expose the
// whole corpus, so it is a startup failure rather than a warning. Stdio has
// no network to guard: the client spawned this process, so it is already
// whoever owns it.
func loadOIDC(lookup Lookup, cfg *Config) error {
	issuer, err := loadOIDCURL(lookup, cfg, EnvPrefix+"OIDC_ISSUER_URL", true, ErrMissingOIDCIssuerURL, ErrInvalidOIDCIssuerURL)
	if err != nil {
		return err
	}
	cfg.OIDCIssuerURL = strings.TrimSuffix(issuer, "/")

	audience, err := loadOIDCURL(lookup, cfg, EnvPrefix+"OIDC_AUDIENCE", true, ErrMissingOIDCAudience, ErrInvalidOIDCAudience)
	if err != nil {
		return err
	}
	cfg.OIDCAudience = audience

	jwksURL, err := loadOIDCURL(lookup, cfg, EnvPrefix+"OIDC_JWKS_URL", false, nil, ErrInvalidOIDCJWKSURL)
	if err != nil {
		return err
	}
	cfg.OIDCJWKSURL = jwksURL

	return nil
}

// loadOIDCURL reads key, trimmed, validating it as an http(s) URL when
// present. If required, it additionally fails with missingErr when the http
// transport was selected and the variable is unset or blank.
func loadOIDCURL(lookup Lookup, cfg *Config, key string, required bool, missingErr, invalidErr error) (string, error) {
	raw, ok := lookup(key)
	raw = strings.TrimSpace(raw)

	if required && cfg.Transport == TransportHTTP && (!ok || raw == "") {
		return "", fmt.Errorf("%s: %w", key, missingErr)
	}
	if raw == "" {
		return "", nil
	}
	if !validHTTPURL(raw) {
		return "", fmt.Errorf("%s=%q: %w", key, raw, invalidErr)
	}

	return raw, nil
}

func loadTransport(lookup Lookup, cfg *Config) error {
	if raw, ok := lookup(EnvPrefix + "TRANSPORT"); ok && strings.TrimSpace(raw) != "" {
		switch Transport(strings.ToLower(strings.TrimSpace(raw))) {
		case TransportStdio:
			cfg.Transport = TransportStdio
		case TransportHTTP:
			cfg.Transport = TransportHTTP
		default:
			return fmt.Errorf("%s=%q: %w", EnvPrefix+"TRANSPORT", raw, ErrUnknownTransport)
		}
	}

	if raw, ok := lookup(EnvPrefix + "ADDR"); ok && strings.TrimSpace(raw) != "" {
		cfg.Addr = strings.TrimSpace(raw)
	}
	if cfg.Transport == TransportHTTP && cfg.Addr == "" {
		return fmt.Errorf("%s: %w", EnvPrefix+"ADDR", ErrMissingAddr)
	}

	return nil
}

func loadNumbers(lookup Lookup, cfg *Config) error {
	timeout, err := lookupDuration(lookup, EnvPrefix+"TIMEOUT", cfg.Timeout)
	if err != nil {
		return err
	}
	cfg.Timeout = timeout

	maxResults, err := lookupBoundedInt(lookup, EnvPrefix+"MAX_RESULTS", cfg.MaxResults, 1, maxResultsCeiling)
	if err != nil {
		return err
	}
	cfg.MaxResults = maxResults

	contextLines, err := lookupBoundedInt(lookup, EnvPrefix+"CONTEXT_LINES", cfg.ContextLines, 0, contextLinesCeiling)
	if err != nil {
		return err
	}
	cfg.ContextLines = contextLines

	return nil
}

func lookupDuration(lookup Lookup, key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := lookup(key)
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", key, raw, ErrInvalidDuration)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s=%q: %w", key, raw, ErrOutOfRange)
	}

	return parsed, nil
}

func lookupBoundedInt(lookup Lookup, key string, fallback, low, high int) (int, error) {
	raw, ok := lookup(key)
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", key, raw, ErrNotAnInteger)
	}
	if parsed < low || parsed > high {
		return 0, fmt.Errorf("%s=%q (want %d..%d): %w", key, raw, low, high, ErrOutOfRange)
	}

	return parsed, nil
}
