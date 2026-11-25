package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A tool with no result is TS `undefined`: JSON.stringify leaves it undefined
// and selectAttributes drops it, so neither integration records a result.
func TestToolWithoutResultRecordsNoResultAttribute(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "nil-result"})
	ctx = integration.OnToolExecutionStart(ctx, TelemetryToolCallStartEvent{Settings: settings, CallID: "nil-result", ToolCallID: "t1", ToolName: "ping"})
	integration.OnToolExecutionEnd(ctx, TelemetryToolCallFinishEvent{Settings: settings, CallID: "nil-result", ToolCallID: "t1", ToolName: "ping"})
	span := findSpan(rec, "execute_tool ping")
	if span == nil {
		t.Fatal("no execute_tool span")
	}
	if v, ok := attrValue(span, "gen_ai.tool.call.result"); ok {
		t.Errorf("gen_ai.tool.call.result = %v, want none", v)
	}
}
