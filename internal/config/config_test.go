package config_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/GyeongHoKim/code-search-platform/internal/config"
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
	zoektURL  = "http://zoekt:6070"
	authToken = "test-token"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "ZOEKT_URL": zoektURL,
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
		config.EnvPrefix + "ZOEKT_URL": zoektURL + "/",
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
			vars: map[string]string{config.EnvPrefix + "ZOEKT_URL": "   "},
			want: config.ErrMissingZoektURL,
		},
		"zoekt url without scheme": {
			vars: map[string]string{config.EnvPrefix + "ZOEKT_URL": "zoekt:6070"},
			want: config.ErrInvalidZoektURL,
		},
		"zoekt url with the wrong scheme": {
			vars: map[string]string{config.EnvPrefix + "ZOEKT_URL": "ftp://zoekt:6070"},
			want: config.ErrInvalidZoektURL,
		},
		"unknown transport": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL": zoektURL,
				config.EnvPrefix + "TRANSPORT": "grpc",
			},
			want: config.ErrUnknownTransport,
		},
		"http transport without an auth token": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL": zoektURL,
				config.EnvPrefix + "TRANSPORT": "http",
			},
			want: config.ErrMissingAuthToken,
		},
		"http transport with a blank auth token": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL":  zoektURL,
				config.EnvPrefix + "TRANSPORT":  "http",
				config.EnvPrefix + "AUTH_TOKEN": "  ,  ,",
			},
			want: config.ErrMissingAuthToken,
		},
		"non numeric max results": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL":   zoektURL,
				config.EnvPrefix + "MAX_RESULTS": "many",
			},
			want: config.ErrNotAnInteger,
		},
		"max results above the ceiling": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL":   zoektURL,
				config.EnvPrefix + "MAX_RESULTS": "10000",
			},
			want: config.ErrOutOfRange,
		},
		"zero max results": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL":   zoektURL,
				config.EnvPrefix + "MAX_RESULTS": "0",
			},
			want: config.ErrOutOfRange,
		},
		"negative timeout": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL": zoektURL,
				config.EnvPrefix + "TIMEOUT":   "-5s",
			},
			want: config.ErrOutOfRange,
		},
		"unparseable timeout": {
			vars: map[string]string{
				config.EnvPrefix + "ZOEKT_URL": zoektURL,
				config.EnvPrefix + "TIMEOUT":   "soon",
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
		config.EnvPrefix + "ZOEKT_URL":     zoektURL,
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

	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "ZOEKT_URL":  zoektURL,
		config.EnvPrefix + "TRANSPORT":  "HTTP",
		config.EnvPrefix + "ADDR":       ":9090",
		config.EnvPrefix + "AUTH_TOKEN": authToken,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Transport != config.TransportHTTP {
		t.Errorf("Transport = %q, want %q", cfg.Transport, config.TransportHTTP)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":9090")
	}
	if len(cfg.AuthTokens) != 1 || cfg.AuthTokens[0] != authToken {
		t.Errorf("AuthTokens = %v, want [%q]", cfg.AuthTokens, authToken)
	}
}

func TestLoadSplitsAuthTokens(t *testing.T) {
	t.Parallel()

	// A comma separated list is what makes rotation possible without a window
	// in which every caller is broken: add the new token, let callers move,
	// then drop the old one.
	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "ZOEKT_URL":  zoektURL,
		config.EnvPrefix + "TRANSPORT":  "http",
		config.EnvPrefix + "AUTH_TOKEN": "  old  , new ,, ",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := []string{"old", "new"}
	if !slices.Equal(cfg.AuthTokens, want) {
		t.Errorf("AuthTokens = %v, want %v", cfg.AuthTokens, want)
	}
}

func TestLoadDoesNotRequireAnAuthTokenOnStdio(t *testing.T) {
	t.Parallel()

	// Stdio has no network to guard: the client spawned this process, so the
	// caller is already whoever owns it. Demanding a token there would only
	// make running the server locally harder.
	cfg, err := config.Load(env(map[string]string{
		config.EnvPrefix + "ZOEKT_URL": zoektURL,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if len(cfg.AuthTokens) != 0 {
		t.Errorf("AuthTokens = %v, want none", cfg.AuthTokens)
	}
}
