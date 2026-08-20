package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/config"
)

// env turns a map into a config.Lookup, so a test states an environment
// instead of mutating the one it runs in.
func env(vars map[string]string) config.Lookup {
	return func(key string) (string, bool) {
		v, ok := vars[key]

		return v, ok
	}
}

const (
	zoektURL     = "http://zoekt:6070"
	oidcIssuer   = "https://dex.example.com"
	oidcAudience = "https://search.example.com/mcp"
)

// httpVars is the minimal set of variables that satisfy the http transport's
// requirements, for tests whose focus is something else.
func httpVars() map[string]string {
	return map[string]string{
		config.EnvPrefix + "UPSTREAM_URL":    zoektURL,
		config.EnvPrefix + "TRANSPORT":       "http",
		config.EnvPrefix + "OIDC_ISSUER_URL": oidcIssuer,
		config.EnvPrefix + "OIDC_AUDIENCE":   oidcAudience,
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "UPSTREAM_URL": zoektURL,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Transport != config.TransportStdio {
		t.Errorf("Transport = %q, want %q", cfg.Transport, config.TransportStdio)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.MaxResults != 50 {
		t.Errorf("MaxResults = %d, want 50", cfg.MaxResults)
	}
	if cfg.ContextLines != 3 {
		t.Errorf("ContextLines = %d, want 3", cfg.ContextLines)
	}
}

func TestLoadTrimsTrailingSlashFromZoektURL(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "UPSTREAM_URL": zoektURL + "/",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Every request path this client builds starts with a slash, so a base
	// URL that also ends in one would produce a double slash.
	if cfg.ZoektURL != zoektURL {
		t.Errorf("ZoektURL = %q, want %q", cfg.ZoektURL, zoektURL)
	}
}

func TestLoadRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		vars map[string]string
		want error
	}{
		"missing zoekt url": {
			vars: map[string]string{},
			want: config.ErrMissingZoektURL,
		},
		"blank zoekt url": {
			vars: map[string]string{config.EnvPrefix + "UPSTREAM_URL": "   "},
			want: config.ErrMissingZoektURL,
		},
		"zoekt url without scheme": {
			vars: map[string]string{config.EnvPrefix + "UPSTREAM_URL": "zoekt:6070"},
			want: config.ErrInvalidZoektURL,
		},
		"zoekt url with the wrong scheme": {
			vars: map[string]string{config.EnvPrefix + "UPSTREAM_URL": "ftp://zoekt:6070"},
			want: config.ErrInvalidZoektURL,
		},
		"unknown transport": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "TRANSPORT":    "grpc",
			},
			want: config.ErrUnknownTransport,
		},
		"http transport without an oidc issuer": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":  zoektURL,
				config.EnvPrefix + "TRANSPORT":     "http",
				config.EnvPrefix + "OIDC_AUDIENCE": oidcAudience,
			},
			want: config.ErrMissingOIDCIssuerURL,
		},
		"http transport with a blank oidc issuer": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":    zoektURL,
				config.EnvPrefix + "TRANSPORT":       "http",
				config.EnvPrefix + "OIDC_ISSUER_URL": "   ",
				config.EnvPrefix + "OIDC_AUDIENCE":   oidcAudience,
			},
			want: config.ErrMissingOIDCIssuerURL,
		},
		"http transport without an oidc audience": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":    zoektURL,
				config.EnvPrefix + "TRANSPORT":       "http",
				config.EnvPrefix + "OIDC_ISSUER_URL": oidcIssuer,
			},
			want: config.ErrMissingOIDCAudience,
		},
		"oidc issuer url without a scheme": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":    zoektURL,
				config.EnvPrefix + "OIDC_ISSUER_URL": "dex:5556",
			},
			want: config.ErrInvalidOIDCIssuerURL,
		},
		"oidc audience without a scheme": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":  zoektURL,
				config.EnvPrefix + "OIDC_AUDIENCE": "search.example.com/mcp",
			},
			want: config.ErrInvalidOIDCAudience,
		},
		"oidc jwks url without a scheme": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL":  zoektURL,
				config.EnvPrefix + "OIDC_JWKS_URL": "dex:5556/keys",
			},
			want: config.ErrInvalidOIDCJWKSURL,
		},
		"non numeric max results": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "MAX_RESULTS":  "many",
			},
			want: config.ErrNotAnInteger,
		},
		"max results above the ceiling": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "MAX_RESULTS":  "10000",
			},
			want: config.ErrOutOfRange,
		},
		"zero max results": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "MAX_RESULTS":  "0",
			},
			want: config.ErrOutOfRange,
		},
		"negative timeout": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "TIMEOUT":      "-5s",
			},
			want: config.ErrOutOfRange,
		},
		"unparseable timeout": {
			vars: map[string]string{
				config.EnvPrefix + "UPSTREAM_URL": zoektURL,
				config.EnvPrefix + "TIMEOUT":      "soon",
			},
			want: config.ErrInvalidDuration,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(tc.vars))
			if !errors.Is(err, tc.want) {
				t.Errorf("Load() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestLoadAcceptsZeroContextLines(t *testing.T) {
	t.Parallel()

	// Zero is a meaningful answer here -- matched lines with no surroundings --
	// so it must not be confused with "unset, use the default".
	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "UPSTREAM_URL":  zoektURL,
		config.EnvPrefix + "CONTEXT_LINES": "0",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.ContextLines != 0 {
		t.Errorf("ContextLines = %d, want 0", cfg.ContextLines)
	}
}

func TestLoadHTTPTransport(t *testing.T) {
	t.Parallel()

	vars := httpVars()
	vars[config.EnvPrefix+"TRANSPORT"] = "HTTP"
	vars[config.EnvPrefix+"ADDR"] = ":9090"
	vars[config.EnvPrefix+"OIDC_JWKS_URL"] = oidcIssuer + "/keys"

	cfg, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Transport != config.TransportHTTP {
		t.Errorf("Transport = %q, want %q", cfg.Transport, config.TransportHTTP)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9090")
	}
	if cfg.OIDCIssuerURL != oidcIssuer {
		t.Errorf("OIDCIssuerURL = %q, want %q", cfg.OIDCIssuerURL, oidcIssuer)
	}
	if cfg.OIDCAudience != oidcAudience {
		t.Errorf("OIDCAudience = %q, want %q", cfg.OIDCAudience, oidcAudience)
	}
	if want := oidcIssuer + "/keys"; cfg.OIDCJWKSURL != want {
		t.Errorf("OIDCJWKSURL = %q, want %q", cfg.OIDCJWKSURL, want)
	}
}

func TestLoadTrimsTrailingSlashFromOIDCIssuerURL(t *testing.T) {
	t.Parallel()

	vars := httpVars()
	vars[config.EnvPrefix+"OIDC_ISSUER_URL"] = oidcIssuer + "/"

	cfg, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// A caller building cfg.OIDCIssuerURL+"/.well-known/..." should never
	// produce a double slash.
	if cfg.OIDCIssuerURL != oidcIssuer {
		t.Errorf("OIDCIssuerURL = %q, want %q", cfg.OIDCIssuerURL, oidcIssuer)
	}
}

func TestLoadDoesNotRequireOIDCOnStdio(t *testing.T) {
	t.Parallel()

	// Stdio has no network to guard: the client spawned this process, so the
	// caller is already whoever owns it. Demanding an authorization server
	// there would only make running the server locally harder.
	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "UPSTREAM_URL": zoektURL,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.OIDCIssuerURL != "" {
		t.Errorf("OIDCIssuerURL = %q, want none", cfg.OIDCIssuerURL)
	}
	if cfg.OIDCAudience != "" {
		t.Errorf("OIDCAudience = %q, want none", cfg.OIDCAudience)
	}
}
