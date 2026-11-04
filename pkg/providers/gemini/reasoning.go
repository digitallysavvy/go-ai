package gemini

import (
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// isGemini3Model reports whether modelID uses Gemini 3 request features.
// It delegates to GetModelCapabilities so that unknown/future Gemini IDs
// (e.g. gemini-4*, gemini-ultra-latest) inherit the newest behavior, matching
// the TS SDK getGoogleModelCapabilities().usesGemini3Features.
func isGemini3Model(modelID string) bool {
	return GetModelCapabilities(modelID).UsesGemini3Features
}

// isGemmaModel reports whether modelID identifies a Gemma model.
// Gemma models do not support systemInstruction.
func isGemmaModel(modelID string) bool {
	return strings.HasPrefix(providerutils.JSToLower(modelID), "gemma-")
}

// maxOutputTokensForGemini25Model mirrors TS getMaxOutputTokensForGemini25Model.
const maxOutputTokensForGemini25Model = 65536

// maxThinkingTokensForModel mirrors TS getMaxThinkingTokensForGemini25Model:
// 2.5-pro and gemini-3-pro-image variants → 32768, everything else → 24576.
func maxThinkingTokensForModel(modelID string) int {
	id := providerutils.JSToLower(modelID)
	if strings.Contains(id, "2.5-pro") || strings.Contains(id, "gemini-3-pro-image") {
		return 32768
	}
	return 24576
}

// minThinkingTokensForGemini25Model mirrors the TS catalog minBudget: Gemini
// 2.5 Pro cannot disable thinking and needs a budget of at least 128 tokens;
// other Gemini 2.5 models disable thinking with a budget of 0.
func minThinkingTokensForGemini25Model(modelID string) int {
	if strings.Contains(providerutils.JSToLower(modelID), "2.5-pro") {
		return 128
	}
	return 0
}

// mirrorReasoningWarnings copies Details into Message for the warnings
// appended since index from (the Go provider warning convention).
func mirrorReasoningWarnings(warnings *[]types.Warning, from int) {
	for i := from; i < len(*warnings); i++ {
		if (*warnings)[i].Message == "" {
			(*warnings)[i].Message = (*warnings)[i].Details
		}
	}
}

// resolveThinkingConfig mirrors TS resolveThinkingConfig. Returns nil when the
// reasoning level is nil or provider-default.
func resolveThinkingConfig(reasoning *types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	if !providerutils.IsCustomReasoning(reasoning) {
		return nil
	}
	if GetModelCapabilities(modelID).UsesGemini3Features && !strings.Contains(modelID, "gemini-3-pro-image") {
		return resolveGemini3ThinkingConfig(*reasoning, modelID, warnings)
	}
	return resolveGemini25ThinkingConfig(*reasoning, modelID, warnings)
}

// resolveGemini3ThinkingConfig mirrors TS (61a315e333): thinking cannot be
// disabled on Gemini 3, so reasoning "none" rounds to the lowest supported
// thinking level, like every other unsupported level, with a compatibility
// warning.
func resolveGemini3ThinkingConfig(reasoning types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	effortMap := map[types.ReasoningLevel]string{
		types.ReasoningLow:    "low",
		types.ReasoningMedium: "medium",
		types.ReasoningHigh:   "high",
	}
	if minimumThinkingLevelForGemini3Model(modelID) == "minimal" {
		effortMap[types.ReasoningMinimal] = "minimal"
	}
	before := len(*warnings)
	mapped, ok := providerutils.MapReasoningToProviderEffort(&reasoning, effortMap, nil, warnings)
	mirrorReasoningWarnings(warnings, before)
	if !ok {
		return nil
	}
	return map[string]interface{}{"thinkingLevel": mapped}
}

// resolveGemini25ThinkingConfig mirrors TS: "none" disables thinking with a
// budget of 0 where the model allows it; otherwise (and on 2.5 Pro for
// "none") the budget is mapReasoningToProviderBudget over 65536 output tokens,
// clamped to [minBudget, maxBudget].
func resolveGemini25ThinkingConfig(reasoning types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	minBudget := minThinkingTokensForGemini25Model(modelID)
	if reasoning == types.ReasoningNone && minBudget == 0 {
		return map[string]interface{}{"thinkingBudget": 0}
	}
	before := len(*warnings)
	budget := providerutils.MapReasoningToProviderBudget(reasoning, providerutils.ReasoningBudgetOptions{
		MaxOutputTokens:    maxOutputTokensForGemini25Model,
		MaxReasoningBudget: maxThinkingTokensForModel(modelID),
		MinReasoningBudget: &minBudget,
	}, warnings)
	mirrorReasoningWarnings(warnings, before)
	return map[string]interface{}{"thinkingBudget": budget}
}
