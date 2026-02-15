package types

// MessageRole represents the role of a message sender in a conversation
type MessageRole string

const (
	// RoleSystem represents system instructions
	RoleSystem MessageRole = "system"
	// RoleUser represents user input
	RoleUser MessageRole = "user"
	// RoleAssistant represents model responses
	RoleAssistant MessageRole = "assistant"
	// RoleTool represents tool execution results
	RoleTool MessageRole = "tool"
)

// Message represents a single message in a conversation
type Message struct {
	// Role of the message sender
	Role MessageRole `json:"role"`

	// Content parts of the message (text, images, tool results, etc.)
	Content []ContentPart `json:"content"`

	// Optional name for the message sender
	Name string `json:"name,omitempty"`
}

// ContentPart represents a part of message content
// This is an interface to support different content types
type ContentPart interface {
	// ContentType returns the type of content ("text", "image", "tool-result", etc.)
	ContentType() string
}

// TextContent represents text content in a message
type TextContent struct {
	Text string `json:"text"`
}

// ContentType implements ContentPart interface
func (t TextContent) ContentType() string {
	return "text"
}

// ReasoningContent represents reasoning/thinking content in a message
// This is used by models that expose their reasoning process (e.g., OpenAI o1, Anthropic Claude with thinking)
type ReasoningContent struct {
	Text string `json:"text"`
}

// ContentType implements ContentPart interface
func (r ReasoningContent) ContentType() string {
	return "reasoning"
}

// ImageContent represents image content in a message
type ImageContent struct {
	// Image data as bytes
	Image []byte `json:"image"`

	// MIME type of the image (e.g., "image/png", "image/jpeg")
	MimeType string `json:"mimeType"`

	// Optional URL if image is hosted remotely
	URL string `json:"url,omitempty"`
}

// ContentType implements ContentPart interface
func (i ImageContent) ContentType() string {
	return "image"
}

// FileContent represents file content in a message
type FileContent struct {
	// File data as bytes
	Data []byte `json:"data"`

	// MIME type of the file
	MimeType string `json:"mimeType"`

	// Optional filename
	Filename string `json:"filename,omitempty"`
}

// ContentType implements ContentPart interface
func (f FileContent) ContentType() string {
	return "file"
}

// ToolResultContent represents a tool execution result in a message
type ToolResultContent struct {
	// ID of the tool call this result corresponds to
	ToolCallID string `json:"toolCallId"`

	// Name of the tool that was executed
	ToolName string `json:"toolName"`

	// Result of the tool execution (can be any type)
	// DEPRECATED: Use Output for new code. Kept for backward compatibility.
	Result interface{} `json:"result,omitempty"`

	// Optional error if tool execution failed
	Error string `json:"error,omitempty"`

	// Structured output (takes precedence over Result)
	// Use this for rich tool outputs with multiple content blocks
	Output *ToolResultOutput `json:"output,omitempty"`
}

// ContentType implements ContentPart interface
func (t ToolResultContent) ContentType() string {
	return "tool-result"
}

// ToolResultOutputType represents the type of tool result output
type ToolResultOutputType string

const (
	// ToolResultOutputText represents simple text output
	ToolResultOutputText ToolResultOutputType = "text"

	// ToolResultOutputJSON represents JSON output
	ToolResultOutputJSON ToolResultOutputType = "json"

	// ToolResultOutputContent represents structured content with multiple blocks
	ToolResultOutputContent ToolResultOutputType = "content"

	// ToolResultOutputError represents error output
	ToolResultOutputError ToolResultOutputType = "error"
)

// ToolResultOutput represents structured tool result output
type ToolResultOutput struct {
	// Type of the output
	Type ToolResultOutputType `json:"type"`

	// Value for text/json/error types
	Value interface{} `json:"value,omitempty"`

	// Content blocks for content type (array of content blocks)
	Content []ToolResultContentBlock `json:"content,omitempty"`
}

// ToolResultContentBlock represents a content block in tool results
// This interface allows different types of content in tool results
type ToolResultContentBlock interface {
	ToolResultContentType() string
}

// TextContentBlock represents text content in tool results
type TextContentBlock struct {
	// Text content
	Text string `json:"text"`

	// Provider-specific options (e.g., for tool-reference)
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (t TextContentBlock) ToolResultContentType() string {
	return "text"
}

// ImageContentBlock represents an image in tool results
type ImageContentBlock struct {
	// Image data as bytes
	Data []byte `json:"data"`

	// MIME type of the image (e.g., "image/png", "image/jpeg")
	MediaType string `json:"mediaType"`

	// Provider-specific options
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (i ImageContentBlock) ToolResultContentType() string {
	return "image"
}

// FileContentBlock represents a file in tool results
type FileContentBlock struct {
	// File data as bytes
	Data []byte `json:"data"`

	// MIME type of the file
	MediaType string `json:"mediaType"`

	// Optional filename
	Filename string `json:"filename,omitempty"`

	// Provider-specific options
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (f FileContentBlock) ToolResultContentType() string {
	return "file"
}

// CustomContentBlock represents provider-specific content
// This is used for features like tool-reference (Anthropic) or other
// provider-specific content types that don't fit standard categories
type CustomContentBlock struct {
	// Provider-specific options that define what this custom block represents
	// For example: map[string]interface{}{"anthropic": map[string]interface{}{"type": "tool-reference", "toolName": "calc"}}
	ProviderOptions map[string]interface{} `json:"providerOptions"`
}

// ToolResultContentType implements ToolResultContentBlock interface
func (c CustomContentBlock) ToolResultContentType() string {
	return "custom"
}

// Prompt represents a prompt that can be either a simple string or a list of messages
type Prompt struct {
	// Messages in the conversation (nil if using simple text prompt)
	Messages []Message

	// System message (optional, can be empty)
	System string

	// Simple text prompt (empty if using messages)
	Text string
}

// IsSimple returns true if this is a simple text prompt
func (p Prompt) IsSimple() bool {
	return p.Text != "" && len(p.Messages) == 0
}

// IsMessages returns true if this uses message-based prompts
func (p Prompt) IsMessages() bool {
	return len(p.Messages) > 0
}

// Helper functions for creating tool results

// SimpleTextResult creates a tool result with simple text (backward compatible)
// This is the old style and is maintained for backward compatibility.
//
// Example:
//   result := types.SimpleTextResult("call_123", "search", "Found 3 results")
func SimpleTextResult(toolCallID, toolName, result string) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Result:     result,
	}
}

// SimpleJSONResult creates a tool result with JSON value (backward compatible)
//
// Example:
//   result := types.SimpleJSONResult("call_123", "calculate", map[string]interface{}{"answer": 42})
func SimpleJSONResult(toolCallID, toolName string, result interface{}) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Result:     result,
	}
}

// ContentResult creates a tool result with structured content blocks (new style)
// This is the recommended way to create tool results with rich content.
//
// Example:
//   result := types.ContentResult("call_123", "search",
//       types.TextContentBlock{Text: "Search results:"},
//       types.ImageContentBlock{Data: imageBytes, MediaType: "image/png"},
//   )
func ContentResult(toolCallID, toolName string, blocks ...ToolResultContentBlock) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Output: &ToolResultOutput{
			Type:    ToolResultOutputContent,
			Content: blocks,
		},
	}
}

// ErrorResult creates a tool result representing an error
//
// Example:
//   result := types.ErrorResult("call_123", "search", "Network timeout")
func ErrorResult(toolCallID, toolName, errorMsg string) ToolResultContent {
	return ToolResultContent{
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Error:      errorMsg,
		Output: &ToolResultOutput{
			Type:  ToolResultOutputError,
			Value: errorMsg,
		},
	}
}
