package bedrock

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// reasoningBudgetPercentages mirrors TS provider-utils
// DEFAULT_REASONING_BUDGET_PERCENTAGES.
var reasoningBudgetPercentages = map[types.ReasoningLevel]float64{
	types.ReasoningMinimal: 0.02,
	types.ReasoningLow:     0.1,
	types.ReasoningMedium:  0.3,
	types.ReasoningHigh:    0.6,
	types.ReasoningXHigh:   0.9,
	types.ReasoningMax:     0.95,
}

// bedrockReasoningEffortMap mirrors TS amazonBedrockReasoningEffortMap.
var bedrockReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "max",
	types.ReasoningMax:     "max",
}

// novaReasoningEffortMap mirrors TS amazonNovaReasoningEffortMap: Nova 2
// supports low/medium/high, so xhigh and max map to high.
var novaReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "high",
	types.ReasoningMax:     "high",
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
// mapReasoningToProviderEffort using the supplied effort map.
func mapReasoningToProviderEffort(reasoning types.ReasoningLevel, effortMap map[types.ReasoningLevel]string, warnings *[]types.Warning) (string, bool) {
	mapped, ok := effortMap[reasoning]
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

// isNovaReasoningModel reports whether modelID identifies an Amazon Nova
// model with known portable-reasoning support, mirroring TS
// isNovaReasoningModel (modelId.includes('amazon.nova-2-lite-v1:0')).
func isNovaReasoningModel(modelID string) bool {
	return strings.Contains(modelID, "amazon.nova-2-lite-v1:0")
}

// resolveBedrockReasoningConfig ports TS resolveAmazonBedrockReasoningConfig:
// it derives a reasoningConfig from the top-level Reasoning call option while
// preserving any explicit amazonBedrock.reasoningConfig provider option on a
// field-by-field basis (an explicit maxReasoningEffort, budgetTokens or type
// always wins, and a field that is explicitly provided is never mapped, so it
// produces no mapping warning). Portable reasoning is only mapped to
// reasoningConfig for non-Anthropic models with known reasoning support
// (OpenAI models, Nova 2 Lite) or when the caller already supplied an
// explicit reasoningConfig; other models emit an "unsupported" warning. A
// disabled provider-option reasoningConfig is preserved without deriving
// effort or budget.
func resolveBedrockReasoningConfig(reasoning *types.ReasoningLevel, existing *ReasoningConfig, isAnthropic bool, isOpenAIModel bool, modelID string, warnings *[]types.Warning) *ReasoningConfig {
	if !isCustomReasoning(reasoning) {
		return existing
	}

	result := &ReasoningConfig{}
	if existing != nil {
		*result = *existing
	}

	// Preserve disabled thinking without deriving settings or warnings that
	// will not be sent.
	if existing != nil && existing.Type == "disabled" {
		result.MaxReasoningEffort = ""
		result.BudgetTokens = nil
		return result
	}

	level := *reasoning
	hasPortableReasoning := level != types.ReasoningNone
	isNovaReasoning := isNovaReasoningModel(modelID)
	supportsPortableReasoning := isOpenAIModel || isNovaReasoning || existing != nil

	if isAnthropic {
		caps := anthropic.GetModelCapabilities(modelID)
		switch {
		case level == types.ReasoningNone:
			result = &ReasoningConfig{Type: "disabled"}
		case caps.SupportsAdaptiveThinking:
			if existing == nil || existing.MaxReasoningEffort == "" {
				effort, _ := mapReasoningToProviderEffort(level, bedrockReasoningEffortMap, warnings)
				result.MaxReasoningEffort = effort
			}
			if result.Type == "" {
				result.Type = "adaptive"
			}
		default:
			if existing == nil || existing.BudgetTokens == nil {
				budget, ok := mapReasoningToProviderBudget(level, caps.MaxOutputTokens, caps.MaxOutputTokens, warnings)
				if ok {
					result.BudgetTokens = &budget
				}
			}
			if result.BudgetTokens != nil && result.Type == "" {
				result.Type = "enabled"
			}
		}
	} else if hasPortableReasoning {
		if supportsPortableReasoning {
			if existing == nil || existing.MaxReasoningEffort == "" {
				effortMap := bedrockReasoningEffortMap
				if isNovaReasoning {
					effortMap = novaReasoningEffortMap
				}
				effort, _ := mapReasoningToProviderEffort(level, effortMap, warnings)
				result.MaxReasoningEffort = effort
			}
			if isNovaReasoning && result.Type == "" {
				result.Type = "enabled"
			}
		} else {
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoning",
				Details: "Portable reasoning is not supported for this model and will be ignored. If the model supports a provider-specific reasoning configuration, use providerOptions.amazonBedrock.reasoningConfig.",
			})
		}
	}

	return result
}
