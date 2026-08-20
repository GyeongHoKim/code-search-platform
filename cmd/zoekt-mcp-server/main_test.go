package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/GyeongHoKim/zoekt-mcp-server/internal/config"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/mcpserver"
	"github.com/GyeongHoKim/zoekt-mcp-server/internal/zoekt"
)

const testToken = "test-token"

// idle is a Searcher that is never reached: these tests end at the transport.
type idle struct{}

func (idle) Search(context.Context, string, zoekt.SearchOptions) (*zoekt.SearchResult, error) {
	return &zoekt.SearchResult{}, nil
}

func (idle) List(context.Context, string) (*zoekt.RepoList, error) {
	return &zoekt.RepoList{}, nil
}

// bearer sends every request with the given Authorization header, or with none
// when the header is empty.
type bearer struct {
	header string
}

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.header != "" {
		req.Header.Set("Authorization", b.header)
	}

	return http.DefaultTransport.RoundTrip(req)
}

// serveGuarded runs the real handler wiring over a test server.
func serveGuarded(t *testing.T) string {
	t.Helper()

	cfg := &config.Config{
		ZoektURL:     "http://127.0.0.1:6070",
		Transport:    config.TransportHTTP,
		Addr:         "127.0.0.1:0",
		Timeout:      5 * time.Second,
		MaxResults:   50,
		ContextLines: 3,
		AuthTokens:   []string{testToken},
	}

	server := httptest.NewServer(httpHandler(mcpserver.New(cfg, idle{}), cfg))
	t.Cleanup(server.Close)

	return server.URL
}

// connectWith opens a session through a client that sends header.
func connectWith(t *testing.T, url, header string) (*mcp.ClientSession, error) {
	t.Helper()

	transport := &mcp.StreamableClientTransport{
		Endpoint:             url,
		HTTPClient:           &http.Client{Transport: bearer{header: header}},
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	return client.Connect(t.Context(), transport, nil)
}

func TestHTTPHandlerRejectsAnUnauthenticatedClient(t *testing.T) {
	t.Parallel()

	url := serveGuarded(t)

	for name, header := range map[string]string{
		"no header":      "",
		"a wrong token":  "Bearer 0000000000000000000000000000000",
		"a wrong scheme": "Token " + testToken,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session, err := connectWith(t, url, header)
			if err == nil {
				t.Error("Connect() error = nil, want a rejection")

				if closeErr := session.Close(); closeErr != nil {
					t.Errorf("closing session: %v", closeErr)
				}
			}
		})
	}
}

func TestHTTPHandlerServesAnAuthenticatedClient(t *testing.T) {
	t.Parallel()

	session, err := connectWith(t, serveGuarded(t), "Bearer "+testToken)
	if err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("closing session: %v", closeErr)
		}
	})

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v, want nil", err)
	}
	if len(tools.Tools) == 0 {
		t.Error("ListTools() returned no tools")
	}
}
