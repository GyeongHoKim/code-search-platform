<!-- markdownlint-disable MD033 -->
# code-search-platform

Self-hosted code search for coding agents. Point it at your internal Git host and your agents can
search every repository your team can read — from Claude Code, Codex, or anything else that speaks
the Model Context Protocol.

[한국어 README](README.ko.md)

## Why this exists

An agent that cannot see your code guesses at it. It reinvents a helper you already have, calls an
internal API with the wrong shape, and writes code that does not match how your team writes code.

The usual answer is to paste files into the context window. That does not scale past one repository,
and it puts the burden of knowing *which* file to paste on the person who asked the question — which
is exactly the thing they wanted the agent to work out.

There are already MCP servers that wrap [Zoekt](https://github.com/sourcegraph/zoekt). What is
missing is everything around one: mirroring hundreds of repositories off a corporate Git host,
keeping the index fresh, running it on a cluster, and exposing it to agents without handing every
engineer read access to code they were never granted. That plumbing is the actual work, and it is
what this repository is.

## Architecture

```mermaid
flowchart LR
    subgraph Host["Your Git host"]
        G[("Gerrit / Gitea / GitLab<br/>GitHub / Bitbucket")]
    end

    subgraph Cluster["Kubernetes"]
        I["indexer<br/><i>CronJob</i>"]
        Z["zoekt-webserver<br/><i>ClusterIP only</i>"]
        M["code-search-mcp<br/><i>the only exposed component</i>"]
        V[("index<br/>PVC")]
    end

    subgraph Agents["Agents"]
        A["Claude Code"]
        C["Codex"]
    end

    G -->|"mirror + fetch"| I
    I -->|"zoekt-git-index"| V
    Z -->|"reads"| V
    M -->|"/api/search<br/>/api/list"| Z
    A -->|"MCP over HTTP"| M
    C -->|"MCP over HTTP"| M
```

Three moving parts, and the boundary between them is the design:

| Component | What it does | Exposed? |
| --- | --- | --- |
| **indexer** | Mirrors your Git host, then rebuilds the Zoekt index | No |
| **zoekt-webserver** | Trigram index and query engine | **No — ClusterIP only** |
| **code-search-mcp** | Translates MCP tool calls into Zoekt queries and compacts the results | Yes, and only this |

Zoekt has no authentication of its own. Anything that can reach it can read every repository you
indexed, so the chart never gives it an Ingress and neither should you. The MCP server is the single
front door, which is also what makes it the natural place for authentication and audit logging.

## Quick start

```bash
helm install code-search oci://ghcr.io/gyeonghokim/charts/code-search-platform \
  --namespace code-search --create-namespace \
  --set indexer.hostKind=gerrit \
  --set indexer.hostURL=https://gerrit.example.com \
  --set indexer.credentials.existingSecret=git-codesearch
```

Or from a checkout, which is also how you review what it will create:

```bash
helm template code-search deploy/helm -f deploy/helm/values-example-gerrit.yaml
helm install code-search deploy/helm -f my-values.yaml -n code-search --create-namespace
```

The index is empty until the indexer has run. To build it now rather than waiting for the schedule:

```bash
kubectl -n code-search create job --from=cronjob/code-search-indexer first-index
kubectl -n code-search logs -f job/first-index
```

## Connecting an agent

**Claude Code**

```bash
claude mcp add --transport http code-search https://search.example.com/mcp/code-search \
  --header "Authorization: Bearer $CODE_SEARCH_AUTH_TOKEN"
```

**Codex** — in `~/.codex/config.toml`:

```toml
[mcp_servers.code-search]
url = "https://search.example.com/mcp/code-search"
# Read at connect time and sent as "Authorization: Bearer ...", so the token
# stays out of config.toml.
bearer_token_env_var = "CODE_SEARCH_AUTH_TOKEN"
```

**Locally, over stdio** — for a client that spawns the binary itself:

```json
{
  "command": "code-search-mcp",
  "env": { "CODE_SEARCH_ZOEKT_URL": "http://127.0.0.1:6070" }
}
```

## Tools

| Tool | What it answers |
| --- | --- |
| `search_code` | "Where is this string, regex or pattern?" — returns `repo:path:line` plus context lines |
| `read_file` | "Show me more of that file" — a line range, so the agent stops re-searching to see context |
| `find_symbol` | "Where is this function/type defined?" — `sym:`, backed by ctags |
| `list_repos` | "What is even in here?" — so an agent can scope a query before running it |

Results are compacted before they are returned. A raw Zoekt `SearchResult` is mostly scoring
metadata, and the reason to run a server rather than hand an agent a `curl` command is that
something has to do that compaction. Tokens are a budget.

`search_code` takes Zoekt's own [query syntax](docs/zoekt/query-syntax.md) — `repo:`, `file:`,
`lang:`, `sym:`, negation, boolean grouping. There is deliberately no second query language layered
on top of it.

**No tool takes a result limit or a context-line count.** Those come from the environment, because
the token budget belongs to whoever runs the server: a caller that could raise them would make
`CODE_SEARCH_MAX_RESULTS` a default rather than a ceiling. An agent that wants more narrows the
query or reads the file. When a search is truncated, the first line says so and how many files
matched in total.

`read_file` returns at most 400 lines per call and says where to resume. Tokens are spent the
moment they arrive, and a caller cannot know a file is eight thousand lines before asking; being
handed the first 400 costs one more round trip, being handed all of it cannot be undone.

`find_symbol` distinguishes "no such symbol" from "this index cannot answer that". `sym:` only
matches when the index was built with ctags on `$PATH`, and Zoekt answers a symbol query on an
index without it with silence rather than an error. When nothing matches, the tool checks whether
the repositories carry symbol data and says which do not. `list_repos` reports the same thing as
`symbols=yes` or `symbols=no`, so an agent can tell before it asks.

## Supported Git hosts

Mirroring is done by Zoekt's own `zoekt-mirror-*` tools, so the list is theirs:

| Host | `indexer.hostKind` | Notes |
| --- | --- | --- |
| Gerrit | `gerrit` | Fully wired: `-active`, `-name`, `-exclude`, `-http-credentials` |
| Gitea | `gitea` | Set host flags via `indexer.extraMirrorArgs` |
| GitLab | `gitlab` | Set host flags via `indexer.extraMirrorArgs` |
| GitHub | `github` | Set host flags via `indexer.extraMirrorArgs` |
| Bitbucket Server | `bitbucket-server` | Set host flags via `indexer.extraMirrorArgs` |
| Anything else | `none` | Populate `/data/repos` yourself; the CronJob only reindexes |

The mirror tools do not share one flag set — Gerrit takes `-active`, GitLab and GitHub take
`-token`, Bitbucket takes `-project`. Only the Gerrit flags are wired into the chart directly,
because those are the ones verified against a real host. For any other host, run
`zoekt-mirror-<kind> -help` and pass what it wants through `indexer.extraMirrorArgs`.

## Authentication

The `http` transport refuses to start without `CODE_SEARCH_AUTH_TOKEN`, and answers `401` to any
request that does not present it as a bearer token. There is no way to configure it off, because
this server is the only front door to an index that has no authentication of its own.

```bash
kubectl create secret generic code-search-token \
  --from-literal=token="$(openssl rand -base64 32)"

helm upgrade code-search ... --set mcp.auth.existingSecret=code-search-token
```

The value is a **comma separated list**, which is what makes rotation possible without a window in
which every caller is broken: add the new token, let engineers move to it, then drop the old one.

Two things this deliberately does not do. It does not rate limit — a static token is guessable at
whatever rate your front door allows, so bound it there (`nginx.ingress.kubernetes.io/limit-rps` or
your Traefik middleware). And it does not identify anyone: every caller holding the token is the
same caller, so the audit trail is "the token", not "who".

Both of those are answered by OAuth 2.1, which is what the MCP specification actually asks for — the
server becomes a Resource Server validating tokens from your IdP. The wiring here is already the
SDK's `auth.RequireBearerToken`, so that change replaces one `auth.TokenVerifier` in
`internal/httpauth` and touches nothing else. Until an authorisation server exists to point at, a
static token is the honest amount of machinery.

## Access control

**This is the part to get right, and it is not the regex.**

Zoekt's index has no concept of per-repository permissions. Once a repository is indexed, anything
that can call the MCP server can read it. So the question is not "who may search" but "what goes
into the index at all".

Do it with a **service account whose read permissions are the whitelist**:

1. Create a dedicated account on your Git host — `svc-codesearch` or similar.
2. Grant it read access to exactly the repositories you intend to expose to agents.
3. Give the indexer that account's credentials.

Then `indexer.include` / `indexer.exclude` are a second line of defence that keeps noise out of the
corpus. Get one of those regexes wrong and the worst case is a missing repository — not a leaked
one, because the clone simply fails. Rely on the regex alone and a typo silently widens the corpus,
and a repository created next week is included by default.

If you genuinely need per-caller filtering — different engineers seeing different repositories — that
belongs in the MCP server, which can inject a `repo:` filter per token. The chart does not do this
today.

## Configuration

The server itself is configured entirely by environment; the chart sets these for you.

| Variable | Default | What it does |
| --- | --- | --- |
| `CODE_SEARCH_ZOEKT_URL` | *(required)* | Base URL of a `zoekt-webserver` started with `-rpc` |
| `CODE_SEARCH_TRANSPORT` | `stdio` | `stdio` or `http` |
| `CODE_SEARCH_ADDR` | `127.0.0.1:8080` | Listen address, `http` transport only |
| `CODE_SEARCH_AUTH_TOKEN` | *(required for `http`)* | Comma separated bearer tokens callers must present |
| `CODE_SEARCH_TIMEOUT` | `30s` | Bounds a single request to Zoekt |
| `CODE_SEARCH_MAX_RESULTS` | `50` | Caps file matches per search (max 500) |
| `CODE_SEARCH_CONTEXT_LINES` | `3` | Lines around each match (max 50) |

`CODE_SEARCH_CONTEXT_LINES` is the knob that decides what a search costs. Three lines is enough to
recognise a match; ten is enough to read the function, and roughly triples the tokens.

## Development

```bash
mise trust && mise install   # pinned toolchain -- see mise.toml
just setup                   # git hooks
just dev-up                  # local Zoekt with this repository indexed into it
just ci                      # everything CI runs
```

`just --list` shows the rest. Every recipe runs on Linux, macOS and Windows.

Contributor notes, conventions and the layering rules are in [AGENTS.md](AGENTS.md) — written for
agents, and just as usable by people.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

The Zoekt reference material vendored under `docs/zoekt/` is copyright the Zoekt authors, also under
Apache-2.0.
