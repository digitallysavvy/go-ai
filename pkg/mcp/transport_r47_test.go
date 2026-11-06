package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TS SseMCPTransport.send signals the POST with AbortSignal.any([transport,
// caller]), so close() aborts an in-flight POST. Go's Send must not keep
// waiting on the server after Close.
func TestSSETransportCloseAbortsInFlightSend(t *testing.T) {
	postStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		postStarted <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer close(release)

	var mu sync.Mutex
	var reported []error
	transport := NewSSETransport(SSETransportConfig{URL: server.URL + "/sse", OnError: func(err error) {
		mu.Lock()
		reported = append(reported, err)
		mu.Unlock()
	}})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"})
	}()
	select {
	case <-postStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("POST never reached the server")
	}
	_ = transport.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send returned nil after Close aborted it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send still blocked after Close; Close must abort in-flight POSTs")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 {
		t.Fatalf("OnError reports = %d, want 1 (TS reports a transport abort)", len(reported))
	}
}

// Two inbound GETs can fail at once (Connect's and one a 202 started), so
// two reconnects can be pending. TS close() returns at once (the timers see
// the aborted signal); Go's Close must stop every pending timer, not wait for
// an untracked one to fire.
func TestHTTPTransportCloseStopsEveryPendingReconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	transport := NewHTTPTransport(HTTPTransportConfig{URL: server.URL, InitialProtocolVersion: LatestLegacyProtocolVersion})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	transport.scheduleInboundSSEReconnection()
	transport.scheduleInboundSSEReconnection()

	start := time.Now()
	_ = transport.Close()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Close took %s waiting for a pending reconnect timer", elapsed)
	}
}
