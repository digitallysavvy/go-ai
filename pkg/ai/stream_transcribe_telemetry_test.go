package ai

import (
	"context"
	"io"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Ported from TS 8c659885c5 (#21427): experimental_streamTranscribe gained
// the same telemetry lifecycle (OnStart/OnEnd/OnError, provider usage
// propagation, audio byte-length accounting) as generateSpeech/transcribe.
// These tests cover the Go port's "ai.streamTranscribe" span creation for
// both the GenAI (OpenTelemetry) and legacy (LegacyOpenTelemetry)
// integrations, and the generic provider-usage attribute flattening.

func TestStreamTranscribe_Telemetry_OpenTelemetry(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-transcribe-telemetry-test")

	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			// Drain the audio stream so the telemetry byte-counting wrapper
			// observes every chunk, mirroring a real provider consuming it.
			for {
				if _, err := opts.Audio.Next(ctx); err != nil {
					break
				}
			}
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{
					Type:       provider.TranscriptionStreamPartTypeFinish,
					FinishText: "hello world",
					Usage:      map[string]interface{}{"seconds": 2.5},
				},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            model,
		Audio:            newMockChanAudioStream([]byte("abcde"), []byte("fg")),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Telemetry: &TelemetrySettings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer, Usage: true})},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	text, err := result.Text()
	if err != nil || text != "hello world" {
		t.Fatalf("Text() = %q, %v", text, err)
	}

	span := findSpanByName(rec.Ended(), "ai.streamTranscribe mock-model-id")
	if span == nil {
		t.Fatal("expected an 'ai.streamTranscribe mock-model-id' span")
	}
	if span.Status().Code == codes.Error {
		t.Fatal("expected span status to not be Error on success")
	}
	gotAttrs := audioTelemetrySpanAttrs(span)
	wantAttrs := map[string]interface{}{
		"gen_ai.operation.name": "ai.streamTranscribe",
		"gen_ai.output.type":    "text",
		"gen_ai.request.stream": true,
		"ai.response.text":      "hello world",
	}
	for k, want := range wantAttrs {
		if got, ok := gotAttrs[k]; !ok || got != want {
			t.Errorf("attribute %s = %v (ok=%v), want %v", k, got, ok, want)
		}
	}
	if got, ok := gotAttrs["ai.request.audio.size"]; !ok || got != int64(7) {
		t.Errorf("ai.request.audio.size = %v (ok=%v), want 7", got, ok)
	}
	if got, ok := gotAttrs["gen_ai.usage.seconds"]; !ok || got != 2.5 {
		t.Errorf("gen_ai.usage.seconds = %v (ok=%v), want 2.5", got, ok)
	}
	if got, ok := gotAttrs["ai.response.usage"]; !ok || got != `{"seconds":2.5}` {
		t.Errorf(`ai.response.usage = %v (ok=%v), want {"seconds":2.5}`, got, ok)
	}
}

func TestStreamTranscribe_Telemetry_ErrorStatus(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-transcribe-telemetry-error-test")

	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream(),
		Telemetry: &TelemetrySettings{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	if _, err := result.Text(); err == nil {
		t.Fatal("expected Text() to return an error")
	}

	span := findSpanByName(rec.Ended(), "ai.streamTranscribe mock-model-id")
	if span == nil {
		t.Fatal("expected an 'ai.streamTranscribe mock-model-id' span")
	}
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status().Code)
	}
}

func TestStreamTranscribe_Telemetry_LegacyOpenTelemetry(t *testing.T) {
	t.Parallel()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("stream-transcribe-telemetry-legacy-test")

	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{
					Type:       provider.TranscriptionStreamPartTypeFinish,
					FinishText: "hola",
					Usage:      map[string]interface{}{"inputTokens": 4},
				},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model:            model,
		Audio:            newMockChanAudioStream(),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		Telemetry: &TelemetrySettings{
			IsEnabled:     telemetry.Bool(true),
			RecordInputs:  true,
			RecordOutputs: true,
			Integrations:  []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tracer})},
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	text, err := result.Text()
	if err != nil || text != "hola" {
		t.Fatalf("Text() = %q, %v", text, err)
	}

	span := findSpanByName(rec.Ended(), "ai.streamTranscribe")
	if span == nil {
		t.Fatal("expected an 'ai.streamTranscribe' span")
	}
	gotAttrs := audioTelemetrySpanAttrs(span)
	if _, ok := gotAttrs["ai.request.audio.mediaType"]; !ok {
		t.Error("expected ai.request.audio.mediaType to be set")
	}
	if got, ok := gotAttrs["ai.response.text"]; !ok || got != "hola" {
		t.Errorf("ai.response.text = %v (ok=%v), want %q", got, ok, "hola")
	}
	// input_tokens alias: normalizeUsageKey("inputTokens") matches the
	// "inputtokens" alias and normalizes to "input_tokens".
	if got, ok := gotAttrs["ai.usage.input_tokens"]; !ok || got != 4.0 {
		t.Errorf("ai.usage.input_tokens = %v (ok=%v), want 4", got, ok)
	}
}

// TestStreamTranscribe_Telemetry_DiagnosticsChannel verifies that
// ExperimentalStreamTranscribe publishes onStart/onEnd diagnostic events for
// "ai.streamTranscribe" (TS's diagnostics-channel tracing, ported via Go's
// PublishDiagnostic calls inside FireOnStart/FireOnEnd), independent of any
// registered OTel integration. This is the mechanism behind TS's
// experimental_onStreamTranscriptionStart/End dispatcher callbacks.
func TestStreamTranscribe_Telemetry_DiagnosticsChannel(t *testing.T) {
	var mu sync.Mutex
	var sawStart, sawEnd bool

	unsubStart := telemetry.SubscribeDiagnosticTyped(telemetry.DiagnosticEventOnStart, func(_ context.Context, e telemetry.TelemetryStartEvent) error {
		if e.OperationType == "ai.streamTranscribe" {
			mu.Lock()
			sawStart = true
			mu.Unlock()
		}
		return nil
	})
	defer unsubStart()
	unsubEnd := telemetry.SubscribeDiagnosticTyped(telemetry.DiagnosticEventOnEnd, func(_ context.Context, e telemetry.TelemetryFinishEvent) error {
		if e.OperationType == "ai.streamTranscribe" {
			mu.Lock()
			sawEnd = true
			mu.Unlock()
		}
		return nil
	})
	defer unsubEnd()

	model := &mockTranscriptionStreamerModel{
		doStreamFn: func(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
			return sliceStreamResult(
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart},
				provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: "diag"},
			), nil
		},
	}

	result, err := ExperimentalStreamTranscribe(context.Background(), StreamTranscribeOptions{
		Model: model,
		Audio: newMockChanAudioStream(),
		Telemetry: &TelemetrySettings{
			IsEnabled: telemetry.Bool(true),
		},
	})
	if err != nil {
		t.Fatalf("ExperimentalStreamTranscribe error = %v", err)
	}
	if _, err := result.Text(); err != nil {
		t.Fatalf("Text() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !sawStart {
		t.Error("expected an onStart diagnostic event for ai.streamTranscribe")
	}
	if !sawEnd {
		t.Error("expected an onEnd diagnostic event for ai.streamTranscribe")
	}
}
