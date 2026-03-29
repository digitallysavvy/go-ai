package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMCPTransportRedirectDefaultError verifies that the default redirect policy
// causes an error when the MCP server returns an HTTP redirect (3xx).
func TestMCPTransportRedirectDefaultError(t *testing.T) {
	// Server that immediately redirects to a different URL.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer target.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirectSrv.Close()

	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       redirectSrv.URL,
		TimeoutMS: 5000,
		// Redirect field defaults to MCPRedirectError (zero value).
		Config: TransportConfig{},
	})
	transport.connected = true // skip Connect() to test Send() directly

	msg := &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "ping"}
	err := transport.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when server redirects and Redirect is default (error), got nil")
	}
	if !strings.Contains(err.Error(), "redirect") && !strings.Contains(err.Error(), "mcp") {
		t.Logf("got error: %v", err)
	}
}

// TestMCPTransportRedirectFollow verifies that setting Redirect: MCPRedirectFollow
// allows the transport to follow HTTP redirects automatically.
func TestMCPTransportRedirectFollow(t *testing.T) {
	// Final destination server — returns a valid JSON response.
	finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer finalSrv.Close()

	// Redirect server — sends a 302 to the final destination.
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL, http.StatusFound)
	}))
	defer redirectSrv.Close()

	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       redirectSrv.URL,
		TimeoutMS: 5000,
		Config: TransportConfig{
			Redirect: MCPRedirectFollow,
		},
	})
	transport.connected = true // skip Connect() to test Send() directly

	msg := &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "ping"}
	err := transport.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("expected redirect to be followed, got error: %v", err)
	}
}
