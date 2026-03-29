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

// MCPRedirectMode controls how the HTTP transport handles HTTP redirects from
// MCP servers. The default is MCPRedirectError (fail on redirect) — MCP servers
// should not silently redirect clients to other endpoints, as this can mask
// configuration errors or, in adversarial contexts, facilitate SSRF attacks.
type MCPRedirectMode string

const (
	// MCPRedirectError causes the transport to return an error when the server
	// responds with an HTTP redirect (3xx). This is the default.
	MCPRedirectError MCPRedirectMode = "error"

	// MCPRedirectFollow allows the transport to follow HTTP redirects automatically.
	// Use this only when you trust the MCP server and expect redirects (e.g. behind
	// a reverse proxy that normalises URLs).
	MCPRedirectFollow MCPRedirectMode = "follow"
)

// TransportConfig contains common configuration for all transports
type TransportConfig struct {
	// Timeout for operations (optional)
	TimeoutMS int

	// Headers for HTTP-based transports
	Headers map[string]string

	// EnableLogging enables transport-level logging
	EnableLogging bool

	// Redirect controls how HTTP redirects are handled.
	// Default (zero value) is MCPRedirectError — redirects cause an error.
	// Set to MCPRedirectFollow to allow the transport to follow redirects.
	Redirect MCPRedirectMode
}
