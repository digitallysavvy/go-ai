package perplexity

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convertPerplexityUsage mirrors TS convert-perplexity-usage.ts for the Agent
// API. Unlike the pre-migration Sonar Chat Completions usage shape (see
// prds/completed and the removed convertPerplexityUsage(perplexityUsage) that
// treated reasoning tokens as ADDITIONAL to completion tokens), the Agent API
// follows the standard OpenAI-style convention: output_tokens is the total,
// and reasoning tokens are a SUBSET of it (text = output_tokens -
// reasoning_tokens, clamped to >=0).
func convertPerplexityUsage(usage *perplexityUsage, rawUsage []byte) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	totalTokens := usage.TotalTokens

	var cacheReadTokens int64
	var cacheWriteTokens int64
	if usage.InputTokensDetails != nil {
		if usage.InputTokensDetails.CacheReadInputTokens != nil {
			cacheReadTokens = *usage.InputTokensDetails.CacheReadInputTokens
		} else if usage.InputTokensDetails.CachedTokens != nil {
			cacheReadTokens = *usage.InputTokensDetails.CachedTokens
		}
		if usage.InputTokensDetails.CacheCreationInputTokens != nil {
			cacheWriteTokens = *usage.InputTokensDetails.CacheCreationInputTokens
		}
	}

	var reasoningTokens int64
	if usage.OutputTokensDetails != nil && usage.OutputTokensDetails.ReasoningTokens != nil {
		reasoningTokens = *usage.OutputTokensDetails.ReasoningTokens
	}

	noCache := inputTokens - cacheReadTokens - cacheWriteTokens
	if noCache < 0 {
		noCache = 0
	}
	textTokens := outputTokens - reasoningTokens
	if textTokens < 0 {
		textTokens = 0
	}

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:    &noCache,
			CacheReadTokens:  &cacheReadTokens,
			CacheWriteTokens: &cacheWriteTokens,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		},
	}

	if len(rawUsage) > 0 {
		var raw map[string]interface{}
		if err := json.Unmarshal(rawUsage, &raw); err == nil {
			result.Raw = raw
		}
	}

	return result
}

// PerplexityUsageMeta mirrors providerMetadata.perplexity.usage. Matches TS
// getProviderMetadata's `usage` field.
type PerplexityUsageMeta struct {
	CitationTokens   *int64 `json:"citationTokens"`
	NumSearchQueries *int64 `json:"numSearchQueries"`
}

// PerplexityCost mirrors providerMetadata.perplexity.cost.
type PerplexityCost struct {
	InputTokensCost   *float64 `json:"inputTokensCost"`
	OutputTokensCost  *float64 `json:"outputTokensCost"`
	RequestCost       *float64 `json:"requestCost"`
	TotalCost         *float64 `json:"totalCost"`
	Currency          *string  `json:"currency"`
	CacheCreationCost *float64 `json:"cacheCreationCost"`
	CacheReadCost     *float64 `json:"cacheReadCost"`
	ToolCallsCost     *float64 `json:"toolCallsCost"`
}

// PerplexityToolCallUsage mirrors one entry of providerMetadata.perplexity.toolCalls.
type PerplexityToolCallUsage struct {
	Invocation *int64 `json:"invocation"`
}

// PerplexityMetadata is the full providerMetadata.perplexity object for the
// Agent API. This is a BREAKING shape change from the pre-migration
// providerMetadata (which had Images/Usage/Cost only, with Usage.CitationTokens
// sourced from a flat usage.citation_tokens field): Images is always null now
// (the Agent API's image search results are not exposed via output items in
// this SDK version, matching TS which always sets `images: null`), and a new
// ToolCalls field surfaces per-tool invocation counts from usage.tool_calls_details.
type PerplexityMetadata struct {
	Usage     PerplexityUsageMeta                `json:"usage"`
	Images    interface{}                        `json:"images"`
	Cost      *PerplexityCost                    `json:"cost"`
	ToolCalls map[string]PerplexityToolCallUsage `json:"toolCalls"`
}

// getPerplexityProviderMetadata mirrors TS getProviderMetadata(usage).
func getPerplexityProviderMetadata(usage *perplexityUsage) map[string]interface{} {
	meta := PerplexityMetadata{Images: nil}

	if usage != nil && usage.ToolCallsDetails != nil {
		var total int64
		names := make([]string, 0, len(usage.ToolCallsDetails))
		for name := range usage.ToolCallsDetails {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !strings.Contains(name, "search") {
				continue
			}
			if inv := usage.ToolCallsDetails[name].Invocation; inv != nil {
				total += *inv
			}
		}
		meta.Usage.NumSearchQueries = &total

		meta.ToolCalls = make(map[string]PerplexityToolCallUsage, len(usage.ToolCallsDetails))
		for _, name := range names {
			meta.ToolCalls[name] = PerplexityToolCallUsage{Invocation: usage.ToolCallsDetails[name].Invocation}
		}
	}

	if usage != nil && usage.Cost != nil {
		c := usage.Cost
		meta.Cost = &PerplexityCost{
			InputTokensCost:   c.InputCost,
			OutputTokensCost:  c.OutputCost,
			RequestCost:       nil,
			TotalCost:         c.TotalCost,
			Currency:          c.Currency,
			CacheCreationCost: c.CacheCreationCost,
			CacheReadCost:     c.CacheReadCost,
			ToolCallsCost:     c.ToolCallsCost,
		}
	}

	return map[string]interface{}{"perplexity": meta}
}
