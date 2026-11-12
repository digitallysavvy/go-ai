package telemetry

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS legacy telemetry ai.response.timestamp is toISOString: a year past 9999
// is written in the expanded form.
func TestR94carryResponseTimestampExpandedYear(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy-r94")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "c1", OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m", Settings: settings,
	})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{
		CallID: "c1", Settings: settings, OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m",
	})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		CallID: "c1", Settings: settings, OperationType: "ai.generateText", FinishReason: "stop",
		ResponseTimestamp: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	integration.OnEnd(ctx, TelemetryFinishEvent{CallID: "c1", Settings: settings, OperationType: "ai.generateText", FinishReason: "stop"})
	if v, _ := attrValue(findSpan(rec, "ai.generateText.doGenerate"), "ai.response.timestamp"); v != "+010000-01-01T00:00:00.000Z" {
		t.Errorf("timestamp = %v", v)
	}
}
