package types

import "context"

// Tool represents a tool that can be called by the model
// Tools allow the model to perform actions or retrieve information
// Updated to match TypeScript AI SDK v6.0 with enhanced capabilities
type Tool struct {
	// Name of the tool (must be unique)
	Name string `json:"name"`

	// Description of what the tool does (helps the model decide when to use it)
	Description string `json:"description"`

	// Title is a short, human-readable title for the tool (optional)
	Title string `json:"title,omitempty"`

	// Parameters schema for the tool input
	Parameters interface{} `json:"parameters"`

	// Execute function that runs the tool
	// This is not serialized to JSON
	Execute ToolExecutor `json:"-"`

	// ========================================================================
	// NEW v6.0 Features
	// ========================================================================

	// ToModelOutput allows custom conversion of tool results to model-readable output
	// This can be used to format results in a specific way for the model
	// If nil, the raw result will be used
	ToModelOutput ToModelOutputFunc `json:"-"`

	// InputExamples provides example inputs to help guide the LLM
	// These examples can improve the model's ability to use the tool correctly
	InputExamples []ToolInputExample `json:"inputExamples,omitempty"`

	// Strict enables strict schema enforcement for tool parameters
	// When true, the model must follow the schema exactly
	Strict bool `json:"strict,omitempty"`

	// NeedsApproval indicates whether tool execution requires user approval
	// Can be a boolean or a function that determines approval based on input
	NeedsApproval interface{} `json:"-"` // bool or NeedsApprovalFunc

	// ProviderExecuted indicates whether this tool is executed by the provider (not locally)
	// When true, the tool is executed by the LLM provider (e.g., Anthropic tool-search, xAI file-search)
	// When false or unset, the tool is executed locally by the client using the Execute function
	// This affects error handling and validation behavior
	ProviderExecuted bool `json:"providerExecuted,omitempty"`

	// ProviderOptions contains provider-specific options for the tool
	// This allows passing provider-specific configuration without polluting the main Tool struct
	// Example: Anthropic cache_control, OpenAI response_format, etc.
	// The value should be provider-specific types (e.g., anthropic.ToolOptions)
	ProviderOptions interface{} `json:"-"`

	// ========================================================================
	// Tool Input Streaming Callbacks (for streaming tool calls)
	// ========================================================================

	// OnInputStart is called when tool input streaming starts
	OnInputStart OnInputStartFunc `json:"-"`

	// OnInputDelta is called for each delta during tool input streaming
	OnInputDelta OnInputDeltaFunc `json:"-"`

	// OnInputAvailable is called when complete tool input is available
	OnInputAvailable OnInputAvailableFunc `json:"-"`
}

// ToolExecutor is a function that executes a tool
// It receives the input arguments and returns the result or an error
// Updated in v6.0 to include options with ToolCallID
type ToolExecutor func(ctx context.Context, input map[string]interface{}, options ToolExecutionOptions) (interface{}, error)

// ToolExecutionOptions contains options passed to tool execution
type ToolExecutionOptions struct {
	// ToolCallID is the unique ID of this tool call
	ToolCallID string

	// UserContext is optional user-defined context that flows through the conversation
	// This is set from GenerateTextOptions.ExperimentalContext
	UserContext interface{}

	// Usage contains token usage information up to this point
	Usage *Usage

	// Metadata contains additional metadata
	Metadata map[string]interface{}
}

// ToModelOutputFunc converts a tool result to model-readable output
// This allows custom formatting of tool results
type ToModelOutputFunc func(ctx context.Context, options ToModelOutputOptions) (ToolResultOutput, error)

// ToModelOutputOptions contains options for converting tool results
type ToModelOutputOptions struct {
	// Result is the raw result from tool execution
	Result interface{}

	// ToolCall is the original tool call
	ToolCall *ToolCall

	// Usage is the current token usage
	Usage *Usage

	// Metadata contains additional metadata
	Metadata map[string]interface{}
}

// ToolResultOutput represents the formatted output of a tool result
type ToolResultOutput struct {
	// Type of the result content ("text", "image", "document", "custom")
	Type string

	// Content is the main content (for text type)
	Content string

	// Data contains structured data for non-text types
	Data interface{}
}

// ToolInputExample represents an example input for a tool
type ToolInputExample struct {
	// Input is the example input value
	Input map[string]interface{}

	// Description explains what this example demonstrates (optional)
	Description string
}

// NeedsApprovalFunc determines if a tool call needs approval based on input
type NeedsApprovalFunc func(ctx context.Context, input map[string]interface{}) bool

// OnInputStartFunc is called when tool input streaming starts
type OnInputStartFunc func(ctx context.Context) error

// OnInputDeltaFunc is called for each delta during tool input streaming
type OnInputDeltaFunc func(ctx context.Context, options OnInputDeltaOptions) error

// OnInputDeltaOptions contains options for input delta callbacks
type OnInputDeltaOptions struct {
	// Delta is the incremental text change
	Delta string

	// Value is the current accumulated value (may be partial)
	Value interface{}
}

// OnInputAvailableFunc is called when complete tool input is available
type OnInputAvailableFunc func(ctx context.Context, options OnInputAvailableOptions) error

// OnInputAvailableOptions contains options for input available callbacks
type OnInputAvailableOptions struct {
	// Value is the complete tool input value
	Value map[string]interface{}
}

// ToolCall represents a tool call made by the model
type ToolCall struct {
	// Unique ID for this tool call
	ID string `json:"id"`

	// Name of the tool to call
	ToolName string `json:"toolName"`

	// Arguments to pass to the tool
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult represents the result of executing a tool
type ToolResult struct {
	// ID of the tool call this result corresponds to
	ToolCallID string `json:"toolCallId"`

	// Name of the tool that was executed
	ToolName string `json:"toolName"`

	// Result of the tool execution
	Result interface{} `json:"result"`

	// Error if tool execution failed
	Error error `json:"error,omitempty"`

	// ProviderExecuted indicates if this tool was executed by the provider (not locally)
	// When true, the tool was executed by the LLM provider (e.g., Anthropic tool-search, xAI file-search)
	// When false or unset, the tool was executed locally by the client
	// This affects error handling and validation behavior
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

// ToolChoice specifies how the model should choose tools
type ToolChoice struct {
	// Type of tool choice
	Type ToolChoiceType `json:"type"`

	// Specific tool name (only used when Type is ToolChoiceTool)
	ToolName string `json:"toolName,omitempty"`
}

// ToolChoiceType represents the type of tool choice
type ToolChoiceType string

const (
	// ToolChoiceAuto lets the model decide whether to call tools
	ToolChoiceAuto ToolChoiceType = "auto"

	// ToolChoiceNone prevents the model from calling any tools
	ToolChoiceNone ToolChoiceType = "none"

	// ToolChoiceRequired forces the model to call at least one tool
	ToolChoiceRequired ToolChoiceType = "required"

	// ToolChoiceTool forces the model to call a specific tool
	ToolChoiceTool ToolChoiceType = "tool"
)

// AutoToolChoice returns a ToolChoice that lets the model decide
func AutoToolChoice() ToolChoice {
	return ToolChoice{Type: ToolChoiceAuto}
}

// NoneToolChoice returns a ToolChoice that prevents tool calls
func NoneToolChoice() ToolChoice {
	return ToolChoice{Type: ToolChoiceNone}
}

// RequiredToolChoice returns a ToolChoice that requires at least one tool call
func RequiredToolChoice() ToolChoice {
	return ToolChoice{Type: ToolChoiceRequired}
}

// SpecificToolChoice returns a ToolChoice for a specific tool
func SpecificToolChoice(toolName string) ToolChoice {
	return ToolChoice{
		Type:     ToolChoiceTool,
		ToolName: toolName,
	}
}
