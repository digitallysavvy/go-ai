package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Ports the inbound SSE (standing background GET, legacy protocol era)
// coverage from TS packages/mcp/src/tool/mcp-http-transport.test.ts:
// "should (re)open inbound SSE after 202 Accepted",
// "should notify and clear the session id when inbound SSE returns 404",
// "should expose HTTP status and URL on GET SSE failure",
// "should handle inbound SSE messages without explicit event field",
// "should handle invalid JSON-RPC messages from inbound SSE",
// "should handle rejected inbound SSE cancel after stream errors",
// plus Go-specific reconnect/backoff/resume, 401 reauth, and goroutine-leak
// coverage the TS suite does not need (no separate goroutine lifecycle).

// scriptedSSEClient dispatches a response per call, indexed by call number,
// mirroring the `({ callNumber }) => ...` pattern used throughout the TS
// suite.
type scriptedSSEClient struct {
	mu      sync.Mutex
	calls   []*http.Request
	respond func(callNumber int, req *http.Request) (*http.Response, error)
}

func (c *scriptedSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	callNumber := len(c.calls)
	c.calls = append(c.calls, req.Clone(req.Context()))
	c.mu.Unlock()
	return c.respond(callNumber, req)
}

func (c *scriptedSSEClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *scriptedSSEClient) callAt(i int) *http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < 0 || i >= len(c.calls) {
		return nil
	}
	return c.calls[i]
}

func waitForCallCount(t *testing.T, sse *scriptedSSEClient, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if sse.callCount() >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d calls, got %d", n, sse.callCount())
}

func statusResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func jsonRPCOKResponse() *http.Response {
	h := make(http.Header)
	h.Set("content-type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)),
	}
}

// newSSEStreamResponse returns a 200 text/event-stream response backed by an
// io.Pipe the test can write to, plus a stop func that must be called once
// the test is done with it. When req's context is canceled, any blocked Read
// on the response body unblocks with the context's error -- matching what a
// real net/http.Client does when a request context is canceled mid-stream,
// which HTTPTransport's readInboundSSEStream relies on to exit promptly.
func newSSEStreamResponse(req *http.Request) (*http.Response, *io.PipeWriter) {
	pr, pw := io.Pipe()
	stop := context.AfterFunc(req.Context(), func() {
		pr.CloseWithError(req.Context().Err())
	})
	h := make(http.Header)
	h.Set("content-type", "text/event-stream")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     h,
		Body: &stopOnCloseBody{
			PipeReader: pr,
			stop:       stop,
		},
	}
	return resp, pw
}

// stopOnCloseBody deregisters the context.AfterFunc callback once the body
// is closed through the normal path (readInboundSSEStream's `defer
// body.Close()`), so a stream that ends cleanly never leaves a dangling
// watcher registered against the request context.
type stopOnCloseBody struct {
	*io.PipeReader
	stop func() bool
}

func (b *stopOnCloseBody) Close() error {
	b.stop()
	return b.PipeReader.Close()
}

// eventuallyNoExtraGoroutines polls runtime.NumGoroutine() until it returns
// to at most `baseline`, or fails the test after timeout. Used to assert
// Close() leaves no inbound-SSE goroutine running.
func eventuallyNoExtraGoroutines(t *testing.T, baseline int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last int
	for time.Now().Before(deadline) {
		last = runtime.NumGoroutine()
		if last <= baseline {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: NumGoroutine() = %d, want <= baseline %d", last, baseline)
}

// TestHTTPTransportInboundSSEOpensStandingGETOnConnect mirrors the implicit
// setup every TS mcp-http-transport.test.ts case relies on ("Call 0: GET
// from start"): Connect() opens a standing background GET with
// Accept: text/event-stream for the legacy protocol era.
func TestHTTPTransportInboundSSEOpensStandingGETOnConnect(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		resp, _ := newSSEStreamResponse(req)
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 1, time.Second)
	req := sse.callAt(0)
	if req.Method != http.MethodGet {
		t.Fatalf("method = %s, want GET", req.Method)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept header = %q, want text/event-stream", got)
	}
	if got := req.Header.Get("mcp-protocol-version"); got != LatestLegacyProtocolVersion {
		t.Fatalf("mcp-protocol-version header = %q, want %q", got, LatestLegacyProtocolVersion)
	}
}

// TestHTTPTransportInboundSSESkippedForModernProtocol verifies
// isModernProtocol() short-circuits the standing GET entirely for the
// 2026-07-28 protocol, matching TS's `isModernProtocol()` guard in
// startInboundSse/openInboundSse.
func TestHTTPTransportInboundSSESkippedForModernProtocol(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request for modern protocol: %s %s", req.Method, req.URL)
		return nil, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	transport.SetProtocolVersion(LatestProtocolVersion)
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	// Give any (unwanted) background GET a chance to fire before asserting
	// none did.
	time.Sleep(20 * time.Millisecond)
	if got := sse.callCount(); got != 0 {
		t.Fatalf("callCount = %d, want 0 for modern protocol", got)
	}
}

// TestHTTPTransportInboundSSESetProtocolVersionSwitchesLifecycle mirrors TS's
// `setProtocolVersion`: switching to the modern protocol mid-connection
// closes the standing inbound SSE GET and starts no more while modern;
// switching back to a legacy protocol (re)opens a fresh one.
func TestHTTPTransportInboundSSESetProtocolVersionSwitchesLifecycle(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		resp, _ := newSSEStreamResponse(req)
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 1, time.Second) // initial legacy GET

	// Switch to modern: the standing GET must be closed immediately, and no
	// further GET attempted while modern.
	transport.SetProtocolVersion(LatestProtocolVersion)
	transport.sseMu.Lock()
	hasConn := transport.sseConnCancel != nil
	transport.sseMu.Unlock()
	if hasConn {
		t.Fatal("sseConnCancel still set right after switching to modern protocol")
	}

	time.Sleep(20 * time.Millisecond)
	if got := sse.callCount(); got != 1 {
		t.Fatalf("callCount = %d, want 1 (no GET while modern)", got)
	}

	// Switch back to legacy: a fresh standing GET must (re)open.
	transport.SetProtocolVersion(LatestLegacyProtocolVersion)
	waitForCallCount(t, sse, 2, time.Second)
	req := sse.callAt(1)
	if req.Method != http.MethodGet {
		t.Fatalf("call 1 method = %s, want GET", req.Method)
	}
	if got := req.Header.Get("mcp-protocol-version"); got != LatestLegacyProtocolVersion {
		t.Fatalf("call 1 mcp-protocol-version = %q, want %q", got, LatestLegacyProtocolVersion)
	}
}

// TestHTTPTransportInboundSSEQueuesMessagesAndTracksLastEventID mirrors TS's
// "should handle inbound SSE messages without explicit event field" and
// additionally verifies the event id is tracked for a subsequent
// Last-Event-Id resume (TS's `lastInboundEventId`).
func TestHTTPTransportInboundSSEQueuesMessagesAndTracksLastEventID(t *testing.T) {
	sse := &scriptedSSEClient{}
	var pw *io.PipeWriter
	ready := make(chan struct{})
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		var resp *http.Response
		resp, pw = newSSEStreamResponse(req)
		close(ready)
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	<-ready
	if _, err := pw.Write([]byte("id: evt-1\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")); err != nil {
		t.Fatalf("write SSE event: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if msg.ID != float64(1) {
		t.Fatalf("message id = %v, want 1", msg.ID)
	}

	transport.sseMu.Lock()
	lastID := transport.lastInboundEventID
	transport.sseMu.Unlock()
	if lastID != "evt-1" {
		t.Fatalf("lastInboundEventID = %q, want evt-1", lastID)
	}
}

// TestHTTPTransportInboundSSEHandlesInvalidJSONRPC mirrors TS's "should
// handle invalid JSON-RPC messages from inbound SSE": a malformed message
// reports an error via OnError but does not tear down the stream (a
// subsequent valid message still arrives).
func TestHTTPTransportInboundSSEHandlesInvalidJSONRPC(t *testing.T) {
	sse := &scriptedSSEClient{}
	var pw *io.PipeWriter
	ready := make(chan struct{})
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		var resp *http.Response
		resp, pw = newSSEStreamResponse(req)
		close(ready)
		return resp, nil
	}

	var errs []error
	var errMu sync.Mutex
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError: func(err error) {
			errMu.Lock()
			errs = append(errs, err)
			errMu.Unlock()
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	<-ready
	// Syntactically invalid JSON (unlike TS, this Go port's inbound-SSE parse
	// step -- like the existing POST-triggered readMCPHTTPSSEMessages -- is a
	// plain unmarshal with no separate JSON-RPC shape validation, so only a
	// JSON syntax error takes the error path).
	if _, err := pw.Write([]byte("event: message\ndata: {not-valid-json\n\n")); err != nil {
		t.Fatalf("write malformed event: %v", err)
	}
	if _, err := pw.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{}}\n\n")); err != nil {
		t.Fatalf("write valid event: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive error: %v", err)
	}
	if msg.ID != float64(2) {
		t.Fatalf("message id = %v, want 2", msg.ID)
	}

	deadline := time.Now().Add(time.Second)
	for {
		errMu.Lock()
		n := len(errs)
		errMu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for OnError from malformed message")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestHTTPTransportInboundSSEExpiresSessionOn404 mirrors TS's "should notify
// and clear the session id when inbound SSE returns 404".
func TestHTTPTransportInboundSSEExpiresSessionOn404(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusNotFound, "Not Found"), nil
	}

	var expired []string
	var mu sync.Mutex
	var httpErrs int32
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "expired-session",
		OnSessionExpired: func(id string) {
			mu.Lock()
			expired = append(expired, id)
			mu.Unlock()
		},
		OnError: func(err error) { atomic.AddInt32(&httpErrs, 1) },
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := len(expired)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for OnSessionExpired")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if transport.SessionID() != "" {
		t.Fatalf("SessionID() = %q, want empty after expiry", transport.SessionID())
	}
	if atomic.LoadInt32(&httpErrs) == 0 {
		t.Fatal("expected OnError to be called for the 404 GET SSE failure")
	}
}

// TestHTTPTransportInboundSSE405IsSilentAndDoesNotReconnect mirrors TS's GET
// SSE 405 handling ("This server does not support HTTP transport" is a POST
// message; the GET-side equivalent in openInboundSse is a silent early
// return): no error is reported and no reconnection is attempted.
func TestHTTPTransportInboundSSE405IsSilentAndDoesNotReconnect(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusMethodNotAllowed, ""), nil
	}

	var errCount int32
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError:   func(error) { atomic.AddInt32(&errCount, 1) },
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 1, time.Second)
	time.Sleep(50 * time.Millisecond)
	if got := sse.callCount(); got != 1 {
		t.Fatalf("callCount = %d, want 1 (no reconnect after 405)", got)
	}
	if atomic.LoadInt32(&errCount) != 0 {
		t.Fatalf("errCount = %d, want 0 (405 is silent)", errCount)
	}
}

// TestHTTPTransportInboundSSEExposesStatusOnGETFailure mirrors TS's "should
// expose HTTP status and URL on GET SSE failure".
func TestHTTPTransportInboundSSEExposesStatusOnGETFailure(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusServiceUnavailable, "Service Unavailable"), nil
	}

	var captured *MCPClientError
	var mu sync.Mutex
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OnError: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			if clientErr, ok := err.(*MCPClientError); ok {
				captured = clientErr
			}
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		got := captured
		mu.Unlock()
		if got != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for OnError")
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if captured.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", captured.StatusCode)
	}
	if captured.URL != "http://localhost:9999/mcp" {
		t.Fatalf("URL = %q, want the MCP endpoint", captured.URL)
	}
	if !strings.Contains(captured.Message, "GET SSE failed") {
		t.Fatalf("Message = %q, want it to mention GET SSE failed", captured.Message)
	}
}

// TestHTTPTransportInboundSSEReopensAfter202Accepted mirrors TS's "should
// (re)open inbound SSE after 202 Accepted": a 405 on the initial GET means no
// SSE connection is active, so a subsequent 202-accepted POST should trigger
// a fresh GET attempt.
func TestHTTPTransportInboundSSEReopensAfter202Accepted(t *testing.T) {
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && callNumber == 0:
			return statusResponse(http.StatusMethodNotAllowed, ""), nil
		case req.Method == http.MethodPost:
			return statusResponse(http.StatusAccepted, ""), nil
		case req.Method == http.MethodGet:
			resp, _ := newSSEStreamResponse(req)
			return resp, nil
		default:
			return statusResponse(http.StatusOK, ""), nil
		}
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 1, time.Second) // initial GET (405)

	msg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// call 0: initial GET (405); call 1: the POST itself (202); call 2: the
	// (re)opened GET the 202 handling triggers.
	waitForCallCount(t, sse, 3, time.Second)
	req := sse.callAt(2)
	if req.Method != http.MethodGet {
		t.Fatalf("call 2 method = %s, want GET", req.Method)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("call 2 Accept = %q, want text/event-stream", got)
	}
}

// TestHTTPTransportInboundSSEUnauthorizedRefreshesTokenOnceThenRetries
// exercises the 401 recovery path: reuses the same refreshOAuthToken
// single-flight path as send(), matching TS's authorizeOnce()-guarded retry
// in openInboundSse.
//
// Connect() itself proactively fetches an OAuth token before Connect
// returns (a pre-existing Go behavior with no TS equivalent), so the token
// is refreshed once before the first inbound SSE GET is ever attempted;
// RefreshTokenFunc returns a distinct token each call so the test can
// isolate the *second* refresh -- the one openInboundSse's own 401 handling
// triggers -- from that first, unrelated one.
func TestHTTPTransportInboundSSEUnauthorizedRefreshesTokenOnceThenRetries(t *testing.T) {
	var refreshes int32
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer token-2" {
			return statusResponse(http.StatusUnauthorized, ""), nil
		}
		resp, _ := newSSEStreamResponse(req)
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OAuth: &OAuthConfig{
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				n := atomic.AddInt32(&refreshes, 1)
				return fmt.Sprintf("token-%d", n), time.Hour, nil
			},
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 2, time.Second) // 401 with token-1, then the retried GET with token-2
	if got := atomic.LoadInt32(&refreshes); got != 2 {
		t.Fatalf("refresh calls = %d, want 2 (1 proactive at Connect + 1 from the 401 recovery)", got)
	}
	if got := sse.callAt(0).Header.Get("Authorization"); got != "Bearer token-1" {
		t.Fatalf("initial Authorization = %q, want Bearer token-1", got)
	}
	if got := sse.callAt(1).Header.Get("Authorization"); got != "Bearer token-2" {
		t.Fatalf("retried Authorization = %q, want Bearer token-2", got)
	}

	// No further calls once the retried request establishes the stream.
	time.Sleep(30 * time.Millisecond)
	if got := sse.callCount(); got != 2 {
		t.Fatalf("callCount = %d, want 2 (no third attempt)", got)
	}
}

// TestHTTPTransportInboundSSEUnauthorizedGivesUpAfterOneRetry verifies a 401
// that persists after the single retry is reported (not looped forever),
// matching TS's `!triedAuth` guard: refreshing must stop growing once the
// one allowed retry has also failed.
func TestHTTPTransportInboundSSEUnauthorizedGivesUpAfterOneRetry(t *testing.T) {
	var refreshes int32
	sse := &scriptedSSEClient{}
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusUnauthorized, ""), nil
	}

	var captured *MCPClientError
	var mu sync.Mutex
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
		OAuth: &OAuthConfig{
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				n := atomic.AddInt32(&refreshes, 1)
				return fmt.Sprintf("token-%d", n), time.Hour, nil
			},
		},
		OnError: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			if clientErr, ok := err.(*MCPClientError); ok {
				captured = clientErr
			}
		},
	})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	waitForCallCount(t, sse, 2, time.Second)
	time.Sleep(30 * time.Millisecond)
	// 1 proactive refresh at Connect + 1 from the 401 recovery attempt; the
	// second attempt's own 401 must NOT trigger a third refresh.
	if got := atomic.LoadInt32(&refreshes); got != 2 {
		t.Fatalf("refresh calls = %d, want exactly 2 (no repeated refresh loop)", got)
	}
	if got := sse.callCount(); got != 2 {
		t.Fatalf("callCount = %d, want 2 (initial 401 + one retried 401, then give up)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if captured == nil || captured.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected a reported 401 MCPClientError, got %#v", captured)
	}
}

// TestHTTPTransportInboundSSEReconnectsWithBackoffAndResumesLastEventID
// exercises TS's scheduleInboundSseReconnection/getNextReconnectionDelay: a
// mid-stream read failure schedules a reconnect (1s initial backoff) that
// resumes with Last-Event-Id set to the last event id seen before the drop.
func TestHTTPTransportInboundSSEReconnectsWithBackoffAndResumesLastEventID(t *testing.T) {
	sse := &scriptedSSEClient{}
	var pw *io.PipeWriter
	firstReady := make(chan struct{})
	var once sync.Once
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		if callNumber == 0 {
			var resp *http.Response
			resp, pw = newSSEStreamResponse(req)
			once.Do(func() { close(firstReady) })
			return resp, nil
		}
		// The reconnect attempt: succeed so we can observe its headers.
		resp, _ := newSSEStreamResponse(req)
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer transport.Close() //nolint:errcheck

	<-firstReady
	if _, err := pw.Write([]byte("id: last-seen\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")); err != nil {
		t.Fatalf("write event: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := transport.Receive(ctx); err != nil {
		t.Fatalf("Receive error: %v", err)
	}

	// Simulate a disconnect: error out the stream (not a clean EOF).
	pw.CloseWithError(fmt.Errorf("connection reset"))

	// Backoff for attempt 0 is 1s; allow generous slack for CI.
	waitForCallCount(t, sse, 2, 3*time.Second)
	req := sse.callAt(1)
	if got := req.Header.Get("Last-Event-Id"); got != "last-seen" {
		t.Fatalf("reconnect Last-Event-Id = %q, want last-seen", got)
	}
}

// TestHTTPTransportInboundSSECloseDuringReconnectHasNoGoroutineLeak is the
// Close-during-reconnect race/leak test: Close() is called while a reconnect
// is scheduled (before its backoff elapses), and must return promptly
// without leaving the pending-reconnect goroutine running.
func TestHTTPTransportInboundSSECloseDuringReconnectHasNoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	sse := &scriptedSSEClient{}
	var pw *io.PipeWriter
	ready := make(chan struct{})
	var once sync.Once
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		if callNumber == 0 {
			var resp *http.Response
			resp, pw = newSSEStreamResponse(req)
			once.Do(func() { close(ready) })
			return resp, nil
		}
		t.Errorf("unexpected reconnect attempt while Close() should have canceled it")
		return statusResponse(http.StatusInternalServerError, ""), nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	<-ready
	// Drop the connection to schedule a reconnect (1s backoff), then close
	// immediately -- well before the backoff elapses -- and confirm Close()
	// doesn't block for anywhere near that long.
	pw.CloseWithError(fmt.Errorf("connection reset"))
	time.Sleep(20 * time.Millisecond) // let scheduleInboundSSEReconnection run

	closeDone := make(chan error, 1)
	start := time.Now()
	go func() { closeDone <- transport.Close() }()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close error: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("Close() took %v, want well under the 1s reconnect backoff", elapsed)
		}
	case <-time.After(900 * time.Millisecond):
		t.Fatal("Close() blocked for close to the full reconnect backoff -- likely a WaitGroup deadlock")
	}

	// Give the (canceled) reconnect timer's goroutine, if any raced in, a
	// moment to observe the cancellation and exit, then confirm no
	// inbound-SSE goroutine is still running.
	eventuallyNoExtraGoroutines(t, baseline, time.Second)
}

// TestHTTPTransportInboundSSECloseCancelsActiveStreamNoLeak mirrors the
// spirit of TS's "should handle rejected inbound SSE cancel after stream
// errors": Close() while a stream is actively (and indefinitely) open must
// cancel the read promptly rather than hang, and must not leak the
// background goroutine.
func TestHTTPTransportInboundSSECloseCancelsActiveStreamNoLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	sse := &scriptedSSEClient{}
	ready := make(chan struct{})
	var once sync.Once
	sse.respond = func(callNumber int, req *http.Request) (*http.Response, error) {
		resp, _ := newSSEStreamResponse(req)
		once.Do(func() { close(ready) })
		return resp, nil
	}

	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", SSEClient: sse})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	<-ready

	closeDone := make(chan error, 1)
	go func() { closeDone <- transport.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close() hung on an active inbound SSE stream")
	}

	eventuallyNoExtraGoroutines(t, baseline, time.Second)
}
