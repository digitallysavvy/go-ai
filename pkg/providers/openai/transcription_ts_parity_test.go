package openai

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestTranscriptionProviderOptionsValidatedLikeTS(t *testing.T) {
	bad := map[string]map[string]interface{}{
		"temperature high":    {"temperature": 1.5},
		"temperature string":  {"temperature": "x"},
		"granularity enum":    {"timestampGranularities": []interface{}{"char"}},
		"response format":     {"responseFormat": "srt"},
		"chunking string":     {"chunkingStrategy": "manual"},
		"chunking type":       {"chunkingStrategy": map[string]interface{}{"type": "vad"}},
		"chunking padding":    {"chunkingStrategy": map[string]interface{}{"type": "server_vad", "prefixPaddingMs": 1.5}},
		"streaming delay":     {"streaming": map[string]interface{}{"delay": "slow"}},
		"include not strings": {"include": []interface{}{1.0}},
	}
	for name, o := range bad {
		err := validateTranscriptionProviderOptions(map[string]interface{}{"openai": o})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Message != "invalid openai provider options" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := validateTranscriptionProviderOptions(map[string]interface{}{"openai": map[string]interface{}{
		"temperature": 0.5, "responseFormat": "json", "streaming": map[string]interface{}{"delay": "low"},
		"chunkingStrategy": map[string]interface{}{"type": "server_vad", "threshold": nil},
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestChunkingStrategyWireOrderAndNulls(t *testing.T) {
	got := string(chunkingStrategyWireFormat(map[string]interface{}{
		"type": "server_vad", "silenceDurationMs": 5.0, "threshold": 0.5, "prefixPaddingMs": nil,
	}))
	if want := `{"type":"server_vad","threshold":0.5,"silence_duration_ms":5}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestStreamingTranscriptionDelayIsStringAndWarningsFollowNullChecks(t *testing.T) {
	o := TranscriptionStreamOpenAIOptions(map[string]interface{}{"openai": map[string]interface{}{
		"prompt": "", "include": []interface{}{}, "temperature": nil,
		"streaming": map[string]interface{}{"delay": "low", "include": []interface{}{}},
	}})
	if !o.HasPrompt || !o.HasInclude || o.HasTemperature {
		t.Fatalf("flags = %+v", o)
	}
	s := BuildOpenAIRealtimeTranscriptionSession("gpt-realtime-whisper", provider.AudioFormat{Type: "audio/pcm"}, o)
	sess := s["session"].(map[string]interface{})
	tr := sess["audio"].(map[string]interface{})["input"].(map[string]interface{})["transcription"].(map[string]interface{})
	if tr["delay"] != "low" {
		t.Fatalf("delay = %v", tr["delay"])
	}
	if inc, ok := sess["include"]; !ok || len(inc.([]string)) != 0 {
		t.Fatalf("include = %v", sess["include"])
	}
}
