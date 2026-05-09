package mcp

import (
	"io"
	"net/http"
	"strings"
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
	called bool
}

func (c *recordingSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.called = true
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
