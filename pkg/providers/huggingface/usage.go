package huggingface

import (
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
	totalTokens := inputTokens + outputTokens // TS core: input total + output total

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

	// raw mirrors TS's `raw: usage` -- the usage object as received (the
	// schema's declared keys, numbers exact); a usage built in Go is
	// re-marshaled through hfUsage so absent optional sub-objects are
	// omitted rather than appearing as null/zero.
	raw := usage.raw
	if raw == nil {
		if data, err := jsonstr.Marshal(usage); err == nil {
			_ = jsonexact.Unmarshal(data, &raw)
		}
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
