package telemetry

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS ports (r82 carry): otel/src/legacy-open-telemetry.test.ts "onStepError"
// (932), "LegacyOpenTelemetry speech and transcription operations" (3810,
// 3868) and otel/src/open-telemetry.test.ts "onStepError" (2245).

func r82carryRecorder(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, tp
}

func r82carryEnded(rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func r82carryHasException(s sdktrace.ReadOnlySpan, message string) bool {
	for _, ev := range s.Events() {
		if ev.Name != "exception" {
			continue
		}
		for _, kv := range ev.Attributes {
			if kv.Key == "exception.message" && kv.Value.AsString() == message {
				return true
			}
		}
	}
	return false
}

// legacy-open-telemetry.test.ts 932 "records error on the step span and keeps
// the root span open".
func TestR82carryLegacyStepErrorKeepsRootOpen(t *testing.T) {
	rec, tp := r82carryRecorder(t)
	it := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	const callID = "r82carry-legacy-step-error"
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m", Settings: settings})
	ctx = it.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m", Settings: settings})
	it.OnStepError(ctx, TelemetryErrorEvent{CallID: callID, Settings: settings, Error: errors.New("step failed")})

	step := r82carryEnded(rec, "ai.generateText.doGenerate")
	if step == nil {
		t.Fatal("step span not ended")
	}
	if !r82carryHasException(step, "step failed") || step.Status().Code != codes.Error {
		t.Fatalf("step span: status %+v, events %+v", step.Status(), step.Events())
	}
	if r82carryEnded(rec, "ai.generateText") != nil {
		t.Fatal("root span ended by onStepError")
	}
	it.OnEnd(ctx, TelemetryFinishEvent{CallID: callID, OperationType: "ai.generateText", Settings: settings, FinishReason: "stop"})
	if r82carryEnded(rec, "ai.generateText") == nil {
		t.Fatal("root span not ended by onEnd")
	}
}

// open-telemetry.test.ts 2245 "records error on step and chat spans and keeps
// the root span open for onEnd".
func TestR82carryGenAIStepErrorKeepsRootOpen(t *testing.T) {
	rec, tp := r82carryRecorder(t)
	it := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	const callID = "r82carry-genai-step-error"
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelProvider: "openai", ModelID: "gpt-4", Settings: settings})
	ctx = it.OnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, OperationType: "ai.generateText", ModelProvider: "openai", ModelID: "gpt-4", Settings: settings})
	ctx = it.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{CallID: callID, ModelProvider: "openai", ModelID: "gpt-4", Settings: settings})
	it.OnStepError(ctx, TelemetryErrorEvent{CallID: callID, Settings: settings, Error: errors.New("step failed")})

	for _, name := range []string{"step 1", "chat gpt-4"} {
		s := r82carryEnded(rec, name)
		if s == nil {
			t.Fatalf("%s not ended", name)
		}
		if s.Status().Code != codes.Error || s.Status().Description != "step failed" {
			t.Fatalf("%s status = %+v, want error step failed", name, s.Status())
		}
	}
	if r82carryEnded(rec, "invoke_agent gpt-4") != nil {
		t.Fatal("root span ended by onStepError")
	}
	it.OnEnd(ctx, TelemetryFinishEvent{CallID: callID, OperationType: "ai.generateText", Settings: settings})
	root := r82carryEnded(rec, "invoke_agent gpt-4")
	if root == nil {
		t.Fatal("root span not ended by onEnd")
	}
	if root.Status().Code != codes.Unset {
		t.Fatalf("root status = %+v, want unset", root.Status())
	}
}

// legacy-open-telemetry.test.ts 3810 "honors speech input and output privacy
// controls".
func TestR82carryLegacySpeechPrivacyControls(t *testing.T) {
	rec, tp := r82carryRecorder(t)
	it := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(false), RecordOutputs: Bool(false)}
	const callID = "r82carry-speech-call"
	maxRetries := 2
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateSpeech", ModelProvider: "openai.speech", ModelID: "gpt-4o-mini-tts", Text: "private text", MaxRetries: &maxRetries, Settings: settings})
	size := int64(1234)
	it.OnEnd(ctx, TelemetryFinishEvent{CallID: callID, OperationType: "ai.generateSpeech", Settings: settings,
		AudioByteLength: &size, AudioMediaType: "audio/mpeg", AudioFormat: "mp3",
		ProviderUsage: map[string]interface{}{"characters": 12}})

	root := r82carryEnded(rec, "ai.generateSpeech")
	if root == nil {
		t.Fatal("speech span not ended")
	}
	got := map[string]string{}
	for _, kv := range root.Attributes() {
		got[string(kv.Key)] = kv.Value.String()
	}
	for _, key := range []string{"ai.request.text", "ai.response.audio.size", "ai.response.audio.mediaType", "ai.response.audio.format"} {
		if v, ok := got[key]; ok {
			t.Errorf("%s = %q, want absent", key, v)
		}
	}
	if got["ai.usage.characters"] != "12" {
		t.Errorf("ai.usage.characters = %q, want 12", got["ai.usage.characters"])
	}
	if got["ai.response.usage"] != `{"characters":12}` {
		t.Errorf("ai.response.usage = %q", got["ai.response.usage"])
	}
}

// legacy-open-telemetry.test.ts 3868 "records streaming transcription errors on
// the operation span".
func TestR82carryLegacyStreamTranscriptionError(t *testing.T) {
	rec, tp := r82carryRecorder(t)
	it := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	const callID = "r82carry-stream-call"
	ctx := it.OnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.streamTranscribe", ModelProvider: "openai.transcription", ModelID: "gpt-realtime-whisper", AudioMediaType: "audio/pcm", Settings: settings})
	it.OnError(ctx, TelemetryErrorEvent{CallID: callID, Settings: settings, Error: errors.New("stream failed")})

	root := r82carryEnded(rec, "ai.streamTranscribe")
	if root == nil {
		t.Fatal("stream transcription span not ended")
	}
	if !r82carryHasException(root, "stream failed") {
		t.Fatalf("no exception event: %+v", root.Events())
	}
	if root.Status().Code != codes.Error || root.Status().Description != "stream failed" {
		t.Fatalf("status = %+v", root.Status())
	}
}
