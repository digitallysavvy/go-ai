package policy

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// NormalizeOPADecision normalizes common OPA decision payloads into the SDK
// approval status object form. Unknown and nil shapes are not-applicable,
// matching the TypeScript policy-opa package.
func NormalizeOPADecision(result any) PolicyDecision {
	if result == nil {
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
	if !isStringKeyedRecord(result) {
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
	reason, _ := recordString(result, "reason")
	if decision, ok := recordString(result, "decision"); ok {
		switch decision {
		case "allow":
			return PolicyDecision{Type: types.ToolApprovalStatusApproved, Reason: reason}
		case "deny":
			return PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: reason}
		case "requires-approval":
			return PolicyDecision{Type: types.ToolApprovalStatusUserApproval}
		case "not-applicable":
			return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
		}
	}
	if allow, ok := recordBool(result, "allow"); ok {
		if allow {
			return PolicyDecision{Type: types.ToolApprovalStatusApproved, Reason: reason}
		}
		return PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: reason}
	}
	return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
}
