package openai

import "testing"

// TS mapResponseUsage keeps the JS number of each usage member.
func TestR92integRealtimeUsageKeepsFractionalTokens(t *testing.T) {
	u := openAIRealtimeResponseUsage(map[string]interface{}{"input_tokens": 10.5, "output_tokens": float64(2)})
	if u == nil || u.InputTokens == nil || *u.InputTokens != 10.5 || *u.OutputTokens != 2 {
		t.Fatalf("usage = %+v", u)
	}
}
