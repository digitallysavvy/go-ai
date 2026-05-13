package mcp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

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
