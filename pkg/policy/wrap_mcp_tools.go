package policy

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// WrappedMCPTools is the result returned by WrapMCPTools.
type WrappedMCPTools struct {
	Tools        map[string]types.Tool
	ToolApproval types.ToolApprovalConfig
}

// WrapMCPTools makes approval total over a discovered tool surface, falling
// back to user-approval for uncovered or not-applicable tools by default.
func WrapMCPTools(tools map[string]types.Tool, approval types.ToolApprovalConfig, defaults ...types.ToolApprovalStatus) WrappedMCPTools {
	fallback := types.ToolApprovalStatusUserApproval
	if len(defaults) > 0 && defaults[0] != "" {
		fallback = defaults[0]
	}
	switch v := approval.(type) {
	case types.GenericToolApprovalFunc, types.ToolApprovalFunc:
		return WrappedMCPTools{
			Tools: tools,
			ToolApproval: types.GenericToolApprovalFunc(func(args types.ToolApprovalOptions) types.ToolApprovalResult {
				decision := decisionFromApproval(evaluateApprovalConfig(v, args))
				if decision.Type == types.ToolApprovalStatusNotApplicable {
					return types.ToolApprovalResult{Status: fallback}
				}
				return approvalFromDecision(decision)
			}),
		}
	case map[string]types.ToolApprovalValue:
		filled := make(map[string]types.ToolApprovalValue, len(tools))
		for name := range tools {
			if value, ok := v[name]; ok && value != nil {
				filled[name] = wrapPerToolApproval(value, fallback)
			} else {
				filled[name] = fallback
			}
		}
		return WrappedMCPTools{Tools: tools, ToolApproval: filled}
	case map[string]interface{}:
		filled := make(map[string]interface{}, len(tools))
		for name := range tools {
			if value, ok := v[name]; ok && value != nil {
				filled[name] = wrapPerToolApproval(value, fallback)
			} else {
				filled[name] = string(fallback)
			}
		}
		return WrappedMCPTools{Tools: tools, ToolApproval: filled}
	default:
		filled := make(map[string]interface{}, len(tools))
		for name := range tools {
			filled[name] = string(fallback)
		}
		return WrappedMCPTools{Tools: tools, ToolApproval: filled}
	}
}

// wrapPerToolApproval mirrors the generic-function form for per-tool approval
// functions so a "no opinion" result (not-applicable or empty) is forced
// through the fallback instead of letting the tool run unapproved (TS
// 47bd0a6). Static statuses the user configured are kept as-is.
func wrapPerToolApproval(value types.ToolApprovalValue, fallback types.ToolApprovalStatus) types.ToolApprovalValue {
	orFallback := func(result types.ToolApprovalResult) types.ToolApprovalResult {
		if result.Status == "" || result.Status == types.ToolApprovalStatusNotApplicable {
			return types.ToolApprovalResult{Status: fallback}
		}
		return result
	}
	switch fn := value.(type) {
	case types.SingleToolApprovalFunc:
		return types.SingleToolApprovalFunc(func(args map[string]interface{}, opts types.SingleToolApprovalOptions) types.ToolApprovalResult {
			return orFallback(fn(args, opts))
		})
	case types.GenericToolApprovalFunc:
		return types.GenericToolApprovalFunc(func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
			return orFallback(fn(opts))
		})
	case types.ToolApprovalFunc:
		return types.ToolApprovalFunc(func(toolCall types.ToolCall, tools []types.Tool, messages []types.Message, runtimeCtx interface{}, toolsCtx map[string]interface{}) types.ToolApprovalResult {
			return orFallback(fn(toolCall, tools, messages, runtimeCtx, toolsCtx))
		})
	default:
		return value
	}
}
