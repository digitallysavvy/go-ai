package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file ports the HTTP transport lifecycle parity behaviors from TS
// HttpMCPTransport (mcp-http-transport.ts), specifically send()'s
// `AbortSignal.any([transportSignal, options.signal])` request signal and its
// content-type dispatch (`application/json` / `text/event-stream` /
// "Unexpected content type"). TS has no dedicated test for the
// abortController-combined-with-signal behavior (it is exercised indirectly
// throughout the suite via per-call timeouts), so the Close-cancels-in-flight
// coverage below is Go-specific: it pins the goroutine-exit guarantee Go
// requires that JS, having no goroutines, does not need to test for.

// blockingUntilCanceledSSEClient responds 405 to any GET (so Connect's
// inbound SSE listener never blocks the test) and, for a POST, blocks until
// the request's context is canceled before returning that context's error --
// mirroring how a real net/http.Client's RoundTrip aborts and returns the
// context's error once its request context is canceled mid-flight.
type blockingUntilCanceledSSEClient struct {
	postStarted chan struct{}
	once        sync.Once
}

func (c *blockingUntilCanceledSSEClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		return &http.Response{StatusCode: http.StatusMethodNotAllowed, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	c.once.Do(func() { close(c.postStarted) })
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// TestHTTPTransportCloseCancelsInFlightPOST mirrors TS send()'s
// `AbortSignal.any([transportSignal, options.signal])`: Close() cancels the
// transport-lifetime signal, which unblocks an in-flight POST even though the
// caller's own context is still live. Matching TS's outer catch guard
// (`if (options?.signal?.aborted) throw error;`, which only looks at the
// caller's own signal), the failure IS still reported via OnError here, since
// only the transport-wide signal fired, not the caller's.
func TestHTTPTransportCloseCancelsInFlightPOST(t *testing.T) {
	sse := &blockingUntilCanceledSSEClient{postStarted: make(chan struct{})}
	var mu sync.Mutex
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError: func(err error) {
			mu.Lock()
			captured = err
			mu.Unlock()
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- transport.Send(context.Background(), msg)
	}()

	select {
	case <-sse.postStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the POST to reach the SSE client")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- transport.Close() }()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Close to return")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Send to return an error once Close canceled the in-flight POST")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send error = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Send to return after Close")
	}

	mu.Lock()
	defer mu.Unlock()
	if captured == nil {
		t.Fatal("expected OnError to fire: only the transport-wide signal fired, not the caller's own, matching TS send()'s options?.signal?.aborted guard")
	}
}

// postSSEStreamClient responds 405 to any GET (disabling inbound SSE) and, for
// a POST, a 200 text/event-stream response built by newSSEStreamResponse (see
// http_transport_inbound_sse_test.go): its body unblocks with the request
// context's error once that context is canceled, mirroring what a real
// net/http.Client's RoundTripper does when a request's context is canceled
// mid-read -- exactly the behavior HTTPTransport's POST-response SSE reader
// relies on to exit promptly once Close() cancels its derived context.
type postSSEStreamClient struct{}

func (c postSSEStreamClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		return &http.Response{StatusCode: http.StatusMethodNotAllowed, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	resp, _ := newSSEStreamResponse(req)
	return resp, nil
}

// TestHTTPTransportCloseCancelsPOSTResponseSSEReader mirrors TS send()'s
// processEvents() outer catch (`options?.signal?.aborted ||
// error.name === 'AbortError'`): once Close() cancels the transport-wide
// signal that the POST-response SSE reader's derived context is watching,
// the reader must exit without reporting an error, and Close() itself must
// not return until that reader's goroutine has actually exited (no leak).
func TestHTTPTransportCloseCancelsPOSTResponseSSEReader(t *testing.T) {
	baseline := runtime.NumGoroutine()
	var mu sync.Mutex
	var captured []error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: postSSEStreamClient{},
		OnError: func(err error) {
			mu.Lock()
			captured = append(captured, err)
			mu.Unlock()
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- transport.Close() }()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Close to return (it must wait for the SSE reader goroutine)")
	}

	// Close() already waited for postSSEWG, so the reader goroutine is gone
	// by construction; this additionally guards against any other lingering
	// goroutine from this test (e.g. the inbound SSE lifecycle) leaking.
	eventuallyNoExtraGoroutines(t, baseline, 2*time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 0 {
		t.Fatalf("OnError should not fire when Close cancels the POST-response SSE reader, got: %v", captured)
	}
}

// TestHTTPTransportUnexpectedContentTypeReportsAndReturnsError mirrors TS
// send()'s final content-type branch: a 2xx, non-notification response whose
// content-type is neither application/json nor text/event-stream is an
// "Unexpected content type" MCPClientError, reported via OnError and
// returned as the Send() error, using TS's exact message text.
func TestHTTPTransportUnexpectedContentTypeReportsAndReturnsError(t *testing.T) {
	sse := responseSSEClient{
		header: http.Header{"Content-Type": []string{"text/plain"}},
		body:   "not json, not sse",
	}
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	sendErr := transport.Send(t.Context(), msg)
	if sendErr == nil {
		t.Fatal("expected an error for an unexpected content type")
	}
	const wantMsg = "MCP HTTP Transport Error: Unexpected content type: text/plain"
	if sendErr.Error() != wantMsg {
		t.Fatalf("Send error = %q, want %q", sendErr.Error(), wantMsg)
	}

	if captured == nil {
		t.Fatal("expected OnError to be called")
	}
	var clientErr *MCPClientError
	if !errors.As(captured, &clientErr) {
		t.Fatalf("OnError error = %T %v, want *MCPClientError", captured, captured)
	}
	if clientErr.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", clientErr.StatusCode)
	}
	if clientErr.URL != "http://localhost:9999/mcp" {
		t.Fatalf("URL = %q, want the transport URL", clientErr.URL)
	}
	if !errors.Is(sendErr, captured) && sendErr.Error() != captured.Error() {
		t.Fatalf("Send() error and OnError error should describe the same failure: %v vs %v", sendErr, captured)
	}
}

// TestHTTPTransportMissingContentTypeReportsUnexpectedContentType mirrors TS
// send()'s `const contentType = response.headers.get('content-type') || ”;`:
// a response with no content-type header at all takes the same "Unexpected
// content type" branch (with an empty type in the message), not a lenient
// JSON fallback.
func TestHTTPTransportMissingContentTypeReportsUnexpectedContentType(t *testing.T) {
	sse := responseSSEClient{
		header: http.Header{},
		body:   `{"jsonrpc":"2.0","id":1,"result":{}}`,
	}
	var captured error
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(err error) { captured = err },
	})
	transport.connected = true

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	sendErr := transport.Send(t.Context(), msg)
	if sendErr == nil {
		t.Fatal("expected an error for a missing content-type header")
	}
	const wantMsg = "MCP HTTP Transport Error: Unexpected content type: "
	if sendErr.Error() != wantMsg {
		t.Fatalf("Send error = %q, want %q", sendErr.Error(), wantMsg)
	}
	if captured == nil {
		t.Fatal("expected OnError to be called")
	}
}
