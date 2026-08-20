package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/render"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

type findSymbolInput struct {
	Symbol string `json:"symbol" jsonschema:"Symbol name as a regexp. Anchor it with ^ and $ for an exact match, as in ^Search$."`
	Repo   string `json:"repo,omitempty" jsonschema:"Restrict the search to this repository, named exactly. Omit to search every repository."`
}

func (s *server) registerFindSymbol(on *mcp.Server) {
	mcp.AddTool(on, &mcp.Tool{
		Name:        "find_symbol",
		Description: "Find where a function, type or other symbol is defined, skipping the call sites and comments a text search would return. Requires an index built with ctags; the result says so when there is none.",
	}, s.findSymbol)
}

func (s *server) findSymbol(ctx context.Context, _ *mcp.CallToolRequest, in findSymbolInput) (*mcp.CallToolResult, any, error) {
	query := "sym:" + in.Symbol
	if in.Repo != "" {
		query += " repo:" + exact(in.Repo)
	}

	result, err := s.searcher.Search(ctx, query, zoekt.SearchOptions{
		NumContextLines:    s.cfg.ContextLines,
		MaxDocDisplayCount: s.cfg.MaxResults,
	})
	if err != nil {
		return fail(err)
	}

	if len(result.Files) == 0 {
		return s.explainEmpty(ctx, in, query)
	}

	return text(render.Search(result, query).String())
}

// explainEmpty separates "no such symbol" from an index that cannot answer
// symbol queries at all, which Zoekt reports as silence either way.
func (s *server) explainEmpty(ctx context.Context, in findSymbolInput, query string) (*mcp.CallToolResult, any, error) {
	scope := everyRepo
	if in.Repo != "" {
		scope = "repo:" + exact(in.Repo)
	}

	list, err := s.searcher.List(ctx, scope)
	if err != nil {
		return fail(err)
	}

	var without []string
	for _, entry := range list.Repos {
		if !entry.Repository.HasSymbols {
			without = append(without, entry.Repository.Name)
		}
	}

	if len(without) == 0 {
		return text(render.Search(&zoekt.SearchResult{}, query).String())
	}

	if len(without) == len(list.Repos) {
		return text(fmt.Sprintf(
			"no symbol data: these repositories were indexed without ctags, so sym: queries cannot match.\n"+
				"this is not the same as \"symbol not found\".\n"+
				"indexed without symbols: %s\n",
			strings.Join(without, ", ")))
	}

	// Part of the scope answered and had nothing; the rest was never searched.
	// Reporting this as a plain miss would tell a model the symbol does not
	// exist, which is the one thing this result cannot establish.
	return text(fmt.Sprintf(
		"%sincomplete: some repositories were indexed without ctags, so sym: queries cannot reach them.\n"+
			"a definition may still exist there.\n"+
			"indexed without symbols: %s\n",
		render.Search(&zoekt.SearchResult{}, query).String(),
		strings.Join(without, ", ")))
}
