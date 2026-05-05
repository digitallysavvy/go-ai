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
// The toolCall, tools, messages, runtimeCtx, and toolsCtx values mirror the
// TypeScript SDK's call-level approval function inputs.
type ToolApprovalFunc func(
	toolCall ToolCall,
	tools []Tool,
	messages []Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
) ToolApprovalResult

// ToolApprovalValue is an approval value used in per-tool approval maps.
// It may be a static ToolApprovalStatus or a ToolApprovalFunc.
type ToolApprovalValue interface{}

// ToolApprovalConfig is the call-level approval configuration. Supported
// values are ToolApprovalFunc, map[string]ToolApprovalValue, and
// map[string]interface{} containing ToolApprovalStatus or ToolApprovalFunc
// entries.
type ToolApprovalConfig interface{}
