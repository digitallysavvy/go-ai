package gemini

import (
	"fmt"
	"math"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	return strings.HasPrefix(strings.ToLower(modelID), "gemma-")
}

// maxOutputTokensForGemini25Model mirrors TS getMaxOutputTokensForGemini25Model.
const maxOutputTokensForGemini25Model = 65536

// maxThinkingTokensForModel mirrors TS getMaxThinkingTokensForGemini25Model:
// 2.5-pro and gemini-3-pro-image variants → 32768, everything else → 24576.
func maxThinkingTokensForModel(modelID string) int {
	id := strings.ToLower(modelID)
	if strings.Contains(id, "2.5-pro") || strings.Contains(id, "gemini-3-pro-image") {
		return 32768
	}
	return 24576
}

var reasoningBudgetPercentages = map[types.ReasoningLevel]float64{
	types.ReasoningMinimal: 0.02,
	types.ReasoningLow:     0.1,
	types.ReasoningMedium:  0.3,
	types.ReasoningHigh:    0.6,
	types.ReasoningXHigh:   0.9,
}

// mapReasoningBudget mirrors TS mapReasoningToProviderBudget as used by
// resolveGemini25ThinkingConfig: round(65536 * pct) clamped to
// [0, modelMaxThinkingTokens]. The second return value is false (and an
// unsupported warning is appended) when the level is unknown.
func mapReasoningBudget(level types.ReasoningLevel, modelID string, warnings *[]types.Warning) (int, bool) {
	pct, ok := reasoningBudgetPercentages[level]
	if !ok {
		appendUnsupportedReasoningWarning(level, warnings)
		return 0, false
	}
	budget := int(math.Round(float64(maxOutputTokensForGemini25Model) * pct))
	if budget < 0 {
		budget = 0
	}
	if max := maxThinkingTokensForModel(modelID); budget > max {
		budget = max
	}
	return budget, true
}

func appendUnsupportedReasoningWarning(level types.ReasoningLevel, warnings *[]types.Warning) {
	details := fmt.Sprintf("reasoning %q is not supported by this model.", string(level))
	*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "reasoning", Details: details, Message: details})
}

// resolveThinkingConfig mirrors TS resolveThinkingConfig. Returns nil when the
// reasoning level is nil or provider-default.
func resolveThinkingConfig(reasoning *types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	if reasoning == nil || *reasoning == types.ReasoningDefault || *reasoning == "" {
		return nil
	}
	if GetModelCapabilities(modelID).UsesGemini3Features && !strings.Contains(modelID, "gemini-3-pro-image") {
		return resolveGemini3ThinkingConfig(*reasoning, modelID, warnings)
	}
	return resolveGemini25ThinkingConfig(*reasoning, modelID, warnings)
}

func resolveGemini3ThinkingConfig(reasoning types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	minimum := minimumThinkingLevelForGemini3Model(modelID)
	if reasoning == types.ReasoningNone {
		// It's not possible to fully disable thinking with Gemini 3.
		return map[string]interface{}{"thinkingLevel": minimum}
	}
	effortMap := map[types.ReasoningLevel]string{
		types.ReasoningMinimal: minimum,
		types.ReasoningLow:     "low",
		types.ReasoningMedium:  "medium",
		types.ReasoningHigh:    "high",
		types.ReasoningXHigh:   "high",
	}
	mapped, ok := effortMap[reasoning]
	if !ok {
		appendUnsupportedReasoningWarning(reasoning, warnings)
		return nil
	}
	if mapped != string(reasoning) {
		details := fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", string(reasoning), mapped)
		*warnings = append(*warnings, types.Warning{Type: "compatibility", Feature: "reasoning", Details: details, Message: details})
	}
	return map[string]interface{}{"thinkingLevel": mapped}
}

func resolveGemini25ThinkingConfig(reasoning types.ReasoningLevel, modelID string, warnings *[]types.Warning) map[string]interface{} {
	if reasoning == types.ReasoningNone {
		return map[string]interface{}{"thinkingBudget": 0}
	}
	budget, ok := mapReasoningBudget(reasoning, modelID, warnings)
	if !ok {
		return nil
	}
	return map[string]interface{}{"thinkingBudget": budget}
}
