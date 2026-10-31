package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// errBody is a response body whose reads fail.
type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
func (errBody) Close() error             { return nil }

// heldBody is a response body that blocks until the request context ends.
func heldBody(ctx context.Context) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		<-ctx.Done()
		_ = w.CloseWithError(ctx.Err())
	}()
	return r
}

func eventStreamResponse(body io.ReadCloser, header http.Header) *http.Response {
	h := http.Header{"Content-Type": []string{"text/event-stream"}}
	for k, v := range header {
		h[k] = v
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: h, Body: body}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("Close called from %s hung", what)
	}
}

// A read error on the inbound GET is reported from its goroutine: Close from
// that OnError must not deadlock, and (TS checks the abort after onerror) the
// failed stream schedules no reconnect, even once a new Connect has reset the
// transport.
func TestR50IntegratorInboundReadErrorCloseAndReconnect(t *testing.T) {
	var gets atomic.Int32
	var tr *HTTPTransport
	closed := make(chan struct{})
	var once sync.Once
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:                    "http://example.test/mcp",
		InitialProtocolVersion: "2025-06-18",
		HTTPClient: oauthTestClient(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			if gets.Add(1) == 1 {
				return eventStreamResponse(errBody{}, nil), nil
			}
			return eventStreamResponse(heldBody(req.Context()), nil), nil
		}),
		OnError: func(err error) {
			once.Do(func() {
				_ = tr.Close()
				if err := tr.Connect(context.Background()); err != nil {
					t.Errorf("reconnect: %v", err)
				}
				close(closed)
			})
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, closed, "OnError after an inbound read error")
	defer tr.Close() //nolint:errcheck
	// The first reconnect delay is 1 s.
	time.Sleep(inboundSSEInitialReconnectionDelay + 500*time.Millisecond)
	if got := gets.Load(); got != 2 {
		t.Fatalf("inbound GETs = %d, want 2 (the failed stream must not schedule a reconnect after onerror closed it)", got)
	}
	if !tr.IsConnected() {
		t.Fatal("not connected after the reconnect")
	}
}

// OnSessionIDChange fired by the inbound GET's response header may close the
// transport.
func TestR50IntegratorCloseFromInboundSessionIDChange(t *testing.T) {
	var tr *HTTPTransport
	closed := make(chan struct{})
	var once sync.Once
	f := false
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:                     "http://example.test/mcp",
		InitialProtocolVersion:  "2025-06-18",
		TerminateSessionOnClose: &f,
		HTTPClient: oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return eventStreamResponse(heldBody(req.Context()), http.Header{"Mcp-Session-Id": []string{"s2"}}), nil
		}),
		OnSessionIDChange: func(id string) {
			if id != "s2" {
				return
			}
			_ = tr.Close()
			once.Do(func() { close(closed) })
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, closed, "OnSessionIDChange on the inbound GET")
	if tr.IsConnected() {
		t.Fatal("connected after Close")
	}
}

// A read error on a text/event-stream POST response is reported from the
// reader goroutine.
func TestR50IntegratorCloseFromPOSTResponseReadError(t *testing.T) {
	var tr *HTTPTransport
	closed := make(chan struct{})
	var once sync.Once
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:                    "http://example.test/mcp",
		InitialProtocolVersion: "2025-06-18",
		HTTPClient: oauthTestClient(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodGet {
				return &http.Response{StatusCode: http.StatusMethodNotAllowed, Status: "405 Method Not Allowed", Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			return eventStreamResponse(errBody{}, nil), nil
		}),
		OnError: func(err error) {
			if !strings.Contains(err.Error(), "failed to read event-stream response") {
				return
			}
			_ = tr.Close()
			once.Do(func() { close(closed) })
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tr.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "tools/list"}); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, closed, "OnError after a POST-response read error")
}

// "Maximum reconnection attempts" is reported from a reconnect timer's
// goroutine.
func TestR50IntegratorCloseFromMaxReconnectError(t *testing.T) {
	var tr *HTTPTransport
	closed := make(chan struct{})
	var once sync.Once
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:                    "http://example.test/mcp",
		InitialProtocolVersion: "2025-06-18",
		HTTPClient: oauthTestClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial refused")
		}),
		OnError: func(err error) {
			if !strings.Contains(err.Error(), "Maximum reconnection attempts") {
				return
			}
			_ = tr.Close()
			once.Do(func() { close(closed) })
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, closed, "OnError after the reconnect limit")
}

// TS attempt() reports a failed token refresh on a 401 through onerror
// before throwing it.
func TestR50IntegratorSSESendReportsRefreshFailure(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	var mu sync.Mutex
	var reported []error
	tr := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &oauthStaleSSEClient{staleToken: "old", getStream: reader},
		OAuth: &OAuthConfig{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(time.Hour),
			RefreshTokenFunc: func(context.Context, *OAuthConfig) (string, time.Duration, error) {
				return "", 0, errors.New("refresh denied")
			},
		},
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tr.Connect(ctx) }()
	_, _ = writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n"))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	err := tr.Send(ctx, &MCPMessage{JSONRpc: "2.0", ID: "1", Method: "test"})
	if err == nil || !strings.Contains(err.Error(), "refresh denied") {
		t.Fatalf("Send error = %v, want the refresh failure", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || reported[0] != err {
		t.Fatalf("OnError got %v, want exactly the returned error", reported)
	}
}

// A message that cannot be encoded fails inside TS attempt()'s try
// (JSON.stringify), so it is reported through onerror too.
func TestR50IntegratorSSESendReportsMarshalFailure(t *testing.T) {
	srv := newEndpointSSEServer(t)
	var mu sync.Mutex
	var reported []error
	tr := NewSSETransport(SSETransportConfig{
		URL: srv.URL + "/sse",
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	err := tr.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", Params: json.RawMessage("{bad")})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal message") {
		t.Fatalf("Send error = %v, want the marshal failure", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || reported[0] != err {
		t.Fatalf("OnError got %v, want exactly the returned error", reported)
	}
}

// Close skips only a goroutine that is inside a user callback: on a
// background goroutine, the session id is recorded before the callback
// starts, so Close (which reads it for the DELETE) never sees a stale id.
func TestR50IntegratorSessionIDWrittenBeforeCallbackWindow(t *testing.T) {
	var tr *HTTPTransport
	var seen []string
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:               "http://example.test/mcp",
		InitialSessionID:  "s1",
		OnSessionIDChange: func(string) {},
		OnSessionExpired:  func(string) {},
	})
	run := func(fn func()) {
		tr.mu.Lock()
		seen = append(seen, tr.sessionID)
		tr.mu.Unlock()
		fn()
	}
	tr.setSessionIDVia("s2", run)
	tr.expireSessionIDVia("s2", run)
	if want := []string{"s2", "", ""}; strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("session id when each callback started = %q, want %q", seen, want)
	}
}

// bgTracker.wait returns once every goroutine has exited or is inside a
// callback, and not before.
func TestR50IntegratorBGTrackerWaitsForNonCallbackGoroutines(t *testing.T) {
	var b bgTracker
	b.add() // in a callback below
	b.add() // plain worker
	inCallback := make(chan struct{})
	release := make(chan struct{})
	go func() {
		defer b.done()
		b.callback(func() { close(inCallback); <-release })
	}()
	<-inCallback
	waited := make(chan struct{})
	go func() { b.wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("wait returned while a goroutine outside a callback was running")
	case <-time.After(100 * time.Millisecond):
	}
	b.done() // the plain worker exits
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return once only a callback goroutine was left")
	}
	close(release)
}
