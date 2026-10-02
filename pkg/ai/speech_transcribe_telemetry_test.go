package ai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ported from TS 8c659885c5 (#21427): generateSpeech and transcribe gained a
// telemetry lifecycle (OnStart/OnEnd/OnError). These tests cover the Go
// port's span creation, request/response attributes, and error status for
// both the GenAI (OpenTelemetry) and legacy (LegacyOpenTelemetry)
// integrations.

func TestGenerateSpeech_Telemetry_OpenTelemetry(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("generate-speech-telemetry-test")

	model := &testutil.MockSpeechModel{
		ModelName: "tts-1",
		DoGenerateFunc: func(_ context.Context, _ *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			return &types.SpeechResult{Audio: []byte("RIFF....WAVEfmt ")}, nil
		},
	}

	result, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model,
		Text:  "Hello world",
		Telemetry: &TelemetrySettings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Audio.Data) == 0 {
		t.Fatal("expected generated audio data")
	}

	span := findSpanByName(rec.Ended(), "ai.generateSpeech tts-1")
	if span == nil {
		t.Fatal("expected an 'ai.generateSpeech tts-1' span")
	}
	if span.Status().Code == codes.Error {
		t.Fatal("expected span status to not be Error on success")
	}
	wantAttrs := map[string]interface{}{
		"gen_ai.operation.name": "ai.generateSpeech",
		"gen_ai.provider.name":  "mock",
		"gen_ai.request.model":  "tts-1",
		"gen_ai.output.type":    "speech",
		"gen_ai.request.stream": false,
		"ai.request.text":       "Hello world",
	}
	gotAttrs := audioTelemetrySpanAttrs(span)
	for k, want := range wantAttrs {
		if got, ok := gotAttrs[k]; !ok || got != want {
			t.Errorf("attribute %s = %v (ok=%v), want %v", k, got, ok, want)
		}
	}
	if _, ok := gotAttrs["ai.response.audio.size"]; !ok {
		t.Error("expected ai.response.audio.size to be set")
	}
	if _, ok := gotAttrs["ai.response.audio.media_type"]; !ok {
		t.Error("expected ai.response.audio.media_type to be set")
	}
	if _, ok := gotAttrs["ai.response.audio.format"]; !ok {
		t.Error("expected ai.response.audio.format to be set")
	}
}

func TestGenerateSpeech_Telemetry_ErrorStatus(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("generate-speech-telemetry-error-test")

	model := &testutil.MockSpeechModel{
		ModelName: "tts-1",
		DoGenerateFunc: func(_ context.Context, _ *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
			// No audio bytes -> NoSpeechGeneratedError, which must still
			// settle the span via FireOnError (mirrors TS's catch block).
			return &types.SpeechResult{}, nil
		},
	}

	_, err := GenerateSpeech(context.Background(), GenerateSpeechOptions{
		Model: model,
		Text:  "Hello world",
		Telemetry: &TelemetrySettings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if !IsNoSpeechGeneratedError(err) {
		t.Fatalf("expected NoSpeechGeneratedError, got %v", err)
	}

	span := findSpanByName(rec.Ended(), "ai.generateSpeech tts-1")
	if span == nil {
		t.Fatal("expected an 'ai.generateSpeech tts-1' span")
	}
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status().Code)
	}
}

func TestTranscribe_Telemetry_LegacyOpenTelemetry(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("transcribe-telemetry-test")

	model := &testutil.MockTranscriptionModel{
		ModelName: "whisper-1",
		DoTranscribeFunc: func(_ context.Context, _ *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{
				Text:  "hello world",
				Usage: types.TranscriptionUsage{DurationSeconds: 1.5},
			}, nil
		},
	}

	result, err := Transcribe(context.Background(), TranscribeOptions{
		Model: model,
		Audio: []byte("RIFF....WAVEfmt "),
		Telemetry: &TelemetrySettings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "hello world" {
		t.Fatalf("result.Text = %q, want %q", result.Text, "hello world")
	}

	span := findSpanByName(rec.Ended(), "ai.transcribe")
	if span == nil {
		t.Fatal("expected an 'ai.transcribe' span")
	}
	gotAttrs := audioTelemetrySpanAttrs(span)
	if _, ok := gotAttrs["ai.request.audio.size"]; !ok {
		t.Error("expected ai.request.audio.size to be set")
	}
	if _, ok := gotAttrs["ai.request.audio.mediaType"]; !ok {
		t.Error("expected ai.request.audio.mediaType to be set")
	}
	if got, ok := gotAttrs["ai.response.text"]; !ok || got != "hello world" {
		t.Errorf("ai.response.text = %v (ok=%v), want %q", got, ok, "hello world")
	}
	if got, ok := gotAttrs["ai.usage.duration_seconds"]; !ok || got != 1.5 {
		t.Errorf("ai.usage.duration_seconds = %v (ok=%v), want 1.5", got, ok)
	}
}

// audioTelemetrySpanAttrs flattens a span's attributes into a map for easy
// lookup (speech/transcribe telemetry tests only; see findSpanByName in
// object_telemetry_test.go for the shared by-name span lookup helper).
func audioTelemetrySpanAttrs(s sdktrace.ReadOnlySpan) map[string]interface{} {
	out := map[string]interface{}{}
	for _, kv := range s.Attributes() {
		out[string(kv.Key)] = kv.Value.AsInterface()
	}
	return out
}
