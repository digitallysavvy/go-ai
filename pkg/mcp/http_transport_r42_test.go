package mcp

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TS parses a failed response body as one JSON-RPC message, so an array
// holding one error is an HTTP transport error, not a delivered response.
func TestHTTPTransportFailedResponseArrayBodyIsHTTPError(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: &staticSSEClient{status: 400, contentType: "application/json", body: strings.NewReader(`[{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"bad"}}]`)},
	})
	transport.connected = true
	msg, _ := CreateRequest(7, "tools/call", nil)
	if err := transport.Send(t.Context(), msg); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("err = %v, want the HTTP 400 transport error", err)
	}
}

// A Connect after Close starts a fresh transport lifecycle; requests no
// longer derive from the canceled one.
func TestHTTPTransportReconnectAfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer srv.Close()
	transport := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL})
	msg, _ := CreateRequest(1, "ping", nil)
	for i := 0; i < 2; i++ {
		if err := transport.Connect(t.Context()); err != nil {
			t.Fatalf("connect %d: %v", i, err)
		}
		if err := transport.Send(t.Context(), msg); err != nil {
			t.Fatalf("send after connect %d: %v", i, err)
		}
		if _, err := transport.Receive(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := transport.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Parse failures carry the parse error as the MCPClientError cause (TS
// `cause`), and the GET SSE failure reports no response body (TS sets only
// status and URL).
func TestHTTPTransportInboundSSEErrorShapes(t *testing.T) {
	var mu sync.Mutex
	var reported []error
	done := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret body"))
	}))
	defer srv.Close()
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL: srv.URL, InitialProtocolVersion: "2025-06-18",
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
			done <- struct{}{}
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-done
	_ = transport.Close()
	mu.Lock()
	defer mu.Unlock()
	var ce *MCPClientError
	if len(reported) == 0 || !errors.As(reported[0], &ce) || !strings.Contains(ce.Message, "GET SSE failed: 500") {
		t.Fatalf("reported = %v", reported)
	}
	if ce.ResponseBody != "" || ce.StatusCode != 500 || ce.URL != srv.URL {
		t.Fatalf("error = %+v, want status and URL only", ce)
	}

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte("data: not-json\n\n"))
		_ = pw.Close()
	}()
	var got error
	h := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, OnError: func(err error) { got = err }})
	h.readMCPHTTPSSEMessages(t.Context(), func() {}, pr)
	if !errors.As(got, &ce) || ce.Message != "MCP HTTP Transport Error: Failed to parse message" || ce.Cause == nil || ce.Data != nil {
		t.Fatalf("parse error = %#v", got)
	}
}
