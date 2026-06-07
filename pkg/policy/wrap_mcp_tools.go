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
				filled[name] = value
			} else {
				filled[name] = fallback
			}
		}
		return WrappedMCPTools{Tools: tools, ToolApproval: filled}
	case map[string]interface{}:
		filled := make(map[string]interface{}, len(tools))
		for name := range tools {
			if value, ok := v[name]; ok && value != nil {
				filled[name] = value
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
