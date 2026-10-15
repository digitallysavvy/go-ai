package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The request timeout bounds requests, not the lifetime of an event stream.
func TestSSETransportStreamOutlivesRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /post\n\n")
		fl.Flush()
		time.Sleep(600 * time.Millisecond)
		_, _ = fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
		fl.Flush()
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	tr := NewSSETransport(SSETransportConfig{URL: srv.URL, TimeoutMS: 200})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := tr.Receive(ctx); err != nil {
		t.Fatalf("event stream was cut off by the request timeout: %v", err)
	}
}

// A dropped event stream is reported to Receive instead of hanging it.
func TestSSETransportReportsStreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /post\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
		hj, _, _ := w.(http.Hijacker).Hijack()
		_ = hj.Close() // abrupt close mid-stream
	}))
	defer srv.Close()
	tr := NewSSETransport(SSETransportConfig{URL: srv.URL})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := tr.Receive(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("Receive err = %v (ctx: %v), want the stream failure", err, ctx.Err())
	}
}

func TestHTTPTransportStreamedPostResponseOutlivesRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fl.Flush()
		time.Sleep(600 * time.Millisecond)
		_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
		fl.Flush()
	}))
	defer srv.Close()
	tr := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, TimeoutMS: 200})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	msg, _ := CreateRequest(1, "tools/call", nil)
	if err := tr.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := tr.Receive(ctx); err != nil {
		t.Fatalf("streamed response was cut off by the request timeout: %v", err)
	}
}

func TestHTTPTransportRequestTimeoutStillAppliesToSlowResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	tr := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL, TimeoutMS: 150})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	msg, _ := CreateRequest(1, "ping", nil)
	start := time.Now()
	err := tr.Send(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}

// Cancelling the context passed to Connect after it returned must not drop
// the stream (TS start() takes no signal that outlives the handshake).
func TestSSETransportStreamSurvivesConnectContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /post\n\n")
		fl.Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
		fl.Flush()
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	tr := NewSSETransport(SSETransportConfig{URL: srv.URL})
	connectCtx, cancelConnect := context.WithCancel(context.Background())
	if err := tr.Connect(connectCtx); err != nil {
		t.Fatal(err)
	}
	cancelConnect()
	defer tr.Close() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := tr.Receive(ctx); err != nil {
		t.Fatalf("stream was dropped when the Connect context was cancelled: %v", err)
	}
}
