package openai

import (
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
