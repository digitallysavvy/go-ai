package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

type tokensFailProvider struct {
	*fakeOAuthProvider
	fail atomic.Bool
}

func (p *tokensFailProvider) Tokens(ctx context.Context) (*OAuthTokens, error) {
	if p.fail.Load() {
		return nil, errors.New("token store unavailable")
	}
	return p.fakeOAuthProvider.Tokens(ctx)
}

func newEndpointSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TS SSE send() loads the tokens (commonHeaders) inside attempt's try, so a
// failure is reported through onerror before it is thrown.
func TestSSETransportSendReportsTokenLoadFailure(t *testing.T) {
	srv := newEndpointSSEServer(t)
	provider := &tokensFailProvider{fakeOAuthProvider: newFakeOAuthProvider()}
	var mu sync.Mutex
	var reported []error
	tr := NewSSETransport(SSETransportConfig{
		URL:          srv.URL + "/sse",
		AuthProvider: provider,
		OnError: func(err error) {
			mu.Lock()
			reported = append(reported, err)
			mu.Unlock()
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	provider.fail.Store(true)
	err := tr.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"})
	if err == nil || !strings.Contains(err.Error(), "token store unavailable") {
		t.Fatalf("Send error = %v, want the token load failure", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 1 || reported[0] != err {
		t.Fatalf("OnError got %v, want exactly the returned error", reported)
	}
}

// TS send() starts with options.signal.throwIfAborted(): an already-canceled
// ctx fails before the connection check, without loading tokens or
// reporting.
func TestTransportSendCanceledContextFailsFirst(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var reported atomic.Int64
	onError := func(error) { reported.Add(1) }
	sse := NewSSETransport(SSETransportConfig{URL: "http://127.0.0.1:1/sse", OnError: onError})
	if err := sse.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "ping"}); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "Not connected") {
		t.Fatalf("SSE Send error = %v, want context.Canceled", err)
	}
	httpTr := NewHTTPTransport(HTTPTransportConfig{URL: "http://127.0.0.1:1/mcp", OnError: onError})
	if err := httpTr.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "ping"}); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "not connected") {
		t.Fatalf("HTTP Send error = %v, want context.Canceled", err)
	}
	if reported.Load() != 0 {
		t.Fatalf("OnError called %d times", reported.Load())
	}
}

type blockingRoundTripper struct{ started chan struct{} }

func (b *blockingRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// The caller canceling Connect's ctx during the OAuth recovery for a 401 is
// the caller's own abort: Connect returns it without reporting it.
func TestSSETransportConnectAuthCanceledByCallerNotReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	rt := &blockingRoundTripper{started: make(chan struct{}, 1)}
	var reported atomic.Int64
	tr := NewSSETransport(SSETransportConfig{
		URL:            srv.URL + "/sse",
		AuthProvider:   newFakeOAuthProvider(),
		AuthHTTPClient: &http.Client{Transport: rt},
		OnError:        func(error) { reported.Add(1) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- tr.Connect(ctx) }()
	select {
	case <-rt.started:
	case <-time.After(3 * time.Second):
		t.Fatal("OAuth discovery never started")
	}
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Connect error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Connect hung")
	}
	if reported.Load() != 0 {
		t.Fatalf("OnError called %d times for the caller's own cancel", reported.Load())
	}
}
