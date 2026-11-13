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
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// applyUserAgentSuffix appends the Go runtime tag to whatever "User-Agent"
// value is already present on req (set by the client's default headers
// and/or request-specific headers — normally each provider's own
// `ai-sdk-<provider>/VERSION` tag, added via version.ProviderUserAgent at
// provider construction). The final shape matches the owner's 2026-09-30
// decision (updated 2026-10-01 for TS #21344's standards-compliant format):
// `ai-sdk-<provider>/<version> go/<goVersion>`.
//
// TS's provider-utils postToApi/getFromApi additionally chain in their own
// package's `ai-sdk-provider-utils/VERSION` tag here (TS has no single Go
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

// DefaultHTTPClient is a shared HTTP client with sensible defaults.
//
// It has no whole-request timeout: TS fetch has none, so a slow
// non-streaming call (a reasoning model, an image generation) or a long
// stream must not be cut off at an arbitrary limit; callers bound a call with
// its context. As with Node's fetch (undici headersTimeout), the server must
// start responding within 5 minutes.
var DefaultHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		DisableCompression:    false,
	},
}

// Client wraps an HTTP client with additional utilities
//
// mu guards baseURL and headers, the only fields mutated after
// construction (via SetBaseURL and SetHeader respectively). Providers may
// legitimately call SetHeader to refresh an auth token on a *Client shared
// across concurrent in-flight requests (e.g. a token refresh racing a
// request goroutine); without synchronization that is a fatal, unrecoverable
// concurrent map read/write that crashes the process, not a catchable panic.
type Client struct {
	client  *http.Client
	baseURL string
	headers map[string]string

	headersFunc  func() (map[string]string, error)
	headersCtx   func(ctx context.Context) (map[string]string, error)
	maxBodyBytes int64

	mu sync.RWMutex
}

// Config contains configuration for an HTTP client
type Config struct {
	// BaseURL is the base URL for all requests
	BaseURL string

	// Headers are default headers to send with all requests
	Headers map[string]string

	// Timeout for requests (default: none; bound a call with its context)
	Timeout time.Duration

	// HTTPClient is the underlying HTTP client to use
	// If nil, DefaultHTTPClient will be used
	HTTPClient *http.Client

	// HeadersFunc, when set, is called for every request and its result is
	// layered over Headers (TS lazy `headers: () => ...`). An error aborts the
	// request before anything is sent.
	HeadersFunc func() (map[string]string, error)

	// HeadersFuncCtx is HeadersFunc for a credential resolved with the
	// request's context (TS `Resolvable` headers and API keys, TS 7716e189ee /
	// 63bf860638, such as an APIKeyFunc). It is called for every request
	// after HeadersFunc and layered over its result; an error aborts the
	// request before anything is sent.
	HeadersFuncCtx func(ctx context.Context) (map[string]string, error)

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

		headersFunc:  cfg.HeadersFunc,
		headersCtx:   cfg.HeadersFuncCtx,
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

	// SearchParams, when non-empty, is an ordered query: its pairs are
	// encoded in insertion order (TS new URLSearchParams + append/set, then
	// toString), ahead of any Query entries (sorted). When it is nil or
	// empty, the URL is built from Query alone, as before.
	SearchParams *SearchParams

	// ValidateResponse, when set, checks a successful (non-error status) body
	// in DoJSON and DoJSONResponse before it is decoded: TS createJsonResponseHandler
	// (response schema). Its error is returned as is.
	ValidateResponse func(statusCode int, headers http.Header, body []byte) error
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// HTTPStatusError preserves non-2xx response metadata for provider error
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

// HTTPStatusDetails implements providererrors.HTTPStatusDetails so
// providererrors.NewProviderError can lift the status code, headers and body
// out of the cause chain.
func (e *HTTPStatusError) HTTPStatusDetails() (int, map[string]string, []byte) {
	if e == nil {
		return 0, nil, nil
	}
	// TS APICallError.responseHeaders is extractResponseHeaders(response):
	// repeated lines joined with ", " (the last Set-Cookie line wins), each
	// byte one character.
	headers := make(map[string]string, len(e.Headers))
	for k, v := range e.Headers {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = providerutils.IsomorphicDecode(providerutils.FetchHeaderEntry(k, v))
		}
	}
	return e.StatusCode, headers, e.Body
}

// HTTPResponseHeader returns the raw response header, so error helpers can
// keep Go canonical key casing and every value of a repeated header.
func (e *HTTPStatusError) HTTPResponseHeader() http.Header {
	if e == nil {
		return nil
	}
	return e.Headers
}

// snapshot returns a point-in-time copy of the mutable client state (base
// URL and default headers) under the read lock, so callers never range over
// or concatenate the live fields while SetHeader/SetBaseURL may be running
// on another goroutine.
func (c *Client) requestDefaults(ctx context.Context) (string, map[string]string, error) {
	baseURL, headers := c.snapshot()
	if err := c.overlayRequestHeaders(ctx, headers); err != nil {
		// TS resolves the config headers (a key or token function) before
		// the request and throws its error as is: a FetchRejection, so a
		// provider's error handler returns it unchanged. A LoadAPIKeyError
		// stays unmarked: every handler already returns it as is.
		var loadKeyErr *providererrors.LoadAPIKeyError
		if errors.As(err, &loadKeyErr) {
			return "", nil, err
		}
		return "", nil, RejectFetch(err)
	}
	return baseURL, headers, nil
}

// overlayRequestHeaders layers the request-time headers (HeadersFunc, then
// HeadersFuncCtx) over headers.
func (c *Client) overlayRequestHeaders(ctx context.Context, headers map[string]string) error {
	if c.headersFunc != nil {
		extra, err := c.headersFunc()
		if err != nil {
			return err
		}
		overlayHeaders(headers, extra)
	}
	if c.headersCtx != nil {
		extra, err := c.headersCtx(ctx)
		if err != nil {
			return err
		}
		overlayHeaders(headers, extra)
	}
	return nil
}

// overlayHeaders layers extra over headers. HTTP header names are
// case-insensitive, so an existing entry that differs only in case (the
// providers' tagged static headers are lower-cased) takes the new value under
// its own spelling instead of keeping both, where map order would pick the
// winner.
func overlayHeaders(headers, extra map[string]string) {
	for k, v := range extra {
		keep := k
		if _, exact := headers[k]; !exact {
			// Keep the lexicographically first spelling already present so the
			// result does not depend on map order.
			first := ""
			for existing := range headers {
				if strings.EqualFold(existing, k) && (first == "" || existing < first) {
					first = existing
				}
			}
			if first != "" {
				keep = first
			}
		}
		for existing := range headers {
			if existing != keep && strings.EqualFold(existing, k) {
				delete(headers, existing)
			}
		}
		headers[keep] = v
	}
}

func (c *Client) snapshot() (string, map[string]string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	headers := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		headers[k] = v
	}
	return c.baseURL, headers
}

// encodeQuery is URLSearchParams.toString() for a parameter map, in sorted
// key order so the URL is deterministic. It uses the
// application/x-www-form-urlencoded byte set: ASCII alphanumerics and
// "*-._" stay literal, space is "+", everything else (including "~", which
// Go's url.QueryEscape leaves alone) is percent-encoded in upper-case hex.
func encodeQuery(q map[string]string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		formEscape(&b, k)
		b.WriteByte('=')
		formEscape(&b, q[k])
	}
	return b.String()
}

// requestQuery is the "?query" suffix of req's URL ("" with no parameters):
// the ordered SearchParams pairs first, then Query in sorted key order.
func requestQuery(req Request) string {
	var parts []string
	if req.SearchParams.Len() > 0 {
		parts = append(parts, req.SearchParams.Encode())
	}
	if len(req.Query) > 0 {
		parts = append(parts, encodeQuery(req.Query))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// EncodeQuery exposes encodeQuery to provider packages that build their own
// URLs (URLSearchParams escaping).
func EncodeQuery(q map[string]string) string { return encodeQuery(q) }

func formEscape(b *strings.Builder, s string) {
	const hex = "0123456789ABCDEF"
	// Invalid UTF-8 becomes U+FFFD, as the WHATWG encoder does.
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('+')
		case r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '*' || r == '-' || r == '.' || r == '_'):
			b.WriteByte(byte(r))
		default:
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			for _, c := range buf[:n] {
				b.WriteByte('%')
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&15])
			}
		}
	}
}

// Do performs an HTTP request
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	baseURL, defaultHeaders, err := c.requestDefaults(ctx)
	if err != nil {
		return nil, err
	}

	// Build full URL
	url := baseURL + req.Path + requestQuery(req)

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		switch body := req.Body.(type) {
		case io.Reader:
			bodyReader = body
		case []byte:
			bodyReader = bytes.NewReader(body)
		default:
			// JSON.stringify bytes: no HTML escaping of <, > and &, no
			// trailing newline (TS postJsonToApi).
			bodyBytes, err := jsonstr.Marshal(req.Body)
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
	for k, v := range defaultHeaders {
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
	httpResp, err := WithFetchRedirects(c.client).Do(httpReq)
	if err != nil {
		return nil, TransportErrorCtx(ctx, err)
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
	resp.Body = responseText(resp.Body)

	// Check for error status codes
	if !statusOK(resp.StatusCode) {
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}
	}

	// TS safeParseJSON (secureJsonParse) rejects a forbidden prototype key
	// before the response schema runs.
	if err := forbiddenPrototype(resp.Body); err != nil {
		return err
	}
	if req.ValidateResponse != nil {
		if err := req.ValidateResponse(resp.StatusCode, resp.Headers, resp.Body); err != nil {
			return err
		}
	}

	// Decode JSON response
	if err := decodeJSONBody(resp.Body, result); err != nil {
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
	resp.Body = responseText(resp.Body)
	if !statusOK(resp.StatusCode) {
		return nil, &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}
	}
	if err := forbiddenPrototype(resp.Body); err != nil {
		return nil, err
	}
	if req.ValidateResponse != nil {
		if err := req.ValidateResponse(resp.StatusCode, resp.Headers, resp.Body); err != nil {
			return nil, err
		}
	}
	if err := decodeJSONBody(resp.Body, result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return resp, nil
}

// decodeJSONBody decodes a response body into result. TS response schemas
// declare counts and timestamps as z.number(), which accepts fractional and
// exponent forms (1.5, 2.0, 1e9) that Go's integer struct fields reject; when
// the strict decode fails on such a number, the body is decoded again with
// providerutils.UnmarshalChunk, which truncates only the numbers under the
// member names the decoder reported. A body that decodes strictly is
// unchanged. When the retry also fails on a number (a number where the
// schema wants another type), the strict decode's error is returned, so it
// names the value as sent; when the retry fails on anything else, that
// error is returned, since it is the real schema violation the strict
// decode did not reach (Go reports only the first type error).
//
// The body is read as text first (responseText), as TS parses
// response.text().
func decodeJSONBody(body []byte, result interface{}) error {
	body = responseText(body)
	// zod matches object keys exactly; drop the ones encoding/json would
	// fill a field with only case-insensitively.
	body = jsonexact.Filter(body, result)
	err := jsonexact.Decode(body, result)
	var typeErr *json.UnmarshalTypeError
	if err == nil || !errors.As(err, &typeErr) || !strings.HasPrefix(typeErr.Value, "number") {
		return err
	}
	retryErr := providerutils.UnmarshalChunk(body, result)
	if retryErr == nil {
		return nil
	}
	var retryTypeErr *json.UnmarshalTypeError
	if errors.As(retryErr, &retryTypeErr) && !strings.HasPrefix(retryTypeErr.Value, "number") {
		return retryErr
	}
	return err
}

// forbiddenPrototype is the response handlers' secureJsonParse check: a body
// with a `__proto__` key or a `constructor.prototype` key fails with the TS
// JSONParseError (inside the decode error DoJSON returns for any body that
// does not parse).
func forbiddenPrototype(body []byte) error {
	if err := jsonexact.CheckPrototype(body); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", providererrors.NewJSONParseError(string(body), err))
	}
	return nil
}

// utf8BOM is the UTF-8 byte order mark TextDecoder drops.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// responseText is a JSON response body as TS response.text() reads it (the
// JSON response and error handlers, response-handler.ts): a WHATWG UTF-8
// decode that drops a leading BOM and turns each maximal invalid sequence
// into one U+FFFD. A valid body without a BOM is returned as is.
func responseText(body []byte) []byte {
	if bytes.HasPrefix(body, utf8BOM) || !utf8.Valid(body) {
		return []byte(prompt.DecodeUTF8Text(body))
	}
	return body
}

// streamClient returns the client used for streaming requests. The shared
// DefaultHTTPClient has no whole-request timeout; if one is ever set on it
// (for example by a caller mutating DefaultHTTPClient), streams use a copy
// without it so a stream still being read is not aborted. A caller-supplied
// client (or an explicit Config.Timeout) is honoured as is.
func (c *Client) streamClient() *http.Client {
	if c.client == DefaultHTTPClient && DefaultHTTPClient.Timeout > 0 {
		clone := *DefaultHTTPClient
		clone.Timeout = 0
		return &clone
	}
	return c.client
}

// statusOK is fetch's response.ok: a 2xx status. TS postToApi / getFromApi
// run the failed-response handler for anything else, including an
// unfollowed 3xx (post-to-api.ts 111, get-from-api.ts 99).
func statusOK(code int) bool {
	return code >= 200 && code < 300
}

// WithFetchRedirects returns a shallow copy of client whose redirect policy
// drops Authorization, Proxy-Authorization and Cookie once the redirect chain crosses an origin (scheme,
// host and port), as fetch does (HTTP-redirect fetch: "if request's current
// URL's origin is not same origin with locationURL's origin, remove
// Authorization"). net/http alone compares only the host, so a redirect to
// another port or scheme of the same host kept the credential. The client's
// own CheckRedirect still runs afterwards (net/http's 10-hop limit when it
// has none). The caller's client is not modified. Code that sends a
// credentialed request with its own http.Client uses it the same way.
func WithFetchRedirects(client *http.Client) *http.Client {
	cp := *client
	next := client.CheckRedirect
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if redirectChainCrossesOrigin(req, via) {
			// undici httpRedirectFetch (fetch/index.js 1351-1360) deletes
			// authorization, proxy-authorization and cookie on a
			// cross-origin hop; net/http keeps Proxy-Authorization always
			// and Cookie for the same host on another port or scheme.
			req.Header.Del("Authorization")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Cookie")
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &cp
}

// redirectChainCrossesOrigin reports whether any hop of via followed by req
// changed the origin. Fetch removes Authorization from the request on the
// first cross-origin hop and never adds it back, while net/http copies the
// first request's headers onto every hop, so every hop is checked.
func redirectChainCrossesOrigin(req *http.Request, via []*http.Request) bool {
	chain := append(append([]*http.Request{}, via...), req)
	for i := 1; i < len(chain); i++ {
		if chain[i].URL == nil || chain[i-1].URL == nil ||
			!providerutils.IsSameOrigin(chain[i].URL.String(), chain[i-1].URL.String()) {
			return true
		}
	}
	return false
}

// DoStream performs an HTTP request that returns a streaming response
func (c *Client) DoStream(ctx context.Context, req Request) (*http.Response, error) {
	baseURL, defaultHeaders, err := c.requestDefaults(ctx)
	if err != nil {
		return nil, err
	}

	// Build full URL
	url := baseURL + req.Path + requestQuery(req)

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		switch body := req.Body.(type) {
		case io.Reader:
			bodyReader = body
		case []byte:
			bodyReader = bytes.NewReader(body)
		default:
			// JSON.stringify bytes: no HTML escaping of <, > and &, no
			// trailing newline (TS postJsonToApi).
			bodyBytes, err := jsonstr.Marshal(req.Body)
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
	for k, v := range defaultHeaders {
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

	// Perform request. http.Client.Timeout covers reading the response body,
	// so a timeout set on the shared default client would cut off
	// long-running streams (TS fetch has no overall timeout; callers cancel
	// via ctx); streamClient drops it.
	httpResp, err := WithFetchRedirects(c.streamClient()).Do(httpReq)
	if err != nil {
		return nil, TransportErrorCtx(ctx, err)
	}

	// Check for error status codes
	if !statusOK(httpResp.StatusCode) {
		defer httpResp.Body.Close() //nolint:errcheck
		errBody, _ := fileutil.ReadResponseWithSizeLimit(httpResp, url, c.maxResponseBytes())
		return nil, &HTTPStatusError{
			StatusCode: httpResp.StatusCode,
			Headers:    httpResp.Header,
			Body:       responseText(errBody),
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
	c.mu.Lock()
	defer c.mu.Unlock()
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
//
// Request-time headers from Config.HeadersFunc (a lazily loaded API key) are
// layered over the defaults. When that function fails (a missing key) the
// defaults are returned as configured; Do and DoStream surface the error.
func (c *Client) Headers() map[string]string {
	c.mu.RLock()
	out := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		out[k] = v
	}
	c.mu.RUnlock()
	with := make(map[string]string, len(out))
	for k, v := range out {
		with[k] = v
	}
	if c.overlayRequestHeaders(context.Background(), with) == nil {
		return with
	}
	return out
}

// RequestHeaders is Headers for a connection that must be authenticated like
// a request (a WebSocket handshake): when Config.HeadersFunc fails (a missing
// API key) the error is returned (a LoadAPIKeyError) instead of handing back
// headers without credentials.
func (c *Client) RequestHeaders() (map[string]string, error) {
	return c.RequestHeadersCtx(context.Background())
}

// RequestHeadersCtx is RequestHeaders with the context HeadersFuncCtx
// resolves its credential with.
func (c *Client) RequestHeadersCtx(ctx context.Context) (map[string]string, error) {
	c.mu.RLock()
	out := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		out[k] = v
	}
	c.mu.RUnlock()
	if err := c.overlayRequestHeaders(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetBaseURL updates the base URL
func (c *Client) SetBaseURL(baseURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = baseURL
}

// transportError is TransportError (the name older client code uses).
func transportError(err error) error { return TransportError(err) }

// TransportError wraps a failure from an http.Client Do call. Mirrors TS
// provider-utils handleFetchError: a cancellation or the caller's own
// timeout (TS AbortError / TimeoutError) is returned unchanged, a
// FetchRejection is returned as is, and every other transport failure,
// connect, TLS and response-header timeouts included (TS
// RETRYABLE_NETWORK_ERROR_CODES ETIMEDOUT, UND_ERR_CONNECT_TIMEOUT,
// UND_ERR_HEADERS_TIMEOUT), becomes the retryable "Cannot connect to API:
// <cause>" error (IsRetryable reports true). The cause stays available to
// errors.As. Provider code that calls a raw http.Client uses it so its
// transport failures read and retry like the internal client's; with the
// request's context at hand, TransportErrorCtx tells a dial timeout from the
// caller's deadline.
func TransportError(err error) error { return transportErrorFor(err, nil) }

// TransportErrorCtx is TransportError for a request made with ctx: once ctx
// is done the failure is the caller's abort or deadline and is returned
// unchanged, and a timeout while ctx is still live is the transport's own
// (a connect, TLS or response-header timeout), which TS retries.
func TransportErrorCtx(ctx context.Context, err error) error {
	return transportErrorFor(err, ctx.Err)
}

// transportErrorFor is TransportError with ctxErr, the request context's
// Err (nil when the context is not known).
func transportErrorFor(err error, ctxErr func() error) error {
	var rejection *FetchRejection
	if errors.As(err, &rejection) {
		return rejection
	}
	if isCallerAbort(err, ctxErr) {
		return err
	}
	return &connectError{err: err}
}

// isCallerAbort reports whether a transport failure is TS isAbortError: the
// caller canceled (context.Canceled), the caller's context ended, or the
// http.Client's own Timeout fired (TS fetch with AbortSignal.timeout throws
// a TimeoutError). Without the context (ctxErr nil) a deadline counts as
// the caller's unless it is net/http's response-header timeout; an OS
// connect timeout (ETIMEDOUT) is no deadline.
func isCallerAbort(err error, ctxErr func() error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if ctxErr != nil && ctxErr() != nil {
		return true
	}
	if isClientTimeout(err) {
		return true
	}
	if ctxErr != nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout awaiting response headers")
}

// isClientTimeout reports an http.Client Timeout (net/http marks it
// "(Client.Timeout exceeded ...)" in the *url.Error text).
func isClientTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout() && strings.Contains(err.Error(), "Client.Timeout exceeded")
}

// connectError is a failure to reach the API (TS: an APICallError with
// isRetryable=true built by handleFetchError). The cause stays available to
// errors.As; a transport timeout's cause no longer matches
// errors.Is(err, context.DeadlineExceeded), since it is not the caller's
// deadline (TS: an APICallError, not a TimeoutError), so callers that stop
// on a deadline still retry it.
type connectError struct{ err error }

func (e *connectError) Error() string { return "Cannot connect to API: " + e.err.Error() }
func (e *connectError) Unwrap() error {
	if errors.Is(e.err, context.DeadlineExceeded) {
		return transportTimeoutCause{e.err}
	}
	return e.err
}

// IsRetryable reports true: TS retries connection failures.
func (e *connectError) IsRetryable() bool { return true }

// transportTimeoutCause is a transport timeout as a connectError cause: it
// reads like err (errors.As reaches err's chain, *url.Error and net.Error
// included, and errors.Is matches what err does) except that it is not
// context.DeadlineExceeded.
type transportTimeoutCause struct{ err error }

func (c transportTimeoutCause) Error() string { return c.err.Error() }

func (c transportTimeoutCause) Is(target error) bool {
	return target != context.DeadlineExceeded && errors.Is(c.err, target)
}

func (c transportTimeoutCause) As(target interface{}) bool { return errors.As(c.err, target) }

// Timeout reports true (net.Error).
func (c transportTimeoutCause) Timeout() bool { return true }

// Temporary reports true (net.Error).
func (c transportTimeoutCause) Temporary() bool { return true }

// FetchRejection marks an error a provider's own http.RoundTripper raises
// before (or instead of) a network round trip: a credential, signing or
// configuration failure. In TS such an error is thrown by the provider's
// fetch wrapper or by its headers() resolution (bedrock / anthropic-aws
// createSigV4FetchFunction, google-vertex and azure token headers), and
// provider-utils handleFetchError returns any rejection that is not a failed
// fetch unchanged: it is not the retryable APICallError "Cannot connect to
// API". transportError therefore returns a FetchRejection itself (without
// the *url.Error "Post <url>:" prefix or the connect wrapper), and a
// provider's error handler returns Err as is (FetchRejectionCause), so the
// caller gets the credential error unchanged and the core retry does not
// retry it. Only pre-network errors are marked: an error of the base
// transport the RoundTripper delegates to keeps the failed-fetch path.
//
// Err must not carry a token or secret in its text.
type FetchRejection struct{ Err error }

// Error returns Err's text.
func (e *FetchRejection) Error() string { return e.Err.Error() }

// Unwrap returns Err.
func (e *FetchRejection) Unwrap() error { return e.Err }

// RejectFetch marks err (when non-nil) as a FetchRejection.
func RejectFetch(err error) error {
	if err == nil {
		return nil
	}
	var existing *FetchRejection
	if errors.As(err, &existing) {
		return err
	}
	return &FetchRejection{Err: err}
}

// UnwrapFetchRejection returns the error a FetchRejection marks, or err
// itself. Call sites that return a request error as is (TS: postToApi lets a
// transport failure propagate unchanged) use it so the marker never reaches
// the caller.
func UnwrapFetchRejection(err error) error {
	if cause, ok := FetchRejectionCause(err); ok {
		return cause
	}
	return err
}

// FetchRejectionCause returns the error a provider RoundTripper rejected the
// request with (see FetchRejection), when err's chain holds one. Provider
// error handlers return it unwrapped, like TS.
func FetchRejectionCause(err error) (error, bool) {
	var rejection *FetchRejection
	if errors.As(err, &rejection) {
		return rejection.Err, true
	}
	return nil, false
}

// StatusProviderError builds a *providererrors.ProviderError for a non-2xx
// provider response, mirroring the TS APICallError a failedResponseHandler
// produces: the status code (and so retryability), response headers and body
// are preserved and the message is taken from the JSON error body when
// present.
func StatusProviderError(providerName string, statusCode int, headers http.Header, body []byte) error {
	cause := &HTTPStatusError{StatusCode: statusCode, Headers: headers, Body: body}
	return providererrors.NewProviderError(providerName, 0, "", cause.Error(), cause)
}
