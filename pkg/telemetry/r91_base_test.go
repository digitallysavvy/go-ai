package telemetry

import (
	"encoding/json"
	"testing"
	"time"
)

// B3 (core1-05, core1-R02, core2-05): the new event fields are declarations
// whose zero values mean "not set", and the helper types marshal with the TS
// key names.
func TestR91baseEventFieldDeclarations(t *testing.T) {
	var start TelemetryStartEvent
	if start.PromptString != nil || start.Voice != "" || start.OutputFormat != "" || start.Instructions != "" ||
		start.Speed != nil || start.Language != "" || start.ProviderOptions != nil || start.InputAudioFormat != nil ||
		start.TimestampGranularities != nil {
		t.Fatal("zero TelemetryStartEvent fields must be unset")
	}
	var step TelemetryStepStartEvent
	if step.System != "" || step.InstructionMessages != nil {
		t.Fatal("zero TelemetryStepStartEvent fields must be unset")
	}
	var fin TelemetryFinishEvent
	if fin.Warnings != nil || fin.Response != nil || fin.Segments != nil || fin.Words != nil ||
		fin.Language != "" || fin.DurationInSeconds != nil {
		t.Fatal("zero TelemetryFinishEvent fields must be unset")
	}

	rate := 16000
	for _, tc := range []struct {
		v    interface{}
		want string
	}{
		{TelemetryAudioFormat{Type: "audio/pcm", Rate: &rate}, `{"type":"audio/pcm","rate":16000}`},
		{TelemetryAudioFormat{Type: "audio/pcm"}, `{"type":"audio/pcm"}`},
		{TelemetryTranscriptTiming{Text: "hi", StartSecond: 0.5, EndSecond: 1}, `{"text":"hi","startSecond":0.5,"endSecond":1}`},
		{TelemetryAudioResponse{Timestamp: time.Unix(0, 0).UTC(), ModelID: "m"}, `{"timestamp":"1970-01-01T00:00:00Z","modelId":"m"}`},
	} {
		b, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.want {
			t.Fatalf("%T = %s, want %s", tc.v, b, tc.want)
		}
	}
}
