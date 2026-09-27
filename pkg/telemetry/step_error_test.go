package telemetry

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// H4 item 2: "span leak on provider error" — unit coverage for the
// OnStepError/FireOnStepError mechanism added to close a still-open step
// span (and, for GenAI, the nested model-call/"chat" span) when the
// provider's doGenerate/doStream call itself fails, before either
// OnStepEnd or OnLanguageModelCallEnd would ever fire for it. See
// pkg/ai/telemetry_span_leak_test.go for end-to-end coverage through
// GenerateText/StreamText/GenerateObject/StreamObject.

func TestLegacyOpenTelemetry_OnStepError_ClosesStepSpanWithErrorStatus(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("step-error-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		OperationType: "ai.generateText", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	// Legacy has no OnLanguageModelCallStart of its own (H4 item 1): it no
	// longer implements the interface, so FireOnLanguageModelCallStart's
	// type-assertion loop leaves ctx unchanged — modelCallCtx == stepCtx.
	modelCallCtx := stepCtx

	wantErr := errors.New("provider boom")
	integration.OnStepError(modelCallCtx, TelemetryErrorEvent{Settings: settings, Error: wantErr})

	stepSpan := findSpan(rec, "ai.generateText.doGenerate")
	if stepSpan == nil {
		t.Fatal("expected an ai.generateText.doGenerate step span")
	}
	if !isEnded(stepSpan) {
		t.Fatal("expected the step span to have been ended by OnStepError")
	}
	if got := stepSpan.Status().Code; got != codes.Error {
		t.Fatalf("step span status = %v, want codes.Error", got)
	}

	// The root span must NOT have been touched by OnStepError (only OnError
	// closes the root span, at the outer ctx — that's a separate call the
	// generate.go/stream.go/object.go deferred cleanup makes).
	if isEnded(findSpan(rec, "ai.generateText")) {
		t.Fatal("OnStepError must not end the root span")
	}
}

func TestLegacyOpenTelemetry_OnStepError_NoErrorStatusWhenErrorNil(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("step-error-abort-test")

	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.streamText", Settings: settings})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		OperationType: "ai.streamText", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})

	// Abort path: Error is nil, mirroring TS onAbort's plain span.end() (no
	// recordSpanError call).
	integration.OnStepError(stepCtx, TelemetryErrorEvent{Settings: settings})

	stepSpan := findSpan(rec, "ai.streamText.doStream")
	if stepSpan == nil {
		t.Fatal("expected an ai.streamText.doStream step span")
	}
	if !isEnded(stepSpan) {
		t.Fatal("expected the step span to have been ended")
	}
	if got := stepSpan.Status().Code; got == codes.Error {
		t.Fatalf("step span status = %v, want anything but codes.Error for an abort", got)
	}
}

func TestOpenTelemetry_OnStepError_ClosesStepAndChatSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("step-error-genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", ModelProvider: "openai", ModelID: "gpt-5", Settings: settings})
	stepCtx := integration.OnStepStart(rootCtx, TelemetryStepStartEvent{
		OperationType: "ai.generateText", Settings: settings, ModelProvider: "openai", ModelID: "gpt-5",
	})
	modelCallCtx := integration.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{
		Settings: settings, CallID: "lm-1", ModelProvider: "openai", ModelID: "gpt-5",
	})

	wantErr := errors.New("provider boom")
	integration.OnStepError(modelCallCtx, TelemetryErrorEvent{Settings: settings, CallID: "lm-1", Error: wantErr})

	chatSpan := findSpan(rec, "chat gpt-5")
	if chatSpan == nil {
		t.Fatal("expected a 'chat gpt-5' model-call span")
	}
	if !isEnded(chatSpan) {
		t.Fatal("expected the chat span to have been ended by OnStepError")
	}
	if got := chatSpan.Status().Code; got != codes.Error {
		t.Fatalf("chat span status = %v, want codes.Error", got)
	}

	// GenAI names its step span "step <N>" (1-indexed — see OnStepStart's
	// `"step " + itoa(e.StepNumber+1)`); StepNumber defaults to 0 above, so
	// this is step 1.
	genAIStepSpan := findSpan(rec, "step 1")
	if genAIStepSpan == nil {
		t.Fatal("expected a 'step 1' GenAI step span")
	}
	if !isEnded(genAIStepSpan) {
		t.Fatal("expected the GenAI step span to have been ended by OnStepError")
	}
}

// isEnded reports whether span has a non-zero EndTime (i.e. End() was
// called on it) — works for both the tracetest.SpanRecorder's still-live
// ReadWriteSpan view (Started()) and its finalized ReadOnlySpan view
// (Ended()), since ReadWriteSpan embeds ReadOnlySpan.
func isEnded(span sdktrace.ReadOnlySpan) bool {
	if span == nil {
		return false
	}
	return !span.EndTime().IsZero()
}
