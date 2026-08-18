package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/GyeongHoKim/code-search-platform/internal/render"
	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

func TestReposAlignsColumnsAndReportsSymbolAvailability(t *testing.T) {
	t.Parallel()

	indexed := time.Date(2026, time.August, 18, 9, 30, 0, 0, time.UTC)
	list := &zoekt.RepoList{Repos: []*zoekt.RepoListEntry{
		{
			Repository: zoekt.Repository{
				Name:       "code-search-platform",
				HasSymbols: true,
				Branches:   []zoekt.RepositoryBranch{{Name: "main", Version: "1a5ca1b"}},
			},
			Stats:         zoekt.RepoStats{Documents: 42},
			IndexMetadata: zoekt.IndexMetadata{IndexTime: indexed},
		},
		{
			Repository: zoekt.Repository{
				Name:     "legacy",
				Branches: []zoekt.RepositoryBranch{{Name: "master", Version: "9f2e1a0"}},
			},
			Stats:         zoekt.RepoStats{Documents: 1183},
			IndexMetadata: zoekt.IndexMetadata{IndexTime: indexed},
		},
	}}

	want := "2 repositories\n" +
		"code-search-platform  symbols=yes  docs=42    main@1a5ca1b    indexed=2026-08-18\n" +
		"legacy                symbols=no   docs=1183  master@9f2e1a0  indexed=2026-08-18\n"

	if diff := cmp.Diff(want, render.Repos(list)); diff != "" {
		t.Errorf("Repos() mismatch (-want +got):\n%s", diff)
	}
}

func TestReposSaysSoWhenTheIndexIsEmpty(t *testing.T) {
	t.Parallel()

	if diff := cmp.Diff("no repositories are indexed\n", render.Repos(&zoekt.RepoList{})); diff != "" {
		t.Errorf("Repos() mismatch (-want +got):\n%s", diff)
	}
}

func TestReposCountsOneRepositoryInTheSingular(t *testing.T) {
	t.Parallel()

	list := &zoekt.RepoList{Repos: []*zoekt.RepoListEntry{
		{Repository: zoekt.Repository{Name: "src"}},
	}}

	if got := render.Repos(list); !strings.HasPrefix(got, "1 repository\n") {
		t.Errorf("Repos() = %q, want it to start with %q", got, "1 repository\n")
	}
}
