package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR98provbCallerToolArgsKeepPrototypeKeys: TS validateTypes checks the
// caller's tool args object without a JSON parse, so a __proto__ key is an
// ordinary (stripped) property, not a "forbidden prototype property" error.
func TestR98provbCallerToolArgsKeepPrototypeKeys(t *testing.T) {
	tool := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "xai.web_search", Name: "web_search",
		ProviderArgs: map[string]interface{}{"__proto__": map[string]interface{}{"x": 1}}}
	if err := validateXAIResponsesToolArgs([]types.Tool{tool}); err != nil {
		t.Fatalf("err = %v, want valid args", err)
	}
}

// TestR98provbRealtimeClientEventKeyOrder: TS serializeXaiRealtimeClientEvent
// returns object literals, so the frames keep their key order (type first);
// Go marshaled maps with sorted keys.
func TestR98provbRealtimeClientEventKeyOrder(t *testing.T) {
	m := NewRealtimeModel(New(Config{APIKey: "k"}), "grok-voice")
	instructions, voice := "Hi", "Ara"
	rate := 24000
	raw, err := m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "session-update", Config: provider.RealtimeSessionConfig{
		Instructions: &instructions, Voice: &voice, InputAudioFormat: &provider.RealtimeAudioFormat{Type: "audio/pcm", Rate: &rate},
		TurnDetection: &provider.RealtimeTurnDetection{Type: "server-vad"},
	}})
	want := `{"type":"session.update","session":{"instructions":"Hi","voice":"Ara","audio":{"input":{"format":{"type":"audio/pcm","rate":24000}}},"turn_detection":{"type":"server_vad"}}}`
	if err != nil || string(raw) != want {
		t.Errorf("session-update = %s, %v\nwant %s", raw, err, want)
	}
	raw, _ = m.SerializeClientEvent(provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "AA=="})
	if want := `{"type":"input_audio_buffer.append","audio":"AA=="}`; string(raw) != want {
		t.Errorf("append = %s", raw)
	}
}
