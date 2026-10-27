package mcp

import (
	"context"
	"errors"
	"testing"
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
