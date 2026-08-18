package render_test

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/GyeongHoKim/code-search-platform/internal/render"
	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// golden reads a hand-written expectation.
func golden(t *testing.T, name string) string {
	t.Helper()

	// The name is a constant in this file, not caller input.
	content, err := os.ReadFile("testdata/" + name) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}

	return string(content)
}

func TestTextGroupsSpansUnderAFileHeader(t *testing.T) {
	t.Parallel()

	result := &zoekt.SearchResult{
		Stats: zoekt.Stats{FileCount: 2, MatchCount: 3},
		Files: []zoekt.FileMatch{
			{
				Repository: "src",
				FileName:   "a.go",
				LineMatches: []zoekt.LineMatch{
					{LineNumber: 10, Before: []byte("nine\n"), Line: []byte("ten\n"), After: []byte("eleven\n")},
					{LineNumber: 12, Before: []byte("eleven\n"), Line: []byte("twelve\n"), After: []byte("thirteen\n")},
					{LineNumber: 41, Before: []byte("forty\n"), Line: []byte("forty-one\n")},
				},
			},
			{
				Repository:  "src",
				FileName:    "b.go",
				LineMatches: []zoekt.LineMatch{{FileName: true}},
			},
		},
	}

	got := render.Search(result, "needle").String()

	if diff := cmp.Diff(golden(t, "search.golden"), got); diff != "" {
		t.Errorf("String() mismatch (-want +got):\n%s", diff)
	}
}

func TestTextReportsTruncationAndTellsTheCallerToNarrow(t *testing.T) {
	t.Parallel()

	result := &zoekt.SearchResult{
		Stats: zoekt.Stats{FileCount: 47, MatchCount: 128},
		Files: []zoekt.FileMatch{{Repository: "src", FileName: "a.go"}},
	}

	want := "47 files, 128 matches; showing 1 -- narrow the query to see the rest\nsrc:a.go\n"
	if diff := cmp.Diff(want, render.Search(result, "needle").String()); diff != "" {
		t.Errorf("String() mismatch (-want +got):\n%s", diff)
	}
}

func TestTextWarnsWhenTheEngineStoppedEarly(t *testing.T) {
	t.Parallel()

	result := &zoekt.SearchResult{
		Stats: zoekt.Stats{FileCount: 1, MatchCount: 1, FilesSkipped: 1204},
		Files: []zoekt.FileMatch{{Repository: "src", FileName: "a.go"}},
	}

	want := "1 file, 1 match\n" +
		"warning: zoekt stopped early; 1204 files were not examined, so these counts are lower bounds\n" +
		"src:a.go\n"
	if diff := cmp.Diff(want, render.Search(result, "needle").String()); diff != "" {
		t.Errorf("String() mismatch (-want +got):\n%s", diff)
	}
}

func TestTextEchoesTheQueryWhenNothingMatched(t *testing.T) {
	t.Parallel()

	got := render.Search(&zoekt.SearchResult{}, "sym:Nope repo:src").String()

	if diff := cmp.Diff("no matches for: sym:Nope repo:src\n", got); diff != "" {
		t.Errorf("String() mismatch (-want +got):\n%s", diff)
	}
}
