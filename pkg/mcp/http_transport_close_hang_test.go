package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A server that never answers the session DELETE must not hang Close forever
// (TS close() is bounded by fetch's own headers timeout).
func TestHTTPTransportCloseBoundsHungSessionDelete(t *testing.T) {
	prev := sessionTerminateTimeout
	sessionTerminateTimeout = 150 * time.Millisecond
	defer func() { sessionTerminateTimeout = prev }()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	transport := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, InitialSessionID: "s1"})
	transport.connected = true

	done := make(chan error, 1)
	go func() { done <- transport.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close hung on a server that never answers the session DELETE")
	}
}
