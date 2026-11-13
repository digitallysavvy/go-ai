package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitBGIdle waits until no background goroutine of tr is left.
func waitBGIdle(t *testing.T, tr *HTTPTransport) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		tr.bg.mu.Lock()
		running := tr.bg.running
		tr.bg.mu.Unlock()
		if running == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d background goroutines still running", running)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func queuedCount(tr *HTTPTransport) int {
	tr.receiveMu.Lock()
	defer tr.receiveMu.Unlock()
	return len(tr.receiveQueue)
}

// TS close() cancels the SSE reader, which discards the events it still
// buffers: once OnError (for a malformed event) has closed the transport, the
// valid event that arrived in the same chunk is not delivered, nor is a
// later parse failure reported. Covers the inbound GET reader and the
// reader draining a text/event-stream POST response.
func TestHTTPTransportNoEventDeliveredAfterCloseFromOnError(t *testing.T) {
	const events = "data: not json\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\"}\n\n" +
		"data: also not json\n\n"
	for _, tc := range []struct {
		name    string
		inbound bool
	}{{"inbound GET", true}, {"POST response", false}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if (r.Method == http.MethodGet) != tc.inbound {
					if r.Method == http.MethodGet {
						w.WriteHeader(http.StatusMethodNotAllowed)
					} else {
						w.WriteHeader(http.StatusAccepted)
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, events)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer srv.Close()
			var tr *HTTPTransport
			var mu sync.Mutex
			var reported []error
			tr = NewHTTPTransport(HTTPTransportConfig{
				URL:                    srv.URL,
				InitialProtocolVersion: "2025-06-18",
				OnError: func(err error) {
					mu.Lock()
					reported = append(reported, err)
					mu.Unlock()
					_ = tr.Close()
				},
			})
			if err := tr.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !tc.inbound {
				if err := tr.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "tools/list"}); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(3 * time.Second)
			for {
				mu.Lock()
				n := len(reported)
				mu.Unlock()
				if n > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("malformed event never reported")
				}
				time.Sleep(5 * time.Millisecond)
			}
			waitBGIdle(t, tr)
			if n := queuedCount(tr); n != 0 {
				t.Fatalf("%d message(s) delivered after Close", n)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(reported) != 1 {
				t.Fatalf("OnError called %d times, want 1 (only the first malformed event): %v", len(reported), reported)
			}
		})
	}
}

// gatedRoundTripper blocks every request until release is closed (or the
// request's ctx ends), then fails it with errAuthServerDown.
type gatedRoundTripper struct {
	started chan struct{}
	release chan struct{}
}

var errAuthServerDown = errors.New("auth server down")

func (g *gatedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case g.started <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
		return nil, errAuthServerDown
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// TS openInboundSse runs authorizeOnce for a 401 untied to the abort signal:
// close() neither waits for it nor cancels it, and its failure is still
// reported through onerror. Go canceled the flow with the inbound GET, so the
// report was a context cancellation instead of the flow's own failure.
func TestHTTPTransportInboundAuthSurvivesClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	rt := &gatedRoundTripper{started: make(chan struct{}, 1), release: make(chan struct{})}
	reported := make(chan error, 4)
	tr := NewHTTPTransport(HTTPTransportConfig{
		URL:                    srv.URL,
		InitialProtocolVersion: "2025-06-18",
		AuthProvider:           newFakeOAuthProvider(),
		AuthHTTPClient:         &http.Client{Transport: rt},
		OnError:                func(err error) { reported <- err },
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rt.started:
	case <-time.After(3 * time.Second):
		t.Fatal("inbound GET never started the OAuth flow")
	}
	closed := make(chan struct{})
	go func() { _ = tr.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for the OAuth flow")
	}
	close(rt.release)
	select {
	case err := <-reported:
		if !errors.Is(err, errAuthServerDown) || errors.Is(err, context.Canceled) {
			t.Fatalf("reported %v, want the OAuth flow's own failure", err)
		}
		if strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("reported %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OAuth flow failure never reported")
	}
	waitBGIdle(t, tr)
}
