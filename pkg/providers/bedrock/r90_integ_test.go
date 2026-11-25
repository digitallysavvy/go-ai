package bedrock

import (
	"encoding/json"
	"testing"
)

// provg r90 low class: TS convert-amazon-bedrock-usage reads z.number()
// counts; a fractional count zeroed the Go field. It is truncated into the
// int64 Usage field now (S1).
func TestR90integConverseFractionalUsageNotZeroed(t *testing.T) {
	u := convertBedrockConverseUsage(json.RawMessage(`{"inputTokens":2.5,"outputTokens":3.5,"totalTokens":6}`))
	if u.OutputTokens == nil || *u.OutputTokens != 3 || u.InputTokens == nil || *u.InputTokens < 2 {
		t.Fatalf("usage = %+v, want output 3 and input from 2", u)
	}
}
