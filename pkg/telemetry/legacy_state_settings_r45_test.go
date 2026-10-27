package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// FireOnStart returns the last integration's ctx. With the GenAI integration
// registered after LegacyOpenTelemetry, Legacy's step span still carries the
// root settings (TS state.settings) and its tool span the runtime context
// (TS state.baseTelemetryAttributes), read from its own call state.
func TestLegacyStepAndToolSpansUseOwnCallStateWithLaterIntegration(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	legacy := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")})
	genAI := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai")})
	settings := &Settings{RecordInputs: Bool(true), RecordOutputs: Bool(true), Integrations: []TelemetryIntegration{legacy, genAI}}
	temperature := 0.25
	callID := "call-r45-legacy-state"
	runtimeCtx := map[string]interface{}{"tenant": "acme"}

	ctx := FireOnStart(context.Background(), TelemetryStartEvent{
		Settings: settings, CallID: callID, OperationType: "ai.generateText",
		ModelProvider: "p", ModelID: "m", Temperature: &temperature, RuntimeContext: runtimeCtx,
	})
	ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{
		Settings: settings, CallID: callID, OperationType: "ai.generateText",
		ModelProvider: "p", ModelID: "m", RuntimeContext: runtimeCtx,
	})
	toolCtx := FireOnToolCallStart(ctx, TelemetryToolCallStartEvent{Settings: settings, CallID: callID, ToolCallID: "t1", ToolName: "tool"})
	FireOnToolCallFinish(toolCtx, TelemetryToolCallFinishEvent{Settings: settings, CallID: callID, ToolCallID: "t1", ToolName: "tool", Result: "ok"})
	FireOnStepEnd(ctx, TelemetryStepEndEvent{Settings: settings, CallID: callID, OperationType: "ai.generateText"})
	FireOnEnd(ctx, TelemetryFinishEvent{Settings: settings, CallID: callID, OperationType: "ai.generateText"})

	attrOf := func(name, key string) (attribute.Value, bool) {
		for _, s := range rec.Ended() {
			if s.Name() != name || s.InstrumentationScope().Name == "genai" {
				continue
			}
			for _, kv := range s.Attributes() {
				if string(kv.Key) == key {
					return kv.Value, true
				}
			}
			return attribute.Value{}, false
		}
		t.Fatalf("no legacy span %q", name)
		return attribute.Value{}, false
	}
	if v, ok := attrOf("ai.generateText.doGenerate", "gen_ai.request.temperature"); !ok || v.AsFloat64() != temperature {
		t.Fatalf("legacy step span gen_ai.request.temperature = %v (%v)", v.String(), ok)
	}
	if v, ok := attrOf("ai.toolCall", "ai.settings.context.tenant"); !ok || v.AsString() != "acme" {
		t.Fatalf("legacy tool span ai.settings.context.tenant = %v (%v)", v.String(), ok)
	}
}
