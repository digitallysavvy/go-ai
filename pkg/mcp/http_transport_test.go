package mcp

import (
	"context"
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
