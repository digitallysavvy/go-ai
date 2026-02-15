package alibaba

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// AlibabaUsage represents token usage information from Alibaba API responses
type AlibabaUsage struct {
	// Standard token counts
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Thinking/reasoning tokens (for qwen-qwq-32b-preview)
	ThinkingTokens *int `json:"thinking_tokens,omitempty"`

	// Prompt caching tokens
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens,omitempty"`
}

// ConvertAlibabaUsage converts Alibaba usage to SDK usage format
func ConvertAlibabaUsage(usage AlibabaUsage) types.Usage {
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

	// Add input details for cache tokens
	if usage.PromptCacheHitTokens != nil || usage.PromptCacheMissTokens != nil {
		result.InputDetails = &types.InputTokenDetails{}

		if usage.PromptCacheHitTokens != nil && *usage.PromptCacheHitTokens > 0 {
			cacheRead := int64(*usage.PromptCacheHitTokens)
			result.InputDetails.CacheReadTokens = &cacheRead
			result.Raw["promptCacheHitTokens"] = *usage.PromptCacheHitTokens
		}

		if usage.PromptCacheMissTokens != nil && *usage.PromptCacheMissTokens > 0 {
			cacheWrite := int64(*usage.PromptCacheMissTokens)
			result.InputDetails.CacheWriteTokens = &cacheWrite
			result.Raw["promptCacheMissTokens"] = *usage.PromptCacheMissTokens
		}
	}

	// Add output details for thinking/reasoning tokens
	if usage.ThinkingTokens != nil && *usage.ThinkingTokens > 0 {
		result.OutputDetails = &types.OutputTokenDetails{}
		reasoningTokens := int64(*usage.ThinkingTokens)
		result.OutputDetails.ReasoningTokens = &reasoningTokens
		result.Raw["thinkingTokens"] = *usage.ThinkingTokens
	}

	return result
}
