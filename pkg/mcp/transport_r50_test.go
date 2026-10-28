package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TS close() called from onerror returns: an OnError handler that closes
// the transport from the inbound SSE GET's goroutine must not deadlock Close
// waiting for that same goroutine.
func TestHTTPTransportCloseFromInboundOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	var tr *HTTPTransport
	closed := make(chan struct{})
	var once sync.Once
	tr = NewHTTPTransport(HTTPTransportConfig{
		URL:                    srv.URL,
		InitialProtocolVersion: "2025-06-18",
		OnError: func(error) {
			_ = tr.Close()
			once.Do(func() { close(closed) })
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close called from OnError on the inbound SSE goroutine hung")
	}
	if tr.IsConnected() {
		t.Fatal("connected after Close")
	}
}

// The same for the reader draining a text/event-stream POST response, and
// for OnSessionExpired called from the inbound SSE GET.
func TestHTTPTransportCloseFromBackgroundCallbacks(t *testing.T) {
	t.Run("post-response reader OnError", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: not json\n\n"))
		}))
		defer srv.Close()
		var tr *HTTPTransport
		closed := make(chan struct{})
		var once sync.Once
		tr = NewHTTPTransport(HTTPTransportConfig{
			URL:                    srv.URL,
			InitialProtocolVersion: "2025-06-18",
			OnError: func(error) {
				_ = tr.Close()
				once.Do(func() { close(closed) })
			},
		})
		if err := tr.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		id := 1
		if err := tr.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: id, Method: "tools/list"}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("Close called from OnError on the POST-response reader hung")
		}
	})
	t.Run("inbound GET OnSessionExpired", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		var tr *HTTPTransport
		closed := make(chan struct{})
		var once sync.Once
		f := false
		tr = NewHTTPTransport(HTTPTransportConfig{
			URL:                     srv.URL,
			InitialProtocolVersion:  "2025-06-18",
			InitialSessionID:        "s1",
			TerminateSessionOnClose: &f,
			OnSessionExpired: func(string) {
				_ = tr.Close()
				once.Do(func() { close(closed) })
			},
		})
		if err := tr.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-closed:
		case <-time.After(3 * time.Second):
			t.Fatal("Close called from OnSessionExpired on the inbound SSE goroutine hung")
		}
	})
}
