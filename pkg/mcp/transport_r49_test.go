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

// handshakeBlockingSSEClient holds each GET until its request context ends
// or release is closed, then answers with an endpoint event.
type handshakeBlockingSSEClient struct {
	started chan struct{}
	release chan struct{}
}

func (c *handshakeBlockingSSEClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	c.started <- struct{}{}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-c.release:
	}
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte("event: endpoint\ndata: /messages\n\n")) }()
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
}

// TS start() creates its abortController before the fetch, so close()
// during the GET aborts it and start never connects. A Close while Connect
// waits for the GET response must abort it: Connect returns an error, the
// transport stays closed, and nothing is reported.
func TestSSETransportCloseDuringHandshakeAbortsConnect(t *testing.T) {
	client := &handshakeBlockingSSEClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	var mu sync.Mutex
	var reported []error
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client, OnError: func(err error) {
		mu.Lock()
		reported = append(reported, err)
		mu.Unlock()
	}})
	done := make(chan error, 1)
	go func() { done <- transport.Connect(context.Background()) }()
	<-client.started
	_ = transport.Close()
	// A client that ignores the abort still answers later; Connect must not
	// adopt that stream either.
	time.AfterFunc(200*time.Millisecond, func() { close(client.release) })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Connect succeeded after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connect hung after Close")
	}
	time.Sleep(300 * time.Millisecond)
	if transport.IsConnected() {
		t.Fatal("transport connected after Close")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 0 {
		t.Fatalf("OnError got %v after an intentional Close", reported)
	}
}

// ignoringAbortSSEClient answers each GET after a delay, whatever its
// request context, with a body that ends only when closed.
type ignoringAbortSSEClient struct {
	delay  time.Duration
	mu     sync.Mutex
	bodies []*io.PipeReader
}

func (c *ignoringAbortSSEClient) Do(req *http.Request) (*http.Response, error) {
	time.Sleep(c.delay)
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte("event: endpoint\ndata: /messages\n\n")) }()
	c.mu.Lock()
	c.bodies = append(c.bodies, pr)
	c.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
}

// The same, with a client that ignores the abort: the late stream is closed
// rather than adopted.
func TestSSETransportCloseDuringHandshakeDropsLateStream(t *testing.T) {
	client := &ignoringAbortSSEClient{delay: 150 * time.Millisecond}
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client})
	done := make(chan error, 1)
	go func() { done <- transport.Connect(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	_ = transport.Close()
	if err := <-done; err == nil {
		t.Fatal("Connect succeeded after Close")
	}
	if transport.IsConnected() {
		t.Fatal("transport connected after Close")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	for i, b := range client.bodies {
		if _, err := b.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("body %d still open (read err %v)", i, err)
		}
	}
}

type failingSSEClient struct{}

func (failingSSEClient) Do(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// TS establishConnection's catch reports a failed fetch through onerror
// before rejecting (only an AbortError is silent).
func TestSSETransportConnectReportsFetchFailure(t *testing.T) {
	var mu sync.Mutex
	var reported []error
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: failingSSEClient{}, OnError: func(err error) {
		mu.Lock()
		reported = append(reported, err)
		mu.Unlock()
	}})
	err := transport.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect succeeded")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || reported[0] != err {
		t.Fatalf("OnError got %v, want the returned error %v once", reported, err)
	}

	// The caller's own cancellation is not reported.
	reported = nil
	mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = transport.Connect(ctx)
	mu.Lock()
	if len(reported) != 0 {
		t.Fatalf("OnError got %v for a canceled Connect", reported)
	}
}

// A second Connect started while the first still waits for its endpoint
// supersedes it: the first returns at once and its stream is closed instead
// of leaking a reader blocked on it.
func TestSSETransportConcurrentConnectClosesSupersededStream(t *testing.T) {
	var mu sync.Mutex
	var bodies []*io.PipeReader
	var writers []*io.PipeWriter
	gets := make(chan struct{}, 2)
	client := sseClientFunc(func(req *http.Request) (*http.Response, error) {
		pr, pw := io.Pipe()
		mu.Lock()
		bodies = append(bodies, pr)
		writers = append(writers, pw)
		mu.Unlock()
		gets <- struct{}{}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: pr}, nil
	})
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client})
	defer transport.Close() //nolint:errcheck
	first := make(chan error, 1)
	go func() { first <- transport.Connect(context.Background()) }()
	<-gets
	time.Sleep(20 * time.Millisecond)
	second := make(chan error, 1)
	go func() { second <- transport.Connect(context.Background()) }()
	<-gets
	select {
	case err := <-first:
		if err == nil {
			t.Fatal("superseded Connect succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("superseded Connect hung")
	}
	mu.Lock()
	go func() { _, _ = writers[1].Write([]byte("event: endpoint\ndata: /messages\n\n")) }()
	mu.Unlock()
	if err := <-second; err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if !transport.IsConnected() {
		t.Fatal("second Connect left the transport disconnected")
	}
}

type sseClientFunc func(*http.Request) (*http.Response, error)

func (f sseClientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// A Connect whose ctx ends after a Close and a newer Connect must close only
// its own stream, not the newer connection.
func TestSSETransportCanceledConnectKeepsNewerStream(t *testing.T) {
	client, bodies := newPipeSSETestClient(2)
	transport := NewSSETransport(SSETransportConfig{URL: "http://example.test/sse", SSEClient: client})
	defer transport.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- transport.Connect(ctx) }()
	<-client.gets
	_ = transport.Close() // pipeBody ignores Close: the first reader stays blocked
	bodies[1].send(t, "endpoint", "/messages")
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	cancel()
	if err := <-first; err == nil {
		t.Fatal("first Connect succeeded")
	}
	if !transport.IsConnected() {
		t.Fatal("the canceled first Connect closed the newer connection")
	}
}
