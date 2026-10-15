package telemetry

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS legacy onStepEnd/onGenerateEnd: ai.response.text is recorded even when
// empty, ai.response.timestamp is toISOString (milliseconds), and
// gen_ai.response.model is set next to ai.response.model.
func TestLegacyOpenTelemetry_StepEndTSShapes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy-r21")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "c1", OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m", Settings: settings,
	})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{
		CallID: "c1", Settings: settings, OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m",
	})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		CallID: "c1", Settings: settings, OperationType: "ai.generateText", FinishReason: "tool-calls",
		ResponseID: "r", ResponseModelID: "resp-model",
		ResponseTimestamp: time.Date(1970, 1, 1, 0, 0, 0, 123_000_000, time.UTC),
	})
	integration.OnEnd(ctx, TelemetryFinishEvent{CallID: "c1", Settings: settings, OperationType: "ai.generateText", FinishReason: "tool-calls"})

	step := findSpan(rec, "ai.generateText.doGenerate")
	if v, ok := attrValue(step, "ai.response.text"); !ok || v.(string) != "" {
		t.Errorf("step ai.response.text = %v ok=%v, want empty string", v, ok)
	}
	if v, _ := attrValue(step, "ai.response.timestamp"); v != "1970-01-01T00:00:00.123Z" {
		t.Errorf("timestamp = %v", v)
	}
	if v, _ := attrValue(step, "gen_ai.response.model"); v != "resp-model" {
		t.Errorf("gen_ai.response.model = %v", v)
	}
	root := findSpan(rec, "ai.generateText")
	if v, ok := attrValue(root, "ai.response.text"); !ok || v.(string) != "" {
		t.Errorf("root ai.response.text = %v ok=%v", v, ok)
	}
}
