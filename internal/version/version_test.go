package version_test

import (
	"strings"
	"testing"

	"github.com/GyeongHoKim/code-search-platform/internal/version"
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
