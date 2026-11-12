package langchain

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// Round 91 gen tests (TS 2ebc78b654; @langchain/langgraph-sdk 1.14.0,
// @langchain/langgraph 1.7.1).

func r91genSSEServer(t *testing.T, check func(*http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: values\ndata: {\"messages\":[]}\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func r91genSend(t *testing.T, transport *LangSmithDeploymentTransport) []error {
	t.Helper()
	chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{})
	_, gotErrs := drain(t, chunks, errs)
	return gotErrs
}

// core3-06: langgraph-sdk getApiKey falls back to LANGGRAPH_API_KEY,
// LANGSMITH_API_KEY, LANGCHAIN_API_KEY (first non-empty), trimmed with one
// leading and one trailing quote removed (client/base.js getApiKey).
func TestR91genLangGraphAPIKeyFromEnv(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		key  string
		want string
	}{
		{env: map[string]string{"LANGSMITH_API_KEY": " 'k' "}, want: "k"},
		{env: map[string]string{"LANGGRAPH_API_KEY": `"a"`, "LANGSMITH_API_KEY": "b"}, want: "a"},
		{env: map[string]string{"LANGCHAIN_API_KEY": `''c''`}, want: "'c'"},
		{env: map[string]string{"LANGSMITH_API_KEY": "b"}, key: "explicit", want: "explicit"},
		{env: map[string]string{}, want: ""},
	} {
		for _, name := range []string{"LANGGRAPH_API_KEY", "LANGSMITH_API_KEY", "LANGCHAIN_API_KEY"} {
			t.Setenv(name, tc.env[name])
		}
		var got string
		var sent bool
		server := r91genSSEServer(t, func(r *http.Request) { _, sent = r.Header["X-Api-Key"]; got = r.Header.Get("x-api-key") })
		if errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL, APIKey: tc.key})); len(errs) != 0 {
			t.Fatal(errs)
		}
		if got != tc.want || sent != (tc.want != "") {
			t.Errorf("env %v key %q: x-api-key %q (sent %v), want %q", tc.env, tc.key, got, sent, tc.want)
		}
	}
}

// core3-18 / core3-19: the API URL loses one trailing slash, and an empty
// URL is http://localhost:8123 (client/base.js:85-95).
func TestR91genLangGraphURL(t *testing.T) {
	var path string
	server := r91genSSEServer(t, func(r *http.Request) { path = r.URL.Path })
	if errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL + "/x//"})); len(errs) != 0 {
		t.Fatal(errs)
	}
	if path != "/x//runs/stream" {
		t.Fatalf("path = %q, want /x//runs/stream", path)
	}
	for _, url := range []string{"", "/"} {
		if got := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: url}).url; got != "http://localhost:8123" {
			t.Errorf("URL %q: %q, want http://localhost:8123", url, got)
		}
	}
}

// r91genDialFailures fails the first n dials with a connection error.
type r91genDialFailures struct {
	n     int32
	dials atomic.Int32
}

func (d *r91genDialFailures) client() *http.Client {
	var dialer net.Dialer
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if d.dials.Add(1) <= d.n {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
		}
		return dialer.DialContext(ctx, network, addr)
	}}}
}

func r91genFastRetries(t *testing.T) {
	t.Helper()
	old := langGraphRetryDelay
	langGraphRetryDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { langGraphRetryDelay = old })
}

// core3-07: langgraph-sdk AsyncCaller retries the run stream POST up to 4
// times on a connection failure (a POST is not idempotent: HTTP errors and
// other network errors are not retried), then fails with ConnectionError
// (utils/async_caller.js).
func TestR91genLangGraphConnectRetries(t *testing.T) {
	r91genFastRetries(t)
	t.Run("recovers after connection failures", func(t *testing.T) {
		d := &r91genDialFailures{n: 4}
		server := r91genSSEServer(t, nil)
		if errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL, HTTPClient: d.client()})); len(errs) != 0 {
			t.Fatal(errs)
		}
		if got := d.dials.Load(); got != 5 {
			t.Fatalf("dials = %d, want 5", got)
		}
	})
	t.Run("connection error after 4 retries", func(t *testing.T) {
		d := &r91genDialFailures{n: 100}
		server := r91genSSEServer(t, nil)
		errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL, HTTPClient: d.client()}))
		var connErr *LangGraphConnectionError
		if len(errs) != 1 || !errors.As(errs[0], &connErr) {
			t.Fatalf("errors = %v, want a ConnectionError", errs)
		}
		if errs[0].Error() != "Unable to connect to LangGraph server. Please ensure the server is running and accessible. Original error: fetch failed" {
			t.Fatalf("text = %q", errs[0].Error())
		}
		if got := d.dials.Load(); got != 5 {
			t.Fatalf("dials = %d, want 5", got)
		}
	})
	t.Run("HTTP 503 is not retried", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, " boom\n")
		}))
		defer server.Close()
		errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL}))
		var httpErr *LangGraphHTTPError
		if len(errs) != 1 || !errors.As(errs[0], &httpErr) || errs[0].Error() != "HTTP 503:  boom\n" {
			t.Fatalf("errors = %q, want HTTP 503 with the untrimmed body", errs)
		}
		if calls.Load() != 1 {
			t.Fatalf("calls = %d, want 1", calls.Load())
		}
	})
}

// core3-19: an updates event whose __interrupt__ is null still throws
// GraphInterrupt (remote.js:356 `data?.__interrupt__ !== undefined`).
func TestR91genLangGraphNullInterrupt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: updates\ndata: {\"__interrupt__\":null}\n\n")
		_, _ = io.WriteString(w, "event: values\ndata: {\"messages\":[]}\n\n")
	}))
	defer server.Close()
	var seen error
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL})
	chunks, errs := transport.SendMessagesWithCallbacks(context.Background(), nil, &StreamCallbacks{OnError: func(err error) error { seen = err; return nil }})
	_, _ = drain(t, chunks, errs)
	var gi *GraphInterruptError
	if !errors.As(seen, &gi) || gi.Interrupts != nil || gi.Error() != "null" {
		t.Fatalf("OnError got %v, want GraphInterrupt(null)", seen)
	}
}

// Extra (streamWithRetry): a run stream response that is not an event
// stream fails with the TS text.
func TestR91genLangGraphContentTypeChecked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	errs := r91genSend(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL}))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "Expected response header Content-Type to contain 'text/event-stream', got 'application/json'") {
		t.Fatalf("errors = %v", errs)
	}
}
