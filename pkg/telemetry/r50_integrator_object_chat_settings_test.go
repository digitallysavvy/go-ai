package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS onObjectStepStart reads the chat span's request settings from the
// object root (state.settings), like the text chat span.
func TestR50IntegratorObjectChatSpanUsesRootSettings(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test")})
	settings := &Settings{IsEnabled: Bool(true)}
	temp, stepTemp := 0.3, 0.9
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateObject", Settings: settings, CallID: "obj-1", Temperature: &temp,
	})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{
		Settings: settings, CallID: "obj-1", ModelProvider: "openai", ModelID: "gpt", Temperature: &stepTemp,
	})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "obj-1", FinishReason: "stop"})
	span := findSpan(rec, "chat gpt")
	if span == nil {
		t.Fatal("expected a 'chat gpt' span")
	}
	if v, ok := attrValue(span, "gen_ai.request.temperature"); !ok || v.(float64) != 0.3 {
		t.Fatalf("gen_ai.request.temperature = %v (ok=%v), want the root's 0.3", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.output.type"); !ok || v.(string) != "json" {
		t.Fatalf("gen_ai.output.type = %v (ok=%v), want json", v, ok)
	}
}
