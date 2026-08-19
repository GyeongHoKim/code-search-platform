package mcpserver_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// envAddr names a running zoekt-webserver to test against.
//
//	just dev-up
//	CODE_SEARCH_TEST_ZOEKT_URL=http://127.0.0.1:6070 go test ./internal/mcpserver/...
const envAddr = "CODE_SEARCH_TEST_ZOEKT_URL"

// liveSearcher skips unless a real server was named, so `just test` stays
// hermetic.
func liveSearcher(t *testing.T) *zoekt.Client {
	t.Helper()

	addr := os.Getenv(envAddr)
	if addr == "" {
		t.Skipf("set %s to run against a real zoekt-webserver", envAddr)
	}

	return zoekt.New(zoekt.Options{BaseURL: addr, Timeout: 10 * time.Second})
}

func TestLiveToolsAnswerFromARealIndex(t *testing.T) {
	t.Parallel()

	session := connect(t, liveSearcher(t))

	// Each case names something only a real answer contains, so that a
	// "no such file" or "no matches" reply fails instead of passing.
	cases := map[string]struct {
		args map[string]any
		want string
	}{
		"list_repos":  {nil, "symbols="},
		"search_code": {map[string]any{"query": "ErrMissingZoektURL"}, "src:internal/config/config.go"},
		"find_symbol": {map[string]any{"symbol": "^Load$"}, "func Load(lookup Lookup)"},
		"read_file": {
			map[string]any{"repo": "src", "path": "internal/config/config.go", "start_line": 86, "end_line": 92},
			"func Load(lookup Lookup)",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, isErr := call(t, session, name, tc.args)
			if isErr {
				t.Fatalf("%s reported an error: %s", name, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("%s output does not contain %q:\n%s", name, tc.want, out)
			}

			t.Logf("\n%s", out)
		})
	}
}
