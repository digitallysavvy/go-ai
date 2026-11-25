package policy

import (
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsdate"

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
	return NewShadow(approval, opts...).Func()
}

// ShadowApproval is Shadow with the WrapMCPTools fallback hook (TS fac1d6a588:
// shadow passes the hook of the policy it wraps on). Shadow itself returns
// the plain function.
type ShadowApproval struct {
	approval types.ToolApprovalConfig
	opts     []ShadowOptions
}

// NewShadow is Shadow returning the approval object.
func NewShadow(approval types.ToolApprovalConfig, opts ...ShadowOptions) *ShadowApproval {
	return &ShadowApproval{approval: approval, opts: opts}
}

// ApplyApprovalFallback implements FallbackApproval when the wrapped approval
// does; it returns nil otherwise (see CanApplyApprovalFallback).
func (s *ShadowApproval) ApplyApprovalFallback(fallback types.ToolApprovalStatus) types.GenericToolApprovalFunc {
	inner, ok := s.approval.(FallbackApproval)
	if !ok {
		return s.Func()
	}
	return NewShadow(inner.ApplyApprovalFallback(fallback), s.opts...).Func()
}

// Func returns the shadow as a GenericToolApprovalFunc.
func (s *ShadowApproval) Func() types.GenericToolApprovalFunc {
	return shadowFunc(s.approval, s.opts...)
}

func shadowFunc(approval types.ToolApprovalConfig, opts ...ShadowOptions) types.GenericToolApprovalFunc {
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
	case *OPAApproval:
		return v.Func()(args)
	case *ShadowApproval:
		return v.Func()(args)
	case types.GenericToolApprovalFunc:
		return v(args)
	case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
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
	case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
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

// isoTimestampMillis is Date#toISOString (jsdate.ISOString: expanded years
// outside 0..9999); the wall clock is always inside the ECMAScript range.
func isoTimestampMillis(t time.Time) string {
	s, _ := jsdate.ISOString(t)
	return s
}
