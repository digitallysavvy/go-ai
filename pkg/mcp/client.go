package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
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
	serverInfo       ServerInfo
	serverCapability ServerCapabilities

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

	// ClientVersion is the version of the client
	ClientVersion string

	// RequestTimeoutMS is the timeout for individual requests in milliseconds
	// Default: 30000 (30 seconds)
	RequestTimeoutMS int

	// EnableLogging enables client-level logging
	EnableLogging bool
}

// NewMCPClient creates a new MCP client with the given transport
func NewMCPClient(transport Transport, config MCPClientConfig) *MCPClient {
	// Set defaults
	if config.ClientName == "" {
		config.ClientName = "go-ai-mcp-client"
	}
	if config.ClientVersion == "" {
		config.ClientVersion = "1.0.0"
	}
	if config.RequestTimeoutMS == 0 {
		config.RequestTimeoutMS = 30000 // 30 seconds
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &MCPClient{
		transport: transport,
		idGen:     NewIDGenerator(),
		pending:   make(map[interface{}]chan *MCPMessage),
		clientInfo: ClientInfo{
			Name:    config.ClientName,
			Version: config.ClientVersion,
		},
		ctx:    ctx,
		cancel: cancel,
		config: config,
	}
}

// Connect connects to the MCP server and initializes the connection
func (c *MCPClient) Connect(ctx context.Context) error {
	// Connect transport
	if err := c.transport.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect transport: %w", err)
	}

	// Start message receiver
	go c.receiveLoop()

	// Send initialize request
	if err := c.initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	c.initialized = true
	return nil
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
		ProtocolVersion: ProtocolVersion,
		Capabilities: ClientCapabilities{
			Experimental: make(map[string]interface{}),
			Roots: &RootsCapability{
				ListChanged: false,
			},
			Sampling: &SamplingCapability{},
		},
		ClientInfo: c.clientInfo,
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	c.serverInfo = result.ServerInfo
	c.serverCapability = result.Capabilities

	// Send initialized notification
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return fmt.Errorf("failed to send initialized notification: %w", err)
	}

	return nil
}

// ListTools lists all available tools from the MCP server
func (c *MCPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := ListToolsParams{}
	var result ListToolsResult

	if err := c.call(ctx, "tools/list", params, &result); err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// TODO: Handle pagination with NextCursor
	return result.Tools, nil
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

// CallTool calls a tool on the MCP server
func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]interface{}) (*CallToolResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("client not initialized")
	}

	params := CallToolParams{
		Name:      name,
		Arguments: arguments,
	}

	var result CallToolResult
	if err := c.call(ctx, "tools/call", params, &result); err != nil {
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}

	return &result, nil
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

// ServerInfo returns information about the connected server
func (c *MCPClient) ServerInfo() ServerInfo {
	return c.serverInfo
}

// ServerCapabilities returns the capabilities of the connected server
func (c *MCPClient) ServerCapabilities() ServerCapabilities {
	return c.serverCapability
}

// call makes a JSON-RPC call and waits for the response
func (c *MCPClient) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	id := c.idGen.Next()
	msg, err := CreateRequest(id, method, params)
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

	// Send request
	if err := c.transport.Send(ctx, msg); err != nil {
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

		// Parse result
		if result != nil && response.Result != nil {
			if err := json.Unmarshal(response.Result, result); err != nil {
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

// handleRequest handles requests from the server
func (c *MCPClient) handleRequest(msg *MCPMessage) {
	// Handle server requests
	// For now, respond with method not found
	response := CreateErrorResponse(msg.ID, ErrorCodeMethodNotFound, "Method not found", nil)
	_ = c.transport.Send(c.ctx, response)
}
