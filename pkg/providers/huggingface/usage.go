package huggingface

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convertHuggingFaceResponsesUsage mirrors TS convertHuggingFaceResponsesUsage.
// A nil usage produces a zero-value types.Usage (matching TS's
// createNullLanguageModelUsage(), which leaves every field undefined).
func convertHuggingFaceResponsesUsage(usage *hfUsage) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	totalTokens := usage.TotalTokens

	var cachedTokens int64
	if usage.InputTokensDetails != nil {
		cachedTokens = usage.InputTokensDetails.CachedTokens
	}
	var reasoningTokens int64
	if usage.OutputTokensDetails != nil {
		reasoningTokens = usage.OutputTokensDetails.ReasoningTokens
	}

	noCache := inputTokens - cachedTokens
	textTokens := outputTokens - reasoningTokens

	// raw mirrors TS's `raw: usage` -- the exact usage object as received,
	// re-marshaled through hfUsage so absent optional sub-objects are
	// omitted rather than appearing as null/zero.
	var raw map[string]interface{}
	if data, err := json.Marshal(usage); err == nil {
		_ = json.Unmarshal(data, &raw)
	}

	return types.Usage{
		InputTokens: &inputTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:   &noCache,
			CacheReadTokens: &cachedTokens,
		},
		OutputTokens: &outputTokens,
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		},
		TotalTokens: &totalTokens,
		Raw:         raw,
	}
}
