package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// applyUserAgentSuffix appends the Go runtime tag to whatever "User-Agent"
// value is already present on req (set by the client's default headers
// and/or request-specific headers — normally each provider's own
// `ai-sdk/<provider>/VERSION` tag, added via version.ProviderUserAgent at
// provider construction). The final shape matches the owner's 2026-09-30
// decision: `ai-sdk/<provider>/<version> runtime/go/<goVersion>`.
//
// TS's provider-utils postToApi/getFromApi additionally chain in their own
// package's `ai-sdk/provider-utils/VERSION` tag here (TS has no single Go
// module equivalent of that internal package, and the owner's decision
// specifies the two-segment shape above), so this only adds the runtime
// tag, not a third "shared layer" segment. http.Header canonicalizes the
// header name for us, so this is case-insensitive with respect to however
// upstream code set it.
func applyUserAgentSuffix(h http.Header) {
	merged := providerutils.WithUserAgentSuffix(
		map[string]string{"user-agent": h.Get("User-Agent")},
		providerutils.RuntimeEnvironmentUserAgent(),
	)
	h.Set("User-Agent", merged["user-agent"])
}

// DefaultHTTPClient is a shared HTTP client with sensible defaults
var DefaultHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	},
}

// Client wraps an HTTP client with additional utilities
type Client struct {
	client  *http.Client
	baseURL string
	headers map[string]string

	maxBodyBytes int64
}

// Config contains configuration for an HTTP client
type Config struct {
	// BaseURL is the base URL for all requests
	BaseURL string

	// Headers are default headers to send with all requests
	Headers map[string]string

	// Timeout for requests (default: 60 seconds)
	Timeout time.Duration

	// HTTPClient is the underlying HTTP client to use
	// If nil, DefaultHTTPClient will be used
	HTTPClient *http.Client

	// MaxResponseBytes bounds buffered response body reads. Zero uses
	// fileutil.DefaultMaxDownloadSize (2 GiB), the TS response-handler limit.
	MaxResponseBytes int64
}

// MergeHeaders returns a new map containing each header map in order. Later
// maps override earlier maps, matching the TypeScript SDK combineHeaders helper.
func MergeHeaders(headers ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, h := range headers {
		for k, v := range h {
			merged[k] = v
		}
	}
	return merged
}

// NewClient creates a new HTTP client with the given config
func NewClient(cfg Config) *Client {
	client := cfg.HTTPClient
	if client == nil {
		// Create a new client with custom timeout if specified
		if cfg.Timeout > 0 {
			client = &http.Client{
				Timeout: cfg.Timeout,
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			}
		} else {
			client = DefaultHTTPClient
		}
	}

	return &Client{
		client:  client,
		baseURL: cfg.BaseURL,
		headers: cfg.Headers,

		maxBodyBytes: cfg.MaxResponseBytes,
	}
}

func (c *Client) maxResponseBytes() int64 {
	if c.maxBodyBytes > 0 {
		return c.maxBodyBytes
	}
	return fileutil.DefaultMaxDownloadSize
}

// HTTPClient returns the underlying HTTP client. It is intended for provider
// code that must make auxiliary requests while preserving configured transport,
// proxy, timeout, and middleware behavior.
func (c *Client) HTTPClient() *http.Client {
	return c.client
}

// Request represents an HTTP request
type Request struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    interface{}
	Query   map[string]string
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// HTTPStatusError preserves non-2xx/3xx response metadata for provider error
// adapters while keeping the historical error string stable.
type HTTPStatusError struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "HTTP <nil>"
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, string(e.Body))
}

// Do performs an HTTP request
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	// Build full URL
	url := c.baseURL + req.Path
	if len(req.Query) > 0 {
		url += "?"
		first := true
		for k, v := range req.Query {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		switch body := req.Body.(type) {
		case io.Reader:
			bodyReader = body
		case []byte:
			bodyReader = bytes.NewReader(body)
		default:
			bodyBytes, err := json.Marshal(req.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add default headers
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Tag the request's User-Agent, appending to (not replacing) whatever
	// the headers above already set. See applyUserAgentSuffix.
	applyUserAgentSuffix(httpReq.Header)

	// Set content type for JSON body
	if req.Body != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Perform request
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, transportError(err)
	}
	defer httpResp.Body.Close() //nolint:errcheck

	// Read response body with the TS size limit (response-handler.ts
	// readResponseBodyAsText → readResponseWithSizeLimit, 2 GiB default) so a
	// hostile or broken endpoint cannot exhaust memory.
	respBody, err := fileutil.ReadResponseWithSizeLimit(httpResp, url, c.maxResponseBytes())
	if err != nil {
		return nil, err
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       respBody,
	}, nil
}

// DoJSON performs an HTTP request and decodes the JSON response
func (c *Client) DoJSON(ctx context.Context, req Request, result interface{}) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}

	// Check for error status codes
	if resp.StatusCode >= 400 {
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}
	}

	// Decode JSON response
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return nil
}

// DoJSONResponse performs an HTTP request, decodes the JSON response body into result,
// and returns the raw Response (status code + headers + body).
// Use this instead of DoJSON when the caller needs response headers (e.g. to populate
// EmbeddingResult.Response).
func (c *Client) DoJSONResponse(ctx context.Context, req Request, result interface{}) (*Response, error) {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}
	}
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return resp, nil
}

// DoStream performs an HTTP request that returns a streaming response
func (c *Client) DoStream(ctx context.Context, req Request) (*http.Response, error) {
	// Build full URL
	url := c.baseURL + req.Path
	if len(req.Query) > 0 {
		url += "?"
		first := true
		for k, v := range req.Query {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		switch body := req.Body.(type) {
		case io.Reader:
			bodyReader = body
		case []byte:
			bodyReader = bytes.NewReader(body)
		default:
			bodyBytes, err := json.Marshal(req.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal request body: %w", err)
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add default headers
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Tag the request's User-Agent, appending to (not replacing) whatever
	// the headers above already set. See applyUserAgentSuffix.
	applyUserAgentSuffix(httpReq.Header)

	// Set content type for JSON body
	if req.Body != nil && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Perform request
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, transportError(err)
	}

	// Check for error status codes
	if httpResp.StatusCode >= 400 {
		defer httpResp.Body.Close() //nolint:errcheck
		errBody, _ := fileutil.ReadResponseWithSizeLimit(httpResp, url, c.maxResponseBytes())
		return nil, &HTTPStatusError{
			StatusCode: httpResp.StatusCode,
			Headers:    httpResp.Header,
			Body:       errBody,
		}
	}

	// Return the response for streaming (caller must close Body)
	return httpResp, nil
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
}

// PostJSON performs a POST request and decodes the JSON response
func (c *Client) PostJSON(ctx context.Context, path string, body, result interface{}) error {
	return c.DoJSON(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	}, result)
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
	})
}

// GetJSON performs a GET request and decodes the JSON response
func (c *Client) GetJSON(ctx context.Context, path string, result interface{}) error {
	return c.DoJSON(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
	}, result)
}

// SetHeader sets a default header for all requests
func (c *Client) SetHeader(key, value string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

// Headers returns a copy of the client's default headers (e.g.
// Authorization, provider-specific auth headers, and any custom headers
// merged in at construction). Used by callers that need to authenticate a
// non-HTTP connection (e.g. a WebSocket handshake) the same way the client
// authenticates its own requests.
func (c *Client) Headers() map[string]string {
	out := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		out[k] = v
	}
	return out
}

// SetBaseURL updates the base URL
func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}

// transportError wraps a failure from http.Client.Do. Mirrors TS
// provider-utils handleFetchError: cancellation and timeouts (TS abort
// errors) are returned unchanged, and other transport failures read
// "Cannot connect to API: <cause>". The cause stays wrapped for errors.Is/As.
func transportError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return err
	}
	return fmt.Errorf("Cannot connect to API: %w", err)
}
