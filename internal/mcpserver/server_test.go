package mcpserver_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/code-search-platform/internal/config"
	"github.com/GyeongHoKim/code-search-platform/internal/mcpserver"
	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// stub records what a tool asked Zoekt for and replays a canned answer.
type stub struct {
	searchErr error
	listErr   error
	result    *zoekt.SearchResult
	list      *zoekt.RepoList

	queries     []string
	listQueries []string
}

func (s *stub) Search(_ context.Context, query string, _ zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	s.queries = append(s.queries, query)
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	if s.result != nil {
		return s.result, nil
	}

	return &zoekt.SearchResult{}, nil
}

func (s *stub) List(_ context.Context, query string) (*zoekt.RepoList, error) {
	s.listQueries = append(s.listQueries, query)
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.list != nil {
		return s.list, nil
	}

	return &zoekt.RepoList{}, nil
}

// connect drives a real session against searcher.
func connect(t *testing.T, searcher mcpserver.Searcher) *mcp.ClientSession {
	t.Helper()

	cfg := &config.Config{
		ZoektURL:     "http://127.0.0.1:6070",
		Transport:    config.TransportStdio,
		Timeout:      5 * time.Second,
		MaxResults:   50,
		ContextLines: 3,
	}

	server := mcpserver.New(cfg, searcher)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatalf("connecting server: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting client: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("closing session: %v", closeErr)
		}
	})

	return session
}

// call runs a tool and returns its text and error flag.
func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v, want nil", name, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("Content = %d blocks, want 1", len(result.Content))
	}

	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] = %T, want *mcp.TextContent", result.Content[0])
	}

	return text.Text, result.IsError
}

func TestTheServerOffersTheFourTools(t *testing.T) {
	t.Parallel()

	session := connect(t, &stub{})

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v, want nil", err)
	}

	got := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}

	for _, want := range []string{"search_code", "read_file", "find_symbol", "list_repos"} {
		if !got[want] {
			t.Errorf("tools/list is missing %q", want)
		}
	}
}

func TestSearchCodePassesTheQueryThroughUntouched(t *testing.T) {
	t.Parallel()

	searcher := &stub{}
	session := connect(t, searcher)

	if _, isErr := call(t, session, "search_code", map[string]any{"query": "repo:src (foo or bar) -lang:go"}); isErr {
		t.Fatal("search_code reported an error, want success")
	}

	want := []string{"repo:src (foo or bar) -lang:go"}
	if fmt.Sprint(searcher.queries) != fmt.Sprint(want) {
		t.Errorf("queries = %v, want %v", searcher.queries, want)
	}
}

func TestReadFileAnchorsAndEscapesTheRepoAndPath(t *testing.T) {
	t.Parallel()

	searcher := &stub{}
	session := connect(t, searcher)

	call(t, session, "read_file", map[string]any{"repo": "src", "path": "internal/zoekt/client.go"})

	if len(searcher.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(searcher.queries))
	}

	// An unescaped dot would also match clientXgo.
	query := searcher.queries[0]
	if !strings.Contains(query, `file:^internal/zoekt/client\.go$`) {
		t.Errorf("query = %q, want an anchored and escaped file atom", query)
	}
	if !strings.Contains(query, `repo:^src$`) {
		t.Errorf("query = %q, want an anchored repo atom", query)
	}
}

func TestFindSymbolPassesTheSymbolThroughAsARegexp(t *testing.T) {
	t.Parallel()

	searcher := &stub{list: &zoekt.RepoList{Repos: []*zoekt.RepoListEntry{
		{Repository: zoekt.Repository{Name: "src", HasSymbols: true}},
	}}}
	session := connect(t, searcher)

	call(t, session, "find_symbol", map[string]any{"symbol": "^Search$"})

	if len(searcher.queries) != 1 || !strings.Contains(searcher.queries[0], "sym:^Search$") {
		t.Errorf("queries = %v, want the symbol kept as a regexp", searcher.queries)
	}
}

func TestFindSymbolSeparatesAMissingSymbolFromAnIndexWithoutSymbols(t *testing.T) {
	t.Parallel()

	searcher := &stub{list: &zoekt.RepoList{Repos: []*zoekt.RepoListEntry{
		{Repository: zoekt.Repository{Name: "src"}},
		{Repository: zoekt.Repository{Name: "legacy"}},
	}}}
	session := connect(t, searcher)

	text, isErr := call(t, session, "find_symbol", map[string]any{"symbol": "Search"})
	if isErr {
		t.Fatal("find_symbol reported an error, want an explanatory result")
	}

	if !strings.Contains(text, "no symbol data") {
		t.Errorf("text = %q, want it to say the index carries no symbols", text)
	}
	if !strings.Contains(text, "src") || !strings.Contains(text, "legacy") {
		t.Errorf("text = %q, want the repositories named", text)
	}
}

func TestFindSymbolReportsAMissingSymbolWhenTheIndexHasSymbols(t *testing.T) {
	t.Parallel()

	searcher := &stub{list: &zoekt.RepoList{Repos: []*zoekt.RepoListEntry{
		{Repository: zoekt.Repository{Name: "src", HasSymbols: true}},
	}}}
	session := connect(t, searcher)

	text, _ := call(t, session, "find_symbol", map[string]any{"symbol": "Nope"})

	if strings.Contains(text, "no symbol data") {
		t.Errorf("text = %q, want a plain no-matches answer", text)
	}
}

func TestListReposMatchesEverythingWithoutAFilter(t *testing.T) {
	t.Parallel()

	searcher := &stub{}
	session := connect(t, searcher)

	call(t, session, "list_repos", nil)

	want := []string{"repo:."}
	if fmt.Sprint(searcher.listQueries) != fmt.Sprint(want) {
		t.Errorf("listQueries = %v, want %v", searcher.listQueries, want)
	}
}

func TestABadQueryIsReportedAsSomethingTheCallerCanFix(t *testing.T) {
	t.Parallel()

	searcher := &stub{searchErr: fmt.Errorf("/api/search: parse error: %w", zoekt.ErrBadQuery)}
	session := connect(t, searcher)

	text, isErr := call(t, session, "search_code", map[string]any{"query": "repo:("})
	if !isErr {
		t.Fatal("search_code succeeded, want an error result")
	}
	if !strings.Contains(text, "query") {
		t.Errorf("text = %q, want it to point at the query", text)
	}
}

func TestAnUnavailableZoektIsNotBlamedOnTheQuery(t *testing.T) {
	t.Parallel()

	searcher := &stub{searchErr: fmt.Errorf("/api/search: %w", zoekt.ErrUnavailable)}
	session := connect(t, searcher)

	text, isErr := call(t, session, "search_code", map[string]any{"query": "needle"})
	if !isErr {
		t.Fatal("search_code succeeded, want an error result")
	}
	if !strings.Contains(text, "not a problem with the query") {
		t.Errorf("text = %q, want it to steer the caller away from rewriting the query", text)
	}
}
