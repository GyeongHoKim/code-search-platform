// Package zoekt is a client for zoekt-webserver's JSON API.
//
// It is the only package in this module that speaks HTTP, and it is a leaf: it
// imports no other package from this module, so it can be tested against an
// httptest server without an MCP session and reused by anything that needs to
// query an index. .golangci.yml enforces that with a depguard rule.
//
// The API it targets is served by zoekt's internal/json package, mounted at
// /api by zoekt-webserver -- and only when that server was started with -rpc.
// Note that this is a different set of types from web/api.go, which serves the
// HTML templates.
package zoekt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors this package returns. Callers match them with errors.Is.
var (
	// ErrBadQuery means Zoekt rejected the query, almost always because it did
	// not parse. It is called out separately because it is the one failure a
	// model can fix by itself: every other error here is about the deployment.
	ErrBadQuery = errors.New("zoekt rejected the query")

	// ErrUnavailable means Zoekt could not be reached or failed internally.
	ErrUnavailable = errors.New("zoekt is unavailable")

	// ErrTimeout means the request outlived its deadline.
	ErrTimeout = errors.New("zoekt request timed out")

	// ErrEmptyQuery means the caller passed no query. Zoekt answers 400 for
	// this, but there is no reason to spend a round trip finding out.
	ErrEmptyQuery = errors.New("query is empty")
)

// maxErrorBody bounds how much of a failed response is quoted back. A proxy
// answering with an HTML error page must not be pulled into memory in full
// just to appear in a message.
const maxErrorBody = 4 << 10

// maxDrainBytes bounds how much of an unread body is discarded so that the
// transport can reuse the connection. Go will not reuse an HTTP/1.x keep-alive
// connection whose body was left unread, and a large error page would
// otherwise cost a new connection every time.
const maxDrainBytes = 1 << 20

// Options configure a Client.
//
// The pointer field leads because govet's fieldalignment wants a struct's
// pointers near the front; the reading order is BaseURL, Timeout, HTTPClient.
type Options struct {
	// HTTPClient is optional. Tests inject one; production leaves it nil and
	// gets a client configured from Timeout.
	HTTPClient *http.Client

	// BaseURL is the root of a zoekt-webserver, without a trailing slash.
	BaseURL string

	// Timeout bounds one request end to end. It is also sent to Zoekt as
	// MaxWallTime, so the server stops working at roughly the same moment the
	// client stops waiting.
	Timeout time.Duration
}

// Client queries a zoekt-webserver.
type Client struct {
	http    *http.Client
	baseURL string
	timeout time.Duration
}

// New builds a Client. The zero value of Options is not useful; BaseURL is
// validated by the config package before it gets here.
func New(opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: opts.Timeout}
	}

	return &Client{
		baseURL: strings.TrimSuffix(opts.BaseURL, "/"),
		timeout: opts.Timeout,
		http:    httpClient,
	}
}

// searchRequest is the wire shape of POST /api/search.
type searchRequest struct {
	Q    string        `json:"Q"`
	Opts SearchOptions `json:"Opts"`
}

// searchResponse is the wire shape of a successful search.
type searchResponse struct {
	Result *SearchResult `json:"Result"`
}

// listRequest is the wire shape of POST /api/list.
type listRequest struct {
	Q string `json:"Q"`
}

// listResponse is the wire shape of a successful listing.
type listResponse struct {
	List *RepoList `json:"List"`
}

// errorResponse is what Zoekt sends with any non-200.
type errorResponse struct {
	Error string `json:"Error"`
}

// Search runs a query. The query is Zoekt's own syntax; this client does not
// interpret it, and deliberately offers no second query language of its own.
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, ErrEmptyQuery
	}

	// Left at zero, Zoekt applies a 20 second default that outlives any
	// timeout the caller configured.
	if opts.MaxWallTime == 0 {
		opts.MaxWallTime = c.timeout
	}

	var resp searchResponse
	if err := c.post(ctx, "/api/search", searchRequest{Q: query, Opts: opts}, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("search returned no result object: %w", ErrUnavailable)
	}

	return resp.Result, nil
}

// List returns the repositories matching a query.
//
// Zoekt restricts this query to repo: atoms -- Searcher.List says so upstream --
// so passing anything else fails at the server rather than being ignored.
func (c *Client) List(ctx context.Context, query string) (*RepoList, error) {
	if strings.TrimSpace(query) == "" {
		return nil, ErrEmptyQuery
	}

	var resp listResponse
	if err := c.post(ctx, "/api/list", listRequest{Q: query}, &resp); err != nil {
		return nil, err
	}
	if resp.List == nil {
		return nil, fmt.Errorf("list returned no list object: %w", ErrUnavailable)
	}

	return resp.List, nil
}

// post sends body to path and decodes a successful response into out.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	// The deadline goes on the context rather than on the http.Client, so the
	// bound holds even when a caller injected a client of their own -- and so
	// that a caller's shorter deadline still wins. Mutating a client we were
	// handed would be worse than not bounding it at all.
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return requestError(path, err)
	}
	// The body is consumed below; a failure to close it is not actionable.
	defer resp.Body.Close() //nolint:errcheck // see above

	if resp.StatusCode != http.StatusOK {
		statusErr := statusError(path, resp)
		drain(resp.Body)

		return statusErr
	}

	decoder := json.NewDecoder(resp.Body)
	if decodeErr := decoder.Decode(out); decodeErr != nil {
		return fmt.Errorf("decoding %s response: %w", path, decodeErr)
	}

	// One JSON value, then the end. Anything after it means the response was
	// truncated, concatenated or came from something that is not Zoekt, and
	// accepting the prefix would report a partial answer as a complete one.
	if trailErr := decoder.Decode(new(json.RawMessage)); !errors.Is(trailErr, io.EOF) {
		return fmt.Errorf("%s: trailing data after the response body: %w", path, ErrUnavailable)
	}

	return nil
}

// drain discards what is left of a body so the transport can reuse the
// connection. A failure here costs one connection and nothing else.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes)) //nolint:errcheck // see above
}

// requestError classifies a transport failure.
//
// The three outcomes lead somewhere different, so they are not collapsed: a
// cancelled request means the caller went away, a deadline means the search
// was too slow, and anything else means the deployment is unwell. Every one of
// them keeps the original error wrapped, so errors.Is still finds
// context.Canceled underneath.
func requestError(path string, err error) error {
	// Checked before DeadlineExceeded: a context that is both cancelled and
	// past its deadline is a caller that gave up, not a slow search.
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", path, err)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", path, err, ErrTimeout)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return fmt.Errorf("%s: %w: %w", path, err, ErrTimeout)
	}

	return fmt.Errorf("%s: %w: %w", path, err, ErrUnavailable)
}

// statusError turns a non-200 into an error carrying whatever Zoekt said.
func statusError(path string, resp *http.Response) error {
	message := decodeError(resp.Body)

	// 400 is almost always a query that did not parse. Reporting it as an
	// outage would send the caller looking at the wrong thing.
	if resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("%s: %s: %w", path, message, ErrBadQuery)
	}

	return fmt.Errorf("%s: %s: %s: %w", path, resp.Status, message, ErrUnavailable)
}

// decodeError pulls Zoekt's error message out of a failed response, falling
// back to the raw body when it is not the JSON shape this client expects.
func decodeError(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBody))
	if err != nil {
		return "unreadable response body"
	}

	var decoded errorResponse
	if unmarshalErr := json.Unmarshal(raw, &decoded); unmarshalErr == nil && decoded.Error != "" {
		return decoded.Error
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "empty response body"
	}

	return trimmed
}
