package types

import "context"

// Tool represents a tool that can be called by the model
// Tools allow the model to perform actions or retrieve information
type Tool struct {
	// Name of the tool (must be unique)
	Name string `json:"name"`

	// Description of what the tool does (helps the model decide when to use it)
	Description string `json:"description"`

	// Parameters schema for the tool input
	Parameters interface{} `json:"parameters"`

	// Execute function that runs the tool
	// This is not serialized to JSON
	Execute ToolExecutor `json:"-"`
}

// ToolExecutor is a function that executes a tool
// It receives the input arguments and returns the result or an error
type ToolExecutor func(ctx context.Context, input map[string]interface{}) (interface{}, error)

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
