package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// r92carryHangingServer accepts requests and never answers until the test
// ends.
func r92carryHangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv
}

func r92carryRetryable(err error) bool {
	r, ok := err.(interface{ IsRetryable() bool })
	return ok && r.IsRetryable()
}

// TS handle-fetch-error.ts: a transport timeout the caller did not set
// (undici UND_ERR_HEADERS_TIMEOUT / UND_ERR_CONNECT_TIMEOUT, ETIMEDOUT) is
// the retryable "Cannot connect to API" APICallError; only the caller's
// abort or timeout (AbortError / TimeoutError) is returned as is.
func TestR92carryResponseHeaderTimeoutIsConnectError(t *testing.T) {
	srv := r92carryHangingServer(t)
	hc := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 50 * time.Millisecond}}
	c := NewClient(Config{BaseURL: srv.URL, HTTPClient: hc})
	for name, call := range map[string]func() error{
		"Do": func() error { _, err := c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/x", Body: map[string]int{"a": 1}}); return err },
		"DoStream": func() error {
			resp, err := c.DoStream(context.Background(), Request{Method: http.MethodPost, Path: "/x", Body: map[string]int{"a": 1}})
			if resp != nil {
				_ = resp.Body.Close()
			}
			return err
		},
	} {
		err := call()
		if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !r92carryRetryable(err) {
			t.Fatalf("%s: header timeout = %v, want the retryable Cannot connect to API error", name, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: a transport timeout must not read as the caller's deadline", name)
		}
		var urlErr *url.Error
		var netErr net.Error
		if !errors.As(err, &urlErr) || !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("%s: the cause is no longer reachable with errors.As: %v", name, err)
		}
		if _, ok := NoResponseError(err); ok {
			t.Fatalf("%s: NoResponseError treated a transport timeout as an abort", name)
		}
	}
}

func TestR92carryCallerDeadlineAndClientTimeoutStayAborts(t *testing.T) {
	srv := r92carryHangingServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewClient(Config{BaseURL: srv.URL}).Do(ctx, Request{Method: http.MethodGet, Path: "/x"})
	if err == nil || strings.HasPrefix(err.Error(), "Cannot connect") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline = %v, want it unchanged", err)
	}
	if got, ok := NoResponseError(err); !ok || got != err {
		t.Fatalf("NoResponseError(caller deadline) = %v, %v", got, ok)
	}

	_, err = NewClient(Config{BaseURL: srv.URL, Timeout: 50 * time.Millisecond}).Do(context.Background(), Request{Method: http.MethodGet, Path: "/x"})
	if err == nil || strings.HasPrefix(err.Error(), "Cannot connect") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Client.Timeout = %v, want it unchanged", err)
	}
	if _, ok := NoResponseError(err); !ok {
		t.Fatal("NoResponseError(Client.Timeout) = false")
	}

	cctx, ccancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); ccancel() }()
	_, err = NewClient(Config{BaseURL: srv.URL}).Do(cctx, Request{Method: http.MethodGet, Path: "/x"})
	if !errors.Is(err, context.Canceled) || strings.HasPrefix(err.Error(), "Cannot connect") {
		t.Fatalf("cancel = %v, want it unchanged", err)
	}
}

// r92carryDialTimeout is a dial timeout like net's own (Timeout and
// errors.Is(context.DeadlineExceeded)).
type r92carryDialTimeout struct{}

func (r92carryDialTimeout) Error() string        { return "i/o timeout" }
func (r92carryDialTimeout) Timeout() bool        { return true }
func (r92carryDialTimeout) Temporary() bool      { return true }
func (r92carryDialTimeout) Is(target error) bool { return target == context.DeadlineExceeded }

func TestR92carryDialTimeoutWithLiveContextIsConnectError(t *testing.T) {
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: network, Err: r92carryDialTimeout{}}
	}}}
	_, err := NewClient(Config{BaseURL: "http://127.0.0.1:1", HTTPClient: hc}).Do(context.Background(), Request{Method: http.MethodGet, Path: "/"})
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !r92carryRetryable(err) {
		t.Fatalf("dial timeout = %v, want the retryable Cannot connect to API error", err)
	}
	if _, ok := NoResponseError(err); ok {
		t.Fatal("NoResponseError treated a dial timeout as an abort")
	}
}

func TestR92carryTransportErrorWithoutContext(t *testing.T) {
	// An OS connect timeout is ETIMEDOUT, no deadline: TS retries it.
	osTimeout := &url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ETIMEDOUT}}
	if got := TransportError(osTimeout); !strings.HasPrefix(got.Error(), "Cannot connect to API: ") || !r92carryRetryable(got) {
		t.Fatalf("TransportError(ETIMEDOUT) = %v", got)
	}
	if _, ok := NoResponseError(osTimeout); ok {
		t.Fatal("NoResponseError(ETIMEDOUT) = true")
	}
	// net/http's response-header timeout.
	srv := r92carryHangingServer(t)
	hc := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 50 * time.Millisecond}}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, raw := hc.Do(req)
	if got := TransportError(raw); !strings.HasPrefix(got.Error(), "Cannot connect to API: ") {
		t.Fatalf("TransportError(header timeout) = %v", got)
	}
	// A bare deadline and a cancellation stay unchanged.
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		if got := TransportError(err); got != err {
			t.Fatalf("TransportError(%v) = %v", err, got)
		}
	}
}
