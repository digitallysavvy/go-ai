package mcp

import (
	"bytes"
	"encoding/json"
)

// ProtocolVersion is the MCP protocol version this client advertises as its
// preferred version during initialization. It is always set to the newest
// supported version.
const ProtocolVersion = "2025-11-25"

// SupportedProtocolVersions lists all MCP protocol versions this client
// understands, in order of preference (newest first). During capability
// negotiation the client advertises these versions so that the server can
// select the highest mutually-supported version.
var SupportedProtocolVersions = []string{
	"2025-11-25",
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// MCPMessage represents a generic MCP protocol message
type MCPMessage struct {
	JSONRpc string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError represents an error in the MCP protocol
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 error codes
const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
)

// MCP-specific error codes
const (
	ErrorCodeToolNotFound      = -32000
	ErrorCodeToolExecutionFail = -32001
	ErrorCodeResourceNotFound  = -32002
	ErrorCodeUnauthorized      = -32003
)

// MCPTool represents a tool exposed via MCP
type MCPTool struct {
	Name         string                 `json:"name"`
	Title        string                 `json:"title,omitempty"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]interface{} `json:"inputSchema"`
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty"`
	Annotations  map[string]interface{} `json:"annotations,omitempty"`
	Meta         map[string]interface{} `json:"_meta,omitempty"`

	titlePresent bool
}

func (t *MCPTool) UnmarshalJSON(data []byte) error {
	type mcpToolJSON struct {
		Name         string                 `json:"name"`
		Title        string                 `json:"title,omitempty"`
		Description  string                 `json:"description,omitempty"`
		InputSchema  map[string]interface{} `json:"inputSchema"`
		OutputSchema map[string]interface{} `json:"outputSchema,omitempty"`
		Annotations  map[string]interface{} `json:"annotations,omitempty"`
		Meta         map[string]interface{} `json:"_meta,omitempty"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out mcpToolJSON
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*t = MCPTool{
		Name:         out.Name,
		Title:        out.Title,
		Description:  out.Description,
		InputSchema:  out.InputSchema,
		OutputSchema: out.OutputSchema,
		Annotations:  out.Annotations,
		Meta:         out.Meta,
	}
	_, t.titlePresent = raw["title"]
	return nil
}

// MCPResource represents a resource exposed via MCP
type MCPResource struct {
	URI         string                 `json:"uri"`
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	MimeType    string                 `json:"mimeType,omitempty"`
	Size        *int64                 `json:"size,omitempty"`
	Metadata    interface{}            `json:"metadata,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
}

// MCPPrompt represents a prompt template exposed via MCP
type MCPPrompt struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Arguments   []MCPPromptArgument    `json:"arguments,omitempty"`
	Template    string                 `json:"template,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// MCPPromptArgument represents an argument to a prompt template
type MCPPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// InitializeParams represents parameters for the initialize request
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// InitializeResult represents the result of an initialize request
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ClientCapabilities represents capabilities of the MCP client
type ClientCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Extensions   map[string]interface{} `json:"extensions,omitempty"`
	Roots        *RootsCapability       `json:"roots,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
	Elicitation  map[string]interface{} `json:"elicitation,omitempty"`
}

// ServerCapabilities represents capabilities of the MCP server
type ServerCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Logging      *LoggingCapability     `json:"logging,omitempty"`
	Prompts      *PromptsCapability     `json:"prompts,omitempty"`
	Resources    *ResourcesCapability   `json:"resources,omitempty"`
	Tools        *ToolsCapability       `json:"tools,omitempty"`
}

// RootsCapability represents the roots capability
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability represents the sampling capability
type SamplingCapability struct{}

// LoggingCapability represents the logging capability
type LoggingCapability struct{}

// PromptsCapability represents the prompts capability
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability represents the resources capability
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolsCapability represents the tools capability
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ClientInfo represents information about the MCP client
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerInfo represents information about the MCP server
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ListToolsParams represents parameters for listing tools
type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListToolsResult represents the result of listing tools
type ListToolsResult struct {
	Tools      []MCPTool `json:"tools"`
	NextCursor string    `json:"nextCursor,omitempty"`
}

// CallToolParams represents parameters for calling a tool
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

func (p CallToolParams) MarshalJSON() ([]byte, error) {
	type callToolParamsJSON struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	args := p.Arguments
	if args == nil {
		args = map[string]interface{}{}
	}
	return json.Marshal(callToolParamsJSON{Name: p.Name, Arguments: args})
}

// CallToolResult represents the result of calling a tool
type CallToolResult struct {
	Content           []ToolResultContent    `json:"content,omitempty"`
	StructuredContent interface{}            `json:"structuredContent,omitempty"`
	ToolResult        interface{}            `json:"toolResult,omitempty"`
	IsError           bool                   `json:"isError,omitempty"`
	Metadata          map[string]interface{} `json:"_meta,omitempty"`

	rawFields                map[string]json.RawMessage
	rawFieldOrder            []string
	isErrorPresent           bool
	contentPresent           bool
	structuredContentPresent bool
	toolResultPresent        bool
	metadataPresent          bool
}

func (r *CallToolResult) UnmarshalJSON(data []byte) error {
	type callToolResultJSON struct {
		Content           []ToolResultContent    `json:"content,omitempty"`
		StructuredContent interface{}            `json:"structuredContent,omitempty"`
		ToolResult        interface{}            `json:"toolResult,omitempty"`
		IsError           bool                   `json:"isError,omitempty"`
		Metadata          map[string]interface{} `json:"_meta,omitempty"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var out callToolResultJSON
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = CallToolResult{
		Content:           out.Content,
		StructuredContent: out.StructuredContent,
		ToolResult:        out.ToolResult,
		IsError:           out.IsError,
		Metadata:          out.Metadata,
		rawFields:         copyRawFields(raw),
		rawFieldOrder:     jsonObjectFieldOrder(data),
	}
	_, r.isErrorPresent = raw["isError"]
	_, r.contentPresent = raw["content"]
	_, r.structuredContentPresent = raw["structuredContent"]
	_, r.toolResultPresent = raw["toolResult"]
	_, r.metadataPresent = raw["_meta"]
	return nil
}

func (r CallToolResult) MarshalJSON() ([]byte, error) {
	contentPresent := r.contentPresent || r.Content != nil
	structuredContentPresent := r.structuredContentPresent || r.StructuredContent != nil
	toolResultPresent := r.toolResultPresent || r.ToolResult != nil
	metadataPresent := r.metadataPresent || r.Metadata != nil
	var buf bytes.Buffer
	buf.WriteByte('{')
	emitted := map[string]bool{}
	first := true
	if contentPresent {
		if err := appendJSONField(&buf, &first, emitted, "content", r.Content); err != nil {
			return nil, err
		}
	}
	if structuredContentPresent {
		if err := appendJSONField(&buf, &first, emitted, "structuredContent", r.StructuredContent); err != nil {
			return nil, err
		}
	}
	if toolResultPresent {
		if err := appendJSONField(&buf, &first, emitted, "toolResult", r.ToolResult); err != nil {
			return nil, err
		}
	}
	if r.IsError || r.isErrorPresent || contentPresent {
		if err := appendJSONField(&buf, &first, emitted, "isError", r.IsError); err != nil {
			return nil, err
		}
	}
	if metadataPresent {
		if err := appendJSONField(&buf, &first, emitted, "_meta", r.Metadata); err != nil {
			return nil, err
		}
	}
	appendRawFields(&buf, &first, emitted, r.rawFields, r.rawFieldOrder)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ToolResultContent represents content in a tool result
type ToolResultContent struct {
	Type        string                 `json:"type"` // "text", "image", "resource", "resource_link"
	Text        string                 `json:"text,omitempty"`
	Data        string                 `json:"data,omitempty"` // base64 for image
	MimeType    string                 `json:"mimeType,omitempty"`
	URI         string                 `json:"uri,omitempty"` // for resource/resource_link
	Name        string                 `json:"name,omitempty"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Resource    *ResourceContent       `json:"resource,omitempty"`
	Metadata    interface{}            `json:"metadata,omitempty"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`

	rawFields             map[string]json.RawMessage
	rawFieldOrder         []string
	textPresent           bool
	textStringPresent     bool
	dataPresent           bool
	dataStringPresent     bool
	mimeTypePresent       bool
	mimeTypeStringPresent bool
}

// UnmarshalJSON tracks presence for fields where the TypeScript MCP
// toModelOutput path uses property-existence checks rather than truthiness.
func (c *ToolResultContent) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = ToolResultContent{
		rawFields:     copyRawFields(raw),
		rawFieldOrder: jsonObjectFieldOrder(data),
	}
	unmarshalStringField(raw, "type", &c.Type)
	c.textPresent, c.textStringPresent = unmarshalStringField(raw, "text", &c.Text)
	c.dataPresent, c.dataStringPresent = unmarshalStringField(raw, "data", &c.Data)
	c.mimeTypePresent, c.mimeTypeStringPresent = unmarshalStringField(raw, "mimeType", &c.MimeType)
	unmarshalStringField(raw, "uri", &c.URI)
	unmarshalStringField(raw, "name", &c.Name)
	unmarshalStringField(raw, "title", &c.Title)
	unmarshalStringField(raw, "description", &c.Description)
	if rawResource, ok := raw["resource"]; ok && string(rawResource) != "null" {
		if err := json.Unmarshal(rawResource, &c.Resource); err != nil {
			return err
		}
	}
	if rawMetadata, ok := raw["metadata"]; ok && string(rawMetadata) != "null" {
		if err := json.Unmarshal(rawMetadata, &c.Metadata); err != nil {
			return err
		}
	}
	if rawMeta, ok := raw["_meta"]; ok && string(rawMeta) != "null" {
		if err := json.Unmarshal(rawMeta, &c.Meta); err != nil {
			return err
		}
	}
	return nil
}

func (c ToolResultContent) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	emitted := map[string]bool{}
	first := true
	appendStringJSONField(&buf, &first, emitted, "type", c.Type, c.Type != "")
	appendStringJSONField(&buf, &first, emitted, "text", c.Text, c.Text != "" || c.textStringPresent)
	appendStringJSONField(&buf, &first, emitted, "data", c.Data, c.Data != "" || c.dataStringPresent)
	appendStringJSONField(&buf, &first, emitted, "mimeType", c.MimeType, c.MimeType != "" || c.mimeTypeStringPresent)
	appendStringJSONField(&buf, &first, emitted, "uri", c.URI, c.URI != "")
	appendStringJSONField(&buf, &first, emitted, "name", c.Name, c.Name != "")
	appendStringJSONField(&buf, &first, emitted, "title", c.Title, c.Title != "")
	appendStringJSONField(&buf, &first, emitted, "description", c.Description, c.Description != "")
	if c.Resource != nil {
		if err := appendJSONField(&buf, &first, emitted, "resource", c.Resource); err != nil {
			return nil, err
		}
	}
	if c.Metadata != nil {
		if err := appendJSONField(&buf, &first, emitted, "metadata", c.Metadata); err != nil {
			return nil, err
		}
	}
	if c.Meta != nil {
		if err := appendJSONField(&buf, &first, emitted, "_meta", c.Meta); err != nil {
			return nil, err
		}
	}
	appendRawFields(&buf, &first, emitted, c.rawFields, c.rawFieldOrder)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func copyRawFields(raw map[string]json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func jsonObjectFieldOrder(data []byte) []string {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil
	}
	var order []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return order
		}
		key, ok := token.(string)
		if !ok {
			return order
		}
		order = append(order, key)
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return order
		}
	}
	return order
}

func unmarshalStringField(raw map[string]json.RawMessage, key string, target *string) (present bool, stringPresent bool) {
	value, ok := raw[key]
	if !ok {
		return false, false
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err == nil {
		*target = decoded
		return true, true
	}
	return true, false
}

func appendStringJSONField(buf *bytes.Buffer, first *bool, emitted map[string]bool, key, value string, emit bool) {
	if !emit {
		return
	}
	keyJSON, _ := json.Marshal(key)
	valueJSON, _ := json.Marshal(value)
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.Write(keyJSON)
	buf.WriteByte(':')
	buf.Write(valueJSON)
	emitted[key] = true
}

func appendJSONField(buf *bytes.Buffer, first *bool, emitted map[string]bool, key string, value interface{}) error {
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return err
	}
	keyJSON, _ := json.Marshal(key)
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.Write(keyJSON)
	buf.WriteByte(':')
	buf.Write(valueJSON)
	emitted[key] = true
	return nil
}

func appendRawFields(buf *bytes.Buffer, first *bool, emitted map[string]bool, rawFields map[string]json.RawMessage, rawFieldOrder []string) {
	seen := map[string]bool{}
	for _, key := range rawFieldOrder {
		if _, ok := rawFields[key]; !ok || emitted[key] {
			continue
		}
		appendRawField(buf, first, key, rawFields[key])
		seen[key] = true
	}
	for key, value := range rawFields {
		if emitted[key] || seen[key] {
			continue
		}
		appendRawField(buf, first, key, value)
	}
}

func appendRawField(buf *bytes.Buffer, first *bool, key string, value json.RawMessage) {
	keyJSON, _ := json.Marshal(key)
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	buf.Write(keyJSON)
	buf.WriteByte(':')
	buf.Write(value)
}

func (c ToolResultContent) hasTextField() bool {
	return c.Text != "" || c.textPresent
}

func (c ToolResultContent) hasDataField() bool {
	return c.Data != "" || c.dataPresent
}

func (c ToolResultContent) hasMimeTypeField() bool {
	return c.MimeType != "" || c.mimeTypePresent
}

// ListResourcesParams represents parameters for listing resources
type ListResourcesParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListResourcesResult represents the result of listing resources
type ListResourcesResult struct {
	Resources  []MCPResource `json:"resources"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

// ReadResourceParams represents parameters for reading a resource
type ReadResourceParams struct {
	URI string `json:"uri"`
}

// ReadResourceResult represents the result of reading a resource
type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

// ResourceContent represents content of a resource
type ResourceContent struct {
	URI      string                 `json:"uri"`
	Name     string                 `json:"name,omitempty"`
	Title    string                 `json:"title,omitempty"`
	MimeType string                 `json:"mimeType,omitempty"`
	Text     string                 `json:"text,omitempty"`
	Blob     string                 `json:"blob,omitempty"` // base64 encoded
	Meta     map[string]interface{} `json:"_meta,omitempty"`
}

// ListPromptsParams represents parameters for listing prompts
type ListPromptsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ListPromptsResult represents the result of listing prompts
type ListPromptsResult struct {
	Prompts    []MCPPrompt `json:"prompts"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

// GetPromptParams represents parameters for getting a prompt
type GetPromptParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

func (p GetPromptParams) MarshalJSON() ([]byte, error) {
	if p.Arguments == nil {
		return json.Marshal(struct {
			Name string `json:"name"`
		}{Name: p.Name})
	}
	return json.Marshal(struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}{Name: p.Name, Arguments: p.Arguments})
}

// GetPromptResult represents the result of getting a prompt
type GetPromptResult struct {
	Description string                 `json:"description,omitempty"`
	Messages    []PromptMessage        `json:"messages"`
	Metadata    map[string]interface{} `json:"_meta,omitempty"`
}

// PromptMessage represents a message in a prompt
type PromptMessage struct {
	Role    string        `json:"role"`
	Content PromptContent `json:"content"`
}

// PromptContent represents content in a prompt message
type PromptContent struct {
	Type        string           `json:"type"` // "text", "image", "resource", "resource_link"
	Text        string           `json:"text,omitempty"`
	Data        string           `json:"data,omitempty"` // base64 for image
	MimeType    string           `json:"mimeType,omitempty"`
	URI         string           `json:"uri,omitempty"` // for resource/resource_link
	Name        string           `json:"name,omitempty"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Resource    *ResourceContent `json:"resource,omitempty"`
}

// McpProviderMetadata is attached to converted MCP tools and propagated through
// tool calls/results under the "mcp" provider metadata key.
type McpProviderMetadata struct {
	ClientName string                 `json:"clientName,omitempty"`
	Title      string                 `json:"title,omitempty"`
	ToolName   string                 `json:"toolName,omitempty"`
	App        map[string]interface{} `json:"app,omitempty"`
}

// LoggingLevel represents the level of logging
type LoggingLevel string

const (
	LoggingLevelDebug     LoggingLevel = "debug"
	LoggingLevelInfo      LoggingLevel = "info"
	LoggingLevelNotice    LoggingLevel = "notice"
	LoggingLevelWarning   LoggingLevel = "warning"
	LoggingLevelError     LoggingLevel = "error"
	LoggingLevelCritical  LoggingLevel = "critical"
	LoggingLevelAlert     LoggingLevel = "alert"
	LoggingLevelEmergency LoggingLevel = "emergency"
)

// SetLevelParams represents parameters for setting log level
type SetLevelParams struct {
	Level LoggingLevel `json:"level"`
}

// LoggingMessageNotification represents a logging message notification
type LoggingMessageNotification struct {
	Level  LoggingLevel `json:"level"`
	Logger string       `json:"logger,omitempty"`
	Data   interface{}  `json:"data"`
}
