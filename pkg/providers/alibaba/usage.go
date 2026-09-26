package alibaba

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AlibabaUsage mirrors the declared fields of Alibaba's OpenAI-compatible
// chat completions usage object. Alibaba returns more fields than declared
// here; UnmarshalJSON preserves the full object (see raw) so
// ConvertAlibabaUsage can surface it in types.Usage.Raw instead of dropping
// it, mirroring the TypeScript SDK's loosely-parsed alibabaUsageSchema.
type AlibabaUsage struct {
	PromptTokens            int                             `json:"prompt_tokens"`
	CompletionTokens        int                             `json:"completion_tokens"`
	TotalTokens             int                             `json:"total_tokens"`
	PromptTokensDetails     *AlibabaPromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *AlibabaCompletionTokensDetails `json:"completion_tokens_details,omitempty"`

	raw map[string]interface{}
}

// AlibabaPromptTokensDetails is the nested cache-token breakdown Alibaba
// reports under prompt_tokens_details.
type AlibabaPromptTokensDetails struct {
	CachedTokens             int `json:"cached_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`

	// CacheType discriminates explicit context caching (selected by sending
	// cache_control) from implicit caching, where the field is absent. The
	// two modes are mutually exclusive and priced differently.
	CacheType string `json:"cache_type,omitempty"`
}

// AlibabaCompletionTokensDetails is the nested reasoning-token breakdown
// Alibaba reports under completion_tokens_details.
type AlibabaCompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// UnmarshalJSON decodes the declared fields above and separately retains the
// full usage object (including anything undeclared) for
// ConvertAlibabaUsage's Raw output.
func (u *AlibabaUsage) UnmarshalJSON(data []byte) error {
	type alias AlibabaUsage
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*u = AlibabaUsage(a)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		u.raw = raw
	}
	return nil
}

// ConvertAlibabaUsage converts Alibaba usage to SDK usage format, mirroring
// the TypeScript SDK's convertAlibabaUsage.
func ConvertAlibabaUsage(usage AlibabaUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)

	var cacheReadTokens, cacheWriteTokens int64
	if usage.PromptTokensDetails != nil {
		cacheReadTokens = int64(usage.PromptTokensDetails.CachedTokens)
		cacheWriteTokens = int64(usage.PromptTokensDetails.CacheCreationInputTokens)
	}

	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil {
		reasoningTokens = int64(usage.CompletionTokensDetails.ReasoningTokens)
	}

	// Alibaba counts both cache reads and cache writes inside prompt_tokens.
	noCacheTokens := promptTokens - cacheReadTokens - cacheWriteTokens

	// Prevent a negative text token count when reasoning tokens are reported
	// as part of completion_tokens (mirrors TS Math.max(0, ...)).
	textTokens := completionTokens - reasoningTokens
	if textTokens < 0 {
		textTokens = 0
	}

	return types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cacheReadTokens,
			CacheWriteTokens: &cacheWriteTokens,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		},
		Raw: usage.raw,
	}
}
