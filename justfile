# Development tasks for zoekt-mcp-server.
#
# Every task runs on Linux, macOS and Windows. Recipes that cannot be written
# once are split with the [unix] / [windows] attributes rather than branching
# inside the recipe body.
#
# Tool versions come from mise.toml, so `mise install` is the only prerequisite.

set windows-shell := ["powershell.exe", "-NoLogo", "-NoProfile", "-Command"]
set shell := ["bash", "-euco", "pipefail"]

BIN := "zoekt-mcp-server"
EXT := if os() == "windows" { ".exe" } else { "" }
OUT := "bin" / BIN + EXT
PKG := "./cmd/zoekt-mcp-server"

# Release builds get their stamps from goreleaser; local builds report "dev"
# unless these are exported, which keeps the recipe free of shell-specific
# git plumbing.
VERSION := env("VERSION", "dev")
COMMIT := env("COMMIT", "none")
DATE := env("DATE", "unknown")

MOD := "github.com/GyeongHoKim/zoekt-mcp-server"
LDFLAGS := "-s -w" + \
    " -X " + MOD + "/internal/version.Version=" + VERSION + \
    " -X " + MOD + "/internal/version.Commit=" + COMMIT + \
    " -X " + MOD + "/internal/version.Date=" + DATE

# Show the available tasks.
default:
    @just --list

# ---------------------------------------------------------------- setup

# Install the toolchain and git hooks.
setup: && install-hooks
    mise install

# Install the lefthook git hooks into .git/hooks.
install-hooks:
    lefthook install

# ---------------------------------------------------------------- build

# Build the server into bin/.
build:
    go build -ldflags "{{ LDFLAGS }}" -o "{{ OUT }}" "{{ PKG }}"

# Run the server straight from source.
run *ARGS:
    go run "{{ PKG }}" {{ ARGS }}

# Cross-compile every release target without publishing anything.
build-all:
    goreleaser build --snapshot --clean

# Build the container image locally, tagged for inspection only.
image:
    docker build -t zoekt-mcp-server:dev .

# ---------------------------------------------------------------- quality

# Apply the configured formatters in place.
fmt:
    golangci-lint fmt

# Fail if anything is not formatted.
fmt-check:
    golangci-lint fmt --diff

# Run the linters.
lint:
    golangci-lint run

# Run the linters, fixing what can be fixed automatically.
lint-fix:
    golangci-lint run --fix

# Validate .golangci.yml against the v2 schema. Catches renamed linters.
config-verify:
    golangci-lint config verify

# Tidy the module graph.
tidy:
    go mod tidy

# Fail if go.mod or go.sum are not tidy.
tidy-check: tidy
    git diff --exit-code go.mod go.sum

# Report known vulnerabilities in the dependency graph.
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# ---------------------------------------------------------------- test

# Run the unit tests.
test:
    go test ./...

# Run the unit tests with the race detector.
test-race:
    go test -race ./...

# Run the tests and write a coverage profile. Keep the value attached on Unix,
# where this is the idiomatic Go flag spelling.
[unix]
test-cover:
    go test -coverprofile=coverage.out ./...

# PowerShell/native Windows argument handling in this toolchain can split the
# suffix of `-coverprofile=coverage.out` into a separate `.out` package
# argument, so the flag value is passed as its own argument here.
#
# Run the tests and write a coverage profile.
[windows]
test-cover:
    go test -coverprofile coverage.out ./...

# Open the coverage profile in a browser.
[unix]
cover-html: test-cover
    go tool cover -html=coverage.out

[windows]
cover-html: test-cover
    go tool cover -html coverage.out

# ---------------------------------------------------------------- docs

# Refetch the Zoekt API reference into docs/zoekt/ at the pinned revision.
docs-fetch:
    go run ./internal/tools/fetchdocs

# ---------------------------------------------------------------- dev loop

# Start a local Zoekt on http://127.0.0.1:6070 with a sample index.
dev-up:
    docker compose up -d --build

# Stop the local Zoekt.
dev-down:
    docker compose down -v

# Build, then drive the server through the MCP Inspector.
inspect: build
    npx @modelcontextprotocol/inspector "{{ OUT }}"

# ---------------------------------------------------------------- deploy

# Catches template errors without touching a cluster.
#
# Render the Helm chart with the example values.
helm-template:
    helm template zoekt-mcp-server deploy/helm -f deploy/helm/values-example-gerrit.yaml

# Lint the Helm chart.
#
# With the example values, like helm-template. The defaults deliberately do not
# render -- mcp.oidc has no default, so that no one deploys the corpus without
# an authorization server -- and linting them reports that refusal as if it
# were a fault.
helm-lint:
    helm lint deploy/helm -f deploy/helm/values-example-gerrit.yaml

# ---------------------------------------------------------------- housekeeping

# Remove build output. (unix)
[unix]
clean:
    rm -rf bin dist coverage.out coverage.html

# -ErrorAction SilentlyContinue hides the error but still leaves $? false, and
# powershell.exe -Command turns that into exit 1 -- so a clean checkout, where
# none of these exist yet, would fail the recipe. Only delete what is there.
#
# Remove build output.
[windows]
clean:
    foreach ($p in "bin", "dist", "coverage.out", "coverage.html") { if (Test-Path $p) { Remove-Item -Recurse -Force $p } }

# ---------------------------------------------------------------- aggregate

# Everything CI runs. Keep this identical to the CI job so they cannot drift.
ci: config-verify fmt-check lint tidy-check test-race vuln
