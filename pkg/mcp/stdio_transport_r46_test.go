package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
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

// The bounded Close must not trade the block for a leak: when it returns, the
// child has been reaped and os/exec's stderr copy goroutine has stopped, even
// though a grandchild still holds the stderr pipe.
func TestStdioTransportCloseReapsChildAndStopsStderrCopy(t *testing.T) {
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
	go func() { _, _ = transport.Receive(context.Background()) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("child never started")
	}
	cmd := transport.cmd
	const copyFrame = "os/exec.(*Cmd).writerDescriptor"
	buf := make([]byte, 1<<20)
	if !strings.Contains(string(buf[:runtime.Stack(buf, true)]), copyFrame) {
		t.Fatal("precondition: no os/exec stderr copy goroutine before Close")
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("Close returned before the child was reaped")
	}
	stacks := string(buf[:runtime.Stack(buf, true)])
	if strings.Contains(stacks, copyFrame) {
		t.Fatalf("stderr copy goroutine still running after Close:\n%s", stacks)
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
