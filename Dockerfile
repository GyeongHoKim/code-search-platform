# Multi-stage: a static binary in a distroless runtime.
#
# The runtime image carries no shell and no package manager, which is the point:
# this process talks to one HTTP endpoint and speaks a protocol on a socket, so
# there is nothing an interactive layer would be for.

FROM golang:1.26.6-alpine AS build

WORKDIR /src

# Resolve dependencies before copying the source, so an edit that does not touch
# go.mod reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# CGO is disabled here rather than in mise.toml: a static binary is a property
# of a release build, and setting it globally would break `go test -race`.
ENV CGO_ENABLED=0

RUN go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/GyeongHoKim/zoekt-mcp-server/internal/version.Version=${VERSION} \
      -X github.com/GyeongHoKim/zoekt-mcp-server/internal/version.Commit=${COMMIT} \
      -X github.com/GyeongHoKim/zoekt-mcp-server/internal/version.Date=${DATE}" \
    -o /out/zoekt-mcp-server \
    ./cmd/zoekt-mcp-server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/zoekt-mcp-server /usr/local/bin/zoekt-mcp-server

# The deployed instance serves Streamable HTTP; stdio is for a client that
# spawns the binary itself, which is not what a container is for.
ENV ZOEKT_MCP_TRANSPORT=http
ENV ZOEKT_MCP_ADDR=0.0.0.0:8080

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/zoekt-mcp-server"]
