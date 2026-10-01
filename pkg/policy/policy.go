package policy

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// PolicyClient is the engine-neutral OPA client contract. Evaluate returns the
// raw decision payload emitted by the policy backend; adapters normalize it for
// tool approval and middleware use.
type PolicyClient interface {
	Evaluate(ctx context.Context, path string, input any) (any, error)
}

// PolicyDecision is the normalized object form of the SDK's tool approval status.
type PolicyDecision struct {
	Type   types.ToolApprovalStatus `json:"type"`
	Reason string                   `json:"reason,omitempty"`
}

// PolicyDecisionEvent is emitted by Shadow after each wrapped approval decision.
type PolicyDecisionEvent struct {
	ToolCall  PolicyToolCall `json:"toolCall"`
	Decision  PolicyDecision `json:"decision"`
	Enforced  bool           `json:"enforced"`
	Effective PolicyDecision `json:"effective"`
	Timestamp string         `json:"timestamp"`
}

// PolicyToolCall is the tool call identity captured in policy telemetry.
type PolicyToolCall struct {
	ToolName   string `json:"toolName"`
	ToolCallID string `json:"toolCallId"`
	Input      any    `json:"input,omitempty"`
}

func approvalFromDecision(decision PolicyDecision) types.ToolApprovalResult {
	status := decision.Type
	if status == "" {
		status = types.ToolApprovalStatusNotApplicable
	}
	return types.ToolApprovalResult{Status: status, Reason: optionalString(decision.Reason)}
}

func decisionFromApproval(value any) PolicyDecision {
	switch v := value.(type) {
	case nil:
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	case string:
		return decisionFromStatus(types.ToolApprovalStatus(v), "")
	case types.ToolApprovalStatus:
		return decisionFromStatus(v, "")
	case types.ToolApprovalResult:
		return decisionFromApproval(&v)
	case *types.ToolApprovalResult:
		if v == nil {
			return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
		}
		return decisionFromStatus(v.Status, derefString(v.Reason))
	case PolicyDecision:
		if v.Type == "" {
			v.Type = types.ToolApprovalStatusNotApplicable
		}
		return v
	case *PolicyDecision:
		if v == nil {
			return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
		}
		out := *v
		if out.Type == "" {
			out.Type = types.ToolApprovalStatusNotApplicable
		}
		return out
	case map[string]any:
		return decisionFromObject(v)
	case map[string]string:
		return decisionFromObject(v)
	default:
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
}

func decisionFromObject(record any) PolicyDecision {
	status, ok := recordString(record, "type")
	if !ok {
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
	reason, _ := recordString(record, "reason")
	if status == "" {
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
	return PolicyDecision{Type: types.ToolApprovalStatus(status), Reason: reason}
}

func decisionFromStatus(status types.ToolApprovalStatus, reason string) PolicyDecision {
	switch status {
	case types.ToolApprovalStatusApproved,
		types.ToolApprovalStatusDenied,
		types.ToolApprovalStatusUserApproval,
		types.ToolApprovalStatusNotApplicable:
		return PolicyDecision{Type: status, Reason: reason}
	default:
		return PolicyDecision{Type: types.ToolApprovalStatusNotApplicable}
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isStringKeyedRecord(value any) bool {
	switch value.(type) {
	case map[string]any, map[string]string, map[string]bool:
		return true
	default:
		return false
	}
}

func recordString(record any, key string) (string, bool) {
	switch v := record.(type) {
	case map[string]any:
		out, ok := v[key].(string)
		return out, ok
	case map[string]string:
		out, ok := v[key]
		return out, ok
	default:
		return "", false
	}
}

func recordBool(record any, key string) (bool, bool) {
	switch v := record.(type) {
	case map[string]any:
		out, ok := v[key].(bool)
		return out, ok
	case map[string]bool:
		out, ok := v[key]
		return out, ok
	default:
		return false, false
	}
}
