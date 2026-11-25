package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS onEmbedOperationStart records ai.value as JSON.stringify(value) for any
// value, so embedding "" records `""` (the GenAI integration does the same).
func TestLegacyEmbedRootRecordsEmptyValue(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.embed",
		ModelProvider: "openai",
		ModelID:       "m",
		Settings:      &Settings{IsEnabled: Bool(true), RecordInputs: Bool(true)},
	})
	span := findSpan(rec, "ai.embed")
	if span == nil {
		t.Fatal("no ai.embed span")
	}
	if v, ok := attrValue(span, "ai.value"); !ok || v.(string) != `""` {
		t.Errorf("ai.value = %v (ok=%v), want %q", v, ok, `""`)
	}
}
