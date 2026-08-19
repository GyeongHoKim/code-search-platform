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
	// ErrMissingAuthToken means the http transport was selected without a token
	// to check callers against.
	ErrMissingAuthToken = errors.New("auth token is required for the http transport")
)

// Config is everything the server needs to start, already validated.
type Config struct {
	// ZoektURL is the base URL of a zoekt-webserver started with -rpc.
	ZoektURL string
	// Transport is how this process speaks MCP.
	Transport Transport
	// Addr is the listen address, used only by the http transport.
	Addr string
	// AuthTokens are the bearer tokens the http transport accepts. Several are
	// live at once so that one can be rotated without a window in which every
	// caller is broken.
	AuthTokens []string
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
	// After loadTransport: whether a token is required depends on which
	// transport was selected.
	if err := loadAuthTokens(lookup, cfg); err != nil {
		return nil, err
	}
	if err := loadNumbers(lookup, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadZoektURL(lookup Lookup, cfg *Config) error {
	raw, ok := lookup(EnvPrefix + "UPSTREAM_URL")
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return fmt.Errorf("%s: %w", EnvPrefix+"UPSTREAM_URL", ErrMissingZoektURL)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", EnvPrefix+"UPSTREAM_URL", raw, ErrInvalidZoektURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s=%q: %w", EnvPrefix+"UPSTREAM_URL", raw, ErrInvalidZoektURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s=%q: %w", EnvPrefix+"UPSTREAM_URL", raw, ErrInvalidZoektURL)
	}

	cfg.ZoektURL = strings.TrimSuffix(raw, "/")

	return nil
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

// loadAuthTokens reads the comma separated list of bearer tokens the http
// transport accepts.
//
// The http transport is the one that puts this server on a network, and this
// server is the only front door to an index that has no authentication of its
// own. Starting without a token there would expose the whole corpus, so it is
// a startup failure rather than a warning. Stdio has no network to guard: the
// client spawned this process, so it is already whoever owns it.
func loadAuthTokens(lookup Lookup, cfg *Config) error {
	raw, _ := lookup(EnvPrefix + "AUTH_TOKEN")

	for token := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(token); trimmed != "" {
			cfg.AuthTokens = append(cfg.AuthTokens, trimmed)
		}
	}

	if cfg.Transport == TransportHTTP && len(cfg.AuthTokens) == 0 {
		return fmt.Errorf("%s: %w", EnvPrefix+"AUTH_TOKEN", ErrMissingAuthToken)
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
