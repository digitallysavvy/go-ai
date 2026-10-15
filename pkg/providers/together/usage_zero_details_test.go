package together

import "testing"

func TestConvertTogetherUsageZeroDetails(t *testing.T) {
	u := convertTogetherUsage(togetherUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	if u.InputDetails == nil || *u.InputDetails.NoCacheTokens != 10 || *u.InputDetails.CacheReadTokens != 0 {
		t.Fatalf("input details = %+v", u.InputDetails)
	}
	if u.OutputDetails == nil || *u.OutputDetails.TextTokens != 5 || *u.OutputDetails.ReasoningTokens != 0 {
		t.Fatalf("output details = %+v", u.OutputDetails)
	}
}
