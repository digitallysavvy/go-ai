package deepseek

import (
	"encoding/json"
	"testing"
)

// TS core asLanguageModelUsage: totalTokens is inputTokens + outputTokens; the
// wire total_tokens is not used.
func TestConvertDeepseekUsageTotalIsInputPlusOutput(t *testing.T) {
	u := convertDeepseekUsage(json.RawMessage(`{"prompt_tokens":10,"completion_tokens":5,"total_tokens":99}`))
	if u.TotalTokens == nil || *u.TotalTokens != 15 {
		t.Fatalf("TotalTokens = %v, want 15", u.TotalTokens)
	}
}
