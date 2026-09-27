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
