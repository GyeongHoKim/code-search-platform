package zoekt_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// envAddr names a running zoekt-webserver to test against.
const envAddr = "CODE_SEARCH_TEST_ZOEKT_URL"

// The unit tests build their fixtures out of this package's own types, so the
// field names are consistent by construction: a name this client spells
// differently from Zoekt would pass every one of them and then decode to a zero
// value in production. These tests exist to catch exactly that, by decoding what
// a real server actually sent.
//
//	just dev-up
//	CODE_SEARCH_TEST_ZOEKT_URL=http://127.0.0.1:6070 go test ./internal/zoekt/...
//
// They skip when the variable is unset, so `just test` stays hermetic.
func liveClient(t *testing.T) *zoekt.Client {
	t.Helper()

	addr := os.Getenv(envAddr)
	if addr == "" {
		t.Skipf("set %s to run against a real zoekt-webserver", envAddr)
	}

	return zoekt.New(zoekt.Options{BaseURL: addr, Timeout: 10 * time.Second})
}

func TestLiveSearchDecodesLineMatches(t *testing.T) {
	t.Parallel()

	client := liveClient(t)

	// A string this file itself contains, so the corpus indexed by `just
	// dev-up` is guaranteed to match it.
	result, err := client.Search(t.Context(), "TestLiveSearchDecodesLineMatches", zoekt.SearchOptions{
		NumContextLines:    2,
		MaxDocDisplayCount: 5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if len(result.Files) == 0 {
		t.Fatal("Files = 0; the dev index does not contain this repository")
	}

	// Zoekt embeds Stats, so these arrive beside Files rather than under a
	// Stats key. Not asserting them is how a named field went unnoticed: every
	// other check in this test passes while all of the counters decode to zero.
	if result.FileCount == 0 {
		t.Error("FileCount = 0; the counters are not arriving where this client looks for them")
	}
	if result.MatchCount == 0 {
		t.Error("MatchCount = 0; the counters are not arriving where this client looks for them")
	}

	file := result.Files[0]
	if file.Repository == "" {
		t.Error("Repository is empty; the field name does not match what Zoekt sends")
	}
	if file.FileName == "" {
		t.Error("FileName is empty; the field name does not match what Zoekt sends")
	}
	if len(file.LineMatches) == 0 {
		t.Fatal("LineMatches = 0; the field name does not match what Zoekt sends")
	}

	match := file.LineMatches[0]
	if match.LineNumber == 0 {
		t.Error("LineNumber = 0; the field name does not match what Zoekt sends")
	}
	// []byte decodes from base64. A string field here would leave the caller
	// holding the base64 instead, and this is what would notice.
	if len(match.Line) == 0 {
		t.Error("Line is empty; the field name does not match what Zoekt sends")
	}
	if !strings.Contains(string(match.Line), "TestLiveSearchDecodesLineMatches") {
		t.Errorf("Line = %q, want it to contain the searched term", match.Line)
	}
	if len(match.Before) == 0 && len(match.After) == 0 {
		t.Error("neither Before nor After is populated despite NumContextLines=2")
	}
}

func TestLiveSearchWholeReturnsFileContent(t *testing.T) {
	t.Parallel()

	client := liveClient(t)

	// The whole point of the Whole option here: a query that matches only a
	// filename still comes back with the file's content, which is what
	// read_file is built on.
	result, err := client.Search(t.Context(), "file:internal/zoekt/client.go", zoekt.SearchOptions{Whole: true})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}
	if len(result.Files) == 0 {
		t.Fatal("Files = 0; expected the filename query to match")
	}

	content := string(result.Files[0].Content)
	if content == "" {
		t.Fatal("Content is empty; Whole did not populate it")
	}
	if !strings.Contains(content, "package zoekt") {
		t.Errorf("Content does not look like the file: first 80 bytes = %q", content[:min(80, len(content))])
	}
}

func TestLiveSearchSymbolsCarrySymbolInfo(t *testing.T) {
	t.Parallel()

	client := liveClient(t)

	result, err := client.Search(t.Context(), "sym:SearchOptions", zoekt.SearchOptions{MaxDocDisplayCount: 10})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	for _, file := range result.Files {
		for _, match := range file.LineMatches {
			for _, fragment := range match.LineFragments {
				if fragment.SymbolInfo != nil && fragment.SymbolInfo.Sym != "" {
					if fragment.SymbolInfo.Kind == "" {
						t.Error("SymbolInfo.Kind is empty; the field name does not match")
					}

					return
				}
			}
		}
	}

	t.Error("no SymbolInfo on any fragment; either the field name is wrong or the index has no symbols")
}

func TestLiveListDecodesRepositories(t *testing.T) {
	t.Parallel()

	client := liveClient(t)

	// Zoekt restricts this query to repo: atoms.
	list, err := client.List(t.Context(), "repo:.")
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if len(list.Repos) == 0 {
		t.Fatal("Repos = 0; the dev index is empty")
	}

	entry := list.Repos[0]
	if entry.Repository.Name == "" {
		t.Error("Repository.Name is empty; the field name does not match what Zoekt sends")
	}
	if entry.Stats.Documents == 0 {
		t.Error("Stats.Documents = 0; the field name does not match what Zoekt sends")
	}
	if entry.IndexMetadata.IndexTime.IsZero() {
		t.Error("IndexMetadata.IndexTime is zero; the field name does not match what Zoekt sends")
	}
}

func TestLiveBadQueryIsDistinguishable(t *testing.T) {
	t.Parallel()

	client := liveClient(t)

	// An unbalanced group. Zoekt answers 400, and the client must report that
	// as a query problem rather than as an outage -- it is the one failure a
	// model can fix on its own.
	_, err := client.Search(t.Context(), "sym:(", zoekt.SearchOptions{})
	if err == nil {
		t.Fatal("Search() error = nil, want a query error")
	}
	if !strings.Contains(err.Error(), "rejected the query") {
		t.Errorf("error = %v, want it to classify as ErrBadQuery", err)
	}
}
