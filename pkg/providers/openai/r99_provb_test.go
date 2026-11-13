package openai

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR99provbLiveSessionInputKeyOrder: TS sends JSON.stringify of the
// session with options.input as the strict zod schema outputs it, whose keys
// follow the schema shape ({type, role, content}, content [{type, text}]).
// Go wrote the caller's item keys sorted (content, role, type).
func TestR99provbLiveSessionInputKeyOrder(t *testing.T) {
	live := NewRealtimeModelLive(New(Config{APIKey: "k"}), "gpt-live-1")
	raw, err := live.SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-start", Config: provider.RealtimeSessionConfig{
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"input": []interface{}{map[string]interface{}{
				"content": []interface{}{map[string]interface{}{"text": "hi", "type": "input_text"}},
				"role":    "user",
				"type":    "message",
			}},
		}},
	}})
	want := `{"type":"session.start","session":{"model":"gpt-live-1","audio":{"format":{"type":"audio/pcm","rate":24000},"output":{"voice":"marin"}},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}}`
	if err != nil || string(raw) != want {
		t.Errorf("session-start = %s, %v\nwant %s", raw, err, want)
	}
}

// TestR99provbBatchErrorInfiniteCode: TS openaiErrorDataSchema's code is
// z.union([z.string(), z.number()]) and z.number() rejects the Infinity
// JSON.parse reads for 1e400, so the item error is the status-code fallback.
// Go accepted it and reported the code "Infinity".
func TestR99provbBatchErrorInfiniteCode(t *testing.T) {
	got := convertOpenAIBatchErrorResponse([]byte(`{"error":{"message":"m","code":1e400}}`), 400)
	if got.Message != "OpenAI batch request failed with status code 400." || got.Code != "" {
		t.Errorf("error = %+v, want the status-code fallback", got)
	}
	got = convertOpenAIBatchErrorResponse([]byte(`{"error":{"message":"m","code":42}}`), 400)
	if got.Message != "m" || got.Code != "42" {
		t.Errorf("error = %+v, want message m code 42", got)
	}
}

// TestR99provbDecisionUsagePlatformInt: a Decisions usage count beyond the
// platform int is clamped instead of an implementation-defined conversion
// (it wraps on 386 / arm and is MinInt64 on amd64).
func TestR99provbDecisionUsagePlatformInt(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{{1e19, math.MaxInt}, {-1e19, math.MinInt}, {12.9, 12}} {
		v := tc.in
		if got := floatToIntPtr(&v); got == nil || float64(*got) != tc.want {
			t.Errorf("floatToIntPtr(%v) = %d, want %v", tc.in, *got, tc.want)
		}
	}
	if big := 3e9; float64(math.MaxInt) < big {
		if got := floatToIntPtr(&big); *got != math.MaxInt {
			t.Errorf("floatToIntPtr(3e9) = %d on a 32-bit int", *got)
		}
	}
}

