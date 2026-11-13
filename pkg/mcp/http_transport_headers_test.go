package mcp

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func postAndCaptureHeaders(t *testing.T, version string, config map[string]string, msg *MCPMessage) http.Header {
	t.Helper()
	var mu sync.Mutex
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed) // no inbound SSE stream
			return
		}
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	transport := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, Config: TransportConfig{Headers: config}})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer transport.Close() //nolint:errcheck
	transport.SetProtocolVersion(version)
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return got
}

// TS getStandardRequestHeaders: a modern-era request carries Mcp-Method and,
// for resources/read, tools/call and prompts/get, Mcp-Name (encoded like any
// MCP header value).
func TestHTTPTransportSendsStandardRequestHeadersInModernEra(t *testing.T) {
	call, _ := CreateRequest(1, "tools/call", map[string]interface{}{"name": "search", "arguments": map[string]interface{}{}})
	h := postAndCaptureHeaders(t, LatestProtocolVersion, nil, call)
	if h.Get("Mcp-Method") != "tools/call" || h.Get("Mcp-Name") != "search" {
		t.Fatalf("headers = %v", h)
	}

	read, _ := CreateRequest(2, "resources/read", map[string]interface{}{"uri": "ui://café"})
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, read)
	if h.Get("Mcp-Method") != "resources/read" || h.Get("Mcp-Name") != "=?base64?dWk6Ly9jYWbDqQ==?=" {
		t.Fatalf("headers = %v", h)
	}

	list, _ := CreateRequest(3, "tools/list", nil)
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, list)
	if h.Get("Mcp-Method") != "tools/list" || h.Get("Mcp-Name") != "" {
		t.Fatalf("headers = %v", h)
	}

	notification, _ := CreateNotification("notifications/initialized", nil)
	h = postAndCaptureHeaders(t, LatestProtocolVersion, nil, notification)
	if h.Get("Mcp-Method") != "" {
		t.Fatalf("a notification must not carry Mcp-Method: %v", h)
	}

	h = postAndCaptureHeaders(t, LatestLegacyProtocolVersion, nil, call)
	if h.Get("Mcp-Method") != "" || h.Get("Mcp-Name") != "" {
		t.Fatalf("legacy era must not carry standard request headers: %v", h)
	}
}

// TS commonHeaders spreads the configured headers first, so they cannot
// override the transport's own content type, accept and protocol version.
func TestHTTPTransportConfiguredHeadersDoNotOverrideProtocolHeaders(t *testing.T) {
	msg, _ := CreateRequest(1, "ping", nil)
	h := postAndCaptureHeaders(t, LatestLegacyProtocolVersion, map[string]string{
		"Content-Type":         "text/plain",
		"Accept":               "text/plain",
		"mcp-protocol-version": "1999-01-01",
		"X-Custom":             "kept",
	}, msg)
	if h.Get("Content-Type") != "application/json" || h.Get("Accept") != mcpHTTPAcceptHeader ||
		h.Get("mcp-protocol-version") != LatestLegacyProtocolVersion || h.Get("X-Custom") != "kept" {
		t.Fatalf("headers = %v", h)
	}
}
