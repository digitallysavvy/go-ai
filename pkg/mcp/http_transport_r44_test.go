package mcp

import (
	"errors"
	"testing"
)

// TS HttpMCPTransport.start() rejects a second start with an MCPClientError.
func TestHTTPTransportSecondConnectUsesTSError(t *testing.T) {
	transport := NewHTTPTransport(HTTPTransportConfig{URL: "http://localhost:9999/mcp", InitialProtocolVersion: LatestProtocolVersion})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transport.Close() }()
	err := transport.Connect(t.Context())
	var ce *MCPClientError
	if !errors.As(err, &ce) || err.Error() != "MCP HTTP Transport Error: Transport already started. Note: client.connect() calls start() automatically." {
		t.Fatalf("second Connect() error = %#v (%v)", err, err)
	}
}
