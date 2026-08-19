package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/code-search-platform/internal/render"
)

// everyRepo matches any repository name. Zoekt's list endpoint takes a query
// rather than nothing at all.
const everyRepo = "repo:."

type listReposInput struct {
	Filter string `json:"filter,omitempty" jsonschema:"Regexp matched against repository names. Omit to list everything."`
}

func (s *server) registerListRepos(on *mcp.Server) {
	mcp.AddTool(on, &mcp.Tool{
		Name:        "list_repos",
		Description: "List indexed repositories with their document counts, branches and whether they carry symbol data. Use this to scope a query, or to check whether find_symbol can answer at all.",
	}, s.listRepos)
}

func (s *server) listRepos(ctx context.Context, _ *mcp.CallToolRequest, in listReposInput) (*mcp.CallToolResult, any, error) {
	query := everyRepo
	if in.Filter != "" {
		query = "repo:" + in.Filter
	}

	list, err := s.searcher.List(ctx, query)
	if err != nil {
		return fail(err)
	}

	return text(render.Repos(list))
}
