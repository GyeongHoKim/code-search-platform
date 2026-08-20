package render_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/render"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

func TestOverlappingContextWindowsMergeIntoOneSpan(t *testing.T) {
	t.Parallel()

	// Two matches three lines apart with one context line each: the windows
	// touch, so they belong to one span with no separator between them.
	result := &zoekt.SearchResult{
		Stats: zoekt.Stats{FileCount: 1, MatchCount: 2},
		Files: []zoekt.FileMatch{{
			Repository: "src",
			FileName:   "a.go",
			LineMatches: []zoekt.LineMatch{
				{LineNumber: 10, Before: []byte("nine\n"), Line: []byte("ten\n"), After: []byte("eleven\n")},
				{LineNumber: 12, Before: []byte("eleven\n"), Line: []byte("twelve\n"), After: []byte("thirteen\n")},
			},
		}},
	}

	got := render.Search(result, "needle")

	want := render.Result{
		Query:   "needle",
		Summary: render.Summary{FileCount: 1, MatchCount: 2, ShownFiles: 1},
		Files: []render.File{{
			Repo: "src",
			Path: "a.go",
			Spans: []render.Span{{Lines: []render.Line{
				{Number: 9, Text: "nine"},
				{Number: 10, Text: "ten", Match: true},
				{Number: 11, Text: "eleven"},
				{Number: 12, Text: "twelve", Match: true},
				{Number: 13, Text: "thirteen"},
			}}},
		}},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Search() mismatch (-want +got):\n%s", diff)
	}
}
