package mcp

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestValidateStdioCommandForGOOS mirrors TS createChildProcess's Windows
// line-break guard (mcp-stdio/create-child-process.ts, hash b352a6a):
// commands/args with CR or LF are rejected only on Windows.
func TestValidateStdioCommandForGOOS(t *testing.T) {
	t.Run("rejects command with line break on windows", func(t *testing.T) {
		if err := validateStdioCommandForGOOS("windows", "node\n", nil); err == nil {
			t.Fatal("expected error for command containing a line break on windows")
		}
	})

	t.Run("rejects arg with carriage return on windows", func(t *testing.T) {
		if err := validateStdioCommandForGOOS("windows", "npx", []string{"-y", "some-arg\r"}); err == nil {
			t.Fatal("expected error for arg containing a carriage return on windows")
		}
	})

	t.Run("allows clean command and args on windows", func(t *testing.T) {
		if err := validateStdioCommandForGOOS("windows", "npx.cmd", []string{"-y", "mcp-server"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("does not check line breaks on non-windows platforms", func(t *testing.T) {
		if err := validateStdioCommandForGOOS("darwin", "node\n", []string{"arg\r"}); err != nil {
			t.Fatalf("unexpected error on non-windows platform: %v", err)
		}
	})
}

func TestStdioTransportConnectSendReceiveAndClose(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "cat",
		Config:  TransportConfig{},
	})

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !transport.IsConnected() {
		t.Fatal("transport should be connected")
	}

	msg, err := CreateRequest(1, "ping", map[string]interface{}{"ok": true})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if err := transport.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	got, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if got.Method != "ping" {
		t.Fatalf("received method = %q, want ping", got.Method)
	}
	if got.JSONRpc != "2.0" {
		t.Fatalf("received jsonrpc = %q, want 2.0", got.JSONRpc)
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if transport.IsConnected() {
		t.Fatal("transport should be disconnected after Close")
	}
}

func TestStdioTransportAlreadyConnected(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{Command: "cat"})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() first call error = %v", err)
	}
	defer func() { _ = transport.Close() }()

	err := transport.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Fatalf("second Connect() error = %v", err)
	}
}

func TestStdioTransportConnectWithInvalidCommand(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{Command: "definitely-not-a-real-command"})
	err := transport.Connect(context.Background())
	if err == nil {
		t.Fatal("expected Connect() error for invalid command")
	}
	if !strings.Contains(err.Error(), "failed to start command") {
		t.Fatalf("Connect() error = %v", err)
	}
}

func TestStdioTransportSendAndReceiveWhenNotConnected(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{Command: "cat"})

	msg, err := CreateRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}

	if err := transport.Send(context.Background(), msg); err == nil {
		t.Fatal("expected Send() not connected error")
	}

	_, err = transport.Receive(context.Background())
	if err == nil {
		t.Fatal("expected Receive() not connected error")
	}
}

// A stdout line that is not JSON-RPC (servers print logs there) is reported
// through OnError and skipped; later valid messages are still delivered (TS
// calls onerror and keeps reading).
func TestStdioTransportSkipsInvalidJSONLines(t *testing.T) {
	var reported []error
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", `printf 'not-json\n{"level":"info"}\n\n{"jsonrpc":"2.0","id":1,"result":{}}\n'`},
		OnError: func(err error) { reported = append(reported, err) },
	})

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = transport.Close() }()

	msg, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if msg.ID == nil {
		t.Fatalf("message = %+v", msg)
	}
	// JSON that is not a JSON-RPC message (a structured log line) is skipped
	// too, as TS parseJSONRPCMessage rejects it.
	if len(reported) != 2 || !strings.Contains(reported[0].Error(), "failed to unmarshal message") || !strings.Contains(reported[1].Error(), "failed to unmarshal message") {
		t.Fatalf("reported = %v", reported)
	}
}

// The child must outlive the context passed to Connect.
func TestStdioTransportChildOutlivesConnectContext(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", `sleep 0.3; printf '{"jsonrpc":"2.0","id":1,"result":{}}\n'`},
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := transport.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	cancel()
	defer func() { _ = transport.Close() }()
	if _, err := transport.Receive(context.Background()); err != nil {
		t.Fatalf("child was killed with the Connect context: %v", err)
	}
}

type countingWriter struct {
	mu sync.Mutex
	n  int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	return len(p), nil
}

// StdioTransportConfig.Stderr (TS StdioConfig.stderr) receives the child's
// stderr, and a chatty child never blocks on it.
func TestStdioTransportStderrWriter(t *testing.T) {
	w := &countingWriter{}
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		// ~1 MiB on stderr (far more than the pipe buffer), then a message.
		Args:   []string{"-c", `head -c 1048576 /dev/zero >&2; printf '{"jsonrpc":"2.0","id":1,"result":{}}\n'`},
		Stderr: w,
	})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = transport.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := transport.Receive(ctx); err != nil {
		t.Fatalf("Receive() error = %v (child blocked on stderr?)", err)
	}
	_ = transport.Close()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.n != 1048576 {
		t.Fatalf("stderr writer received %d bytes, want 1048576", w.n)
	}
}

// With no Stderr option the child's stderr is inherited from the parent
// process (TS default 'inherit').
func TestStdioTransportInheritsStderrByDefault(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{Command: "true"})
	if err := transport.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transport.Close() }()
	if transport.cmd.Stderr != os.Stderr {
		t.Fatalf("cmd.Stderr = %v, want os.Stderr", transport.cmd.Stderr)
	}
}

func TestStdioTransportReceiveEOF(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", "exit 0"},
	})

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = transport.Close() }()

	_, err := transport.Receive(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Receive() error = %v, want EOF", err)
	}
}

// TestStdioTransportReceiveCloseRace is a permanent regression test for
// R2-2: Receive read t.connected without t.mu, while Connect/Close write it
// under the lock — a background receive loop (mirroring MCPClient.
// receiveLoop) racing a caller's Close() tripped `go test -race`. Adapted
// from the bug report's TestZZBugReviewStdioTransportReceiveCloseRace
// (state/parity/sep_23_2026/bug-review/R2.md), with a bounded iteration
// count instead of 2000000 so the test finishes promptly once Close wins.
func TestStdioTransportReceiveCloseRace(t *testing.T) {
	tr := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", "while true; do echo hi; done"},
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000000; i++ {
			if _, err := tr.Receive(context.Background()); err != nil {
				return
			}
		}
	}()
	_ = tr.Close()
	wg.Wait()
}

// TS StdioMCPTransport sets supportsProtocolVersionDiscovery = true, so the
// client probes server/discover over stdio too.
func TestStdioTransportSupportsProtocolVersionDiscovery(t *testing.T) {
	var transport Transport = NewStdioTransport(StdioTransportConfig{Command: "true"})
	client := NewMCPClient(transport, MCPClientConfig{})
	if !client.supportsProtocolVersionDiscovery() {
		t.Fatal("stdio transport must support protocol version discovery")
	}
}

// TestStdioSendHonorsContextOnFullPipe: a child that never reads stdin fills
// the pipe; Send must return when ctx expires instead of blocking forever.
func TestStdioSendHonorsContextOnFullPipe(t *testing.T) {
	tr := NewStdioTransport(StdioTransportConfig{Command: "sleep", Args: []string{"30"}})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	big := `"` + strings.Repeat("x", 4<<20) + `"`
	msg := &MCPMessage{JSONRpc: "2.0", ID: 0, Method: "m", Params: []byte(big)}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tr.Send(ctx, msg) }()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want deadline exceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not honor ctx")
	}
}
