package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// sessionSSEClient records requests (method, headers) and lets a test script
// which response each POST/DELETE receives, to exercise the streamable HTTP
// session id lifecycle (hash 241a8c5).
type sessionSSEClient struct {
	mu       sync.Mutex
	requests []*http.Request

	// issueSessionID, when non-empty, is echoed back as `mcp-session-id` on
	// every 2xx POST response.
	issueSessionID string
	// notFoundOnSessionID, when non-empty, makes any request carrying that
	// session id in its own `mcp-session-id` header receive a 404.
	notFoundOnSessionID string

	deleteRequests []*http.Request
}

func (c *sessionSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req.Clone(req.Context()))
	if req.Method == http.MethodDelete {
		c.deleteRequests = append(c.deleteRequests, req.Clone(req.Context()))
	}
	c.mu.Unlock()

	if req.Method == http.MethodDelete {
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	}

	if c.notFoundOnSessionID != "" && req.Header.Get("mcp-session-id") == c.notFoundOnSessionID {
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("session expired"))}, nil
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if c.issueSessionID != "" {
		header.Set("mcp-session-id", c.issueSessionID)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`)),
	}, nil
}

func (c *sessionSSEClient) lastRequest() *http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil
	}
	return c.requests[len(c.requests)-1]
}

// TestHTTPTransportCapturesAndEchoesSessionID mirrors TS HTTPTransport's
// mcp-session-id capture/echo (hash 241a8c5): a session id returned on one
// response is sent as a request header on the next request.
func TestHTTPTransportCapturesAndEchoesSessionID(t *testing.T) {
	sse := &sessionSSEClient{issueSessionID: "session-abc"}
	var changedTo []string
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:               "http://localhost:9999/mcp",
		SSEClient:         sse,
		OnSessionIDChange: func(id string) { changedTo = append(changedTo, id) },
	})
	transport.connected = true

	msg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("first Send error: %v", err)
	}
	if transport.SessionID() != "session-abc" {
		t.Fatalf("SessionID() = %q, want session-abc", transport.SessionID())
	}
	if len(changedTo) != 1 || changedTo[0] != "session-abc" {
		t.Fatalf("OnSessionIDChange calls = %v", changedTo)
	}

	msg2, _ := CreateRequest(2, "tools/list", nil)
	if err := transport.Send(context.Background(), msg2); err != nil {
		t.Fatalf("second Send error: %v", err)
	}
	if got := sse.lastRequest().Header.Get("mcp-session-id"); got != "session-abc" {
		t.Fatalf("second request mcp-session-id header = %q, want session-abc", got)
	}
}

// TestHTTPTransportExpiresSessionIDOn404 mirrors TS HTTPTransport's
// expireSessionId (hash 241a8c5): a 404 for a request carrying the current
// session id clears it and fires OnSessionExpired.
func TestHTTPTransportExpiresSessionIDOn404(t *testing.T) {
	sse := &sessionSSEClient{notFoundOnSessionID: "session-abc"}
	var expired []string
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
		OnSessionExpired: func(id string) { expired = append(expired, id) },
	})
	transport.connected = true

	msg, _ := CreateRequest(1, "tools/list", nil)
	err := transport.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected an error for the 404 response")
	}
	if transport.SessionID() != "" {
		t.Fatalf("SessionID() = %q, want empty after expiry", transport.SessionID())
	}
	if len(expired) != 1 || expired[0] != "session-abc" {
		t.Fatalf("OnSessionExpired calls = %v", expired)
	}
}

// TestHTTPTransportTerminatesSessionOnClose mirrors TS HTTPTransport's
// terminateSessionOnClose default (hash 241a8c5): Close sends a DELETE
// carrying the session id.
func TestHTTPTransportTerminatesSessionOnClose(t *testing.T) {
	sse := &sessionSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
	})
	transport.connected = true

	if err := transport.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	sse.mu.Lock()
	defer sse.mu.Unlock()
	if len(sse.deleteRequests) != 1 {
		t.Fatalf("DELETE requests = %d, want 1", len(sse.deleteRequests))
	}
	if got := sse.deleteRequests[0].Header.Get("mcp-session-id"); got != "session-abc" {
		t.Fatalf("DELETE mcp-session-id = %q, want session-abc", got)
	}
}

// TestHTTPTransportSkipsTerminateSessionOnCloseWhenDisabled verifies
// TerminateSessionOnClose=false suppresses the DELETE.
func TestHTTPTransportSkipsTerminateSessionOnCloseWhenDisabled(t *testing.T) {
	sse := &sessionSSEClient{}
	disabled := false
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:                     "http://localhost:9999/mcp",
		SSEClient:               sse,
		InitialSessionID:        "session-abc",
		TerminateSessionOnClose: &disabled,
	})
	transport.connected = true

	if err := transport.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	sse.mu.Lock()
	defer sse.mu.Unlock()
	if len(sse.deleteRequests) != 0 {
		t.Fatalf("DELETE requests = %d, want 0 when disabled", len(sse.deleteRequests))
	}
}

// TestHTTPTransportNoSessionCloseIsNoop verifies Close does not send a
// DELETE when no session was ever established.
func TestHTTPTransportNoSessionCloseIsNoop(t *testing.T) {
	sse := &sessionSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:       "http://localhost:9999/mcp",
		SSEClient: sse,
	})
	transport.connected = true

	if err := transport.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	sse.mu.Lock()
	defer sse.mu.Unlock()
	if len(sse.deleteRequests) != 0 {
		t.Fatalf("DELETE requests = %d, want 0 without an established session", len(sse.deleteRequests))
	}
}

// TestHTTPTransportOmitsSessionIDOnInitializeRequest mirrors TS's "should
// send initial session id on resumed HTTP requests": an `initialize` request
// never carries `mcp-session-id`, even when a session id was already
// established (e.g. resumed via InitialSessionID), matching TS send()'s
// `includeSessionId: !isInitializeRequest`. A subsequent non-initialize
// request does carry it.
func TestHTTPTransportOmitsSessionIDOnInitializeRequest(t *testing.T) {
	sse := &sessionSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "saved-session",
	})
	transport.connected = true

	initMsg, err := CreateRequest(1, "initialize", nil)
	if err != nil {
		t.Fatalf("CreateRequest error: %v", err)
	}
	if err := transport.Send(context.Background(), initMsg); err != nil {
		t.Fatalf("initialize Send error: %v", err)
	}
	if got := sse.lastRequest().Header.Get("mcp-session-id"); got != "" {
		t.Fatalf("initialize request mcp-session-id = %q, want empty", got)
	}

	listMsg, _ := CreateRequest(2, "tools/list", nil)
	if err := transport.Send(context.Background(), listMsg); err != nil {
		t.Fatalf("tools/list Send error: %v", err)
	}
	if got := sse.lastRequest().Header.Get("mcp-session-id"); got != "saved-session" {
		t.Fatalf("tools/list request mcp-session-id = %q, want saved-session", got)
	}
}

// TestHTTPTransportModernProtocolOmitsSessionIDHeader verifies that the
// modern (2026-07-28) protocol era never attaches `mcp-session-id` on
// outbound requests, matching TS commonHeaders'
// `!this.isModernProtocol() && includeSessionId && this.sessionId`.
func TestHTTPTransportModernProtocolOmitsSessionIDHeader(t *testing.T) {
	sse := &sessionSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "saved-session",
	})
	transport.connected = true
	transport.SetProtocolVersion(LatestProtocolVersion)

	msg, _ := CreateRequest(1, "tools/list", nil)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if got := sse.lastRequest().Header.Get("mcp-session-id"); got != "" {
		t.Fatalf("mcp-session-id = %q, want empty for the modern protocol era", got)
	}
}

// TestHTTPTransportModernProtocolIgnoresResponseSessionIDHeader verifies
// that a `mcp-session-id` response header is not captured for the modern
// protocol era, matching TS applySessionIdFromResponse's
// `if (this.isModernProtocol()) return;`.
func TestHTTPTransportModernProtocolIgnoresResponseSessionIDHeader(t *testing.T) {
	sse := &sessionSSEClient{issueSessionID: "session-abc"}
	var changed []string
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:               "http://localhost:9999/mcp",
		SSEClient:         sse,
		OnSessionIDChange: func(id string) { changed = append(changed, id) },
	})
	transport.connected = true
	transport.SetProtocolVersion(LatestProtocolVersion)

	msg, _ := CreateRequest(1, "tools/list", nil)
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if transport.SessionID() != "" {
		t.Fatalf("SessionID() = %q, want empty: modern protocol should ignore the response header", transport.SessionID())
	}
	if len(changed) != 0 {
		t.Fatalf("OnSessionIDChange calls = %v, want none for the modern protocol era", changed)
	}
}

// TestHTTPTransportModernProtocolSkips404SessionHandling verifies that a 404
// POST response neither expires a session id nor adds a session-related
// error suffix for the modern protocol era, matching TS send()'s
// `if (!this.isModernProtocol() && sessionIdForRequest) { ... } else if
// (!this.isModernProtocol()) { ... }` (both branches gated on legacy only).
func TestHTTPTransportModernProtocolSkips404SessionHandling(t *testing.T) {
	sse := &errorSSEClient{status: http.StatusNotFound, body: "Not Found"}
	var expired []string
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
		OnSessionExpired: func(id string) { expired = append(expired, id) },
	})
	transport.connected = true
	transport.SetProtocolVersion(LatestProtocolVersion)

	// The 404 fires unconditionally (independent of whether a session id was
	// sent) so this isolates the response-side gating: even though the
	// server 404s, the modern protocol era must not expire the session or
	// add either 404 suffix.
	msg, _ := CreateRequest(1, "tools/list", nil)
	err := transport.Send(context.Background(), msg)
	if err == nil {
		t.Fatal("expected an error for the 404 response")
	}
	if strings.Contains(err.Error(), "session expired") || strings.Contains(err.Error(), "does not support HTTP transport") {
		t.Fatalf("Send error = %v, should not mention session expiry or transport support for the modern protocol era", err)
	}
	if len(expired) != 0 {
		t.Fatalf("OnSessionExpired calls = %v, want none for the modern protocol era", expired)
	}
	if transport.SessionID() != "session-abc" {
		t.Fatalf("SessionID() = %q, want unchanged session-abc", transport.SessionID())
	}
}

// TestHTTPTransportModernProtocolSkipsDeleteOnClose verifies Close does not
// send a session-termination DELETE for the modern protocol era, matching TS
// close()'s `!this.isModernProtocol() && this.sessionId && ...`.
func TestHTTPTransportModernProtocolSkipsDeleteOnClose(t *testing.T) {
	sse := &sessionSSEClient{}
	transport := NewHTTPTransport(HTTPTransportConfig{
		URL:              "http://localhost:9999/mcp",
		SSEClient:        sse,
		InitialSessionID: "session-abc",
	})
	transport.connected = true
	transport.SetProtocolVersion(LatestProtocolVersion)

	if err := transport.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	sse.mu.Lock()
	defer sse.mu.Unlock()
	if len(sse.deleteRequests) != 0 {
		t.Fatalf("DELETE requests = %d, want 0 for the modern protocol era", len(sse.deleteRequests))
	}
}
