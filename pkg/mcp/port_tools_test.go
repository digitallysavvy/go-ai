package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TS 95b78c93e7: a POST whose SSE response ends (cleanly or broken) before its
// JSON-RPC response gets a request-specific error so the client rejects.
func TestPortToolsHTTPSSEEndsEarlyRejectsPendingRequest(t *testing.T) {
	for _, mode := range []string{"ends cleanly", "breaks off"} {
		t.Run(mode, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, ": keep-alive\n\n")
				w.(http.Flusher).Flush()
				<-release
				if mode == "breaks off" {
					panic(http.ErrAbortHandler)
				}
			}))
			defer srv.Close()
			tr := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL, OnError: func(error) {}})
			tr.connected = true
			msg, _ := CreateRequest(7, "tools/call", nil)
			if err := tr.Send(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			close(release)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			var got *MCPMessage
			for ctx.Err() == nil {
				m, err := tr.Receive(ctx)
				if err == nil && m != nil {
					got = m
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if got == nil || got.Error == nil || got.Error.Code != -32000 ||
				!strings.Contains(got.Error.Message, "SSE stream ended before the response for request 7") {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestPortToolsHTTPSSEMatchingResponseSuppressesEndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
	}))
	defer srv.Close()
	tr := NewHTTPTransport(HTTPTransportConfig{InitialProtocolVersion: LatestLegacyProtocolVersion, URL: srv.URL, OnError: func(error) {}})
	tr.connected = true
	msg, _ := CreateRequest(7, "tools/call", nil)
	if err := tr.Send(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	var all []*MCPMessage
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		m, err := tr.Receive(ctx)
		cancel()
		if err != nil || m == nil {
			break
		}
		all = append(all, m)
	}
	if len(all) != 1 || all[0].Error != nil {
		t.Fatalf("messages = %+v", all)
	}
}
