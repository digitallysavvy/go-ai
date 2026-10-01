package moonshot

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MoonshotUsage represents token usage information from Moonshot API responses.
// Kept for callers that want to construct a typed usage value directly (e.g.
// tests); ConvertMoonshotUsage marshals it to JSON and delegates to
// ConvertMoonshotUsageRaw so behavior matches the wire-parsed path exactly.
type MoonshotUsage struct {
	// Standard token counts
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Cached tokens (for prompt caching)
	CachedTokens *int `json:"cached_tokens,omitempty"`

	// Detailed token breakdowns
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
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

// moonshotUsageKnownFields decodes the subset of the usage object the SDK
// interprets. Type mismatches on these fields (e.g. a string where a number
// is expected) produce a decode error, mirroring TS's Zod-validated
// tokenUsageSchema rejecting malformed usage payloads.
type moonshotUsageKnownFields struct {
	PromptTokens        *float64 `json:"prompt_tokens"`
	CompletionTokens    *float64 `json:"completion_tokens"`
	TotalTokens         *float64 `json:"total_tokens"`
	CachedTokens        *float64 `json:"cached_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *float64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens *float64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// ConvertMoonshotUsage converts a typed MoonshotUsage value to SDK usage
// format. Provided for callers constructing usage directly (e.g. tests); the
// wire-parsing path uses ConvertMoonshotUsageRaw so unknown fields survive
// into Usage.Raw.
func ConvertMoonshotUsage(usage MoonshotUsage) types.Usage {
	raw, err := json.Marshal(usage)
	if err != nil {
		return types.Usage{}
	}
	result, err := ConvertMoonshotUsageRaw(raw)
	if err != nil {
		return types.Usage{}
	}
	return result
}

// ConvertMoonshotUsageRaw converts a raw JSON usage object into SDK usage
// format. Usage.Raw preserves the FULL decoded object -- including fields the
// SDK doesn't model -- mirroring TS convertMoonshotAIChatUsage, which returns
// `raw: usage` (the whole parsed, loosely-typed object) rather than a
// hand-built subset. Output text-token counts are clamped to zero when
// reasoning tokens exceed completion tokens.
func ConvertMoonshotUsageRaw(raw json.RawMessage) (types.Usage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return types.Usage{}, nil
	}

	var known moonshotUsageKnownFields
	if err := json.Unmarshal(raw, &known); err != nil {
		return types.Usage{}, err
	}

	var rawMap map[string]interface{}
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return types.Usage{}, err
	}

	promptTokens := int64(derefFloat(known.PromptTokens))
	completionTokens := int64(derefFloat(known.CompletionTokens))
	totalTokens := int64(derefFloat(known.TotalTokens))

	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
		Raw:          rawMap,
	}

	var cacheReadTokens int64
	if known.CachedTokens != nil {
		cacheReadTokens = int64(*known.CachedTokens)
	} else if known.PromptTokensDetails != nil && known.PromptTokensDetails.CachedTokens != nil {
		cacheReadTokens = int64(*known.PromptTokensDetails.CachedTokens)
	}
	if cacheReadTokens > 0 {
		result.InputDetails = &types.InputTokenDetails{CacheReadTokens: &cacheReadTokens}
		noCacheTokens := promptTokens - cacheReadTokens
		if noCacheTokens > 0 {
			result.InputDetails.NoCacheTokens = &noCacheTokens
		}
	}

	var reasoningTokens int64
	if known.CompletionTokensDetails != nil && known.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = int64(*known.CompletionTokensDetails.ReasoningTokens)
	}
	if reasoningTokens > 0 {
		textTokens := completionTokens - reasoningTokens
		if textTokens < 0 {
			textTokens = 0
		}
		result.OutputDetails = &types.OutputTokenDetails{
			ReasoningTokens: &reasoningTokens,
			TextTokens:      &textTokens,
		}
	}

	return result, nil
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
