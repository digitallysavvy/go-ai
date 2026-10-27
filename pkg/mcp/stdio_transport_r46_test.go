package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TS StdioMCPTransport.send throws MCPClientError("StdioClientTransport not
// connected") before start(). Go keeps *TransportError reachable for callers
// that match it with errors.As.
func TestStdioTransportSendNotConnectedUsesTSError(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{Command: "unused"})
	err := transport.Send(context.Background(), &MCPMessage{JSONRpc: "2.0", Method: "ping"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got, want := err.Error(), "StdioClientTransport not connected"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	var clientErr *MCPClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("expected *MCPClientError, got %T", err)
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("expected *TransportError to stay reachable, got %T", err)
	}
}

// TS close() aborts the child and returns without waiting for it. Go waits
// for the child, but a grandchild that keeps the stderr pipe open must not
// hold Close (and the transport lock) until it exits.
func TestStdioTransportCloseDoesNotWaitForGrandchildStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
	var stderr bytes.Buffer
	started := make(chan struct{})
	var startedOnce sync.Once
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "/bin/sh",
		Args:    []string{"-c", "sleep 6 & echo started; exec cat"},
		Stderr:  &lockedWriter{w: &stderr},
		OnError: func(error) { startedOnce.Do(func() { close(started) }) },
	})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Wait until the shell has started the grandchild: its "started" line
	// is not JSON-RPC and reaches OnError.
	go func() { _, _ = transport.Receive(context.Background()) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("child never started")
	}
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- transport.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Close blocked on the grandchild's stderr")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Close took %s", elapsed)
	}
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
