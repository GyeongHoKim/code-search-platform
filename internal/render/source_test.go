package render_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/GyeongHoKim/code-search-platform/internal/render"
)

func TestSourceNumbersTheRequestedRange(t *testing.T) {
	t.Parallel()

	content := []byte("package main\n\nfunc main() {}\n")

	want := "src:a.go lines 2-3 of 3\n" +
		"     2\n" +
		"     3  func main() {}\n"

	if diff := cmp.Diff(want, render.Source("src", "a.go", content, 2, 3)); diff != "" {
		t.Errorf("Source() mismatch (-want +got):\n%s", diff)
	}
}

func TestSourceStartsAtTheTopWhenNoRangeIsGiven(t *testing.T) {
	t.Parallel()

	content := []byte("one\ntwo\n")

	want := "src:a.go lines 1-2 of 2\n" +
		"     1  one\n" +
		"     2  two\n"

	if diff := cmp.Diff(want, render.Source("src", "a.go", content, 0, 0)); diff != "" {
		t.Errorf("Source() mismatch (-want +got):\n%s", diff)
	}
}

func TestSourceCapsALongFileAndSaysHowToContinue(t *testing.T) {
	t.Parallel()

	var content strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&content, "line %d\n", i)
	}

	got := render.Source("src", "big.go", []byte(content.String()), 0, 0)

	if !strings.HasPrefix(got, "src:big.go lines 1-400 of 500\n") {
		t.Errorf("header = %q, want the range capped at 400", strings.SplitN(got, "\n", 2)[0])
	}
	if strings.Contains(got, "line 401") {
		t.Error("output ran past the cap")
	}

	wantTail := "-- truncated at line 400 of 500; call read_file again with start_line=401\n"
	if !strings.HasSuffix(got, wantTail) {
		t.Errorf("tail = %q, want %q", got[max(0, len(got)-len(wantTail)):], wantTail)
	}
}

func TestSourceReportsARangePastTheEndOfTheFile(t *testing.T) {
	t.Parallel()

	got := render.Source("src", "a.go", []byte("one\ntwo\n"), 9, 0)

	if diff := cmp.Diff("src:a.go has 2 lines; start_line=9 is past the end\n", got); diff != "" {
		t.Errorf("Source() mismatch (-want +got):\n%s", diff)
	}
}
