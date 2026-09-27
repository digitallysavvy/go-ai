package harness

import "github.com/digitallysavvy/go-ai/pkg/ai"

// DefaultPermissionMode is the baseline permission mode when unset. Mirrors
// TS `DEFAULT_PERMISSION_MODE`.
const DefaultPermissionMode = PermissionModeAllowAll

// ResolvePermissionMode returns permissionMode, defaulting to
// DefaultPermissionMode. Mirrors TS `resolvePermissionMode`.
func ResolvePermissionMode(permissionMode PermissionMode) PermissionMode {
	if permissionMode == "" {
		return DefaultPermissionMode
	}
	return permissionMode
}

// PermissionModeNeedsBuiltinSupport reports whether permissionMode requires
// the adapter to support built-in tool approvals. Mirrors TS
// `permissionModeNeedsBuiltinSupport`.
func PermissionModeNeedsBuiltinSupport(permissionMode PermissionMode) bool {
	return permissionMode != PermissionModeAllowAll
}

// CustomToolApprovalDecisionType discriminates CustomToolApprovalDecision.
type CustomToolApprovalDecisionType string

const (
	CustomToolApprovalAllow   CustomToolApprovalDecisionType = "allow"
	CustomToolApprovalDeny    CustomToolApprovalDecisionType = "deny"
	CustomToolApprovalRequest CustomToolApprovalDecisionType = "request"
)

// CustomToolApprovalDecision is the outcome of ResolveCustomToolApproval.
// Mirrors TS `CustomToolApprovalDecision`.
type CustomToolApprovalDecision struct {
	Type   CustomToolApprovalDecisionType
	Reason string
}

// ToolApprovalConfiguration maps host-tool names to a static approval status.
// Mirrors TS `HarnessAgentToolApprovalConfiguration`.
type ToolApprovalConfiguration map[string]ai.ToolApprovalStatus

// ResolveCustomToolApproval maps a static per-tool approval configuration
// entry to a decision for one host tool call. Mirrors TS
// `resolveCustomToolApproval`.
func ResolveCustomToolApproval(toolName string, toolApproval ToolApprovalConfiguration) CustomToolApprovalDecision {
	status := ai.ToolApprovalStatusNotApplicable
	if toolApproval != nil {
		if s, ok := toolApproval[toolName]; ok && s != "" {
			status = s
		}
	}
	switch status {
	case ai.ToolApprovalStatusNotApplicable, ai.ToolApprovalStatusApproved:
		return CustomToolApprovalDecision{Type: CustomToolApprovalAllow}
	case ai.ToolApprovalStatusDenied:
		return CustomToolApprovalDecision{Type: CustomToolApprovalDeny}
	case ai.ToolApprovalStatusUserApproval:
		return CustomToolApprovalDecision{Type: CustomToolApprovalRequest}
	default:
		return CustomToolApprovalDecision{Type: CustomToolApprovalAllow}
	}
}
