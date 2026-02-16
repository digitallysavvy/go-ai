package deepinfra

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// LanguageModel implements the provider.LanguageModel interface for DeepInfra
// with token counting fixes for Gemini/Gemma models
type LanguageModel struct {
	*openai.LanguageModel
}

// NewLanguageModel creates a new DeepInfra language model
func NewLanguageModel(provider *openai.Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		LanguageModel: openai.NewLanguageModel(provider, modelID),
	}
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "deepinfra"
}

// DoGenerate performs non-streaming text generation with token usage fixes
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Call parent implementation
	result, err := m.LanguageModel.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Fix usage if needed
	if len(result.Usage.Raw) > 0 {
		result.Usage = fixUsageForGeminiModels(result.Usage)
	}

	return result, nil
}

// fixUsageForGeminiModels fixes incorrect token usage for Gemini/Gemma models from DeepInfra.
//
// DeepInfra's API returns completion_tokens that don't include reasoning_tokens
// for Gemini/Gemma models, which violates the OpenAI-compatible spec.
// According to the spec, completion_tokens should include reasoning_tokens.
//
// Example of incorrect data from DeepInfra:
//
//	{
//	  "completion_tokens": 84,    // text-only tokens
//	  "completion_tokens_details": {
//	    "reasoning_tokens": 1081  // reasoning tokens not included above
//	  }
//	}
//
// This would result in negative text tokens: 84 - 1081 = -997
//
// The fix: If reasoning_tokens > completion_tokens, add reasoning_tokens
// to completion_tokens: 84 + 1081 = 1165
func fixUsageForGeminiModels(usage types.Usage) types.Usage {
	// Only fix if we have raw usage data
	if len(usage.Raw) == 0 {
		return usage
	}

	completionTokensDetails, ok := usage.Raw["completion_tokens_details"].(map[string]interface{})
	if !ok || completionTokensDetails == nil {
		return usage
	}

	reasoningTokensRaw, ok := completionTokensDetails["reasoning_tokens"]
	if !ok || reasoningTokensRaw == nil {
		return usage
	}

	// Convert reasoning_tokens to int64
	var reasoningTokens int64
	switch v := reasoningTokensRaw.(type) {
	case int:
		reasoningTokens = int64(v)
	case int64:
		reasoningTokens = v
	case float64:
		reasoningTokens = int64(v)
	default:
		return usage
	}

	if reasoningTokens == 0 {
		return usage
	}

	// Get current completion tokens
	var completionTokens int64
	if usage.OutputTokens != nil {
		completionTokens = *usage.OutputTokens
	}

	// If reasoning tokens exceed completion tokens, the API data is incorrect
	// DeepInfra is returning only text tokens in completion_tokens, not including reasoning
	if reasoningTokens > completionTokens {
		// Add reasoning_tokens to completion_tokens to get the correct total
		correctedCompletionTokens := completionTokens + reasoningTokens

		// Recalculate usage with fixed data
		usage.OutputTokens = &correctedCompletionTokens

		// Update total tokens if present
		if usage.TotalTokens != nil {
			correctedTotalTokens := *usage.TotalTokens + reasoningTokens
			usage.TotalTokens = &correctedTotalTokens
		}

		// Update output details
		textTokens := completionTokens // Original value was text-only
		usage.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}

		// Update raw usage
		usage.Raw["completion_tokens"] = int(correctedCompletionTokens)
		if totalTokens, ok := usage.Raw["total_tokens"].(int); ok {
			usage.Raw["total_tokens"] = totalTokens + int(reasoningTokens)
		}
	}

	return usage
}
