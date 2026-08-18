package zoekt_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/GyeongHoKim/code-search-platform/internal/zoekt"
)

// searchResponseJSON is a recorded /api/search body, written out by hand
// rather than marshalled from this package's types.
//
// Two things it pins down that a marshalled fixture cannot. The counters sit
// beside Files instead of under a Stats key, because upstream embeds Stats --
// a named field here decodes them all to zero and no test built from our own
// structs would notice. And the byte slices arrive as base64, which is how
// encoding/json writes []byte; declaring them as string would hand the caller
// the base64 itself. ShardsScanned is here to be dropped: the client decodes a
// subset and unknown fields are not an error.
const searchResponseJSON = `{
  "Result": {
    "FileCount": 1,
    "MatchCount": 3,
    "FilesSkipped": 7,
    "ShardsScanned": 12,
    "Files": [
      {
        "FileName": "internal/config/config.go",
        "Repository": "src",
        "Language": "Go",
        "Version": "abc123",
        "Branches": ["HEAD"],
        "LineMatches": [
          {
            "Line": "CUVyck1pc3Npbmdab2VrdFVSTCA9IGVycm9ycy5OZXcoInpvZWt0IHVybCBpcyByZXF1aXJlZCIpCg==",
            "LineNumber": 52,
            "Before": "dmFyICgK",
            "After": "CS8vIG5leHQK",
            "LineFragments": [{"LineOffset": 1, "MatchLength": 18}]
          }
        ]
      }
    ]
  }
}`

// newClient starts a server running handler and returns a Client pointed at it.
func newClient(t *testing.T, handler http.HandlerFunc) *zoekt.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return zoekt.New(zoekt.Options{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		HTTPClient: server.Client(),
	})
}

// respondJSON writes v as the body of a 200.
func respondJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding fixture: %v", err)
	}
}

// respondRaw writes body verbatim, so a fixture can be the exact bytes Zoekt
// sends rather than whatever this package's types marshal to.
func respondRaw(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("writing fixture: %v", err)
	}
}

func TestSearchDecodesAResult(t *testing.T) {
	t.Parallel()

	var got struct {
		Q    string
		Opts zoekt.SearchOptions
	}

	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/search" {
			t.Errorf("path = %q, want /api/search", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}

		respondRaw(t, w, searchResponseJSON)
	})

	result, err := client.Search(t.Context(), "ErrMissingZoektURL", zoekt.SearchOptions{NumContextLines: 2})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if got.Q != "ErrMissingZoektURL" {
		t.Errorf("sent Q = %q, want %q", got.Q, "ErrMissingZoektURL")
	}
	if got.Opts.NumContextLines != 2 {
		t.Errorf("sent NumContextLines = %d, want 2", got.Opts.NumContextLines)
	}

	if len(result.Files) != 1 {
		t.Fatalf("Files = %d, want 1", len(result.Files))
	}
	file := result.Files[0]
	if diff := cmp.Diff("internal/config/config.go", file.FileName); diff != "" {
		t.Errorf("FileName mismatch (-want +got):\n%s", diff)
	}
	if len(file.LineMatches) != 1 {
		t.Fatalf("LineMatches = %d, want 1", len(file.LineMatches))
	}

	line := string(file.LineMatches[0].Line)
	want := "\tErrMissingZoektURL = errors.New(\"zoekt url is required\")\n"
	if diff := cmp.Diff(want, line); diff != "" {
		t.Errorf("Line mismatch (-want +got):\n%s", diff)
	}
	if before := string(file.LineMatches[0].Before); before != "var (\n" {
		t.Errorf("Before = %q, want %q", before, "var (\n")
	}
}

func TestSearchSendsTheClientTimeoutAsMaxWallTime(t *testing.T) {
	t.Parallel()

	var got struct {
		Opts zoekt.SearchOptions
	}

	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		respondJSON(t, w, map[string]any{"Result": zoekt.SearchResult{}})
	})

	if _, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{}); err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	// Left at zero Zoekt applies its own 20 second default, which outlives
	// every timeout this deployment configures.
	if got.Opts.MaxWallTime != 5*time.Second {
		t.Errorf("sent MaxWallTime = %v, want 5s", got.Opts.MaxWallTime)
	}
}

func TestSearchKeepsAnExplicitMaxWallTime(t *testing.T) {
	t.Parallel()

	var got struct {
		Opts zoekt.SearchOptions
	}

	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		respondJSON(t, w, map[string]any{"Result": zoekt.SearchResult{}})
	})

	opts := zoekt.SearchOptions{MaxWallTime: time.Second}
	if _, err := client.Search(t.Context(), "needle", opts); err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	if got.Opts.MaxWallTime != time.Second {
		t.Errorf("sent MaxWallTime = %v, want 1s", got.Opts.MaxWallTime)
	}
}

func TestSearchRejectsAnEmptyQueryWithoutARequest(t *testing.T) {
	t.Parallel()

	called := false
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		respondJSON(t, w, map[string]any{"Result": zoekt.SearchResult{}})
	})

	_, err := client.Search(t.Context(), "   ", zoekt.SearchOptions{})
	if !errors.Is(err, zoekt.ErrEmptyQuery) {
		t.Errorf("Search() error = %v, want ErrEmptyQuery", err)
	}
	if called {
		t.Error("an empty query reached the server; it should be refused locally")
	}
}

func TestSearchClassifiesFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		want   error
		body   string
		status int
	}{
		// The one failure a model can fix on its own, so it must not look
		// like an outage.
		"unparseable query": {
			status: http.StatusBadRequest,
			body:   `{"Error":"parse error: expected atom"}`,
			want:   zoekt.ErrBadQuery,
		},
		"server error": {
			status: http.StatusInternalServerError,
			body:   `{"Error":"shard unavailable"}`,
			want:   zoekt.ErrUnavailable,
		},
		// A proxy in the way answers HTML, not Zoekt's JSON shape.
		"html from a proxy": {
			status: http.StatusBadGateway,
			body:   "<html><body>502</body></html>",
			want:   zoekt.ErrUnavailable,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				if _, err := w.Write([]byte(tc.body)); err != nil {
					t.Errorf("writing fixture: %v", err)
				}
			})

			_, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{})
			if !errors.Is(err, tc.want) {
				t.Errorf("Search() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSearchQuotesZoektsMessage(t *testing.T) {
	t.Parallel()

	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"Error":"parse error: expected atom"}`)); err != nil {
			t.Errorf("writing fixture: %v", err)
		}
	})

	_, err := client.Search(t.Context(), "sym:(", zoekt.SearchOptions{})
	if err == nil {
		t.Fatal("Search() error = nil, want an error")
	}
	// Zoekt says why the query did not parse. Dropping that on the floor
	// leaves the caller guessing at syntax.
	if got := err.Error(); !strings.Contains(got, "expected atom") {
		t.Errorf("error = %q, want it to quote Zoekt's message", got)
	}
}

func TestSearchRejectsAMissingResultObject(t *testing.T) {
	t.Parallel()

	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, map[string]any{})
	})

	_, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{})
	if !errors.Is(err, zoekt.ErrUnavailable) {
		t.Errorf("Search() error = %v, want ErrUnavailable", err)
	}
}

func TestListDecodesRepositories(t *testing.T) {
	t.Parallel()

	var got struct{ Q string }

	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/list" {
			t.Errorf("path = %q, want /api/list", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		respondJSON(t, w, map[string]any{
			"List": zoekt.RepoList{
				Repos: []*zoekt.RepoListEntry{{
					Repository: zoekt.Repository{
						Name:       "src",
						URL:        "https://example.com/src",
						HasSymbols: true,
						Branches:   []zoekt.RepositoryBranch{{Name: "HEAD", Version: "abc123"}},
					},
					Stats: zoekt.RepoStats{Documents: 50},
				}},
			},
		})
	})

	list, err := client.List(t.Context(), "repo:src")
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if got.Q != "repo:src" {
		t.Errorf("sent Q = %q, want %q", got.Q, "repo:src")
	}
	if len(list.Repos) != 1 {
		t.Fatalf("Repos = %d, want 1", len(list.Repos))
	}
	repo := list.Repos[0]
	if repo.Repository.Name != "src" {
		t.Errorf("Name = %q, want %q", repo.Repository.Name, "src")
	}
	// Without this the caller cannot tell "no such symbol" from "this index
	// was built without ctags".
	if !repo.Repository.HasSymbols {
		t.Error("HasSymbols = false, want true")
	}
	if repo.Stats.Documents != 50 {
		t.Errorf("Documents = %d, want 50", repo.Stats.Documents)
	}
}

func TestRequestsThatOutliveTheirDeadlineAreTimeouts(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		respondJSON(t, w, map[string]any{"Result": zoekt.SearchResult{}})
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Search(ctx, "needle", zoekt.SearchOptions{})
	if !errors.Is(err, zoekt.ErrTimeout) {
		t.Errorf("Search() error = %v, want ErrTimeout", err)
	}
}

func TestSearchDecodesStatsFlattenedIntoTheResult(t *testing.T) {
	t.Parallel()

	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respondRaw(t, w, searchResponseJSON)
	})

	result, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v, want nil", err)
	}

	// Upstream embeds Stats, so its counters arrive as siblings of Files.
	// Declaring a named Stats field instead decodes every one of them to zero
	// on every search, and reports it as a successful search of nothing.
	if result.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", result.FileCount)
	}
	if result.MatchCount != 3 {
		t.Errorf("MatchCount = %d, want 3", result.MatchCount)
	}
	if result.FilesSkipped != 7 {
		t.Errorf("FilesSkipped = %d, want 7", result.FilesSkipped)
	}
}

func TestTheConfiguredTimeoutBoundsAnInjectedHTTPClient(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	// An injected client with no Timeout of its own. The bound has to come
	// from Options.Timeout, or a stalled proxy hangs a tool call forever --
	// and copying the deadline onto a client we were handed would mutate
	// something the caller owns.
	client := zoekt.New(zoekt.Options{
		BaseURL:    server.URL,
		Timeout:    50 * time.Millisecond,
		HTTPClient: &http.Client{},
	})

	_, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{})
	if !errors.Is(err, zoekt.ErrTimeout) {
		t.Errorf("Search() error = %v, want ErrTimeout", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Search() error = %v, want it to wrap context.DeadlineExceeded", err)
	}
}

func TestCancellationIsNotReportedAsATimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	client := newClient(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := client.Search(ctx, "needle", zoekt.SearchOptions{})
	// A caller that went away is not a slow search. Collapsing the two sends
	// whoever reads the log to look at Zoekt's latency for a shutdown.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Search() error = %v, want it to wrap context.Canceled", err)
	}
	if errors.Is(err, zoekt.ErrTimeout) {
		t.Errorf("Search() error = %v, want it not to be classified as a timeout", err)
	}
}

func TestALargeErrorBodyDoesNotCostAConnection(t *testing.T) {
	t.Parallel()

	// Comfortably past the prefix that gets quoted back, so there is a
	// remainder left to drain.
	body := `{"Error":"` + strings.Repeat("x", 32<<10) + `"}`

	var conns atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		respondRaw(t, w, body)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	client := zoekt.New(zoekt.Options{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		HTTPClient: server.Client(),
	})

	for range 3 {
		_, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{})
		if !errors.Is(err, zoekt.ErrUnavailable) {
			t.Fatalf("Search() error = %v, want ErrUnavailable", err)
		}
	}

	// Go will not reuse a keep-alive connection whose body was left unread, so
	// without the drain a Zoekt answering 500s pays for a new handshake on
	// every call -- at exactly the moment it can least afford one.
	if got := conns.Load(); got != 1 {
		t.Errorf("connections opened = %d, want 1", got)
	}
}

func TestSearchRejectsABodyThatIsNotOneJSONValue(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":            "",
		"truncated":        `{"Result":{"Files":[`,
		"trailing garbage": `{"Result":{"Files":[]}}garbage`,
		"two objects":      `{"Result":{"Files":[]}}{"Result":{"Files":[]}}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				respondRaw(t, w, body)
			})

			// Taking the leading object would report a truncated or
			// concatenated response as a complete answer, and an empty result
			// set is indistinguishable from a real one.
			if _, err := client.Search(t.Context(), "needle", zoekt.SearchOptions{}); err == nil {
				t.Error("Search() error = nil, want an error")
			}
		})
	}
}

func TestListRejectsAnEmptyQueryWithoutARequest(t *testing.T) {
	t.Parallel()

	called := false
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		respondJSON(t, w, map[string]any{"List": zoekt.RepoList{}})
	})

	if _, err := client.List(t.Context(), "   "); !errors.Is(err, zoekt.ErrEmptyQuery) {
		t.Errorf("List() error = %v, want ErrEmptyQuery", err)
	}
	if called {
		t.Error("an empty query reached the server; it should be refused locally")
	}
}

func TestListClassifiesFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		want   error
		body   string
		status int
	}{
		// Listing takes a query too, so it fails the same way a search does
		// when that query does not parse.
		"unparseable query": {
			status: http.StatusBadRequest,
			body:   `{"Error":"parse error: expected atom"}`,
			want:   zoekt.ErrBadQuery,
		},
		"server error": {
			status: http.StatusInternalServerError,
			body:   `{"Error":"shard unavailable"}`,
			want:   zoekt.ErrUnavailable,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				respondRaw(t, w, tc.body)
			})

			_, err := client.List(t.Context(), "repo:src")
			if !errors.Is(err, tc.want) {
				t.Errorf("List() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestListRejectsAMissingListObject(t *testing.T) {
	t.Parallel()

	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, map[string]any{})
	})

	_, err := client.List(t.Context(), "repo:src")
	if !errors.Is(err, zoekt.ErrUnavailable) {
		t.Errorf("List() error = %v, want ErrUnavailable", err)
	}
}

func TestListRejectsABodyThatIsNotOneJSONValue(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":            "",
		"truncated":        `{"List":{"Repos":[`,
		"trailing garbage": `{"List":{"Repos":[]}}garbage`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				respondRaw(t, w, body)
			})

			if _, err := client.List(t.Context(), "repo:src"); err == nil {
				t.Error("List() error = nil, want an error")
			}
		})
	}
}
