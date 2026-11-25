package deepseek

import (
	"encoding/json"
	"testing"
)

// provg r90 low class: TS reads usage counts with z.number(); a fractional
// count zeroed the Go field (json.Unmarshal skips it). It is truncated into
// the int64 Usage field now (S1: Usage token fields are integers).
func TestR90integFractionalUsageNotZeroed(t *testing.T) {
	u := convertDeepseekUsage(json.RawMessage(`{"prompt_tokens":2.5,"completion_tokens":3.5,"total_tokens":6}`))
	if u.InputTokens == nil || *u.InputTokens != 2 || u.OutputTokens == nil || *u.OutputTokens != 3 {
		t.Fatalf("usage = %+v, want input 2, output 3", u)
	}
}
