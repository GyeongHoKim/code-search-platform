package version_test

import (
	"strings"
	"testing"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/version"
)

func TestStringIncludesEveryStamp(t *testing.T) {
	t.Parallel()

	got := version.String()

	for _, want := range []string{version.Version, version.Commit, version.Date} {
		if !strings.Contains(got, want) {
			t.Errorf("version.String() = %q, want it to contain %q", got, want)
		}
	}
}
