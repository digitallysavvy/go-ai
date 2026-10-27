package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// gatedSSEBody serves an endpoint event, then blocks reads until release is
// closed and fails with readErr. Close does not unblock it, so the test
// decides when the reader goroutine of a closed stream wakes up.
type gatedSSEBody struct {
	r       io.Reader
	release chan struct{}
	readErr error
}

func (b *gatedSSEBody) Read(p []byte) (int, error) {
	if n, err := b.r.Read(p); n > 0 || err != io.EOF {
		return n, err
	}
	<-b.release
	return 0, b.readErr
}

func (b *gatedSSEBody) Close() error { return nil }

type queuedSSEClient struct {
	mu     sync.Mutex
	bodies []io.ReadCloser
}

func (c *queuedSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if req.Method != http.MethodGet || len(c.bodies) == 0 {
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	body := c.bodies[0]
	c.bodies = c.bodies[1:]
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
}

// The reader goroutine of a closed SSE stream must not clear the state of
// the stream a later Connect opened (TS returns silently once its
// abortController is aborted). Before the fix the stale reader's read error
// marked the new connection disconnected and reported it through OnError.
func TestSSETransportStaleReaderDoesNotClobberReconnect(t *testing.T) {
	for _, readErr := range []error{io.EOF, errors.New("connection reset")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			first := &gatedSSEBody{r: strings.NewReader("event: endpoint\ndata: /messages\n\n"), release: make(chan struct{}), readErr: readErr}
			second := &gatedSSEBody{r: strings.NewReader("event: endpoint\ndata: /messages\n\n"), release: make(chan struct{}), readErr: io.EOF}
			defer close(second.release)
			var mu sync.Mutex
			var reported []error
			transport := NewSSETransport(SSETransportConfig{
				URL:       "http://example.test/sse",
				SSEClient: &queuedSSEClient{bodies: []io.ReadCloser{first, second}},
				OnError: func(err error) {
					mu.Lock()
					reported = append(reported, err)
					mu.Unlock()
				},
			})
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("first Connect: %v", err)
			}
			_ = transport.Close()
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("second Connect: %v", err)
			}
			close(first.release)
			time.Sleep(100 * time.Millisecond)

			if !transport.IsConnected() {
				t.Fatal("stale reader of the closed stream disconnected the new one")
			}
			if err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"}); err != nil {
				t.Fatalf("Send on the new stream: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(reported) != 0 {
				t.Fatalf("stale reader reported %v", reported)
			}
			_ = transport.Close()
		})
	}
}

// pipeSSEClient answers GETs (and, when post is set, POSTs) with an
// io.Pipe-backed event stream that never ends and does not watch the request
// context: only closing the body unblocks its reader.
type pipeSSEClient struct{ post bool }

func (c *pipeSSEClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet || (c.post && req.Method == http.MethodPost) {
		pr, pw := io.Pipe()
		go func() { _, _ = pw.Write([]byte(": keep-alive\n\n")) }()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
	}
	return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
}

// TS close() cancels the inbound SSE reader and aborts POST-response
// readers, which cancels their bodies. Go's Close waits for those reader
// goroutines, so before the fix a custom SSEClient body that ignores the
// request context made Close hang forever.
func TestHTTPTransportCloseClosesCustomStreamBodies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		post     bool
		protocol string
	}{
		{"inbound GET", false, "2025-06-18"},
		// The modern protocol opens no inbound GET, so only the POST
		// reader is in play.
		{"POST response", true, LatestProtocolVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := NewHTTPTransport(HTTPTransportConfig{
				URL:                    "http://example.test/mcp",
				InitialProtocolVersion: tc.protocol,
				SSEClient:              &pipeSSEClient{post: tc.post},
			})
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if tc.post {
				if err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "ping"}); err != nil {
					t.Fatalf("Send: %v", err)
				}
			}
			time.Sleep(50 * time.Millisecond)
			done := make(chan struct{})
			go func() {
				_ = transport.Close()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Close hung on a stream body that ignores the request context")
			}
		})
	}
}

type nilBodySSEClient struct{}

func (nilBodySSEClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{}}, nil
}

// TS SseMCPTransport.start fails with `MCP SSE Transport Error: 200 OK` when
// an OK response has no body (`!response.ok || !response.body`). Before the
// fix Go read the missing body as an empty stream and failed with "Connection
// closed before endpoint event was received".
func TestSSETransportConnectOKWithoutBody(t *testing.T) {
	var mu sync.Mutex
	var reported []error
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: nilBodySSEClient{}, OnError: func(err error) {
		mu.Lock()
		reported = append(reported, err)
		mu.Unlock()
	}})
	err := transport.Connect(context.Background())
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "MCP SSE Transport Error: 200 OK" {
		t.Fatalf("Connect error = %#v, want MCP SSE Transport Error: 200 OK", err)
	}
	if mcpErr.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", mcpErr.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || reported[0] != err {
		t.Fatalf("OnError got %v, want the Connect error once", reported)
	}
}
