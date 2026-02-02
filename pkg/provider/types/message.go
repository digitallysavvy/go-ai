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
	Result interface{} `json:"result"`

	// Optional error if tool execution failed
	Error string `json:"error,omitempty"`
}

// ContentType implements ContentPart interface
func (t ToolResultContent) ContentType() string {
	return "tool-result"
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
