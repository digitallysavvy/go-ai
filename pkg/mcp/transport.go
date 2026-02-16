package mcp

import (
	"context"
)

// Transport defines the interface for MCP transport mechanisms
// Transports handle the low-level communication with MCP servers
type Transport interface {
	// Connect establishes a connection to the MCP server
	Connect(ctx context.Context) error

	// Close closes the connection to the MCP server
	Close() error

	// Send sends a message to the MCP server
	Send(ctx context.Context, message *MCPMessage) error

	// Receive receives a message from the MCP server
	// Returns io.EOF when the connection is closed
	Receive(ctx context.Context) (*MCPMessage, error)

	// IsConnected returns true if the transport is connected
	IsConnected() bool
}

// TransportConfig contains common configuration for all transports
type TransportConfig struct {
	// Timeout for operations (optional)
	TimeoutMS int

	// Headers for HTTP-based transports
	Headers map[string]string

	// EnableLogging enables transport-level logging
	EnableLogging bool
}
