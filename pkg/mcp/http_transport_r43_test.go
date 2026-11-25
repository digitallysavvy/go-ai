package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TS response.json() rejects an empty application/json body, so the request
// fails (and onerror fires) instead of waiting for a response.
func TestHTTPTransportEmptyJSONResponseFails(t *testing.T) {
	var reported []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: &staticSSEClient{status: 200, contentType: "application/json", body: strings.NewReader("")},
		OnError:   func(err error) { reported = append(reported, err) },
	})
	transport.connected = true
	msg, _ := CreateRequest(3, "tools/list", nil)
	if err := transport.Send(t.Context(), msg); err == nil {
		t.Fatal("want an error for an empty JSON body")
	}
	if len(reported) != 1 {
		t.Fatalf("OnError calls = %d, want 1", len(reported))
	}
}

// TS close() sends the session DELETE only for a started transport.
func TestHTTPTransportCloseWithoutConnectKeepsSession(t *testing.T) {
	var deletes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes.Add(1)
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer srv.Close()
	transport := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, InitialSessionID: "s1", InitialProtocolVersion: LatestLegacyProtocolVersion})
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if n := deletes.Load(); n != 0 {
		t.Fatalf("DELETE sent %d times for a transport that never connected", n)
	}
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if n := deletes.Load(); n != 1 {
		t.Fatalf("DELETE sent %d times after Connect+Close, want 1", n)
	}
}
