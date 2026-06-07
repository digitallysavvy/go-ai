package policy

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ShadowOptions configures Shadow.
type ShadowOptions struct {
	Enforce    bool
	OnDecision func(PolicyDecisionEvent)
}

// Shadow wraps approval in audit mode. By default the wrapped policy is
// evaluated and reported, but the effective decision is approved.
func Shadow(approval types.ToolApprovalConfig, opts ...ShadowOptions) types.GenericToolApprovalFunc {
	cfg := ShadowOptions{}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	return func(args types.ToolApprovalOptions) types.ToolApprovalResult {
		raw := evaluateApprovalConfig(approval, args)
		decision := decisionFromApproval(raw)
		effective := PolicyDecision{Type: types.ToolApprovalStatusApproved}
		if cfg.Enforce {
			effective = decision
		}
		if cfg.OnDecision != nil {
			event := PolicyDecisionEvent{
				ToolCall: PolicyToolCall{
					ToolName:   args.ToolCall.ToolName,
					ToolCallID: args.ToolCall.ID,
					Input:      args.ToolCall.Arguments,
				},
				Decision:  decision,
				Enforced:  cfg.Enforce,
				Effective: effective,
				Timestamp: isoTimestampMillis(time.Now()),
			}
			go func() {
				defer func() { _ = recover() }()
				cfg.OnDecision(event)
			}()
		}
		return approvalFromDecision(effective)
	}
}

func evaluateApprovalConfig(approval types.ToolApprovalConfig, args types.ToolApprovalOptions) any {
	switch v := approval.(type) {
	case nil:
		return nil
	case types.GenericToolApprovalFunc:
		return v(args)
	case types.ToolApprovalFunc:
		return v(args.ToolCall, args.Tools, args.Messages, args.RuntimeContext, args.ToolsContext)
	case map[string]types.ToolApprovalValue:
		return evaluateApprovalValue(v[args.ToolCall.ToolName], args)
	case map[string]interface{}:
		return evaluateApprovalValue(v[args.ToolCall.ToolName], args)
	default:
		return v
	}
}

func evaluateApprovalValue(value any, args types.ToolApprovalOptions) any {
	switch v := value.(type) {
	case nil:
		return nil
	case types.GenericToolApprovalFunc:
		return v(args)
	case types.ToolApprovalFunc:
		return v(args.ToolCall, args.Tools, args.Messages, args.RuntimeContext, args.ToolsContext)
	case types.SingleToolApprovalFunc:
		return v(args.ToolCall.Arguments, types.SingleToolApprovalOptions{
			RuntimeContext: args.RuntimeContext,
			ToolCallID:     args.ToolCall.ID,
			Messages:       args.Messages,
		})
	default:
		return v
	}
}

func isoTimestampMillis(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}
