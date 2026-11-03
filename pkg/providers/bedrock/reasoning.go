package bedrock

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Effort maps only list the supported levels; the shared helper rounds the
// other levels to the closest supported one (TS 61a315e333).

// bedrockReasoningEffortMap mirrors TS amazonBedrockReasoningEffortMap
// (xhigh is added for Anthropic models that support it).
func bedrockReasoningEffortMap() map[types.ReasoningLevel]string {
	return map[types.ReasoningLevel]string{
		types.ReasoningLow:    "low",
		types.ReasoningMedium: "medium",
		types.ReasoningHigh:   "high",
		types.ReasoningMax:    "max",
	}
}

// novaReasoningEffortMap mirrors TS amazonNovaReasoningEffortMap.
var novaReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningLow:    "low",
	types.ReasoningMedium: "medium",
	types.ReasoningHigh:   "high",
}

// openAIGptOssReasoningEffortMap mirrors TS openAIGptOssReasoningEffortMap.
var openAIGptOssReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningLow:    "low",
	types.ReasoningMedium: "medium",
	types.ReasoningHigh:   "high",
}

// openAIReasoningEffortMap mirrors TS openAIReasoningEffortMap: the efforts
// of other OpenAI models are not known, so reasoning values are sent as is.
var openAIReasoningEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningNone:    "none",
	types.ReasoningMinimal: "minimal",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "medium",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "xhigh",
	types.ReasoningMax:     "max",
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
	if !providerutils.IsCustomReasoning(reasoning) {
		return existing
	}

	result := &ReasoningConfig{}
	if existing != nil {
		*result = *existing
	}

	// Do not derive effort or budget from portable reasoning when thinking
	// is explicitly disabled.
	if existing != nil && existing.Type == "disabled" {
		result.MaxReasoningEffort = ""
		result.BudgetTokens = nil
		return result
	}

	level := *reasoning
	isNovaReasoning := isNovaReasoningModel(modelID)
	supportsPortableReasoning := isOpenAIModel || isNovaReasoning || existing != nil

	switch {
	case isAnthropic:
		caps := anthropic.GetModelCapabilities(modelID)
		switch {
		case level == types.ReasoningNone:
			result = &ReasoningConfig{Type: "disabled"}
		case caps.SupportsAdaptiveThinking:
			if existing == nil || existing.MaxReasoningEffort == "" {
				effortMap := bedrockReasoningEffortMap()
				if caps.SupportsXHighEffort {
					effortMap[types.ReasoningXHigh] = "xhigh"
				}
				effort, _ := providerutils.MapReasoningToProviderEffort(&level, effortMap, nil, warnings)
				result.MaxReasoningEffort = effort
			}
			if result.Type == "" {
				result.Type = "adaptive"
			}
		default:
			if existing == nil || existing.BudgetTokens == nil {
				budget := providerutils.MapReasoningToProviderBudget(level, providerutils.ReasoningBudgetOptions{
					MaxOutputTokens:    caps.MaxOutputTokens,
					MaxReasoningBudget: caps.MaxOutputTokens,
				}, warnings)
				result.BudgetTokens = &budget
			}
			if result.Type == "" {
				result.Type = "enabled"
			}
		}
	case level == types.ReasoningNone && isNovaReasoning:
		// Nova 2 reasoning is disabled unless a reasoning effort is sent, so
		// there is nothing to add.
	case supportsPortableReasoning:
		if existing == nil || existing.MaxReasoningEffort == "" {
			effortMap := bedrockReasoningEffortMap()
			switch {
			case isNovaReasoning:
				effortMap = novaReasoningEffortMap
			case isOpenAIModel && strings.Contains(modelID, "openai.gpt-oss-"):
				effortMap = openAIGptOssReasoningEffortMap
			case isOpenAIModel:
				// OpenAI efforts such as "none" are sent as reasoning_effort
				// even though the reasoningConfig provider option does not
				// accept them.
				effortMap = openAIReasoningEffortMap
			}
			effort, _ := providerutils.MapReasoningToProviderEffort(&level, effortMap, nil, warnings)
			result.MaxReasoningEffort = effort
		}
		if isNovaReasoning && result.Type == "" {
			result.Type = "enabled"
		}
	default:
		*warnings = append(*warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoning",
			Details: "Portable reasoning is not supported for this model and will be ignored. If the model supports a provider-specific reasoning configuration, use providerOptions.amazonBedrock.reasoningConfig.",
		})
	}

	return result
}
