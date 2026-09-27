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

// ProtocolVersionTransport is implemented by transports that need the MCP
// protocol version negotiated during initialize when constructing subsequent
// requests.
type ProtocolVersionTransport interface {
	SetProtocolVersion(version string)
}

// ProtocolVersionDiscoveryTransport is implemented by transports that support
// probing the 2026-07-28 `server/discover` method before falling back to a
// legacy `initialize` handshake (hash e6a9927). Only HTTPTransport
// (streamable HTTP) reports true, matching TS StreamableHTTPClientTransport;
// stdio and legacy SSE transports do not implement this interface.
type ProtocolVersionDiscoveryTransport interface {
	SupportsProtocolVersionDiscovery() bool
}

// MCPToolParameterHeadersTransport is implemented by transports that support
// binding tool call arguments to HTTP headers via the `x-mcp-header` JSON
// Schema extension (hash 0c60a40). Only HTTPTransport reports true.
type MCPToolParameterHeadersTransport interface {
	SupportsMCPToolParameterHeaders() bool
}

// HeaderedSendTransport is implemented by transports that can attach
// additional per-request HTTP headers to a single JSON-RPC message, used to
// carry x-mcp-header-derived `Mcp-Param-*` headers on a `tools/call` request
// (hash 0c60a40).
type HeaderedSendTransport interface {
	SendWithHeaders(ctx context.Context, message *MCPMessage, headers map[string]string) error
}

// MCPTransportSendOptions mirrors TS's `MCPTransportSendOptions`
// (mcp-transport.ts, hash 97f0565): the per-call options TS's
// `Transport.send(message, options?)` accepts, for transports that need to
// correlate an outgoing message with an in-flight request or resume an
// interrupted one. `Signal` and `Headers` are deliberately omitted here: Go
// already has idiomatic equivalents for both (ctx cancellation, and the
// existing HeaderedSendTransport capability interface above), so mirroring
// them again on this struct would duplicate, not port, TS's shape.
//
// As in TS — where MCPClient never actually populates relatedRequestId,
// resumptionToken, or onresumptiontoken on any send() call it makes
// (grep packages/mcp/src for relatedRequestId: only the type declares it) —
// no transport or client code in this SDK currently sets these fields
// either. The type exists so a custom Transport can accept them from a
// caller that has its own correlation/resumption needs, matching the shape
// custom TS transports can already rely on, not because AI SDK's own
// built-in transports drive resumption through it (they don't, in either
// language, today).
type MCPTransportSendOptions struct {
	// RelatedRequestID associates this outgoing message with an incoming
	// request (TS: `relatedRequestId?: string | number`).
	RelatedRequestID interface{}

	// ResumptionToken resumes a previously interrupted request.
	ResumptionToken string

	// OnResumptionToken receives updated resumption tokens from transports
	// that support them.
	OnResumptionToken func(token string)
}

// SendOptionsTransport is implemented by transports that accept the fuller
// MCPTransportSendOptions on a per-call basis, matching TS's
// `send(message, options?: MCPTransportSendOptions)`. It is a separate
// capability interface (rather than a change to Transport.Send's signature)
// for the same reason HeaderedSendTransport is: most transports need none
// of this, and Go has no optional-parameter syntax to add it for free.
type SendOptionsTransport interface {
	SendWithOptions(ctx context.Context, message *MCPMessage, opts MCPTransportSendOptions) error
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
