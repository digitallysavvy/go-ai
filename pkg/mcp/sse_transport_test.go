package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type sseRecordingClient struct {
	mu         sync.Mutex
	getReq     *http.Request
	postReqs   []*http.Request
	postBodies []string

	stream io.ReadCloser

	postStatus int
	postBody   string
}

func (c *sseRecordingClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch req.Method {
	case http.MethodGet:
		c.getReq = req.Clone(req.Context())
		if c.stream == nil {
			c.stream = io.NopCloser(bytes.NewReader(nil))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       c.stream,
		}, nil
	case http.MethodPost:
		body, _ := io.ReadAll(req.Body)
		c.postReqs = append(c.postReqs, req.Clone(req.Context()))
		c.postBodies = append(c.postBodies, string(body))
		status := c.postStatus
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(strings.NewReader(c.postBody)),
		}, nil
	default:
		return nil, errors.New("unexpected method")
	}
}

func (c *sseRecordingClient) firstPost() (*http.Request, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.postReqs) == 0 {
		return nil, ""
	}
	return c.postReqs[0], c.postBodies[0]
}

func TestSSETransportEstablishesEndpointAndSendsHeaders(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	client := &sseRecordingClient{stream: reader}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
		Config: TransportConfig{Headers: map[string]string{
			"authorization":   "Bearer test-token",
			"x-custom-header": "test-value",
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	if _, err := writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	if client.getReq.Method != http.MethodGet {
		t.Fatalf("GET method = %s", client.getReq.Method)
	}
	if got := client.getReq.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("GET Accept = %q", got)
	}
	if got := client.getReq.Header.Get("mcp-protocol-version"); got != ProtocolVersion {
		t.Fatalf("GET mcp-protocol-version = %q", got)
	}
	if got := client.getReq.Header.Get("authorization"); got != "Bearer test-token" {
		t.Fatalf("GET authorization = %q", got)
	}

	msg := &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1", Params: []byte(`{"foo":"bar"}`)}
	if err := transport.Send(ctx, msg); err != nil {
		t.Fatalf("Send error = %v", err)
	}
	postReq, postBody := client.firstPost()
	if postReq == nil {
		t.Fatal("missing POST request")
	}
	if postReq.URL.String() != "http://localhost:3000/messages" {
		t.Fatalf("POST URL = %s", postReq.URL.String())
	}
	if got := postReq.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("POST Content-Type = %q", got)
	}
	if got := postReq.Header.Get("Accept"); got != "" {
		t.Fatalf("POST Accept = %q, want omitted like TS SSE transport", got)
	}
	if !strings.Contains(postBody, `"method":"test"`) {
		t.Fatalf("POST body = %s", postBody)
	}
}

func TestSSETransportReceivesMessageWithoutExplicitEvent(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &sseRecordingClient{stream: reader},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: /messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	writer.Write([]byte(`data: {"jsonrpc":"2.0","method":"test","params":{"foo":"bar"},"id":"1"}` + "\n\n")) //nolint:errcheck
	got, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive error = %v", err)
	}
	if got.Method != "test" || got.ID != "1" {
		t.Fatalf("message = %#v", got)
	}
}

func TestSSETransportConnectIsIdempotentWhenAlreadyConnected(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &sseRecordingClient{stream: reader},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("second Connect error = %v, want nil like TS start()", err)
	}
}

func TestSSETransportRejectsCrossOriginEndpoint(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &sseRecordingClient{stream: reader},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3333/messages\n\n")) //nolint:errcheck
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "Endpoint origin does not match connection origin: http://localhost:3333") {
		t.Fatalf("Connect error = %v", err)
	}
	if transport.IsConnected() {
		t.Fatal("transport should not be connected after cross-origin endpoint")
	}
	if err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1"}); err == nil || !strings.Contains(err.Error(), "Not connected") {
		t.Fatalf("Send error = %v", err)
	}
}

func TestSSETransportIgnoresEndpointEventsAfterConnecting(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	client := &sseRecordingClient{stream: reader}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close()                                                           //nolint:errcheck
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3333/messages\n\n")) //nolint:errcheck

	if err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1"}); err != nil {
		t.Fatalf("Send error = %v", err)
	}
	postReq, _ := client.firstPost()
	if postReq == nil || postReq.URL.String() != "http://localhost:3000/messages" {
		t.Fatalf("POST URL = %v", postReq)
	}
}

func TestSSETransportPostErrorReturnsMCPClientErrorAndStaysConnected(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	client := &sseRecordingClient{
		stream:     reader,
		postStatus: http.StatusInternalServerError,
		postBody:   "Internal Server Error",
	}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1"})
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("Send error = %T %v, want MCPClientError", err, err)
	}
	if !strings.Contains(clientErr.Message, "MCP SSE Transport Error: POSTing to endpoint (HTTP 500): Internal Server Error") {
		t.Fatalf("message = %q", clientErr.Message)
	}
	if clientErr.StatusCode != http.StatusInternalServerError || clientErr.URL != "http://localhost:3000/messages" || clientErr.ResponseBody != "Internal Server Error" {
		t.Fatalf("structured HTTP fields = %#v", clientErr)
	}
	if !transport.IsConnected() {
		t.Fatal("transport should stay connected after POST error")
	}
}

func TestSSETransportUsesNegotiatedProtocolVersionInPost(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	client := &sseRecordingClient{stream: reader}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	transport.SetProtocolVersion("2025-06-18")
	if err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "tools/list", ID: "1"}); err != nil {
		t.Fatalf("Send error = %v", err)
	}
	postReq, _ := client.firstPost()
	if got := postReq.Header.Get("mcp-protocol-version"); got != "2025-06-18" {
		t.Fatalf("POST mcp-protocol-version = %q", got)
	}
}

func TestSSETransportInvalidMessageDoesNotCloseStream(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &sseRecordingClient{stream: reader},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	writer.Write([]byte(`event: message` + "\n" + `data: {"jsonrpc":"2.0","id":1,"result":{"__proto__":{"polluted":true}}}` + "\n\n")) //nolint:errcheck
	writer.Write([]byte(`event: message` + "\n" + `data: {"jsonrpc":"2.0","id":"2","result":{"ok":true}}` + "\n\n"))                   //nolint:errcheck
	got, err := transport.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive error = %v", err)
	}
	if got.ID != "2" {
		t.Fatalf("received message = %#v", got)
	}
}

func TestSSETransportUnexpectedCloseReturnsReceiveError(t *testing.T) {
	reader, writer := io.Pipe()
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: &sseRecordingClient{stream: reader},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	writer.Close() //nolint:errcheck

	_, err := transport.Receive(ctx)
	if err == nil || !strings.Contains(err.Error(), "MCP SSE Transport Error: Connection closed unexpectedly") {
		t.Fatalf("Receive error = %v", err)
	}
}

// oauthStaleSSEClient serves the GET SSE handshake normally, and treats any
// POST whose Authorization header still carries the original token as
// unauthorized (401) — a fresh token (whatever value RefreshTokenFunc
// returns) always succeeds. This lets tests simulate a 401 that becomes
// stale mid-flight without needing to coordinate exact call counts.
type oauthStaleSSEClient struct {
	mu           sync.Mutex
	staleToken   string
	authAttempts []string
	getStream    io.ReadCloser
}

func (c *oauthStaleSSEClient) Do(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       c.getStream,
		}, nil
	}
	auth := req.Header.Get("Authorization")
	c.mu.Lock()
	c.authAttempts = append(c.authAttempts, auth)
	stale := auth == "Bearer "+c.staleToken
	c.mu.Unlock()
	if stale {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader("unauthorized"))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`))}, nil
}

// TestSSETransportRetriesOnce401WithRefresh mirrors TS mcp-sse-transport's
// 401 recovery (hash 4b5cb49): a 401 triggers exactly one token refresh and
// the request is retried with the new token.
func TestSSETransportRetriesOnce401WithRefresh(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck

	var refreshes int32
	client := &oauthStaleSSEClient{staleToken: "old", getStream: reader}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
		OAuth: &OAuthConfig{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(time.Hour), // not proactively expired
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				atomic.AddInt32(&refreshes, 1)
				return "fresh", time.Hour, nil
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	if err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: "1"}); err != nil {
		t.Fatalf("Send error = %v", err)
	}
	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("refresh count = %d, want 1", got)
	}
	client.mu.Lock()
	attempts := append([]string(nil), client.authAttempts...)
	client.mu.Unlock()
	if len(attempts) != 2 || attempts[0] != "Bearer old" || attempts[1] != "Bearer fresh" {
		t.Fatalf("auth attempts = %v, want [Bearer old, Bearer fresh]", attempts)
	}
}

// TestSSETransportConcurrent401sRefreshOnceAndStaleRetriesSkipRefresh mirrors
// TS mcp-sse-transport.test.ts's dedup + stale-401 behavior (hash 4b5cb49):
// several concurrent Sends that all observe a 401 for the same stale token
// trigger exactly one refresh, and every Send still succeeds by retrying
// with the (by-then) current token without each triggering its own refresh.
func TestSSETransportConcurrent401sRefreshOnceAndStaleRetriesSkipRefresh(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close() //nolint:errcheck

	var refreshes int32
	client := &oauthStaleSSEClient{staleToken: "old", getStream: reader}
	transport := NewSSETransport(SSETransportConfig{
		URL:       "http://localhost:3000/sse",
		SSEClient: client,
		OAuth: &OAuthConfig{
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(time.Hour),
			RefreshTokenFunc: func(ctx context.Context, cfg *OAuthConfig) (string, time.Duration, error) {
				atomic.AddInt32(&refreshes, 1)
				time.Sleep(30 * time.Millisecond)
				return "fresh", time.Hour, nil
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Connect(ctx) }()
	writer.Write([]byte("event: endpoint\ndata: http://localhost:3000/messages\n\n")) //nolint:errcheck
	if err := <-done; err != nil {
		t.Fatalf("Connect error = %v", err)
	}
	defer transport.Close() //nolint:errcheck

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := transport.Send(ctx, &MCPMessage{JSONRpc: "2.0", Method: "test", ID: id}); err != nil {
				t.Errorf("Send %d error = %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("refresh count = %d, want 1 (deduplicated across concurrent 401s)", got)
	}
}
