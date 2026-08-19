// Package mcpserver defines the tools this binary exposes and registers them
// on an MCP server.
//
// This is the only package that knows the protocol. Nothing below it depends
// back on it, which is what makes those layers testable without a client.
package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/code-search-platform/internal/config"
	"github.com/GyeongHoKim/code-search-platform/internal/version"
	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// ServerName is the name reported to clients during initialisation.
const ServerName = "code-search-platform"

// Searcher is the part of the Zoekt client these tools use.
type Searcher interface {
	Search(ctx context.Context, query string, opts zoekt.SearchOptions) (*zoekt.SearchResult, error)
	List(ctx context.Context, query string) (*zoekt.RepoList, error)
}

// server holds what every tool handler needs.
type server struct {
	searcher Searcher
	cfg      *config.Config
}

// New builds the MCP server described by cfg.
func New(cfg *config.Config, searcher Searcher) *mcp.Server {
	built := mcp.NewServer(&mcp.Implementation{
		Name:    ServerName,
		Version: version.Version,
	}, nil)

	s := &server{cfg: cfg, searcher: searcher}
	s.registerSearchCode(built)
	s.registerReadFile(built)
	s.registerFindSymbol(built)
	s.registerListRepos(built)

	return built
}

// text is a tool result carrying nothing but the rendered output.
//
// The Out type stays untyped and nil on purpose: a typed Out makes the SDK
// send the same payload twice, once as structuredContent and once escaped
// into content.
func text(rendered string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: rendered}},
	}, nil, nil
}

// fail turns a Zoekt failure into something a model can act on. The message
// is what the model reads, so it says whose problem this is.
func fail(err error) (*mcp.CallToolResult, any, error) {
	switch {
	case errors.Is(err, zoekt.ErrBadQuery), errors.Is(err, zoekt.ErrEmptyQuery):
		return nil, nil, fmt.Errorf("%w: rewrite the query and call again", err)
	default:
		return nil, nil, fmt.Errorf("%w: this is not a problem with the query but with the search deployment", err)
	}
}
