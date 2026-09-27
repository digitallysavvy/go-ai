package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/version"
)

func TestNewHTTPTransportUsesCustomHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 123 * time.Millisecond}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:        "http://localhost:9999/mcp",
		HTTPClient: custom,
	})
	if transport.client != custom {
		t.Fatal("expected custom HTTP client to be used")
	}
}

type recordingSSEClient struct {
	called         bool
	protocolHeader string
	acceptHeader   string
}

func (c *recordingSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.called = true
	c.protocolHeader = req.Header.Get("mcp-protocol-version")
	c.acceptHeader = req.Header.Get("Accept")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

func TestHTTPTransportUsesCustomSSEClient(t *testing.T) {
	sse := &recordingSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !sse.called {
		t.Fatal("expected custom SSE client to be used")
	}
	if sse.acceptHeader != "application/json, text/event-stream" {
		t.Fatalf("Accept = %q, want TS HTTP accept header", sse.acceptHeader)
	}
}

func TestHTTPTransportUsesNegotiatedProtocolVersionHeader(t *testing.T) {
	sse := &recordingSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
	})
	transport.connected = true
	transport.SetProtocolVersion("2025-06-18")

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if sse.protocolHeader != "2025-06-18" {
		t.Fatalf("mcp-protocol-version = %q", sse.protocolHeader)
	}
}

type responseSSEClient struct {
	header http.Header
	body   string
}

func (c responseSSEClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     c.header,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    req,
	}, nil
}

type streamingResponseClient struct {
	body io.ReadCloser
}

func (c streamingResponseClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       c.body,
		Request:    req,
	}, nil
}

func TestHTTPTransportQueuesJSONBatchResponsesLikeTypeScript(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "http://localhost:9999/mcp",
		SSEClient: responseSSEClient{
			header: http.Header{"Content-Type": []string{"application/json"}},
			body:   `[{"jsonrpc":"2.0","id":1,"result":{"first":true}},{"jsonrpc":"2.0","id":2,"result":{"second":true}}]`,
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	first, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive first error: %v", err)
	}
	second, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive second error: %v", err)
	}
	if first.ID != float64(1) || second.ID != float64(2) {
		t.Fatalf("queued IDs = %#v %#v, want 1 then 2", first.ID, second.ID)
	}
}

func TestHTTPTransportQueuesEventStreamMessagesWithoutExplicitEvent(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "http://localhost:9999/mcp",
		SSEClient: responseSSEClient{
			header: http.Header{"Content-Type": []string{"text/event-stream"}},
			body:   "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n",
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	got, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if got.ID != float64(1) || string(got.Result) != `{"ok":true}` {
		t.Fatalf("queued SSE message = %+v, want id 1 result ok", got)
	}
}

func TestHTTPTransportEventStreamSendReturnsBeforeStreamCloses(t *testing.T) {
	reader, writer := io.Pipe()
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: streamingResponseClient{body: reader},
	})
	transport.connected = true
	defer writer.Close() //nolint:errcheck

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- transport.Send(t.Context(), msg)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Send error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Send blocked waiting for SSE stream EOF")
	}

	if _, err := writer.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")); err != nil {
		t.Fatalf("write SSE event: %v", err)
	}
	got, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if got.ID != float64(1) || string(got.Result) != `{"ok":true}` {
		t.Fatalf("queued SSE message = %+v, want id 1 result ok", got)
	}
}

// TestHTTPTransportReportsMalformedPOSTResponseSSEMessage mirrors TS send()'s
// processEvents() inner catch around parseJSONRPCMessage: a malformed
// JSON-RPC message on a POST-response text/event-stream is reported via
// OnError (message contains "Failed to parse message") but does not stop the
// reader, matching TS's `this.onerror?.(e)` without rethrowing.
func TestHTTPTransportReportsMalformedPOSTResponseSSEMessage(t *testing.T) {
	reader, writer := io.Pipe()
	var mu sync.Mutex
	var errs []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: streamingResponseClient{body: reader},
		OnError: func(err error) {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		},
	})
	transport.connected = true
	defer writer.Close() //nolint:errcheck

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Syntactically invalid JSON (unlike TS, this Go port's SSE message parse
	// step is a plain unmarshal with no separate JSON-RPC shape validation,
	// matching the existing inbound-SSE malformed-message test), so only a
	// JSON syntax error takes the error path.
	if _, err := writer.Write([]byte("event: message\ndata: {not-valid-json\n\n")); err != nil {
		t.Fatalf("write malformed SSE event: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(errs)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for OnError from malformed POST-response SSE message")
		case <-time.After(time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	var clientErr *MCPClientError
	if !errors.As(errs[0], &clientErr) {
		t.Fatalf("error = %T %v, want *MCPClientError", errs[0], errs[0])
	}
	if !strings.Contains(clientErr.Message, "Failed to parse message") {
		t.Fatalf("message = %q, want it to mention Failed to parse message", clientErr.Message)
	}

	// The reader keeps running after a malformed message: a subsequent valid
	// message is still queued.
	if _, err := writer.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")); err != nil {
		t.Fatalf("write valid SSE event: %v", err)
	}
	got, err := transport.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if got.ID != float64(1) {
		t.Fatalf("queued SSE message = %+v, want id 1", got)
	}
}

type errorSSEClient struct {
	status         int
	body           string
	protocolHeader string
}

func (c *errorSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.protocolHeader = req.Header.Get("mcp-protocol-version")
	return &http.Response{
		StatusCode: c.status,
		Status:     http.StatusText(c.status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    req,
	}, nil
}

type statusOnlySSEClient struct {
	status int
}

func (c statusOnlySSEClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestHTTPTransportAcceptedResponseCompletesSend(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: statusOnlySSEClient{status: http.StatusAccepted},
	})
	transport.connected = true

	msg, err := CreateNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatalf("CreateNotification error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error for 202 Accepted = %v, want nil", err)
	}
}

func TestHTTPTransportOKNotificationCompletesWithoutJSONRPCResponse(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "http://localhost:9999/mcp",
		SSEClient: &errorSSEClient{
			status: http.StatusOK,
			body:   `{"ack":true}`,
		},
	})
	transport.connected = true

	msg, err := CreateNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatalf("CreateNotification error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error for 200 notification ack = %v, want nil", err)
	}
}

func TestHTTPTransportNon2xxReturnsStructuredMCPClientError(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusServiceUnavailable, body: "Service Unavailable"}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
	})
	transport.connected = true
	transport.SetProtocolVersion("2025-06-18")

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	err = transport.Send(t.Context(), msg)
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("Send error = %T %v, want *MCPClientError", err, err)
	}
	if clientErr.StatusCode != http.StatusServiceUnavailable || clientErr.URL != "http://localhost:9999/mcp" || clientErr.ResponseBody != "Service Unavailable" {
		t.Fatalf("structured HTTP fields mismatch: %#v", clientErr)
	}
	if clientErr.Code != 0 {
		t.Fatalf("Code = %d, want JSON-RPC zero value for HTTP transport error", clientErr.Code)
	}
	if sse.protocolHeader != "2025-06-18" {
		t.Fatalf("mcp-protocol-version = %q, want negotiated version", sse.protocolHeader)
	}
}

// TestHTTPTransportPOST404MessageWithoutSessionID mirrors TS send()'s "does
// not support HTTP transport" 404 suffix, which fires when the failed
// request carried no session id (e.g. the `initialize` request, or any
// request before a session was ever established).
func TestHTTPTransportPOST404MessageWithoutSessionID(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusNotFound, body: "Not Found"}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
	})
	transport.connected = true

	msg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	err = transport.Send(t.Context(), msg)
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("Send error = %T %v, want *MCPClientError", err, err)
	}
	if !strings.Contains(clientErr.Message, "does not support HTTP transport") {
		t.Fatalf("message = %q, want it to mention does not support HTTP transport", clientErr.Message)
	}
	if strings.Contains(clientErr.Message, "session expired") {
		t.Fatalf("message = %q, should not mention session expired without a session id", clientErr.Message)
	}
}

// TestHTTPTransportPOST404MessageWithSessionID mirrors TS send()'s
// session-expired 404 suffix, which fires when the failed request carried a
// (now stale) session id.
func TestHTTPTransportPOST404MessageWithSessionID(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusNotFound, body: "Not Found"}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
	})
	transport.connected = true

	msg, err := CreateRequest(1, "tools/list", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	err = transport.Send(t.Context(), msg)
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("Send error = %T %v, want *MCPClientError", err, err)
	}
	if !strings.Contains(clientErr.Message, "The MCP session expired") {
		t.Fatalf("message = %q, want it to mention the session expired", clientErr.Message)
	}
	if strings.Contains(clientErr.Message, "does not support HTTP transport") {
		t.Fatalf("message = %q, should not mention does-not-support-transport when a session id was sent", clientErr.Message)
	}
}

// TestHTTPTransportReportsErrorOnNon2xxPOST mirrors TS's "should report HTTP
// errors from POST": a non-ok POST response is reported via OnError with the
// same *MCPClientError returned to the caller, matching TS send()'s
// `this.onerror?.(error); throw error;` in the `!response.ok` branch.
func TestHTTPTransportReportsErrorOnNon2xxPOST(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusInternalServerError, body: "Internal Server Error"}
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	msg, err := CreateRequest(3, "test", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	sendErr := transport.Send(t.Context(), msg)
	if sendErr == nil {
		t.Fatal("expected an error for the 500 response")
	}
	if !strings.Contains(sendErr.Error(), "POSTing to endpoint") {
		t.Fatalf("Send error = %v, want it to mention POSTing to endpoint", sendErr)
	}

	if captured == nil {
		t.Fatal("expected OnError to be called")
	}
	var clientErr *MCPClientError
	if !errors.As(captured, &clientErr) {
		t.Fatalf("OnError error = %T %v, want *MCPClientError", captured, captured)
	}
	if clientErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", clientErr.StatusCode)
	}
	if clientErr.URL != "http://localhost:9999/mcp" {
		t.Fatalf("URL = %q, want the transport URL", clientErr.URL)
	}
	if clientErr.ResponseBody != "Internal Server Error" {
		t.Fatalf("ResponseBody = %q, want Internal Server Error", clientErr.ResponseBody)
	}
	if !errors.Is(sendErr, captured) && sendErr.Error() != captured.Error() {
		t.Fatalf("Send() error and OnError error should describe the same failure: %v vs %v", sendErr, captured)
	}
}

// TestHTTPTransportReportsErrorOn404NotSupportedFromPOST mirrors TS's
// "should expose HTTP status, URL, and response body on 404 from POST".
func TestHTTPTransportReportsErrorOn404NotSupportedFromPOST(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusNotFound, body: "Not Found"}
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	msg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err == nil {
		t.Fatal("expected an error for the 404 response")
	}

	if captured == nil {
		t.Fatal("expected OnError to be called")
	}
	var clientErr *MCPClientError
	if !errors.As(captured, &clientErr) {
		t.Fatalf("OnError error = %T %v, want *MCPClientError", captured, captured)
	}
	if clientErr.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", clientErr.StatusCode)
	}
	if clientErr.URL != "http://localhost:9999/mcp" {
		t.Fatalf("URL = %q, want the transport URL", clientErr.URL)
	}
	if clientErr.ResponseBody != "Not Found" {
		t.Fatalf("ResponseBody = %q, want Not Found", clientErr.ResponseBody)
	}
	if !strings.Contains(clientErr.Message, "does not support HTTP transport") {
		t.Fatalf("message = %q, want it to mention does not support HTTP transport", clientErr.Message)
	}
}

// TestHTTPTransportReportsErrorOnPOSTNetworkFailure mirrors TS's catch-all
// `this.onerror?.(error); throw error;` around a failed fetch() call: a
// transport-level network failure on the POST path is also reported via
// OnError.
func TestHTTPTransportReportsErrorOnPOSTNetworkFailure(t *testing.T) {
	cause := errors.New("dial refused")
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: networkErrorSSEClient{err: cause},
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err == nil {
		t.Fatal("expected a network error")
	}

	if captured == nil {
		t.Fatal("expected OnError to be called for the network failure")
	}
	if !errors.Is(captured, cause) {
		t.Fatalf("OnError error should wrap cause %v: %v", cause, captured)
	}
}

// TestHTTPTransportSetsUserAgentOnAllRequestKinds mirrors TS commonHeaders'
// User-Agent application (withUserAgentSuffix/getRuntimeEnvironmentUserAgent),
// which covers every request the transport makes: POST send, GET inbound
// SSE, and DELETE close.
func TestHTTPTransportSetsUserAgentOnAllRequestKinds(t *testing.T) {
	sse := &userAgentRecordingSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	sse.mu.Lock()
	defer sse.mu.Unlock()
	for _, req := range sse.requests {
		if got := req.Header.Get("User-Agent"); got != version.UserAgent() {
			t.Fatalf("%s User-Agent = %q, want %q", req.Method, got, version.UserAgent())
		}
	}
	if len(sse.requests) < 2 {
		t.Fatalf("expected at least a POST and a DELETE request, got %d", len(sse.requests))
	}
}

type userAgentRecordingSSEClient struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (c *userAgentRecordingSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req.Clone(req.Context()))
	c.mu.Unlock()
	if req.Method == http.MethodDelete {
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

type networkErrorSSEClient struct {
	err error
}

func (c networkErrorSSEClient) Do(req *http.Request) (*http.Response, error) {
	return nil, c.err
}

func TestHTTPTransportNetworkErrorRemainsTransportError(t *testing.T) {
	cause := errors.New("dial refused")
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: networkErrorSSEClient{err: cause},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	err = transport.Send(t.Context(), msg)
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Send error = %T %v, want *TransportError", err, err)
	}
	var clientErr *MCPClientError
	if errors.As(err, &clientErr) {
		t.Fatalf("network error should not become MCPClientError: %#v", clientErr)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("Send error should wrap cause %v: %v", cause, err)
	}
}

type okSSEClient struct{}

func (c okSSEClient) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

func TestHTTPTransportDeduplicatesConcurrentOAuthRefresh(t *testing.T) {
	var refreshes int32
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: okSSEClient{},
		OAuth: &OAuthConfig{
			AccessToken: "expired",
			ExpiresAt:   time.Now().Add(-time.Minute),
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				atomic.AddInt32(&refreshes, 1)
				time.Sleep(50 * time.Millisecond)
				return "fresh", time.Hour, nil
			},
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := transport.Send(t.Context(), msg); err != nil {
				t.Errorf("Send error: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
}

type countingSSEClient struct {
	calls int32
}

func (c *countingSSEClient) Do(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&c.calls, 1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

func TestHTTPTransportInitialOAuthFetchCompletesBeforeConnectReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: "http://localhost:9999/mcp",
		OAuth: &OAuthConfig{
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				close(started)
				select {
				case <-release:
					return "initial-token", time.Hour, nil
				case <-ctx.Done():
					return "", 0, ctx.Err()
				}
			},
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- transport.Connect(t.Context())
	}()

	<-started
	select {
	case err := <-done:
		t.Fatalf("Connect returned before OAuth token fetch completed: %v", err)
	default:
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	if transport.oauth.AccessToken != "initial-token" {
		t.Fatalf("access token = %q", transport.oauth.AccessToken)
	}
}

func TestHTTPTransportRefreshWaitsAndDoesNotSendAfterAuthFailure(t *testing.T) {
	sse := &countingSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OAuth: &OAuthConfig{
			AccessToken: "expired",
			ExpiresAt:   time.Now().Add(-time.Minute),
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				return "", 0, errors.New("auth failed")
			},
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	err = transport.Send(t.Context(), msg)
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("Send error = %v, want auth failed", err)
	}
	if got := atomic.LoadInt32(&sse.calls); got != 0 {
		t.Fatalf("HTTP request count = %d, want 0", got)
	}
}

// TestHTTPTransportSuppressesOnErrorWhenCallerContextAlreadyCanceled mirrors
// TS send()'s outer catch guard, `if (options?.signal?.aborted) { throw
// error; }`: when the caller's own context/signal is already canceled, the
// resulting error is still returned but must not be reported via OnError.
func TestHTTPTransportSuppressesOnErrorWhenCallerContextAlreadyCanceled(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusInternalServerError, body: "Internal Server Error"}
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg, err := CreateRequest(1, "test", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(ctx, msg); err == nil {
		t.Fatal("expected an error for the 500 response")
	}
	if captured != nil {
		t.Fatalf("OnError should not fire once the caller's context is canceled, got: %v", captured)
	}
}

// TestHTTPTransportReportsOAuthRetryRefreshFailureOnce guards against
// double-reporting when a 401 response triggers the retry-refresh path (the
// scenario TS's authorizeOnce catch + outer catch double-reports for its own
// authProvider flow): the refresh failure must be reported via OnError
// exactly once.
func TestHTTPTransportReportsOAuthRetryRefreshFailureOnce(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusUnauthorized, body: "Unauthorized"}
	var mu sync.Mutex
	var captured []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError: func(err error) {
			mu.Lock()
			captured = append(captured, err)
			mu.Unlock()
		},
		OAuth: &OAuthConfig{
			AccessToken: "still-valid-until-server-said-no",
			ExpiresAt:   time.Now().Add(time.Hour),
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				return "", 0, errors.New("refresh rejected")
			},
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	sendErr := transport.Send(t.Context(), msg)
	if sendErr == nil || !strings.Contains(sendErr.Error(), "refresh rejected") {
		t.Fatalf("Send error = %v, want it to mention refresh rejected", sendErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("OnError calls = %d, want exactly 1 (no double-reporting): %v", len(captured), captured)
	}
}

// TestHTTPTransportReportsRepeated401AfterSuccessfulRefreshOnce guards
// against double-reporting when the retry-refresh succeeds but the retried
// request still comes back 401 (refresh produced a token the server still
// rejects): the final non-2xx error must be reported via OnError exactly
// once, not once per attempt.
func TestHTTPTransportReportsRepeated401AfterSuccessfulRefreshOnce(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusUnauthorized, body: "Unauthorized"}
	var mu sync.Mutex
	var captured []error
	var refreshes int32
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError: func(err error) {
			mu.Lock()
			captured = append(captured, err)
			mu.Unlock()
		},
		OAuth: &OAuthConfig{
			AccessToken: "stale",
			ExpiresAt:   time.Now().Add(time.Hour),
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				atomic.AddInt32(&refreshes, 1)
				return "still-rejected", time.Hour, nil
			},
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	sendErr := transport.Send(t.Context(), msg)
	if sendErr == nil {
		t.Fatal("expected an error: server keeps returning 401")
	}
	var clientErr *MCPClientError
	if !errors.As(sendErr, &clientErr) || clientErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Send error = %T %v, want *MCPClientError with StatusCode 401", sendErr, sendErr)
	}
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("refresh calls = %d, want exactly 1 (retried once, per TS's single-retry attempt(true))", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("OnError calls = %d, want exactly 1 (no double-reporting across the retry): %v", len(captured), captured)
	}
}

// closeSignalingReadCloser wraps a reader and signals (closes a channel) the
// moment its Close() is called, letting a test detect that a reader
// goroutine actually ran its deferred body.Close() and exited, rather than
// leaking.
type closeSignalingReadCloser struct {
	io.Reader
	closed chan struct{}
}

func (c *closeSignalingReadCloser) Close() error {
	close(c.closed)
	return nil
}

// TestHTTPTransportEventStreamReaderExitsSilentlyWhenContextCanceled mirrors
// TS send()'s processEvents() outer catch guard
// (`options?.signal?.aborted... return;`): once the caller's context is
// canceled, a subsequent read failure on the POST-response event-stream must
// not be reported via OnError, and the reader goroutine must still exit
// (closing the response body) rather than leak.
func TestHTTPTransportEventStreamReaderExitsSilentlyWhenContextCanceled(t *testing.T) {
	reader, writer := io.Pipe()
	wrapped := &closeSignalingReadCloser{Reader: reader, closed: make(chan struct{})}
	var mu sync.Mutex
	var captured []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: streamingResponseClient{body: wrapped},
		OnError: func(err error) {
			mu.Lock()
			captured = append(captured, err)
			mu.Unlock()
		},
	})
	transport.connected = true

	ctx, cancel := context.WithCancel(context.Background())
	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(ctx, msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Cancel the caller's context, then force the read to fail (a real HTTP
	// round-tripper would abort the read itself on context cancellation; the
	// mock SSEClient does not, so the failure is injected directly).
	cancel()
	if err := writer.CloseWithError(errors.New("connection reset")); err != nil {
		t.Fatalf("CloseWithError: %v", err)
	}

	select {
	case <-wrapped.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the SSE reader goroutine to exit (body.Close never called)")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 0 {
		t.Fatalf("OnError should not fire once the caller's context is canceled, got: %v", captured)
	}
}

// TestHTTPTransportEventStreamReaderReportsReadFailureWhenContextLive is the
// counterpart to the canceled-context test above: a read failure while the
// caller's context is still live must be reported via OnError, matching TS's
// processEvents() outer catch (`this.onerror?.(error)`).
func TestHTTPTransportEventStreamReaderReportsReadFailureWhenContextLive(t *testing.T) {
	reader, writer := io.Pipe()
	wrapped := &closeSignalingReadCloser{Reader: reader, closed: make(chan struct{})}
	var mu sync.Mutex
	var captured []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: streamingResponseClient{body: wrapped},
		OnError: func(err error) {
			mu.Lock()
			captured = append(captured, err)
			mu.Unlock()
		},
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	if err := writer.CloseWithError(errors.New("connection reset")); err != nil {
		t.Fatalf("CloseWithError: %v", err)
	}

	select {
	case <-wrapped.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the SSE reader goroutine to exit (body.Close never called)")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("OnError calls = %d, want exactly 1 for a live-context read failure: %v", len(captured), captured)
	}
	if !strings.Contains(captured[0].Error(), "failed to read event-stream response") {
		t.Fatalf("error = %v, want it to mention failed to read event-stream response", captured[0])
	}
}
