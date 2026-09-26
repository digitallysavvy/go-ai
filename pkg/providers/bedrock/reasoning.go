package bedrock

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// reasoningBudgetPercentages mirrors TS provider-utils
// DEFAULT_REASONING_BUDGET_PERCENTAGES.
var reasoningBudgetPercentages = map[types.ReasoningLevel]float64{
	types.ReasoningMinimal: 0.02,
	types.ReasoningLow:     0.1,
	types.ReasoningMedium:  0.3,
	types.ReasoningHigh:    0.6,
	types.ReasoningXHigh:   0.9,
}

// bedrockReasoningEffortMap mirrors TS amazonBedrockReasoningEffortMap.
var bedrockReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "max",
}

// mapReasoningToProviderBudget ports TS provider-utils
// mapReasoningToProviderBudget: converts a reasoning level to an absolute
// token budget by multiplying maxOutputTokens by a percentage, clamped to
// [minReasoningBudget, maxReasoningBudget]. Returns (0, false) when the level
// has no percentage mapping (pushing an unsupported warning).
func mapReasoningToProviderBudget(reasoning types.ReasoningLevel, maxOutputTokens, maxReasoningBudget int, warnings *[]types.Warning) (int, bool) {
	pct, ok := reasoningBudgetPercentages[reasoning]
	if !ok {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: fmt.Sprintf("reasoning %q is not supported by this model.", reasoning),
		})
		return 0, false
	}
	const minReasoningBudget = 1024
	budget := int(float64(maxOutputTokens)*pct + 0.5)
	if budget < minReasoningBudget {
		budget = minReasoningBudget
	}
	if budget > maxReasoningBudget {
		budget = maxReasoningBudget
	}
	return budget, true
}

// mapReasoningToProviderEffort ports TS provider-utils
// mapReasoningToProviderEffort using bedrockReasoningEffortMap.
func mapReasoningToProviderEffort(reasoning types.ReasoningLevel, warnings *[]types.Warning) (string, bool) {
	mapped, ok := bedrockReasoningEffortMap[reasoning]
	if !ok {
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: fmt.Sprintf("reasoning %q is not supported by this model.", reasoning),
		})
		return "", false
	}
	if mapped != string(reasoning) {
		*warnings = append(*warnings, types.Warning{
			Type:    "compatibility",
			Feature: "reasoning",
			Details: fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", reasoning, mapped),
		})
	}
	return mapped, true
}

// isCustomReasoning reports whether reasoning is set to a level other than
// nil or types.ReasoningDefault ("provider-default"), mirroring TS
// isCustomReasoning.
func isCustomReasoning(reasoning *types.ReasoningLevel) bool {
	return reasoning != nil && *reasoning != types.ReasoningDefault
}

// resolveBedrockReasoningConfig ports TS
// resolveAmazonBedrockReasoningConfig: it derives a reasoningConfig from the
// top-level Reasoning call option and merges any explicit
// amazonBedrock.reasoningConfig provider option over the derived defaults
// (explicit provider options win, matching TS spread order).
func resolveBedrockReasoningConfig(reasoning *types.ReasoningLevel, existing *ReasoningConfig, isAnthropic bool, modelID string, warnings *[]types.Warning) *ReasoningConfig {
	if !isCustomReasoning(reasoning) {
		return existing
	}

	result := &ReasoningConfig{}
	if existing != nil {
		*result = *existing
	}

	level := *reasoning

	if isAnthropic {
		caps := bedrockAnthropicModelCapabilities(modelID)
		switch {
		case level == types.ReasoningNone:
			result.Type = "disabled"
		case caps.SupportsAdaptiveThinking:
			effort, ok := mapReasoningToProviderEffort(level, warnings)
			result.Type = "adaptive"
			if ok {
				result.MaxReasoningEffort = effort
			}
			if existing != nil {
				overlayReasoningConfigStruct(result, existing)
			}
		default:
			budget, ok := mapReasoningToProviderBudget(level, caps.MaxOutputTokens, caps.MaxOutputTokens, warnings)
			if ok {
				result.Type = "enabled"
				result.BudgetTokens = &budget
				if existing != nil {
					overlayReasoningConfigStruct(result, existing)
				}
			}
		}
	} else if level != types.ReasoningNone {
		effort, ok := mapReasoningToProviderEffort(level, warnings)
		if ok {
			result.MaxReasoningEffort = effort
		}
		if existing != nil {
			overlayReasoningConfigStruct(result, existing)
		}
	}

	// Mirror anthropic-messages-language-model.ts: when the merged type ends
	// up 'disabled', strip derived effort/budget.
	if result.Type == "disabled" {
		result.MaxReasoningEffort = ""
		result.BudgetTokens = nil
	}

	return result
}

// overlayReasoningConfigStruct overlays non-zero fields of override onto base
// (override wins), mirroring the TS spread `{ ...derived, ...existing }`.
func overlayReasoningConfigStruct(base *ReasoningConfig, override *ReasoningConfig) {
	if override == nil {
		return
	}
	if override.Type != "" {
		base.Type = override.Type
	}
	if override.BudgetTokens != nil {
		base.BudgetTokens = override.BudgetTokens
	}
	if override.MaxReasoningEffort != "" {
		base.MaxReasoningEffort = override.MaxReasoningEffort
	}
	if override.Display != "" {
		base.Display = override.Display
	}
}
