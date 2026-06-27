package types

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

const (
	// ToolTypeFunction is the default locally executed function tool.
	ToolTypeFunction = "function"

	// ToolTypeDynamic is a dynamic tool whose schema can vary at runtime.
	ToolTypeDynamic = "dynamic"

	// ToolTypeProviderDefined is a provider-defined native tool. The wire value
	// remains "provider" to match the TypeScript SDK shape.
	ToolTypeProviderDefined = "provider"

	// ToolTypeProviderExecuted is a provider-executed tool.
	ToolTypeProviderExecuted = "provider-executed"
)

// Tool represents a tool that can be called by the model
// Tools allow the model to perform actions or retrieve information
// Updated to match TypeScript AI SDK v6.0 with enhanced capabilities
type Tool struct {
	// Name of the tool (must be unique)
	Name string `json:"name"`

	// Description of what the tool does (helps the model decide when to use it)
	Description string `json:"description"`

	// DescriptionFunc resolves the tool description dynamically at call-prep
	// time using the active per-tool context and sandbox.
	DescriptionFunc func(ctx context.Context, options ToolDescriptionOptions) string `json:"-"`

	// Title is a short, human-readable title for the tool (optional)
	Title string `json:"title,omitempty"`

	// Parameters schema for the tool input
	Parameters interface{} `json:"parameters"`

	// OutputSchema is the schema for provider-executed tool output. It mirrors
	// TypeScript provider tool outputSchema and is not sent as part of function
	// tool definitions.
	OutputSchema interface{} `json:"outputSchema,omitempty"`

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

	// ContextSchema optionally validates the tool-specific context passed to the
	// tool execution and approval callbacks.
	ContextSchema schema.Schema `json:"-"`

	// ToolApproval indicates whether tool execution requires user approval.
	// Can be a boolean, ToolApprovalStatus string, ToolNeedsApprovalFunc, or
	// deprecated NeedsApprovalFunc.
	ToolApproval interface{} `json:"-"`

	// NeedsApproval is a deprecated alias for ToolApproval.
	// Can be a boolean or a function that determines approval based on input.
	// Deprecated: use ToolApproval.
	NeedsApproval interface{} `json:"-"`

	// ProviderExecuted indicates whether this tool is executed by the provider (not locally)
	// When true, the tool is executed by the LLM provider (e.g., Anthropic tool-search, xAI file-search)
	// When false or unset, the tool is executed locally by the client using the Execute function
	// This affects error handling and validation behavior
	ProviderExecuted bool `json:"providerExecuted,omitempty"`

	// SupportsDeferredResults indicates that this provider-executed tool may not
	// return its result in the same response that contains the tool call. When
	// true, the step loop continues even if FinishReason is not ToolCalls,
	// waiting for the provider to deliver the result in a later response.
	// Only meaningful when ProviderExecuted is also true.
	SupportsDeferredResults bool `json:"supportsDeferredResults,omitempty"`

	// ProviderOptions contains provider-specific options for the tool
	// This allows passing provider-specific configuration without polluting the main Tool struct
	// Example: Anthropic cache_control, OpenAI response_format, etc.
	// The value should be provider-specific types (e.g., anthropic.ToolOptions)
	ProviderOptions interface{} `json:"-"`

	// ProviderMetadata carries provider-specific metadata for the tool and is
	// propagated onto tool calls and results produced for this tool.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// Metadata carries tool-specific metadata that is not sent to models and is
	// propagated via ToolCall.ToolMetadata and ToolResult.ToolMetadata.
	Metadata map[string]interface{} `json:"-"`

	// Meta preserves provider/tool-definition metadata fields such as MCP
	// `_meta`. It is exposed for callers that need the raw provider metadata
	// but is not sent to models.
	Meta map[string]interface{} `json:"_meta,omitempty"`

	// ProviderName identifies the provider that owns a provider-defined tool.
	ProviderName string `json:"providerName,omitempty"`

	// Type is "function" (default), "dynamic", or "provider" for
	// provider-defined native tools. When Type is "provider", ProviderID and
	// ProviderArgs specify the native tool.
	Type string `json:"type,omitempty"`

	// ProviderID is the identifier for provider-defined tools.
	// Example: "google.google_search", "google.url_context", "google.code_execution"
	ProviderID string `json:"providerId,omitempty"`

	// ProviderArgs are optional arguments for provider-defined tools.
	// The structure depends on the specific provider tool.
	ProviderArgs map[string]interface{} `json:"providerArgs,omitempty"`

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

// ToolDescriptionOptions contains context for dynamic tool descriptions.
// It mirrors the TypeScript SDK's description options object.
type ToolDescriptionOptions struct {
	// Context is the per-tool context value from ToolsContext[toolName].
	Context interface{}

	// ExperimentalSandbox is the sandbox environment for this call.
	ExperimentalSandbox interface{}
}

// ToolExecutionOptions contains options passed to tool execution
type ToolExecutionOptions struct {
	// ToolCallID is the unique ID of this tool call
	ToolCallID string

	// UserContext is optional user-defined context that flows through the conversation.
	// Deprecated: use RuntimeContext.
	UserContext interface{}

	// RuntimeContext is the user-defined runtime context for the generation call.
	RuntimeContext interface{}

	// ToolContext is the per-tool context validated against the tool's ContextSchema.
	ToolContext interface{}

	// Usage contains token usage information up to this point
	Usage *Usage

	// Metadata contains additional metadata
	Metadata map[string]interface{}

	// ToolMetadata contains metadata attached to the tool call by the provider.
	ToolMetadata map[string]interface{}

	// ExperimentalSandbox is the sandbox environment for this tool execution.
	// It is intentionally typed as interface{} so applications can provide their
	// own sandbox implementation while core APIs preserve TypeScript parity.
	ExperimentalSandbox interface{}
}

// ToModelOutputFunc converts a tool result to model-readable output
// This allows custom formatting of tool results
// Note: ToolResultOutput is defined in message.go and supports rich content blocks
type ToModelOutputFunc func(ctx context.Context, options ToModelOutputOptions) (*ToolResultOutput, error)

// ToModelOutputOptions contains options for converting tool results
type ToModelOutputOptions struct {
	// ToolCallID is the unique ID of the tool call. It matches the TypeScript
	// SDK toModelOutput option name.
	ToolCallID string

	// Input is the original tool input. It matches the TypeScript SDK
	// toModelOutput option name.
	Input map[string]interface{}

	// Output is the raw result from tool execution. It matches the TypeScript
	// SDK toModelOutput option name.
	Output interface{}

	// Result is a deprecated alias for Output.
	//
	// Deprecated: use Output.
	Result interface{}

	// ToolCall is the original tool call
	ToolCall *ToolCall

	// Usage is the current token usage
	Usage *Usage

	// Metadata contains additional metadata
	Metadata map[string]interface{}
}

// ToolInputExample represents an example input for a tool
type ToolInputExample struct {
	// Input is the example input value
	Input map[string]interface{}

	// Description explains what this example demonstrates (optional)
	Description string
}

// ToolNeedsApprovalOptions contains the callback options for tool-defined
// approval, matching the TypeScript ToolNeedsApprovalFunction options object.
type ToolNeedsApprovalOptions struct {
	// ToolCallID is the unique identifier for this tool call.
	ToolCallID string

	// Messages are the messages sent to the model before the assistant response
	// that contained this tool call.
	Messages []Message

	// Context is the per-tool context validated against the tool's ContextSchema.
	Context interface{}
}

// ToolNeedsApprovalFunc determines if a tool call needs approval based on input
// and the validated tool context.
type ToolNeedsApprovalFunc func(ctx context.Context, input map[string]interface{}, options ToolNeedsApprovalOptions) bool

// NeedsApprovalFunc determines if a tool call needs approval based on input.
//
// Deprecated: use ToolNeedsApprovalFunc.
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

	// Title is a short, human-readable title for the tool call.
	Title string `json:"title,omitempty"`

	// Arguments to pass to the tool
	Arguments map[string]interface{} `json:"arguments"`

	// RawArguments preserves the provider's raw streamed JSON input when it is
	// available. Response-message conversion uses it to skip invalid streamed
	// tool inputs instead of sending malformed arguments back to a provider.
	RawArguments string `json:"-"`

	// ProviderExecuted indicates if this tool was executed by the provider (not locally).
	// When true, the provider handled execution server-side (e.g., xAI file_search, web_search).
	ProviderExecuted bool `json:"providerExecuted,omitempty"`

	// ProviderMetadata carries provider-specific metadata associated with this tool call.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// ToolMetadata carries tool-specific metadata associated with this tool call.
	ToolMetadata map[string]interface{} `json:"toolMetadata,omitempty"`

	// ThoughtSignature is Google's cryptographic token that seals the model's
	// thinking chain across tool calls. Populated by the Google/Vertex providers
	// when the API returns a thoughtSignature on a functionCall part. Must be
	// forwarded verbatim when re-sending the assistant message in multi-turn
	// conversations so the API can verify the reasoning chain was not modified.
	ThoughtSignature string `json:"thoughtSignature,omitempty"`

	// Dynamic indicates this tool call came from a dynamically registered (untyped) tool.
	// Mirrors the dynamic/static tool call split in the TypeScript SDK.
	Dynamic bool `json:"dynamic,omitempty"`

	// Invalid marks a tool call whose input could not be parsed or validated.
	// Invalid calls are preserved as error tool results instead of being dropped.
	Invalid bool `json:"invalid,omitempty"`

	// Error is the validation/parsing error associated with an invalid tool call.
	Error error `json:"-"`
}

// ToolResult represents the result of executing a tool
type ToolResult struct {
	// ID of the tool call this result corresponds to
	ToolCallID string `json:"toolCallId"`

	// Name of the tool that was executed
	ToolName string `json:"toolName"`

	// Title is a short, human-readable title for the tool result.
	Title string `json:"title,omitempty"`

	// Input contains the arguments that were passed to the tool
	Input map[string]interface{} `json:"input,omitempty"`

	// Result of the tool execution
	Result interface{} `json:"result"`

	// ModelOutput is the optional model-facing representation of Result. It is
	// populated from Tool.ToModelOutput and deliberately omitted from JSON so
	// user-facing tool results preserve the raw Result value.
	ModelOutput *ToolResultOutput `json:"-"`

	// Error if tool execution failed
	Error error `json:"error,omitempty"`

	// ApprovalStatus captures approval-driven outcomes such as denied or
	// user-approval pending tool calls.
	ApprovalStatus ToolApprovalStatus `json:"approvalStatus,omitempty"`

	// ApprovalID identifies the approval request associated with this tool
	// result. It is distinct from ToolCallID when the request was created by
	// the SDK, matching the TypeScript SDK's generated approval IDs.
	ApprovalID string `json:"approvalId,omitempty"`

	// ApprovalSignature carries the server-issued signature for approval
	// requests when a tool approval secret is configured.
	ApprovalSignature string `json:"-"`

	// ApprovalReason contains the optional approval reason for denied or
	// approved tool calls.
	ApprovalReason *string `json:"approvalReason,omitempty"`

	// Dynamic indicates this tool result came from a dynamically registered (untyped) tool.
	// Mirrors the dynamic/static tool result split in the TypeScript SDK.
	Dynamic bool `json:"dynamic,omitempty"`

	// Preliminary indicates this is an intermediate streamed result rather than
	// the final result for the tool call.
	Preliminary bool `json:"preliminary,omitempty"`

	// ProviderExecuted indicates if this tool was executed by the provider (not locally)
	// When true, the tool was executed by the LLM provider (e.g., Anthropic tool-search, xAI file-search)
	// When false or unset, the tool was executed locally by the client
	// This affects error handling and validation behavior
	ProviderExecuted bool `json:"providerExecuted,omitempty"`

	// ProviderMetadata carries provider-specific metadata associated with this tool result.
	ProviderMetadata map[string]interface{} `json:"providerMetadata,omitempty"`

	// ToolMetadata carries tool-specific metadata associated with this tool result.
	ToolMetadata map[string]interface{} `json:"toolMetadata,omitempty"`
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
