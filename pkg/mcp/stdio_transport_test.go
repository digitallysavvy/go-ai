package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
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

func TestStdioTransportReceiveInvalidJSON(t *testing.T) {
	transport := NewStdioTransport(StdioTransportConfig{
		Command: "sh",
		Args:    []string{"-c", "printf 'not-json\\n'"},
	})

	if err := transport.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = transport.Close() }()

	_, err := transport.Receive(context.Background())
	if err == nil {
		t.Fatal("expected Receive() unmarshal error")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal message") {
		t.Fatalf("Receive() error = %v", err)
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
