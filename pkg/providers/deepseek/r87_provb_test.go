package deepseek

import (
	"encoding/json"
	"testing"
)

// TS deepSeekErrorSchema: a stream error's message is a required string, so
// "" is the message (Go used the raw JSON text).
func TestR87provbStreamErrorEmptyMessage(t *testing.T) {
	raw := json.RawMessage(`{"message":"","type":"server_error"}`)
	if got := deepseekStreamErrorText(raw); got != "" {
		t.Fatalf("text = %q, want empty", got)
	}
	if perr := newDeepSeekStreamProviderErrorChunk(raw, nil); perr == nil || perr.Message != "" {
		t.Fatalf("stream error = %#v, want an empty message", perr)
	}
}
