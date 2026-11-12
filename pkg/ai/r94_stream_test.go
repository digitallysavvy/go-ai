package ai

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// r94streamTranscribeEvents records the streamTranscribe start and end
// events a custom telemetry integration receives.
type r94streamTranscribeEvents struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	start *telemetry.TelemetryStartEvent
	end   *telemetry.TelemetryFinishEvent
}

func (r *r94streamTranscribeEvents) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = &e
	return ctx
}

func (r *r94streamTranscribeEvents) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.end = &e
}

func (r *r94streamTranscribeEvents) get() (*telemetry.TelemetryStartEvent, *telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.start, r.end
}

// r94streamTranscribeModel streams stream-start (with a warning), a
// response-metadata part (headers and a body) and a finish part with
// segments, language and duration.
func r94streamTranscribeModel(seen *map[string]interface{}) *mockTranscriptionStreamerModel {
	duration := 1.5
	return &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			if seen != nil {
				*seen = opts.ProviderOptions
			}
			for {
				if _, err := opts.Audio.Next(ctx); err != nil {
					break
				}
			}
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: []types.Warning{{Type: "other", Message: "test warning"}}},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeResponseMetadata, ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}, Body: map[string]interface{}{"raw": true}},
				provider.TranscriptionStreamPart{
					Type:              provider.TranscriptionStreamPartTypeFinish,
					FinishText:        "Hello",
					Segments:          []provider.TranscriptSegment{{Text: "Hello", StartSecond: 0, EndSecond: 1}},
					Language:          "en",
					DurationInSeconds: &duration,
				},
			), nil
		},
	}
}

// TS stream-transcribe.ts 158-171 and 277-294: the start event carries
// inputAudioFormat and providerOptions (`providerOptions = {}` when none);
// the end event carries segments, language, durationInSeconds, the
// stream-start warnings and the current response metadata.
func TestR94streamTranscribeTelemetryEventFields(t *testing.T) {
	for _, tc := range []struct {
		name            string
		providerOptions map[string]interface{}
		want            map[string]interface{}
	}{
		{name: "no provider options", providerOptions: nil, want: map[string]interface{}{}},
		{name: "provider options", providerOptions: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}}, want: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r94streamTranscribeEvents{}
			var seen map[string]interface{}
			rate := 16000
			result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
				Model:            r94streamTranscribeModel(&seen),
				Audio:            newMockChanAudioStream([]byte("abc")),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate},
				ProviderOptions:  tc.providerOptions,
				Telemetry:        &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := result.Text(); err != nil {
				t.Fatal(err)
			}
			var start *telemetry.TelemetryStartEvent
			var end *telemetry.TelemetryFinishEvent
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if start, end = rec.get(); start != nil && end != nil {
					break
				}
			}
			if start == nil || end == nil {
				t.Fatalf("start = %v, end = %v; want both events", start, end)
			}
			if start.InputAudioFormat == nil || start.InputAudioFormat.Type != "audio/pcm" || start.InputAudioFormat.Rate == nil || *start.InputAudioFormat.Rate != 16000 {
				t.Errorf("start InputAudioFormat = %#v, want audio/pcm at 16000", start.InputAudioFormat)
			}
			if start.ProviderOptions == nil || !reflect.DeepEqual(start.ProviderOptions, tc.want) {
				t.Errorf("start ProviderOptions = %#v, want %#v", start.ProviderOptions, tc.want)
			}
			if seen == nil || !reflect.DeepEqual(seen, tc.want) {
				t.Errorf("DoStream ProviderOptions = %#v, want %#v", seen, tc.want)
			}
			wantSegments := []telemetry.TelemetryTranscriptTiming{{Text: "Hello", StartSecond: 0, EndSecond: 1}}
			if !reflect.DeepEqual(end.Segments, wantSegments) {
				t.Errorf("end Segments = %#v, want %#v", end.Segments, wantSegments)
			}
			if end.Language != "en" || end.DurationInSeconds == nil || *end.DurationInSeconds != 1.5 {
				t.Errorf("end Language = %q, DurationInSeconds = %v; want en, 1.5", end.Language, end.DurationInSeconds)
			}
			wantWarnings := []types.Warning{{Type: "other", Message: "test warning"}}
			if !reflect.DeepEqual(end.Warnings, wantWarnings) {
				t.Errorf("end Warnings = %#v, want %#v", end.Warnings, wantWarnings)
			}
			wantResponse := &telemetry.TelemetryAudioResponse{Timestamp: time.Unix(0, 0), ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}}
			if !reflect.DeepEqual(end.Response, wantResponse) {
				t.Errorf("end Response = %#v, want %#v", end.Response, wantResponse)
			}
		})
	}
}

// TS stream-transcribe.ts 237-243: a response-metadata part updates the
// timestamp, model ID and headers of the transcription response; its body
// is not kept (TranscriptionModelResponseMetadata has no body).
func TestR94streamTranscribeResponseMetadataDropsBody(t *testing.T) {
	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            r94streamTranscribeModel(nil),
		Audio:            newMockChanAudioStream([]byte("abc")),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	responses, err := result.Responses()
	if err != nil {
		t.Fatal(err)
	}
	want := []TranscriptionModelResponseMetadata{{Timestamp: time.Unix(0, 0), ModelID: "resolved-model", Headers: map[string]string{"x-test": "value"}}}
	if !reflect.DeepEqual(responses, want) {
		t.Fatalf("Responses() = %#v, want %#v", responses, want)
	}
}
