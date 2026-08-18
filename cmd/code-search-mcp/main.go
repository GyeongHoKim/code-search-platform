// Command code-search-mcp exposes an indexed corpus of source code over the
// Model Context Protocol, so that any MCP-capable agent can search a company's
// internal repositories without leaving its session.
//
// It is a client of zoekt-webserver, which must be running with -rpc.
//
// On the stdio transport the process speaks JSON-RPC on stdout. Nothing else
// may ever be written there -- diagnostics go to stderr.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/code-search-platform/internal/config"
	"github.com/GyeongHoKim/code-search-platform/internal/mcpserver"
	"github.com/GyeongHoKim/code-search-platform/internal/version"
)

// readHeaderTimeout bounds how long a client may take to send its headers.
// Without it a single idle connection can hold a slot open indefinitely.
const readHeaderTimeout = 10 * time.Second

// shutdownGrace is how long in-flight requests get to finish once the process
// has been asked to stop.
const shutdownGrace = 10 * time.Second

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}

		log.New(os.Stderr, "code-search-mcp: ", 0).Println(err)
		os.Exit(1)
	}
}

// run parses arguments and serves, returning any error rather than exiting so
// that it stays testable.
//
// Both writers are injected because which one a message lands on is the single
// invariant this binary cannot get wrong: on stdio, stdout is the JSON-RPC
// channel and belongs to the transport alone.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("code-search-mcp", flag.ContinueOnError)
	// Usage text and parse errors are diagnostics. -version is the one thing
	// here that belongs on stdout, and it writes there explicitly below.
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version information and exit")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if *showVersion {
		if _, err := fmt.Fprintln(stdout, version.String()); err != nil {
			return fmt.Errorf("writing version: %w", err)
		}

		return nil
	}

	return serve(ctx, os.LookupEnv, stderr)
}

// serve builds the server from the environment and runs it until the client
// disconnects or the process is asked to stop.
//
// The environment and the diagnostic sink are injected so that a test can
// drive the whole wiring; main supplies the real ones.
func serve(ctx context.Context, lookup config.Lookup, stderr io.Writer) error {
	cfg, err := config.Load(lookup)
	if err != nil {
		return fmt.Errorf("reading configuration: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))

	// Ctrl-C and SIGTERM unwind the same path a disconnecting client does.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := mcpserver.New(cfg)

	switch cfg.Transport {
	case config.TransportStdio:
		return serveStdio(ctx, server, logger, cfg)
	case config.TransportHTTP:
		return serveHTTP(ctx, server, logger, cfg)
	default:
		// config.Load rejects anything else, so reaching here means the
		// validation and this switch have drifted apart.
		return fmt.Errorf("%q: %w", cfg.Transport, config.ErrUnknownTransport)
	}
}

// serveStdio connects the server to the process's own stdin and stdout.
func serveStdio(ctx context.Context, server *mcp.Server, logger *slog.Logger, cfg *config.Config) error {
	logger.Info("serving over stdio", "zoekt", cfg.ZoektURL, "version", version.Version)

	// Not mcp.Server.Run, which collapses connecting and serving into one
	// error. Failing to connect is this process failing to start; a session
	// ending is the client going away, and the two do not deserve the same
	// exit status.
	session, err := server.Connect(ctx, &mcp.StdioTransport{}, nil)
	if err != nil {
		return fmt.Errorf("connecting over stdio: %w", err)
	}

	closed := make(chan error, 1)
	go func() { closed <- session.Wait() }()

	select {
	case sessionErr := <-closed:
		if sessionErr != nil {
			logger.Info("session ended", "err", sessionErr)
		}
	case <-ctx.Done():
		if closeErr := session.Close(); closeErr != nil {
			logger.Info("closing session", "err", closeErr)
		}
		<-closed
	}

	return nil
}

// serveHTTP serves Streamable HTTP on the configured address.
//
// This is the transport a deployed instance uses: the server runs in the
// cluster and agents reach it over the network, so it is also the layer where
// authentication and audit logging belong.
func serveHTTP(ctx context.Context, server *mcp.Server, logger *slog.Logger, cfg *config.Config) error {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	listening := make(chan error, 1)
	go func() { listening <- httpServer.ListenAndServe() }()

	logger.Info("serving over http", "addr", cfg.Addr, "zoekt", cfg.ZoektURL, "version", version.Version)

	select {
	case err := <-listening:
		// A server told to stop reports this, and it is not a failure.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving on %s: %w", cfg.Addr, err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutting down: %w", err)
		}
		<-listening
	}

	return nil
}
