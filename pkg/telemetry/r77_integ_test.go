package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Round 77 integrator survivor test (carry fix 5): the object operation root
// spans of both integrations record no finish reason when it is unset (TS
// selectAttributes / sanitizeAttributeValue drop undefined).
func TestR77Integ_ObjectRootUnsetFinishReasonIsOmitted(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	settings := &Settings{IsEnabled: Bool(true)}
	for _, integration := range []TelemetryIntegration{
		NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
		NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
	} {
		for _, op := range []string{"ai.generateObject", "ai.streamObject"} {
			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{CallID: "r77integ-" + op, OperationType: op, ModelID: "m", Settings: settings})
			integration.OnFinish(ctx, TelemetryFinishEvent{CallID: "r77integ-" + op, OperationType: op, Settings: settings, Object: map[string]interface{}{"a": 1}})
		}
	}
	_ = tp.Shutdown(context.Background())
	if len(rec.Ended()) != 4 {
		t.Fatalf("ended spans = %d, want 4", len(rec.Ended()))
	}
	for _, s := range rec.Ended() {
		for _, k := range []string{"gen_ai.response.finish_reasons", "ai.response.finishReason"} {
			if v, ok := attrValue(s, k); ok {
				t.Errorf("span %q: %s = %v, want none for an unset finish reason", s.Name(), k, v)
			}
		}
	}
}
