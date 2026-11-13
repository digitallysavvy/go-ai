package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Ports mcp-http-transport.test.ts "should abort an unterminated
// initialization response on timeout" and "should bound session cleanup after
// failed initialization".
func TestHTTPInitializationTimeoutAbortsUnterminatedResponse(t *testing.T) {
	aborted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(aborted)
	}))
	defer srv.Close()

	disabled := false
	client := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: srv.URL}), MCPClientConfig{
		ProtocolVersionDiscovery: &disabled,
		InitializationOptions:    &RequestOptions{TimeoutMS: 100},
	})
	err := client.Connect(context.Background())
	if err == nil || err.Error() != "MCP client initialization timed out after 100ms" {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the unterminated response was not aborted")
	}
}

func TestHTTPInitializationTimeoutBoundsSessionCleanup(t *testing.T) {
	deleteStarted := make(chan struct{})
	deleteAborted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodDelete:
			close(deleteStarted)
			<-r.Context().Done()
			close(deleteAborted)
		default:
			var msg map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&msg)
			if msg["method"] == "initialize" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("mcp-session-id", "cleanup-session")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"jsonrpc": "2.0", "id": msg["id"],
					"result": map[string]interface{}{
						"protocolVersion": "2025-11-25", "capabilities": map[string]interface{}{},
						"serverInfo": map[string]interface{}{"name": "test-server", "version": "1.0.0"},
					},
				})
				return
			}
			http.Error(w, "failed initialized notification", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	disabled := false
	client := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: srv.URL}), MCPClientConfig{
		ProtocolVersionDiscovery: &disabled,
		InitializationOptions:    &RequestOptions{TimeoutMS: 100},
	})
	done := make(chan error, 1)
	go func() { done <- client.Connect(context.Background()) }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "MCP client initialization timed out after 100ms" {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return: session cleanup is not bounded by the initialization timeout")
	}
	select {
	case <-deleteStarted:
	default:
		t.Fatal("the session DELETE never started")
	}
	select {
	case <-deleteAborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the session DELETE was not aborted")
	}
}

// Ports mcp-sse-transport.test.ts "should abort a hanging POST with the
// request signal".
func TestSSETransportAbortsHangingPOSTWithContext(t *testing.T) {
	postStarted := make(chan struct{})
	postAborted := make(chan struct{})
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: endpoint\ndata: " + srv.URL + "/messages\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a closed connection once the body is read
		close(postStarted)
		<-r.Context().Done()
		close(postAborted)
	}))
	defer srv.Close()

	transport := NewSSETransport(SSETransportConfig{URL: srv.URL + "/sse"})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer transport.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	sent := make(chan error, 1)
	go func() {
		sent <- transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1"})
	}()
	<-postStarted
	cancel()
	select {
	case err := <-sent:
		if err == nil {
			t.Fatal("Send should fail when its context is cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return after the context was cancelled")
	}
	select {
	case <-postAborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the POST request was not aborted")
	}
}
