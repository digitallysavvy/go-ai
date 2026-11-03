package openai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR86provbStreamingIncludeGoTypedList: a []string streaming.include is
// sent in the realtime session.update like its JSON form (TS
// `include: providerOptions.streaming.include`); it was dropped.
func TestR86provbStreamingIncludeGoTypedList(t *testing.T) {
	session := func(include interface{}) string {
		opts := TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]interface{}{
			"language":  "en",
			"streaming": map[string]interface{}{"delay": "low", "include": include},
		}})
		b, err := json.Marshal(BuildOpenAIRealtimeTranscriptionSession("gpt-live-transcribe", provider.AudioFormat{Type: "audio/pcm"}, opts))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	got := session([]string{"item.input_audio_transcription.logprobs"})
	want := session([]interface{}{"item.input_audio_transcription.logprobs"})
	if got != want {
		t.Fatalf("typed = %s\njson  = %s", got, want)
	}
	const tsWant = `{"session":{"audio":{"input":{"format":{"type":"audio/pcm"},"transcription":{"delay":"low","languages":["en"],"model":"gpt-live-transcribe"},"turn_detection":null}},"include":["item.input_audio_transcription.logprobs"],"type":"transcription"},"type":"session.update"}`
	if got != tsWant {
		t.Fatalf("session = %s\nwant      %s", got, tsWant)
	}
}
