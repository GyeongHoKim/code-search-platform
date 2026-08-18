// Package mcpserver defines the tools this binary exposes and registers them
// on an MCP server.
//
// This is the only package that knows the protocol. It depends on the layers
// below it -- the Zoekt client and the renderers -- and nothing below depends
// back on it, which is what makes those layers testable without a client.
// .golangci.yml enforces that direction with a depguard rule rather than
// leaving it to a paragraph nobody reads.
package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/code-search-platform/internal/config"
	"github.com/GyeongHoKim/code-search-platform/internal/version"
)

// ServerName is the name reported to clients during initialisation.
const ServerName = "code-search-platform"

// New builds the MCP server described by cfg.
//
// The tool set is deliberately empty for now: the toolchain, the quality gates
// and the deployment story landed first, and the four tools -- search_code,
// read_file, find_symbol and list_repos -- are registered here next. A client
// that connects today completes initialisation and receives an empty
// tools/list, which is a truthful answer rather than a broken one.
func New(_ *config.Config) *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{
		Name:    ServerName,
		Version: version.Version,
	}, nil)
}
