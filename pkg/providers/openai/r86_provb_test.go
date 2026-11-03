package openai

import (
	"encoding/json"
	"strings"
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

// TestR86provbDecisionSafetyIdentifierLength: zod .max(128) counts UTF-16
// code units, so 100 non-ASCII characters (300 UTF-8 bytes) are accepted and
// 129 are not.
func TestR86provbDecisionSafetyIdentifierLength(t *testing.T) {
	ok, err := parseDecisionProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": strings.Repeat("é", 100)}})
	if err != nil || ok.SafetyIdentifier == nil {
		t.Fatalf("100 code units rejected: %v", err)
	}
	if _, err := parseDecisionProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": strings.Repeat("é", 129)}}); err == nil {
		t.Fatal("129 code units accepted")
	}
	if _, err := parseDecisionProviderOptions(map[string]interface{}{"openai": map[string]interface{}{"safetyIdentifier": strings.Repeat("😀", 65)}}); err == nil {
		t.Fatal("130 code units (65 surrogate pairs) accepted")
	}
}
