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
}

func (c *recordingSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.called = true
	c.protocolHeader = req.Header.Get("mcp-protocol-version")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
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
		Header:     make(http.Header),
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
