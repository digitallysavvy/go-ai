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

// Round 87e: known differences of the MCP transports fixed against TS
// packages/mcp/src/tool (mcp-http-transport.ts send(), mcp-sse-transport.ts
// processEvents(), mcp-stdio/mcp-stdio-transport.ts).

type r87etoolsErrors struct {
	mu   sync.Mutex
	errs []error
}

func (r *r87etoolsErrors) add(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, err)
}

func (r *r87etoolsErrors) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errs)
}

type r87etoolsDoFunc func(*http.Request) (*http.Response, error)

func (f r87etoolsDoFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func r87etoolsResponse(req *http.Request, status int, contentType, body string) *http.Response {
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: h,
		Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

// TS send(): sites that call `this.onerror?.(error)` before throwing are
// reported twice (again by attempt()'s outer catch); errors thrown on their
// own (a failed fetch) once.
func TestR87etoolsHTTPSendOnErrorCounts(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(*http.Request) (*http.Response, error)
		want int
	}{
		{"non-ok status", func(r *http.Request) (*http.Response, error) {
			return r87etoolsResponse(r, 500, "text/plain", "boom"), nil
		}, 2},
		{"unexpected content type", func(r *http.Request) (*http.Response, error) {
			return r87etoolsResponse(r, 200, "text/html", "<p>"), nil
		}, 2},
		{"fetch rejection", func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}, 1},
		{"invalid JSON body", func(r *http.Request) (*http.Response, error) {
			return r87etoolsResponse(r, 200, "application/json", "{"), nil
		}, 1},
	} {
		rec := &r87etoolsErrors{}
		transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
			URL: "http://localhost:9999/mcp", SSEClient: r87etoolsDoFunc(tc.do), OnError: rec.add})
		transport.connected = true
		msg, _ := CreateRequest(1, "ping", nil)
		if err := transport.Send(context.Background(), msg); err == nil {
			t.Fatalf("%s: Send succeeded", tc.name)
		}
		if got := rec.count(); got != tc.want {
			t.Fatalf("%s: OnError calls = %d, want %d: %v", tc.name, got, tc.want, rec.errs)
		}
	}
}

// A caller-cancelled send: TS's outer catch rethrows without reporting, but
// the site's own report before the throw stays.
func TestR87etoolsHTTPSendOnErrorCountWithCancelledCaller(t *testing.T) {
	rec := &r87etoolsErrors{}
	ctx, cancel := context.WithCancel(context.Background())
	transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
		URL: "http://localhost:9999/mcp", OnError: rec.add,
		SSEClient: r87etoolsDoFunc(func(r *http.Request) (*http.Response, error) {
			cancel()
			return r87etoolsResponse(r, 500, "text/plain", "boom"), nil
		})})
	transport.connected = true
	msg, _ := CreateRequest(1, "ping", nil)
	if err := transport.Send(ctx, msg); err == nil {
		t.Fatal("Send succeeded")
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("OnError calls = %d, want 1: %v", got, rec.errs)
	}
}

// The retried attempt(true) after a 401 runs inside the first attempt's try:
// a failed fetch in the retry is reported by both catches.
func TestR87etoolsHTTPSendRetriedFetchFailureReportedTwice(t *testing.T) {
	rec := &r87etoolsErrors{}
	calls := 0
	transport := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion,
		URL: "http://localhost:9999/mcp", OnError: rec.add,
		SSEClient: r87etoolsDoFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return r87etoolsResponse(r, 401, "text/plain", "Unauthorized"), nil
			}
			return nil, errors.New("connection reset")
		}),
		OAuth: &OAuthConfig{AccessToken: "stale", ExpiresAt: time.Now().Add(time.Hour),
			RefreshTokenFunc: func(context.Context, *OAuthConfig) (string, time.Duration, error) {
				return "fresh", time.Hour, nil
			}},
	})
	transport.connected = true
	msg, _ := CreateRequest(1, "ping", nil)
	if err := transport.Send(context.Background(), msg); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Send error = %v", err)
	}
	if got := rec.count(); got != 2 {
		t.Fatalf("OnError calls = %d, want 2: %v", got, rec.errs)
	}
}
