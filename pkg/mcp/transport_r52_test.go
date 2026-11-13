package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// errBodyAfterEvents yields events, then fails every later Read with err.
type errBodyAfterEvents struct {
	r   io.Reader
	err error
}

func (b *errBodyAfterEvents) Read(p []byte) (int, error) {
	if n, _ := b.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, b.err
}

func (b *errBodyAfterEvents) Close() error { return nil }

// A read error on a POST event-stream response right after an OnError that
// called Close is not reported: TS close() aborts the reader, so the read
// ends with an AbortError. The request context follows the transport-lifetime
// context only asynchronously, so the reader must check the latter too.
func TestHTTPTransportPOSTReadErrorAfterCloseFromOnErrorNotReported(t *testing.T) {
	for i := 0; i < 200; i++ {
		client := sseClientFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       &errBodyAfterEvents{r: strings.NewReader("data: not json\n\n"), err: errors.New("connection reset")},
			}, nil
		})
		var tr *HTTPTransport
		var mu sync.Mutex
		var reported []error
		tr = NewHTTPTransport(HTTPTransportConfig{
			URL:       "http://mcp.test/mcp",
			SSEClient: client,
			OnError: func(err error) {
				mu.Lock()
				reported = append(reported, err)
				mu.Unlock()
				_ = tr.Close()
			},
		})
		if err := tr.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := tr.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", ID: 1, Method: "tools/list"}); err != nil {
			t.Fatal(err)
		}
		waitBGIdle(t, tr)
		mu.Lock()
		n := len(reported)
		mu.Unlock()
		if n != 1 {
			t.Fatalf("iteration %d: OnError called %d times, want 1 (only the malformed event): %v", i, n, reported)
		}
	}
}

// TS applySessionIdFromResponse ignores the session id of a response that
// arrives once the protocol is modern: a GET opened in the legacy era that
// completes after SetProtocolVersion switched to 2026-07-28 does not set it.
func TestHTTPTransportInboundGETSessionIDIgnoredAfterModernSwitch(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	client := sseClientFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			return &http.Response{StatusCode: http.StatusAccepted, Body: http.NoBody}, nil
		}
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type":   []string{"text/event-stream"},
				"Mcp-Session-Id": []string{"s-new"},
			},
			Body: io.NopCloser(strings.NewReader("")),
		}, nil
	})
	var mu sync.Mutex
	var changes []string
	tr := NewHTTPTransport(HTTPTransportConfig{
		URL:                    "http://mcp.test/mcp",
		SSEClient:              client,
		InitialProtocolVersion: LatestLegacyProtocolVersion,
		OnSessionIDChange: func(id string) {
			mu.Lock()
			changes = append(changes, id)
			mu.Unlock()
		},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	<-started
	tr.SetProtocolVersion(LatestProtocolVersion)
	close(release)
	waitBGIdle(t, tr)
	if id := tr.SessionID(); id != "" {
		t.Errorf("SessionID() = %q, want empty", id)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(changes) != 0 {
		t.Errorf("OnSessionIDChange called with %v, want no call", changes)
	}
}
