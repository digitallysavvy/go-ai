package moonshot

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MoonshotUsage represents token usage information from Moonshot API responses
// Supports prompt caching and thinking/reasoning token tracking
type MoonshotUsage struct {
	// Standard token counts
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Cached tokens (for prompt caching)
	CachedTokens *int `json:"cached_tokens,omitempty"`

	// Detailed token breakdowns
	PromptTokensDetails *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

// PromptTokensDetails provides detailed breakdown of prompt tokens
type PromptTokensDetails struct {
	// Tokens that were read from cache
	CachedTokens *int `json:"cached_tokens,omitempty"`
}

// CompletionTokensDetails provides detailed breakdown of completion tokens
type CompletionTokensDetails struct {
	// Tokens used for reasoning (thinking tokens)
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
}

// ConvertMoonshotUsage converts Moonshot usage to SDK usage format
// Handles thinking tokens, cached tokens, and detailed token breakdowns
func ConvertMoonshotUsage(usage MoonshotUsage) types.Usage {
	// Convert to int64 pointers
	inputTokens := int64(usage.PromptTokens)
	outputTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		Raw:          make(map[string]interface{}),
	}

	// Calculate cache read tokens from either top-level or nested field
	var cacheReadTokens int64 = 0
	if usage.CachedTokens != nil && *usage.CachedTokens > 0 {
		cacheReadTokens = int64(*usage.CachedTokens)
	} else if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil && *usage.PromptTokensDetails.CachedTokens > 0 {
		cacheReadTokens = int64(*usage.PromptTokensDetails.CachedTokens)
	}

	// Add input details for cache tokens
	if cacheReadTokens > 0 {
		result.InputDetails = &types.InputTokenDetails{
			CacheReadTokens: &cacheReadTokens,
		}

		// Calculate no-cache tokens (total - cached)
		noCacheTokens := inputTokens - cacheReadTokens
		if noCacheTokens > 0 {
			result.InputDetails.NoCacheTokens = &noCacheTokens
		}

		result.Raw["cachedTokens"] = cacheReadTokens
	}

	// Add output details for reasoning/thinking tokens
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil && *usage.CompletionTokensDetails.ReasoningTokens > 0 {
		reasoningTokens := int64(*usage.CompletionTokensDetails.ReasoningTokens)
		result.OutputDetails = &types.OutputTokenDetails{
			ReasoningTokens: &reasoningTokens,
		}

		// Calculate text tokens (total output - reasoning)
		textTokens := outputTokens - reasoningTokens
		if textTokens > 0 {
			result.OutputDetails.TextTokens = &textTokens
		}

		result.Raw["reasoningTokens"] = *usage.CompletionTokensDetails.ReasoningTokens
	}

	return result
}
