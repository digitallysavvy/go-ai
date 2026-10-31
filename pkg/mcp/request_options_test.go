package mcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMCPClientErrorMessageIsServerText(t *testing.T) {
	// A server reply with a JSON-RPC error surfaces with the server's text only.
	err := GetError(&MCPMessage{Error: &MCPError{Code: -32601, Message: "Method not found", Data: "x"}})
	if err.Error() != "Method not found" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestMCPClientNullIDResponseIsProtocolError(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	var mu sync.Mutex
	var got []error
	client := NewMCPClient(tr, MCPClientConfig{OnError: func(err error) {
		mu.Lock()
		got = append(got, err)
		mu.Unlock()
	}})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck

	tr.in <- &MCPMessage{JSONRpc: "2.0", Error: &MCPError{Code: -32700, Message: "Parse error"}}
	tr.in <- &MCPMessage{JSONRpc: "2.0", ID: float64(4242), Result: []byte(`{}`)}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 protocol errors, got %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(got[0].Error(), "Protocol error: Received a response without a message ID: {") {
		t.Fatalf("null id error = %q", got[0])
	}
	if !strings.HasPrefix(got[1].Error(), "Protocol error: Received a response for an unknown message ID: {") {
		t.Fatalf("unknown id error = %q", got[1])
	}
}

func TestMCPClientPerRequestTimeoutOption(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	tr.onRequest = func(*MCPMessage) {} // never answers
	client := NewMCPClient(tr, MCPClientConfig{RequestTimeoutMS: 60000})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck

	ctx := WithRequestOptions(context.Background(), RequestOptions{TimeoutMS: 5000, MaxTotalTimeoutMS: 30})
	_, err := client.ListTools(ctx)
	var te *TimeoutError
	if !errors.As(err, &te) || te.TimeoutMS != 30 {
		t.Fatalf("err = %v, want TimeoutError after 30ms (min of the two)", err)
	}
	if te.Error() != "Request timed out after 30ms" {
		t.Fatalf("message = %q", te.Error())
	}
}

func TestMCPClientAbortedRequestMessage(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	tr.onRequest = func(*MCPMessage) {}
	client := NewMCPClient(tr, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ListTools(ctx)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Request was aborted" || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// silentInitTransport never answers initialize.
func TestMCPClientInitializationOptionsTimeout(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	tr.silentInit = true
	client := NewMCPClient(tr, MCPClientConfig{RequestTimeoutMS: 60000, InitializationOptions: &RequestOptions{TimeoutMS: 40}})
	// The 40ms init timeout is the behavior under test; the wait below only
	// guards against a hang, so it must not depend on how fast a loaded
	// machine schedules the goroutines (it used to compare against a 5s
	// wall-clock bound).
	done := make(chan error, 1)
	go func() { done <- client.Connect(context.Background()) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Minute):
		t.Fatal("initialization timeout ignored")
	}
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "MCP client initialization timed out after 40ms" {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPClientInitializationAborted(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{})
	tr.silentInit = true
	client := NewMCPClient(tr, MCPClientConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	err := client.Connect(ctx)
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "MCP client initialization was aborted" || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// TS has no default request timeout: with RequestTimeoutMS unset a pending
// request is bounded only by the call's context.
func TestMCPClientHasNoDefaultRequestTimeout(t *testing.T) {
	tr := newLifecycleTransport(ServerCapabilities{Tools: &ToolsCapability{}})
	tr.onRequest = func(*MCPMessage) {}
	client := NewMCPClient(tr, MCPClientConfig{})
	if client.config.RequestTimeoutMS != 0 {
		t.Fatalf("RequestTimeoutMS = %d, want 0", client.config.RequestTimeoutMS)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer client.Close() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := client.ListTools(ctx)
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) {
		t.Fatalf("got a request timeout error %v; only the context should end the call", err)
	}
	var mcpErr *MCPClientError
	if !errors.As(err, &mcpErr) || mcpErr.Message != "Request was aborted" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}
