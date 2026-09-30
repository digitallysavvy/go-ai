package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/retry"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// MCPClient represents an MCP client that can communicate with MCP servers
type MCPClient struct {
	transport   Transport
	idGen       *IDGenerator
	initialized bool

	// Pending requests (for matching responses)
	pendingMu sync.RWMutex
	pending   map[interface{}]chan *MCPMessage

	// Server info
	serverInfo         ServerInfo
	serverCapability   ServerCapabilities
	serverInstructions string

	// initializeResultMu guards initializeResult, which is written once by
	// Connect (from either the modern server/discover or legacy initialize
	// handshake) and read by InitializeResult, matching TS's cached
	// `get initializeResult()` (mcp-client.ts: `_initializeResult`).
	initializeResultMu sync.RWMutex
	initializeResult   InitializeResult

	// protocolEra is "legacy" (classic `initialize` handshake) or "modern"
	// (negotiated via `server/discover`, hash e6a9927). The zero value
	// ("") behaves as "legacy".
	protocolEra string
	// protocolVersion is the negotiated MCP protocol version, injected into
	// modern-era request `_meta` (hash e6a9927).
	protocolVersion string

	// toolHeaderBindings caches, per tool name, the x-mcp-header bindings
	// computed the last time tools were listed (hash 0c60a40). Only
	// populated in the modern protocol era on a transport that supports
	// per-tool parameter headers.
	toolHeaderBindingsMu sync.Mutex
	toolHeaderBindings   map[string][]MCPToolHeaderBinding

	// elicitationHandler, when set via OnElicitationRequest, handles
	// server->client `elicitation/create` requests (matching TS
	// DefaultMCPClient.elicitationRequestHandler). Only one handler may be
	// registered at a time.
	elicitationMu      sync.RWMutex
	elicitationHandler func(ElicitationRequest) (ElicitResult, error)

	// Client info
	clientInfo ClientInfo

	// Context for operations
	ctx    context.Context
	cancel context.CancelFunc

	// Configuration
	config MCPClientConfig
}

// MCPClientConfig contains configuration for the MCP client
type MCPClientConfig struct {
	// ClientName is the name of the client
	ClientName string

	// Name is a deprecated alias for ClientName.
	//
	// Deprecated: use ClientName.
	Name string

	// ClientVersion is the version of the client
	ClientVersion string

	// Capabilities are optional client capabilities advertised during initialize.
	Capabilities ClientCapabilities

	// RequestTimeoutMS is the timeout for individual requests in milliseconds
	// Default: 30000 (30 seconds)
	RequestTimeoutMS int

	// EnableLogging enables client-level logging
	EnableLogging bool

	// MaxRetries is the maximum number of times a failed "tools/call" request
	// is retried with exponential backoff, matching TypeScript's
	// MCPClient maxRetries option (TS prepareMaxRetries). Default: 0 (no
	// retries). A negative value makes Connect return an error (TS throws
	// MCPClientError "maxRetries must be >= 0" from the constructor; Go has
	// no fallible constructor, so the check runs at Connect instead).
	//
	// Only transient failures are retried: HTTP status 408/409/429/>=500 and
	// transport-level connection errors (refused/reset/timeout/broken pipe/
	// closed). JSON-RPC application errors (a non-zero MCPClientError.Code)
	// and successful results with IsError=true are never retried.
	MaxRetries int

	// ProtocolVersionDiscovery controls whether the client probes the
	// 2026-07-28 `server/discover` method before falling back to the legacy
	// `initialize` handshake (hash e6a9927). Default true. Only takes effect
	// when the transport implements ProtocolVersionDiscoveryTransport and
	// reports support (only HTTPTransport does today).
	ProtocolVersionDiscovery *bool

	// OnError, when set, receives non-fatal diagnostics the client would
	// otherwise drop silently: a tool skipped because its x-mcp-header
	// annotation is invalid, a tool call whose header binding failed (hash
	// 0c60a40), or a registered OnElicitationRequest handler that returned
	// an error or an invalid ElicitResult (matching TS
	// DefaultMCPClient.onRequestMessage's this.onError(error) call).
	OnError func(error) `json:"-"`
}

// protocolVersionDiscoveryEnabled reports the effective value of
// ProtocolVersionDiscovery, defaulting to true when unset.
func (c MCPClientConfig) protocolVersionDiscoveryEnabled() bool {
	return c.ProtocolVersionDiscovery == nil || *c.ProtocolVersionDiscovery
}

// NewMCPClient creates a new MCP client with the given transport
func NewMCPClient(transport Transport, config MCPClientConfig) *MCPClient {
	// Set defaults
	if config.ClientName == "" {
		config.ClientName = config.Name
	}
	if config.ClientName == "" {
		config.ClientName = "ai-sdk-mcp-client"
	}
	if config.ClientVersion == "" {
		config.ClientVersion = "1.0.0"
	}
	if config.RequestTimeoutMS == 0 {
		config.RequestTimeoutMS = 30000 // 30 seconds
	}
	// config.MaxRetries is intentionally left as given (even if negative):
	// Connect validates and rejects a negative value, matching TS's
	// constructor-time prepareMaxRetries throw as closely as Go's
	// non-fallible constructor allows.

	ctx, cancel := context.WithCancel(context.Background())

	return &MCPClient{
		transport: transport,
		idGen:     NewIDGenerator(),
		pending:   make(map[interface{}]chan *MCPMessage),
		clientInfo: ClientInfo{
			Name:    config.ClientName,
			Version: config.ClientVersion,
		},
		// Matches TS's pre-connect default (`_initializeResult` field
		// initializer, mcp-client.ts): legacy protocol version, empty
		// capabilities, empty server info.
		initializeResult: InitializeResult{
			ProtocolVersion: LatestLegacyProtocolVersion,
		},
		ctx:    ctx,
		cancel: cancel,
		config: config,
	}
}

// Connect connects to the MCP server and initializes the connection. When
// the transport supports it (hash e6a9927), it first probes the 2026-07-28
// `server/discover` method with a short timeout; on any failure other than a
// hard modern-protocol error it falls back to the legacy `initialize`
// handshake.
func (c *MCPClient) Connect(ctx context.Context) error {
	// TS prepareMaxRetries throws synchronously from the constructor when
	// maxRetries is negative; Go's constructor can't fail, so this is the
	// first fallible call instead.
	if c.config.MaxRetries < 0 {
		return NewMCPClientError(0, "maxRetries must be >= 0", nil)
	}

	// Connect transport
	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect transport: %w", err)
	}

	// Start message receiver
	go c.receiveLoop()

	if c.supportsProtocolVersionDiscovery() {
		discovered, err := c.tryProtocolDiscovery(ctx)
		if err != nil {
			return fmt.Errorf("protocol discovery failed: %w", err)
		}
		if discovered {
			c.initialized = true
			return nil
		}
	}

	c.protocolEra = "legacy"
	c.protocolVersion = LatestLegacyProtocolVersion

	// Send initialize request
	if err := c.initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	c.initialized = true
	return nil
}

// supportsProtocolVersionDiscovery reports whether protocol-version
// discovery should be attempted: the config option is enabled (default
// true) and the transport implements ProtocolVersionDiscoveryTransport and
// reports support.
func (c *MCPClient) supportsProtocolVersionDiscovery() bool {
	if !c.config.protocolVersionDiscoveryEnabled() {
		return false
	}
	transport, ok := c.transport.(ProtocolVersionDiscoveryTransport)
	return ok && transport.SupportsProtocolVersionDiscovery()
}

// tryProtocolDiscovery probes `server/discover`, matching TS
// MCPClient.tryProtocolDiscovery (mcp-client.ts, hash e6a9927). It returns
// (true, nil) when modern-era negotiation succeeded; (false, nil) when the
// server does not support server/discover (or its result doesn't match, in
// which case the caller should fall back to the legacy initialize
// handshake); and (false, err) when the server reported a hard
// modern-protocol error (a JSON-RPC error whose code is in
// ModernProtocolErrorCodes).
func (c *MCPClient) tryProtocolDiscovery(ctx context.Context) (bool, error) {
	c.protocolEra = "modern"
	c.protocolVersion = LatestProtocolVersion
	if versionTransport, ok := c.transport.(ProtocolVersionTransport); ok {
		versionTransport.SetProtocolVersion(c.protocolVersion)
	}

	discoverCtx, cancel := context.WithTimeout(ctx, time.Duration(DefaultProtocolDiscoveryTimeoutMS)*time.Millisecond)
	defer cancel()

	var result DiscoverResult
	if err := c.call(discoverCtx, "server/discover", map[string]interface{}{}, &result); err != nil {
		var mcpErr *MCPClientError
		if errors.As(err, &mcpErr) && intSliceContains(ModernProtocolErrorCodes, mcpErr.Code) {
			return false, err
		}
		return false, nil
	}

	if err := c.applyDiscoverResult(result); err != nil {
		return false, nil
	}
	return true, nil
}

// applyDiscoverResult validates and applies a `server/discover` result,
// matching TS applyDiscoverResult (mcp-client.ts).
func (c *MCPClient) applyDiscoverResult(result DiscoverResult) error {
	if !containsOAuthString(result.SupportedVersions, c.protocolVersion) {
		return fmt.Errorf("server does not support the requested protocol version: %s", c.protocolVersion)
	}
	if info, ok := DiscoverResultServerInfo(result); ok {
		c.serverInfo = info
	}
	c.serverCapability = result.Capabilities
	c.serverInstructions = result.Instructions
	c.setInitializeResult(InitializeResult{
		ProtocolVersion: c.protocolVersion,
		Capabilities:    result.Capabilities,
		ServerInfo:      c.serverInfo,
		Instructions:    result.Instructions,
	})
	return nil
}

// setInitializeResult caches result for InitializeResult, matching TS's
// `this._initializeResult = ...` assignments in applyDiscoverResult and
// applyInitializeResult (mcp-client.ts).
func (c *MCPClient) setInitializeResult(result InitializeResult) {
	c.initializeResultMu.Lock()
	c.initializeResult = result
	c.initializeResultMu.Unlock()
}

// InitializeResult returns the cached result of the handshake that
// established the connection — either the legacy `initialize` response or
// the fields synthesized from a modern `server/discover` response — matching
// TS's `get initializeResult()` (mcp-client.ts). Before Connect succeeds, it
// returns the same pre-connect default TS does: legacy protocol version,
// empty capabilities, and empty server info.
func (c *MCPClient) InitializeResult() InitializeResult {
	c.initializeResultMu.RLock()
	defer c.initializeResultMu.RUnlock()
	return c.initializeResult
}

// Close closes the connection to the MCP server
func (c *MCPClient) Close() error {
	c.cancel()

	// Close all pending requests
	c.pendingMu.Lock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = make(map[interface{}]chan *MCPMessage)
	c.pendingMu.Unlock()

	return c.transport.Close()
}

// initialize sends the initialize request to the server
func (c *MCPClient) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: c.protocolVersion,
		Capabilities:    c.config.Capabilities,
		ClientInfo:      c.clientInfo,
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}
	if !isSupportedProtocolVersion(result.ProtocolVersion) {
		return fmt.Errorf("server's protocol version is not supported: %s", result.ProtocolVersion)
	}

	c.serverInfo = result.ServerInfo
	c.serverCapability = result.Capabilities
	c.serverInstructions = result.Instructions
	c.protocolEra = "legacy"
	c.protocolVersion = result.ProtocolVersion
	c.setInitializeResult(result)
	if versionTransport, ok := c.transport.(ProtocolVersionTransport); ok {
		versionTransport.SetProtocolVersion(result.ProtocolVersion)
	}

	// Send initialized notification
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	return nil
}

// ListTools lists all available tools from the MCP server
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	result, err := c.ListToolsWithCursor(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// ListTools intentionally returns the current page's tools only, matching
	// the TypeScript SDK's listTools behavior. Use GetSerializableTools when the
	// pagination cursor needs to be preserved by the caller.
	return result.Tools, nil
}

// ListToolsWithCursor lists one page of tools from the MCP server, honoring
// an explicit pagination cursor (empty string requests the first page). It
// returns the full ListToolsResult (including NextCursor) so callers can
// paginate manually.
func (c *MCPClient) ListToolsWithCursor(ctx context.Context, cursor string) (*ListToolsResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ListToolsParams{Cursor: cursor}
	var result ListToolsResult
	if err := c.call(ctx, "tools/list", params, &result); err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}
	c.prepareToolDefinitions(&result, cursor == "")
	return &result, nil
}

// prepareToolDefinitions computes and caches x-mcp-header bindings for each
// tool (hash 0c60a40), filtering out any tool whose input schema has an
// invalid x-mcp-header annotation (reported via MCPClientConfig.OnError,
// when set). Matches TS MCPClient.prepareToolDefinitions. Only applies in
// the modern protocol era, and only when the transport supports per-tool
// parameter headers. resetHeaderBindings clears the previously cached
// bindings first, matching TS's behavior of resetting the cache on an
// unpaginated (cursor-less) listing.
func (c *MCPClient) prepareToolDefinitions(result *ListToolsResult, resetHeaderBindings bool) {
	if c.protocolEra != "modern" {
		return
	}
	transport, ok := c.transport.(MCPToolParameterHeadersTransport)
	if !ok || !transport.SupportsMCPToolParameterHeaders() {
		return
	}

	c.toolHeaderBindingsMu.Lock()
	if resetHeaderBindings || c.toolHeaderBindings == nil {
		c.toolHeaderBindings = map[string][]MCPToolHeaderBinding{}
	}
	c.toolHeaderBindingsMu.Unlock()

	tools := make([]MCPTool, 0, len(result.Tools))
	for _, tool := range result.Tools {
		bindings, err := GetMCPToolHeaderBindings(tool.InputSchema)
		if err != nil {
			if c.config.OnError != nil {
				c.config.OnError(NewMCPClientError(0, fmt.Sprintf("Ignoring MCP tool %q: %s", tool.Name, err.Error()), nil))
			}
			continue
		}
		c.toolHeaderBindingsMu.Lock()
		c.toolHeaderBindings[tool.Name] = bindings
		c.toolHeaderBindingsMu.Unlock()
		tools = append(tools, tool)
	}
	result.Tools = tools
}

// toolCallHeaders computes the x-mcp-header-derived request headers for a
// `tools/call` invocation, matching TS getToolRequestHeaders (mcp-client.ts,
// hash 0c60a40). Returns nil outside the modern era, for any other method,
// or when the tool has no header bindings.
func (c *MCPClient) toolCallHeaders(method string, params interface{}) map[string]string {
	if c.protocolEra != "modern" || method != "tools/call" {
		return nil
	}
	callParams, ok := params.(CallToolParams)
	if !ok {
		return nil
	}
	c.toolHeaderBindingsMu.Lock()
	bindings := c.toolHeaderBindings[callParams.Name]
	c.toolHeaderBindingsMu.Unlock()
	if len(bindings) == 0 {
		return nil
	}
	headers, err := CreateMCPToolHeaders(bindings, callParams.Arguments)
	if err != nil {
		if c.config.OnError != nil {
			c.config.OnError(NewMCPClientError(0, fmt.Sprintf("Failed to create MCP headers for tool %q", callParams.Name), err))
		}
		return nil
	}
	return headers
}

// ListAllTools fetches every page of tools from the MCP server, following
// NextCursor until exhausted. This matches TypeScript's MCPClient.tools()
// (mcp-client.ts, hash 1175434), which is used by ConvertToGoAITools /
// ConvertToGoAIToolsWithSchemas so the resulting tool set is complete even
// when the server paginates tools/list.
func (c *MCPClient) ListAllTools(ctx context.Context) ([]MCPTool, error) {
	var allTools []MCPTool
	cursor := ""
	for {
		result, err := c.ListToolsWithCursor(ctx, cursor)
		if err != nil {
			return nil, err
		}
		allTools = append(allTools, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	return allTools, nil
}

// GetSerializableTools returns tool definitions in a format that can be stored or transmitted.
// Unlike ListTools, this returns the complete ListToolsResult including pagination support.
// The result is JSON-serializable and can be used for caching, storage, or transmission.
//
// Example usage:
//
//	tools := server.GetSerializableTools(ctx)
//	// Store tools for later use
//	data, _ := json.Marshal(tools)
//	cache.Set("tools", data)
func (c *MCPClient) GetSerializableTools(ctx context.Context) (*ListToolsResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ListToolsParams{}
	var result ListToolsResult

	if err := c.call(ctx, "tools/list", params, &result); err != nil {
		return nil, fmt.Errorf("failed to get serializable tools: %w", err)
	}

	return &result, nil
}

// CallTool calls a tool on the MCP server. Failed calls are retried with
// exponential backoff up to MCPClientConfig.MaxRetries times (mirrors
// TypeScript mcp-client.ts callToolWithRetry / DEFAULT_MAX_TOOL_CALL_RETRIES).
func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	return c.callToolWithRetry(ctx, func() (*CallToolResult, error) {
		params := CallToolParams{
			Name:      name,
			Arguments: arguments,
		}

		var result CallToolResult
		if err := c.call(ctx, "tools/call", params, &result); err != nil {
			return nil, fmt.Errorf("failed to call tool: %w", err)
		}

		return &result, nil
	})
}

// callToolWithRetry mirrors TypeScript's MCPClient.callToolWithRetry: with
// MaxRetries==0 (the default) it just executes once; otherwise it retries
// transient failures with exponential backoff.
func (c *MCPClient) callToolWithRetry(ctx context.Context, execute func() (*CallToolResult, error)) (*CallToolResult, error) {
	if c.config.MaxRetries <= 0 {
		return execute()
	}

	var result *CallToolResult
	cfg := retry.Config{
		MaxRetries:   c.config.MaxRetries,
		InitialDelay: 2 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		ShouldRetry:  isRetryableMCPToolCallError,
	}

	err := retry.Do(ctx, cfg, func(ctx context.Context) error {
		r, err := execute()
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	if err != nil {
		var retryErr *providererrors.RetryError
		if errors.As(err, &retryErr) {
			return nil, NewMCPClientError(0, retryErr.Message, nil)
		}
		return nil, err
	}
	return result, nil
}

// isRetryableMCPToolCallError mirrors TypeScript's isRetryableMCPToolCallError
// in mcp-client.ts: HTTP status 408/409/429/>=500 is retryable; a JSON-RPC
// application error (non-zero MCPClientError.Code) is never retryable;
// otherwise fall back to transport-level connection error detection (the Go
// analogue of TS's DEFAULT_RETRY_ERROR_CODES string codes).
func isRetryableMCPToolCallError(err error) bool {
	if err == nil {
		return false
	}
	var mcpErr *MCPClientError
	if errors.As(err, &mcpErr) {
		if mcpErr.StatusCode != 0 {
			return mcpErr.StatusCode == 408 || mcpErr.StatusCode == 409 || mcpErr.StatusCode == 429 || mcpErr.StatusCode >= 500
		}
		if mcpErr.Code != 0 {
			return false
		}
	}
	return isRetryableMCPTransportError(err)
}

// isRetryableMCPTransportError detects connection-level failures equivalent
// to TS DEFAULT_RETRY_ERROR_CODES: ConnectionRefused, ConnectionClosed,
// FailedToOpenSocket, ECONNRESET, ECONNREFUSED, ETIMEDOUT, EPIPE.
func isRetryableMCPTransportError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"connection refused", "connection reset", "broken pipe", "connection closed", "i/o timeout"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// ListResources lists all available resources from the MCP server
func (c *MCPClient) ListResources(ctx context.Context) ([]MCPResource, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ListResourcesParams{}
	var result ListResourcesResult

	if err := c.call(ctx, "resources/list", params, &result); err != nil {
		return nil, fmt.Errorf("failed to list resources: %w", err)
	}

	return result.Resources, nil
}

// ReadResource reads a resource from the MCP server
func (c *MCPClient) ReadResource(ctx context.Context, uri string) (*ReadResourceResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ReadResourceParams{
		URI: uri,
	}

	var result ReadResourceResult
	if err := c.call(ctx, "resources/read", params, &result); err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}

	return &result, nil
}

// ListResourceTemplates lists resource templates from the MCP server via
// `resources/templates/list`, matching TS
// MCPClient.listResourceTemplates (mcp-client.ts).
func (c *MCPClient) ListResourceTemplates(ctx context.Context) (*ListResourceTemplatesResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	var result ListResourceTemplatesResult
	if err := c.call(ctx, "resources/templates/list", nil, &result); err != nil {
		return nil, fmt.Errorf("failed to list resource templates: %w", err)
	}

	return &result, nil
}

// ListPrompts lists all available prompts from the MCP server
func (c *MCPClient) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ListPromptsParams{}
	var result ListPromptsResult

	if err := c.call(ctx, "prompts/list", params, &result); err != nil {
		return nil, fmt.Errorf("failed to list prompts: %w", err)
	}

	return result.Prompts, nil
}

// GetPrompt gets a prompt from the MCP server
func (c *MCPClient) GetPrompt(ctx context.Context, name string, arguments map[string]interface{}) (*GetPromptResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := GetPromptParams{
		Name:      name,
		Arguments: arguments,
	}

	var result GetPromptResult
	if err := c.call(ctx, "prompts/get", params, &result); err != nil {
		return nil, fmt.Errorf("failed to get prompt: %w", err)
	}

	return &result, nil
}

// Complete requests argument autocompletion suggestions from the server for
// a prompt or resource template reference (hash 68a739a). It errors if the
// server did not advertise the completions capability.
func (c *MCPClient) Complete(ctx context.Context, params CompleteRequestParams) (*CompleteResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}
	if c.serverCapability.Completions == nil {
		return nil, NewMCPClientError(0, "Server does not support completions", nil)
	}

	var result CompleteResult
	if err := c.call(ctx, "completion/complete", params, &result); err != nil {
		return nil, fmt.Errorf("failed to complete: %w", err)
	}
	return &result, nil
}

// OnElicitationRequest registers handler for server->client
// `elicitation/create` requests (interactive user-input requests), matching
// TS MCPClient.onElicitationRequest (mcp-client.ts). Registering a new
// handler replaces any previously registered one. When no handler is
// registered, an incoming elicitation/create request is answered with a
// "Method not found" (-32601) error, matching TS.
func (c *MCPClient) OnElicitationRequest(handler func(ElicitationRequest) (ElicitResult, error)) {
	c.elicitationMu.Lock()
	c.elicitationHandler = handler
	c.elicitationMu.Unlock()
}

// ServerInfo returns information about the connected server
func (c *MCPClient) ServerInfo() ServerInfo {
	return c.serverInfo
}

// ServerCapabilities returns the capabilities of the connected server
func (c *MCPClient) ServerCapabilities() ServerCapabilities {
	return c.serverCapability
}

// ServerInstructions returns the server-provided instructions from the
// initialize response, if the server supplied them.
func (c *MCPClient) ServerInstructions() string {
	return c.serverInstructions
}

// call makes a JSON-RPC call and waits for the response
func (c *MCPClient) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := c.idGen.Next()
	preparedParams, err := c.prepareRequestParams(method, params)
	if err != nil {
		return err
	}
	msg, err := CreateRequest(id, method, preparedParams)
	if err != nil {
		return err
	}

	// Create response channel
	responseCh := make(chan *MCPMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = responseCh
	c.pendingMu.Unlock()

	// Ensure cleanup
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	// Send request, attaching x-mcp-header-derived headers for a modern-era
	// tools/call when the transport supports per-request headers (hash
	// 0c60a40).
	if err := c.sendMessage(ctx, msg, c.toolCallHeaders(method, params)); err != nil {
		return NewTransportError("failed to send request", err)
	}

	// Wait for response with timeout
	timeout := time.Duration(c.config.RequestTimeoutMS) * time.Millisecond
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case response := <-responseCh:
		if response == nil {
			return fmt.Errorf("connection closed")
		}

		// Check for error
		if response.Error != nil {
			return GetError(response)
		}

		if err := c.validateModernResult(method, response.Result); err != nil {
			return err
		}

		// Parse result
		if result != nil && response.Result != nil {
			if err := unmarshalSafeJSON(response.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}

		return nil

	case <-timer.C:
		return NewTimeoutError(method)

	case <-ctx.Done():
		return ctx.Err()

	case <-c.ctx.Done():
		return fmt.Errorf("client closed")
	}
}

// notify sends a JSON-RPC notification (no response expected)
func (c *MCPClient) notify(ctx context.Context, method string, params interface{}) error {
	msg, err := CreateNotification(method, params)
	if err != nil {
		return err
	}

	return c.transport.Send(ctx, msg)
}

// receiveLoop continuously receives messages from the transport
func (c *MCPClient) receiveLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		msg, err := c.transport.Receive(c.ctx)
		if err != nil {
			// Connection closed or error
			if c.config.EnableLogging {
				fmt.Printf("MCP receive error: %v\n", err)
			}
			return
		}

		// Handle message
		if IsResponse(msg) {
			// Response to a request
			c.pendingMu.RLock()
			ch, ok := c.pending[msg.ID]
			c.pendingMu.RUnlock()

			if ok {
				select {
				case ch <- msg:
				default:
					// Channel full or closed
				}
			}
		} else if IsNotification(msg) {
			// Handle notification (server -> client)
			c.handleNotification(msg)
		} else if IsRequest(msg) {
			// Handle request (server -> client)
			c.handleRequest(msg)
		}
	}
}

// handleNotification handles notifications from the server
func (c *MCPClient) handleNotification(msg *MCPMessage) {
	// Handle server notifications
	// For now, just log them
	if c.config.EnableLogging {
		fmt.Printf("MCP notification: %s\n", msg.Method)
	}
}

// handleRequest handles requests from the server. Matches TS
// DefaultMCPClient.onRequestMessage (mcp-client.ts): `ping` is answered
// directly; `elicitation/create` is dispatched to a registered
// OnElicitationRequest handler (validating params before, and the result
// after, the handler call); every other method is rejected with "Method not
// found".
func (c *MCPClient) handleRequest(msg *MCPMessage) {
	if msg.Method == "ping" {
		response, err := CreateResponse(msg.ID, map[string]interface{}{})
		if err != nil {
			response = CreateErrorResponse(msg.ID, ErrorCodeInternalError, err.Error(), nil)
		}
		_ = c.transport.Send(c.ctx, response)
		return
	}

	if msg.Method != "elicitation/create" {
		response := CreateErrorResponse(msg.ID, ErrorCodeMethodNotFound, fmt.Sprintf("Unsupported request method: %s", msg.Method), nil)
		_ = c.transport.Send(c.ctx, response)
		return
	}

	c.handleElicitationRequest(msg)
}

// handleElicitationRequest dispatches a server->client `elicitation/create`
// request to the registered OnElicitationRequest handler, matching TS
// DefaultMCPClient.onRequestMessage's `elicitation/create` branch
// (mcp-client.ts): no handler registered -> -32601; invalid params -> -32602;
// handler error or an invalid ElicitResult (Action not one of
// accept/decline/cancel) -> -32603, and the error is also reported via
// MCPClientConfig.OnError, matching TS's this.onError(error).
func (c *MCPClient) handleElicitationRequest(msg *MCPMessage) {
	c.elicitationMu.RLock()
	handler := c.elicitationHandler
	c.elicitationMu.RUnlock()

	if handler == nil {
		response := CreateErrorResponse(msg.ID, ErrorCodeMethodNotFound, "No elicitation handler registered on client", nil)
		_ = c.transport.Send(c.ctx, response)
		return
	}

	request, err := parseElicitationRequestParams(msg.Params)
	if err != nil {
		response := CreateErrorResponse(msg.ID, ErrorCodeInvalidParams, fmt.Sprintf("Invalid elicitation request: %s", err.Error()), nil)
		_ = c.transport.Send(c.ctx, response)
		return
	}

	result, err := handler(request)
	if err == nil && result.Action != "accept" && result.Action != "decline" && result.Action != "cancel" {
		err = fmt.Errorf(`invalid elicit result: action must be "accept", "decline", or "cancel"`)
	}
	if err != nil {
		response := CreateErrorResponse(msg.ID, ErrorCodeInternalError, "Failed to handle elicitation request", nil)
		_ = c.transport.Send(c.ctx, response)
		if c.config.OnError != nil {
			c.config.OnError(err)
		}
		return
	}

	response, respErr := CreateResponse(msg.ID, result)
	if respErr != nil {
		response = CreateErrorResponse(msg.ID, ErrorCodeInternalError, respErr.Error(), nil)
	}
	_ = c.transport.Send(c.ctx, response)
}

// parseElicitationRequestParams validates and extracts an ElicitationRequest
// from raw JSON-RPC params, matching TS's
// `ElicitationRequestSchema.safeParse({method, params})`: params must be a
// JSON object with a string "message" field. "requestedSchema" and "_meta"
// are carried through unvalidated (TS: z.unknown() / a loose object).
func parseElicitationRequestParams(raw json.RawMessage) (ElicitationRequest, error) {
	if len(raw) == 0 {
		return ElicitationRequest{}, fmt.Errorf("params is required")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ElicitationRequest{}, fmt.Errorf("params must be an object: %w", err)
	}

	messageRaw, ok := fields["message"]
	if !ok {
		return ElicitationRequest{}, fmt.Errorf("params.message is required")
	}
	var message string
	if err := json.Unmarshal(messageRaw, &message); err != nil {
		return ElicitationRequest{}, fmt.Errorf("params.message must be a string: %w", err)
	}

	var requestedSchema interface{}
	if schemaRaw, ok := fields["requestedSchema"]; ok {
		if err := json.Unmarshal(schemaRaw, &requestedSchema); err != nil {
			return ElicitationRequest{}, fmt.Errorf("params.requestedSchema is invalid: %w", err)
		}
	}

	var meta map[string]interface{}
	if metaRaw, ok := fields["_meta"]; ok {
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return ElicitationRequest{}, fmt.Errorf("params._meta must be an object: %w", err)
		}
	}

	return ElicitationRequest{Message: message, RequestedSchema: requestedSchema, Meta: meta}, nil
}

func isSupportedProtocolVersion(version string) bool {
	for _, supported := range SupportedProtocolVersions {
		if version == supported {
			return true
		}
	}
	return false
}

// prepareRequestParams injects the modern-era `_meta` protocol/capabilities/
// client-info fields into params, matching TS's request() preparedRequest
// (mcp-client.ts, hash e6a9927). Legacy-era requests and `initialize` itself
// are left unchanged.
func (c *MCPClient) prepareRequestParams(method string, params interface{}) (interface{}, error) {
	if c.protocolEra != "modern" || method == "initialize" {
		return params, nil
	}

	paramMap := map[string]interface{}{}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		if len(data) > 0 && string(data) != "null" {
			if err := json.Unmarshal(data, &paramMap); err != nil {
				// params did not marshal to a JSON object (e.g. a bare
				// array/string); there is nowhere to attach _meta, so send
				// it unmodified rather than losing the original params.
				return params, nil
			}
		}
	}

	meta, _ := paramMap["_meta"].(map[string]interface{})
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["io.modelcontextprotocol/protocolVersion"] = c.protocolVersion
	meta["io.modelcontextprotocol/clientCapabilities"] = c.config.Capabilities
	meta["io.modelcontextprotocol/clientInfo"] = c.clientInfo
	paramMap["_meta"] = meta
	return paramMap, nil
}

// validateModernResult enforces the modern-era result invariant: every
// result must carry a `resultType`, and `input_required` is rejected because
// multi-round-trip requests are not yet supported. Matches TS's generic
// response handler (mcp-client.ts, hash e6a9927).
func (c *MCPClient) validateModernResult(method string, rawResult json.RawMessage) error {
	if c.protocolEra != "modern" {
		return nil
	}
	var probe struct {
		ResultType *string `json:"resultType"`
	}
	if len(rawResult) > 0 {
		_ = json.Unmarshal(rawResult, &probe)
	}
	if probe.ResultType == nil {
		return NewMCPClientError(0, "Modern MCP result is missing resultType", nil)
	}
	if *probe.ResultType == "input_required" {
		return NewMCPClientError(0, "Server requested additional input, but multi round-trip requests are not supported yet", nil)
	}
	return nil
}

// sendMessage sends msg via the transport, attaching headers through
// HeaderedSendTransport when the transport supports it and headers is
// non-empty (hash 0c60a40).
func (c *MCPClient) sendMessage(ctx context.Context, msg *MCPMessage, headers map[string]string) error {
	if len(headers) > 0 {
		if headered, ok := c.transport.(HeaderedSendTransport); ok {
			return headered.SendWithHeaders(ctx, msg, headers)
		}
	}
	return c.transport.Send(ctx, msg)
}

func intSliceContains(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
