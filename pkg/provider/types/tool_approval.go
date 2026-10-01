package types

// ToolApprovalStatus represents the approval outcome for a tool call.
type ToolApprovalStatus string

const (
	ToolApprovalStatusNotApplicable ToolApprovalStatus = "not-applicable"
	ToolApprovalStatusApproved      ToolApprovalStatus = "approved"
	ToolApprovalStatusDenied        ToolApprovalStatus = "denied"
	ToolApprovalStatusUserApproval  ToolApprovalStatus = "user-approval"
)

// ToolApprovalResult is the normalized approval response for a tool call.
type ToolApprovalResult struct {
	Status ToolApprovalStatus
	Reason *string
}

// ToolApprovalFunc determines approval for a tool call.
//
// Deprecated: use GenericToolApprovalFunc instead, which uses an options-struct
// for forward compatibility.
type ToolApprovalFunc func(
	toolCall ToolCall,
	tools []Tool,
	messages []Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
) ToolApprovalResult

// ToolApprovalOptions holds the arguments passed to a GenericToolApprovalFunc.
// Mirrors the options object of GenericToolApprovalFunction in the TypeScript SDK.
type ToolApprovalOptions struct {
	// ToolCall is the specific tool call being evaluated for approval.
	ToolCall ToolCall

	// Tools is the full set of tools available to the model.
	Tools []Tool

	// ToolsContext is the per-tool context map.
	ToolsContext map[string]interface{}

	// RuntimeContext is the user-defined runtime context.
	RuntimeContext interface{}

	// Messages are the messages sent to the model (excluding system prompt and
	// the assistant response that contained the tool call).
	Messages []Message
}

// GenericToolApprovalFunc is the options-based approval function that mirrors
// the TypeScript SDK's GenericToolApprovalFunction signature.
type GenericToolApprovalFunc func(opts ToolApprovalOptions) ToolApprovalResult

// SingleToolApprovalOptions holds the per-tool context for a SingleToolApprovalFunc.
type SingleToolApprovalOptions struct {
	// ToolContext is the context value for this specific tool from ToolsContext.
	ToolContext interface{}

	// RuntimeContext is the user-defined runtime context.
	RuntimeContext interface{}

	// ToolCallID is the unique identifier for this tool call.
	ToolCallID string

	// StepNumber is the step in which this tool call occurs.
	StepNumber int

	// Messages are the messages sent to the model.
	Messages []Message
}

// SingleToolApprovalFunc is a per-tool approval function that receives the parsed
// tool arguments and per-tool context. Use in a per-tool approval map.
// Mirrors SingleToolApprovalFunction in the TypeScript SDK.
type SingleToolApprovalFunc func(args map[string]interface{}, opts SingleToolApprovalOptions) ToolApprovalResult

// ToolApprovalValue is an approval value used in per-tool approval maps.
// It may be a static ToolApprovalStatus, a ToolApprovalFunc, a GenericToolApprovalFunc,
// or a SingleToolApprovalFunc.
type ToolApprovalValue interface{}

// ToolApprovalConfig is the call-level approval configuration. Supported
// values are GenericToolApprovalFunc, ToolApprovalFunc (deprecated),
// map[string]ToolApprovalValue, and map[string]interface{} containing
// ToolApprovalStatus or approval function entries.
type ToolApprovalConfig interface{}
