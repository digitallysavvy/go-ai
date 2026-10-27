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

// gatedSSEBody serves an endpoint event, then blocks reads until release is
// closed and fails with readErr. Close does not unblock it, so the test
// decides when the reader goroutine of a closed stream wakes up.
type gatedSSEBody struct {
	r       io.Reader
	release chan struct{}
	readErr error
}

func (b *gatedSSEBody) Read(p []byte) (int, error) {
	if n, err := b.r.Read(p); n > 0 || err != io.EOF {
		return n, err
	}
	<-b.release
	return 0, b.readErr
}

func (b *gatedSSEBody) Close() error { return nil }

type queuedSSEClient struct {
	mu     sync.Mutex
	bodies []io.ReadCloser
}

func (c *queuedSSEClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if req.Method != http.MethodGet || len(c.bodies) == 0 {
		return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	body := c.bodies[0]
	c.bodies = c.bodies[1:]
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
}

// The reader goroutine of a closed SSE stream must not clear the state of
// the stream a later Connect opened (TS returns silently once its
// abortController is aborted). Before the fix the stale reader's read error
// marked the new connection disconnected and reported it through OnError.
func TestSSETransportStaleReaderDoesNotClobberReconnect(t *testing.T) {
	for _, readErr := range []error{io.EOF, errors.New("connection reset")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			first := &gatedSSEBody{r: strings.NewReader("event: endpoint\ndata: /messages\n\n"), release: make(chan struct{}), readErr: readErr}
			second := &gatedSSEBody{r: strings.NewReader("event: endpoint\ndata: /messages\n\n"), release: make(chan struct{}), readErr: io.EOF}
			defer close(second.release)
			var mu sync.Mutex
			var reported []error
			transport := NewSSETransport(SSETransportConfig{
				URL:       "http://example.test/sse",
				SSEClient: &queuedSSEClient{bodies: []io.ReadCloser{first, second}},
				OnError: func(err error) {
					mu.Lock()
					reported = append(reported, err)
					mu.Unlock()
				},
			})
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("first Connect: %v", err)
			}
			_ = transport.Close()
			if err := transport.Connect(context.Background()); err != nil {
				t.Fatalf("second Connect: %v", err)
			}
			close(first.release)
			time.Sleep(100 * time.Millisecond)

			if !transport.IsConnected() {
				t.Fatal("stale reader of the closed stream disconnected the new one")
			}
			if err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", Method: "notifications/initialized"}); err != nil {
				t.Fatalf("Send on the new stream: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(reported) != 0 {
				t.Fatalf("stale reader reported %v", reported)
			}
			_ = transport.Close()
		})
	}
}
