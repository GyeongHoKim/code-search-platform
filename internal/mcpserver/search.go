package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/render"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

type searchInput struct {
	Query string `json:"query" jsonschema:"Zoekt query. Plain text and regexps match file contents. Narrow with atoms: repo:NAME file:PATH lang:go sym:NAME case:yes. Prefix an atom with - to exclude it, combine alternatives with 'or', and group with parentheses. Example: repo:api (retry or backoff) -file:_test\\.go"`
}

func (s *server) registerSearchCode(on *mcp.Server) {
	mcp.AddTool(on, &mcp.Tool{
		Name:        "search_code",
		Description: "Search indexed source code and return matching lines with surrounding context, as repo:path followed by numbered lines. Use this first to find where something lives.",
	}, s.searchCode)
}

func (s *server) searchCode(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
	result, err := s.searcher.Search(ctx, in.Query, zoekt.SearchOptions{
		NumContextLines:    s.cfg.ContextLines,
		MaxDocDisplayCount: s.cfg.MaxResults,
	})
	if err != nil {
		return fail(err)
	}

	return text(render.Search(result, in.Query).String())
}
