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

// ResultsForPublicView returns the entries of results that TS reports in
// `toolResults` (the content parts of type "tool-result"). Internal tool
// execution also yields bookkeeping entries that TS keeps out of that list:
//
//   - a call awaiting user approval (the request is a tool-approval-request
//     content part),
//   - a call denied by an approval rule (tool-approval-response and
//     tool-output-denied, never a tool-result), and
//   - a provider-executed call whose result has not arrived (pending marker,
//     or an approved provider-executed call, whose result the provider
//     returns itself).
//
// The input slice is returned as is when nothing is dropped.
func ResultsForPublicView(results []ToolResult) []ToolResult {
	drop := func(tr ToolResult) bool {
		switch tr.ApprovalStatus {
		case ToolApprovalStatusUserApproval, ToolApprovalStatusDenied:
			return true
		}
		return tr.ProviderExecuted && tr.Result == nil && tr.Error == nil && tr.ModelOutput == nil
	}
	n := 0
	for _, tr := range results {
		if drop(tr) {
			n++
		}
	}
	if n == 0 {
		return results
	}
	out := make([]ToolResult, 0, len(results)-n)
	for _, tr := range results {
		if !drop(tr) {
			out = append(out, tr)
		}
	}
	return out
}
