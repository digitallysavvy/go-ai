package groq

import "testing"

// TS convertGroqUsage keeps a reported zero (cacheRead 0, reasoning 0) and
// leaves the field undefined when the wire omits it.
func TestConvertGroqUsageKeepsReportedZeros(t *testing.T) {
	zero := 0
	u := convertGroqUsage(groqUsage{PromptTokens: 5, CompletionTokens: 7})
	if u.InputDetails.CacheReadTokens != nil || u.OutputDetails.ReasoningTokens != nil {
		t.Fatalf("absent details became values: %+v %+v", u.InputDetails, u.OutputDetails)
	}
	var with groqUsage
	with.PromptTokens, with.CompletionTokens = 5, 7
	with.PromptTokensDetails = &struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
		AudioTokens  *int `json:"audio_tokens,omitempty"`
		TextTokens   *int `json:"text_tokens,omitempty"`
		ImageTokens  *int `json:"image_tokens,omitempty"`
	}{CachedTokens: &zero}
	with.CompletionTokensDetails = &struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	}{ReasoningTokens: &zero}
	u = convertGroqUsage(with)
	if u.InputDetails.CacheReadTokens == nil || *u.InputDetails.CacheReadTokens != 0 || u.OutputDetails.ReasoningTokens == nil || *u.OutputDetails.ReasoningTokens != 0 {
		t.Fatalf("reported zeros lost: %+v %+v", u.InputDetails, u.OutputDetails)
	}
}
