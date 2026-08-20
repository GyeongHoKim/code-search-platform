# AGENTS.md

Working notes for agents and humans changing this repository. For what the product *is*, read
[README.md](README.md) — this file is about how it is built.

## What this is

One Go binary, `zoekt-mcp-server`, that puts an indexed corpus of source code behind the Model
Context Protocol. It is a client of `zoekt-webserver` and nothing else.

The repository also carries the layer around it — the indexer that mirrors a Git host and keeps
the index fresh, and the Helm chart that runs the three pieces together — because the hard part
of internal code search is never the protocol shim.

**On the stdio transport, stdout is the JSON-RPC channel.** Never print to it. Not a debug line,
not a progress message, not a stray `fmt.Println`. Anything that is not protocol traffic corrupts
the stream and the client disconnects. Diagnostics go to stderr, through the `slog` logger that
`serve` builds.

## Setup

```bash
mise trust && mise install   # pinned toolchain -- see mise.toml
just setup                   # git hooks
```

If you are on Windows and `CLAUDE.md` shows up as a text file containing the word `AGENTS.md`,
your checkout did not restore symlinks: `git config core.symlinks true` and re-checkout.

## Commands

Always go through `just`. The recipes carry the right flags and CI runs the same ones, so a green
`just ci` locally means a green pipeline.

| Command | What it does |
| --- | --- |
| `just build` | Build the binary into `bin/` |
| `just run` | Run the server from source |
| `just test` / `just test-race` | Tests, with and without the race detector |
| `just lint` / `just lint-fix` | Linters |
| `just fmt` / `just fmt-check` | Formatters |
| `just config-verify` | Validate `.golangci.yml` against the v2 schema |
| `just tidy-check` | Fail if `go.mod`/`go.sum` are untidy |
| `just vuln` | govulncheck |
| `just docs-fetch` | Refetch the Zoekt reference into `docs/zoekt/` |
| `just dev-up` / `just dev-down` | Local Zoekt with a sample index |
| `just inspect` | Drive the server through the MCP Inspector |
| `just helm-lint` / `just helm-template` | Check the chart without a cluster |
| `just ci` | Everything CI runs |

Two things about these that cost an afternoon each:

- **`just dev-up` indexes git HEAD, not the working tree.** `zoekt-git-index` reads the
  repository, so a file you have written but not committed does not exist as far as the local
  index is concerned. A test that searches for code you just wrote fails until you commit and
  re-run `just dev-up`, and the failure looks exactly like a broken client.
- **`just ci` fails on an uncommitted `go.mod`.** `tidy-check` compares against the checked-in
  file, so adding a dependency and running the gate before committing reports "untidy" for a
  `go.mod` that is perfectly tidy. CI never sees this because its checkout is clean.

## Verification

**After changing any code, run these three and confirm they pass before reporting the work done:**

```bash
just fmt      # apply the formatters
just lint     # then the linters
just test     # then the tests
```

Run them in that order — `fmt` rewrites files, and linting or testing a file you are about to
reformat wastes the run. If you did not execute all three, the work is not finished, however
obviously correct the change looks. Before committing, run `just ci`, which adds the race
detector, the schema check, the tidy check and govulncheck.

Do not report success on the strength of a change "looking right". Paste the command output or
say plainly that it was not run.

## Layout

```text
cmd/zoekt-mcp-server/        entry point: flags, environment, transport selection
internal/config/            environment parsing and validation
internal/zoekt/             the Zoekt JSON API client -- HTTP lives here and nowhere else
internal/render/            Zoekt types to the compact text a model reads
internal/httpauth/          the bearer token guard on the http transport
internal/mcpserver/         tool definitions and registration
internal/version/           ldflags-injected build stamps
internal/tools/fetchdocs/   vendors docs/zoekt/ at a pinned revision
deploy/helm/                the chart: Zoekt, the indexer CronJob, this server
deploy/argocd/              an Argo CD Application that points at the chart
docs/zoekt/                 vendored Zoekt reference -- generated, never hand edited
```

**`internal/zoekt` is a leaf.** It must not import `internal/mcpserver`, `internal/render` or
`internal/config`. HTTP concerns stay on one side of that line and protocol concerns on the other,
which is what lets the client be tested without an MCP server and reused by anything else that
needs Zoekt. This is not a convention anyone has to remember: `.golangci.yml` has a `depguard`
rule named `zoekt-client-is-a-leaf` that fails the build.

## Conventions

**Follow the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md).** The
points that come up most in this codebase:

- Define interfaces where they are **consumed**, not where they are implemented. A `Searcher`
  interface belongs next to the tool that calls it.
- Receivers are consistently pointer or consistently value across a type. Do not mix.
- `error` is the last return value, and callers match with `errors.Is`/`errors.As` — never `==`.
- Make the zero value useful. A `Config{}` that panics is a design smell.
- Never copy a mutex. Embed by pointer or keep it unexported behind a constructor.
- Copy slices and maps at the boundary. Returning an internal slice hands a caller your state.
- No `init()`. Wiring happens in `main`, where it can be read top to bottom. `gochecknoinits`
  enforces this.
- Prefer functional options to a constructor with five booleans.
- Enums are typed constants with a `String()`, not bare strings scattered through switch arms.

**Follow [Effective Go](https://go.dev/doc/effective_go).** In particular:

- Short names in short scopes; long names only where the distance justifies them.
- Do not stutter: `zoekt.Client`, not `zoekt.ZoektClient`.
- `defer` the cleanup on the line after the acquisition, so the pair is read together.
- Interfaces are small. One or two methods is normal; five is a design that has not been thought
  through yet.
- Error strings are lower case and have no trailing punctuation: `"searching index"`, not
  `"Searching index."`.
- Pick channels or mutexes for a given piece of state and stay with it.

**Wrap every error with context**: `fmt.Errorf("searching %q: %w", query, err)`. Sentinel errors
are declared at package level in the file whose operations return them, so callers can use
`errors.Is`. `err113` fails the build on an error created inline.

**Never return raw Zoekt JSON to the model.** A `SearchResult` is enormous and most of it is
scoring metadata. Everything the model sees goes through `internal/render`, which compacts it to
`repo:path:line` plus the configured context lines. Tokens are a budget, and the whole reason this
server exists instead of handing an agent a `curl` command is that something has to do this
compaction.

**Dependencies are capped.** The MCP Go SDK is the only external module allowed in shipped code;
tests may also use `go-cmp`. `depguard` enforces it. If you find yourself reaching for a helper
library, write the twenty lines instead — a small dependency graph is a feature of a binary that
companies install on their own infrastructure.

## MCP SDK notes

Verified against `github.com/modelcontextprotocol/go-sdk v1.7.0`.

- A tool is registered with the package-level generic `mcp.AddTool(server, &mcp.Tool{...}, handler)`.
  The handler has the shape `func(ctx, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)`,
  and the SDK derives the JSON schema from `In` — `jsonschema:"..."` struct tags become the
  parameter descriptions a model reads, so write them for the model, not for a Go reviewer.
- `serveStdio` uses `server.Connect(ctx, transport, nil)` and then `session.Wait()` rather than
  `server.Run`. `Run` collapses two different failures into one error: failing to connect is this
  process failing to start, and a session ending is the client going away. They do not deserve
  the same exit status.
- The HTTP transport is `mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server, opts)`.
  The function is called per request, which is the hook where per-caller authorisation belongs
  once it exists.
- Tests drive a real session over `mcp.NewInMemoryTransports()`. There is no need to spawn a
  process or open a port to assert what a tool returns.

## Zoekt API notes

These will waste your afternoon if you do not know them. The vendored reference is in
`docs/zoekt/` — run `just docs-fetch` to refresh it.

- **`/api/search` only exists when `zoekt-webserver` was started with `-rpc`.** Without that flag
  the server answers HTML and every request fails in a way that looks like a routing bug.
- **`Opts.NumContextLines` decides how large a result is.** It is the single most important knob
  for token cost; it is surfaced as `ZOEKT_MCP_CONTEXT_LINES` and defaults to 3.
- **`sym:` only matches if the index was built with ctags available.** An index built without it
  answers symbol queries with silence, not an error, which reads as "no such symbol".
- **`/api/list` takes a query too.** Listing repositories is a search with a `repo:` atom, not a
  separate endpoint with its own filters.
- **The query language is the real API.** `repo:`, `file:`, `lang:`, `sym:`, `case:`, negation and
  boolean grouping all live in `docs/zoekt/query-syntax.md`. Tool parameters are a thin translation
  into that string; resist inventing a second query language on top of it.

## Testing

- `internal/zoekt`: `httptest.Server` returning recorded Zoekt payloads.
- `internal/render`: golden files written by hand, not captured with `-update`. A golden taken
  from the implementation records only what the code happens to do.
- `internal/mcpserver`: tools driven over `mcp.NewInMemoryTransports()` against a stub Zoekt.
- `internal/config`: the environment is injected as a `config.Lookup`, so no test mutates the
  environment of the test binary and cases can run in parallel.
- Tests run with `-race` in CI on Linux, macOS and Windows.

## Commits

Conventional Commits, checked in CI. A **scope is required** and is free-form and lower case —
say which part of the system moved.

```text
feat(tools): add find_symbol
fix(zoekt): send NumContextLines with every search
```

Scopes in use: `zoekt`, `mcp`, `tools`, `config`, `render`, `deploy`, `helm`, `ci`, `docs`,
`deps`, `lint`, `repo`. Reach for one of those before inventing a synonym. Subject is lower case, no
trailing period, 72 characters for the whole header.

## Releasing

Tag-driven and automated; do not publish by hand.

1. `git tag v1.2.3 && git push origin v1.2.3`
2. `.github/workflows/release.yml` runs goreleaser, which cuts the GitHub Release with
   cross-compiled binaries and pushes a multi-arch image to GHCR.

The tag is the only version source — nothing in git carries a real version number, and a local
build deliberately reports `dev`.
