// Command fetchdocs vendors the Zoekt API reference into docs/zoekt/.
//
// Zoekt publishes no OpenAPI document. The usable reference is a handful of
// markdown files plus the Go types the JSON endpoints marshal, so those are
// what this fetches, pinned to one revision. Vendoring them means the reference
// is available offline, reviewable in a diff when it changes, and impossible to
// disagree with the version of Zoekt this server was written against.
//
// It is a Go program rather than a shell script or a .mjs file so that it runs
// identically on every platform without adding a second toolchain.
//
// Usage:
//
//	just docs-fetch
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// pinnedRev is the sourcegraph/zoekt revision the vendored files come from.
// Bump this to refresh, then run `just docs-fetch` and review the diff.
const pinnedRev = "ecfe212954ea0d10e00e68a3605f2f68a05a303a"

const (
	upstreamRepo = "sourcegraph/zoekt"
	upstreamURL  = "https://github.com/" + upstreamRepo
	rawBase      = "https://raw.githubusercontent.com/" + upstreamRepo + "/" + pinnedRev + "/"
	outputDir    = "docs/zoekt"
)

const (
	requestTimeout = 30 * time.Second
	dirPerm        = 0o750
	filePerm       = 0o600
)

// ErrUnexpectedStatus means the upstream server did not return 200 for a file
// that the pinned revision is supposed to contain.
var ErrUnexpectedStatus = errors.New("unexpected http status")

// document is one vendored file: where it lives upstream, what it is called
// here, and why it is worth carrying.
type document struct {
	upstream string
	local    string
	why      string
}

// documents is the manifest. Adding a line here is the only thing needed to
// vendor another file.
func documents() []document {
	return []document{
		// The two Go files are vendored under a leading underscore on purpose.
		// The go tool ignores paths beginning with "_", so these stay out of the
		// build and the lint run -- they are another project's package, importing
		// another project's modules -- while keeping the .go suffix an editor
		// needs to highlight them.
		{
			upstream: "doc/json-api.md",
			local:    "json-api.md",
			why:      "the /api/search and /api/list endpoints, and the Opts object",
		},
		{
			upstream: "doc/query_syntax.md",
			local:    "query-syntax.md",
			why:      "the query language every tool parameter is translated into",
		},
		{
			upstream: "doc/indexing.md",
			local:    "indexing.md",
			why:      "how the indexer is driven, which the CronJob mirrors",
		},
		{
			upstream: "doc/ctags.md",
			local:    "ctags.md",
			why:      "what has to be true for sym: to return anything",
		},
		{
			upstream: "api.go",
			local:    "_api.go",
			why:      "SearchOptions, SearchResult, FileMatch and ChunkMatch as actually defined",
		},
		{
			upstream: "web/api.go",
			local:    "_web-api.go",
			why:      "the request and response types of the JSON endpoints",
		},
	}
}

func main() {
	if err := run(context.Background()); err != nil {
		log.New(os.Stderr, "fetchdocs: ", 0).Println(err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := os.MkdirAll(outputDir, dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", outputDir, err)
	}

	client := &http.Client{Timeout: requestTimeout}

	docs := documents()
	for _, doc := range docs {
		body, err := fetch(ctx, client, rawBase+doc.upstream)
		if err != nil {
			return fmt.Errorf("fetching %s: %w", doc.upstream, err)
		}

		path := filepath.Join(outputDir, doc.local)
		if writeErr := os.WriteFile(path, body, filePerm); writeErr != nil {
			return fmt.Errorf("writing %s: %w", path, writeErr)
		}

		fmt.Fprintf(os.Stderr, "wrote %s (%d bytes)\n", path, len(body))
	}

	if err := writeNotice(docs); err != nil {
		return err
	}

	return nil
}

// fetch retrieves one file, refusing anything but a 200 so that a moved or
// renamed upstream path fails loudly instead of vendoring an error page.
func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting: %w", err)
	}
	// The body is fully read below; a failure to close it is not actionable.
	defer resp.Body.Close() //nolint:errcheck // see above

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %w", resp.Status, ErrUnexpectedStatus)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return body, nil
}

// writeNotice records where the vendored files came from, which revision they
// are pinned to, and how to refresh them. It is generated rather than hand
// written so the revision in it can never disagree with the one used above.
func writeNotice(docs []document) error {
	var b []byte
	add := func(format string, args ...any) {
		b = append(b, fmt.Sprintf(format, args...)...)
	}

	add("# Zoekt API reference\n\n")
	add("Vendored verbatim from [%s](%s), licensed under the Apache License,\n", upstreamRepo, upstreamURL)
	add("Version 2.0. Copyright the Zoekt authors.\n\n")
	add("**Pinned revision:** [`%s`](%s/tree/%s)\n\n", pinnedRev, upstreamURL, pinnedRev)
	add("Zoekt publishes no OpenAPI document. These files are the usable reference:\n")
	add("the markdown describes the endpoints and the query language, and the Go\n")
	add("sources are the request and response types as actually defined.\n\n")
	add("The two `_`-prefixed Go files are ignored by the go tool by virtue of\n")
	add("that prefix, so vendoring another project's package here cannot affect\n")
	add("this module's build.\n\n")
	add("| File | Upstream | Why it is here |\n| --- | --- | --- |\n")

	for _, doc := range docs {
		add("| `%s` | `%s` | %s |\n", doc.local, doc.upstream, doc.why)
	}

	add("\n## Refreshing\n\n")
	add("Edit `pinnedRev` in `internal/tools/fetchdocs/main.go`, then:\n\n")
	add("```sh\njust docs-fetch\n```\n\n")
	add("Review the diff. A refresh that changes `api.go` is a change to the\n")
	add("contract this server is written against, not a documentation update.\n\n")
	add("Do not edit anything in this directory by hand -- `just docs-fetch`\n")
	add("overwrites it, and CI compares the two.\n")

	path := filepath.Join(outputDir, "NOTICE.md")
	if err := os.WriteFile(path, b, filePerm); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
