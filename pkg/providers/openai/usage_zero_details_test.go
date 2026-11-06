package openai

import "testing"

// TS convertOpenAIChatUsage always reports input/output details, with zeros
// when no cached/reasoning tokens are present.
func TestConvertOpenAIUsageZeroCachedAndReasoning(t *testing.T) {
	u := convertOpenAIUsage(openAIUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	if u.InputDetails == nil || *u.InputDetails.NoCacheTokens != 10 || *u.InputDetails.CacheReadTokens != 0 {
		t.Fatalf("input details = %+v", u.InputDetails)
	}
	if u.InputDetails.CacheWriteTokens != nil {
		t.Fatalf("cacheWrite must stay undefined: %v", *u.InputDetails.CacheWriteTokens)
	}
	if u.OutputDetails == nil || *u.OutputDetails.TextTokens != 5 || *u.OutputDetails.ReasoningTokens != 0 {
		t.Fatalf("output details = %+v", u.OutputDetails)
	}
}
