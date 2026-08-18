# Zoekt API reference

Vendored verbatim from [sourcegraph/zoekt](https://github.com/sourcegraph/zoekt), licensed under the Apache License,
Version 2.0. Copyright the Zoekt authors.

**Pinned revision:** [`ecfe212954ea0d10e00e68a3605f2f68a05a303a`](https://github.com/sourcegraph/zoekt/tree/ecfe212954ea0d10e00e68a3605f2f68a05a303a)

Zoekt publishes no OpenAPI document. These files are the usable reference:
the markdown describes the endpoints and the query language, and the Go
sources are the request and response types as actually defined.

The two `_`-prefixed Go files are ignored by the go tool by virtue of
that prefix, so vendoring another project's package here cannot affect
this module's build.

| File | Upstream | Why it is here |
| --- | --- | --- |
| `json-api.md` | `doc/json-api.md` | the /api/search and /api/list endpoints, and the Opts object |
| `query-syntax.md` | `doc/query_syntax.md` | the query language every tool parameter is translated into |
| `indexing.md` | `doc/indexing.md` | how the indexer is driven, which the CronJob mirrors |
| `ctags.md` | `doc/ctags.md` | what has to be true for sym: to return anything |
| `_api.go` | `api.go` | SearchOptions, SearchResult, FileMatch and ChunkMatch as actually defined |
| `_web-api.go` | `web/api.go` | the request and response types of the JSON endpoints |

## Refreshing

Edit `pinnedRev` in `internal/tools/fetchdocs/main.go`, then:

```sh
just docs-fetch
```

Review the diff. A refresh that changes `api.go` is a change to the
contract this server is written against, not a documentation update.

Do not edit anything in this directory by hand -- `just docs-fetch`
overwrites it, and CI compares the two.
