package policy

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// NormalizeOPADecision normalizes common OPA decision payloads into the SDK
// approval status object form, matching the TypeScript policy-opa package:
//
//   - {"decision": "allow"|"deny"|"requires-approval"|"not-applicable", "reason"?}
//   - legacy {"allow": bool, "reason"?}
//
// nil is not-applicable so a Rego rule that matches no branch means "no
// opinion". Any other unrecognized result is denied (fail closed) so malformed
// policy output cannot silently bypass the approval gate (TS f29566e).
func NormalizeOPADecision(result any) PolicyDecision {
	if result == nil {
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
	if !isStringKeyedRecord(result) {
		return unrecognizedOPADecision()
	}
	reason, _ := recordString(result, "reason")
	if decision, ok := recordString(result, "decision"); ok {
		switch decision {
		case "allow":
			return PolicyDecision{Type: types.ToolApprovalStatusApproved, Reason: reason}
		case "deny":
			return PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: reason}
		case "requires-approval":
			return PolicyDecision{Type: types.ToolApprovalStatusUserApproval, Reason: reason}
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
	return unrecognizedOPADecision()
}

func unrecognizedOPADecision() PolicyDecision {
	return PolicyDecision{Type: types.ToolApprovalStatusDenied, Reason: "unrecognized OPA policy decision"}
}
