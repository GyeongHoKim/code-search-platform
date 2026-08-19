package mcpserver

import (
	"context"
	"fmt"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/render"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

type readFileInput struct {
	Repo      string `json:"repo" jsonschema:"Repository name exactly as search_code reported it, before the colon."`
	Path      string `json:"path" jsonschema:"File path within the repository, exactly as search_code reported it."`
	StartLine int    `json:"start_line,omitempty" jsonschema:"First line to return, 1-based. Defaults to 1."`
	EndLine   int    `json:"end_line,omitempty" jsonschema:"Last line to return, inclusive. Defaults to the end of the file, capped at 400 lines per call."`
}

func (s *server) registerReadFile(on *mcp.Server) {
	mcp.AddTool(on, &mcp.Tool{
		Name:        "read_file",
		Description: "Read a range of lines from one indexed file, numbered. Use this after search_code when three lines of context are not enough.",
	}, s.readFile)
}

func (s *server) readFile(ctx context.Context, _ *mcp.CallToolRequest, in readFileInput) (*mcp.CallToolResult, any, error) {
	query := fmt.Sprintf("repo:%s file:%s", exact(in.Repo), exact(in.Path))

	result, err := s.searcher.Search(ctx, query, zoekt.SearchOptions{
		Whole:              true,
		MaxDocDisplayCount: 1,
	})
	if err != nil {
		return fail(err)
	}

	if len(result.Files) == 0 {
		return text(fmt.Sprintf("no such file: %s:%s\n", in.Repo, in.Path))
	}

	return text(render.Source(in.Repo, in.Path, result.Files[0].Content, in.StartLine, in.EndLine))
}

// exact turns an identifier into an anchored literal. repo: and file: are Go
// regexps, so an unescaped dot in a path matches any character.
func exact(name string) string {
	return "^" + regexp.QuoteMeta(name) + "$"
}
