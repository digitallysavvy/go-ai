package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fetch gives a 204 or 205 reply a null body (node 24: response.body ===
// null), so every TS `!response.body` check fails it; net/http hands back
// http.NoBody instead (cr-r99-carry-to-tools).

func TestR99toolsHTTPSendEventStreamNullBodyStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
		}))
		transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL})
		transport.connected = true
		msg, _ := CreateRequest(1, "ping", nil)
		err := transport.Send(t.Context(), msg)
		var ce *MCPClientError
		if !errors.As(err, &ce) || ce.Message != "MCP HTTP Transport Error: text/event-stream response without body" || ce.StatusCode != status {
			t.Errorf("%d: err = %v", status, err)
		}
		srv.Close()
	}
}

func TestR99toolsHTTPInboundSSENullBodyStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		var mu sync.Mutex
		var captured *MCPClientError
		transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL,
			OnError: func(err error) {
				mu.Lock()
				defer mu.Unlock()
				var ce *MCPClientError
				if captured == nil && errors.As(err, &ce) {
					captured = ce
				}
			}})
		if err := transport.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			got := captured
			mu.Unlock()
			if got != nil {
				want := fmt.Sprintf("MCP HTTP Transport Error: GET SSE failed: %d %s", status, http.StatusText(status))
				if got.Message != want || got.StatusCode != status {
					t.Errorf("%d: error = %q (%d), want %q", status, got.Message, got.StatusCode, want)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("%d: no GET SSE failure reported", status)
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		_ = transport.Close()
		srv.Close()
	}
}

func TestR99toolsSSEStartNullBodyStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(status)
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		tr := NewSSETransport(SSETransportConfig{URL: srv.URL})
		err := tr.Connect(ctx)
		cancel()
		var ce *MCPClientError
		want := fmt.Sprintf("MCP SSE Transport Error: %d %s", status, http.StatusText(status))
		if !errors.As(err, &ce) || ce.Message != want || ce.StatusCode != status {
			t.Errorf("%d: err = %v, want %q", status, err, want)
		}
		_ = tr.Close()
		srv.Close()
	}
}
