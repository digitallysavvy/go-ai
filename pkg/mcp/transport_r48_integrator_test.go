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

// pipeBody is an SSE body the test writes to. Close does not unblock reads,
// so the test decides when the reader of a closed stream sees more events.
type pipeBody struct {
	*io.PipeReader
	w *io.PipeWriter
}

func newPipeBody() *pipeBody {
	pr, pw := io.Pipe()
	return &pipeBody{PipeReader: pr, w: pw}
}

func (b *pipeBody) Close() error { return nil }

func (b *pipeBody) send(t *testing.T, event, data string) {
	t.Helper()
	go func() { _, _ = b.w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n")) }()
}

// pipeSSETestClient hands out one queued body per GET, signals each GET on
// gets, and records every POST URL.
type pipeSSETestClient struct {
	mu     sync.Mutex
	bodies []*pipeBody
	posts  []string
	gets   chan struct{}
}

func (c *pipeSSETestClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if req.Method == http.MethodPost {
		c.posts = append(c.posts, req.URL.String())
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	body := c.bodies[0]
	c.bodies = c.bodies[1:]
	c.gets <- struct{}{}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
}

func newPipeSSETestClient(n int) (*pipeSSETestClient, []*pipeBody) {
	c := &pipeSSETestClient{gets: make(chan struct{}, n)}
	for i := 0; i < n; i++ {
		c.bodies = append(c.bodies, newPipeBody())
	}
	return c, append([]*pipeBody(nil), c.bodies...)
}

// A message the closed stream's reader parses after a reconnect belongs to
// the old stream. TS aborts that reader, so it never reaches onmessage.
func TestSSETransportStaleReaderDropsMessagesAfterReconnect(t *testing.T) {
	client, bodies := newPipeSSETestClient(2)
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client})
	bodies[0].send(t, "endpoint", "/messages")
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	_ = transport.Close()
	bodies[1].send(t, "endpoint", "/messages")
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	defer transport.Close() //nolint:errcheck
	bodies[0].send(t, "message", `{"jsonrpc":"2.0","method":"notifications/stale"}`)
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if msg, err := transport.Receive(ctx); err == nil {
		t.Fatalf("received %q from the closed stream", msg.Method)
	}
}

// An endpoint event the closed stream's reader parses while a later Connect
// still waits for its own must not lock the old stream's endpoint: the new
// Connect would then ignore its endpoint event and never return.
func TestSSETransportStaleEndpointDoesNotHijackReconnect(t *testing.T) {
	client, bodies := newPipeSSETestClient(2)
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client})
	bodies[0].send(t, "endpoint", "/messages")
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	<-client.gets
	_ = transport.Close()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(context.Background()) }()
	<-client.gets
	bodies[0].send(t, "endpoint", "/stale")
	time.Sleep(100 * time.Millisecond)
	bodies[1].send(t, "endpoint", "/messages")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second Connect: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Connect never returned: the stale endpoint was locked")
	}
	defer transport.Close() //nolint:errcheck
	if err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.posts) != 1 || client.posts[0] != "http://example.test/messages" {
		t.Fatalf("POSTs = %v, want the new stream's endpoint", client.posts)
	}
}

// Close while Connect waits for the endpoint event must make Connect return,
// leave the transport disconnected, and report nothing (TS returns silently
// once aborted; its start() promise never settles, which Go cannot mirror
// with a blocking Connect).
func TestSSETransportCloseDuringConnectReturns(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, b *pipeBody)
	}{
		{"endpoint after close", func(t *testing.T, b *pipeBody) { b.send(t, "endpoint", "/messages") }},
		{"EOF after close", func(t *testing.T, b *pipeBody) { _ = b.w.Close() }},
		{"read error after close", func(t *testing.T, b *pipeBody) { _ = b.w.CloseWithError(errors.New("reset")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, bodies := newPipeSSETestClient(1)
			var mu sync.Mutex
			var reported []error
			transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client, OnError: func(err error) {
				mu.Lock()
				reported = append(reported, err)
				mu.Unlock()
			}})
			done := make(chan error, 1)
			go func() { done <- transport.Connect(context.Background()) }()
			<-client.gets
			time.Sleep(20 * time.Millisecond)
			_ = transport.Close()
			tc.end(t, bodies[0])
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("Connect succeeded after Close")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Connect hung after Close")
			}
			if transport.IsConnected() {
				t.Fatal("transport connected after Close")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(reported) != 0 {
				t.Fatalf("OnError got %v after an intentional Close", reported)
			}
		})
	}
}

// closeCountingBody counts Close calls and, like an io.Pipe, unblocks reads
// only when closed.
type closeCountingBody struct {
	mu     sync.Mutex
	closes int
	closed chan struct{}
	first  []byte
}

func (b *closeCountingBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if len(b.first) > 0 {
		n := copy(p, b.first)
		b.first = b.first[n:]
		b.mu.Unlock()
		return n, nil
	}
	b.mu.Unlock()
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *closeCountingBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closes++
	if b.closes == 1 {
		close(b.closed)
	}
	return nil
}

type closeCountingSSEClient struct {
	mu     sync.Mutex
	post   bool
	bodies []*closeCountingBody
}

func (c *closeCountingSSEClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet || (c.post && req.Method == http.MethodPost) {
		b := &closeCountingBody{closed: make(chan struct{}), first: []byte(": keep-alive\n\n")}
		c.mu.Lock()
		c.bodies = append(c.bodies, b)
		c.mu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: b}, nil
	}
	return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
}

// HTTP Close closes each custom stream body exactly once.
func TestHTTPTransportCloseClosesStreamBodiesOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		post     bool
		protocol string
	}{
		{"inbound GET", false, "2025-06-18"},
		{"POST response", true, LatestProtocolVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &closeCountingSSEClient{post: tc.post}
			transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://example.test/mcp", InitialProtocolVersion: tc.protocol, SSEClient: client})
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if tc.post {
				if err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "ping"}); err != nil {
					t.Fatalf("Send: %v", err)
				}
			}
			time.Sleep(50 * time.Millisecond)
			_ = transport.Close()
			client.mu.Lock()
			defer client.mu.Unlock()
			if len(client.bodies) == 0 {
				t.Fatal("no stream opened")
			}
			for i, b := range client.bodies {
				b.mu.Lock()
				if b.closes != 1 {
					t.Errorf("body %d closed %d times, want 1", i, b.closes)
				}
				b.mu.Unlock()
			}
		})
	}
}
