package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS open-telemetry.ts onStepStart (`'ai.prompt.toolChoice': {input: ...}`)
// and onObjectOperationStart (`'ai.schema': {input: ...}`): both are input
// attributes, so recordInputs: false omits them even with the toolChoice and
// schema options on. ai.schema.name/description and ai.settings.output are
// plain values and stay.
func TestR77Carry_ToolChoiceAndSchemaAreInputGated(t *testing.T) {
	for _, record := range []bool{true, false} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t"), ToolChoice: true, Schema: true})
		settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(record)}

		ctx := integration.OnStart(context.Background(), TelemetryStartEvent{CallID: "r77-text", OperationType: "ai.generateText", ModelID: "m", Settings: settings})
		stepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "r77-text", Settings: settings, ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired}})
		integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{CallID: "r77-text", Settings: settings})
		integration.OnFinish(ctx, TelemetryFinishEvent{CallID: "r77-text", OperationType: "ai.generateText", Settings: settings})

		octx := integration.OnStart(context.Background(), TelemetryStartEvent{
			CallID: "r77-object", OperationType: "ai.generateObject", ModelID: "m", Settings: settings,
			Schema: map[string]interface{}{"type": "object"}, SchemaName: "n", SchemaDescription: "d", SettingsOutput: "object",
		})
		integration.OnFinish(octx, TelemetryFinishEvent{CallID: "r77-object", OperationType: "ai.generateObject", Settings: settings})
		_ = tp.Shutdown(context.Background())

		step := findSpan(rec, "step 1")
		root := findSpan(rec, "invoke_agent m")
		if step == nil || root == nil {
			t.Fatalf("record=%v: missing spans", record)
		}
		if _, ok := attrValue(step, "ai.prompt.toolChoice"); ok != record {
			t.Errorf("record=%v: ai.prompt.toolChoice present = %v", record, ok)
		}
		var object sdktrace.ReadOnlySpan
		for _, s := range rec.Ended() {
			if v, ok := attrValue(s, "gen_ai.output.type"); ok && v == "json" {
				object = s
			}
		}
		if object == nil {
			t.Fatalf("record=%v: no object root span", record)
		}
		if _, ok := attrValue(object, "ai.schema"); ok != record {
			t.Errorf("record=%v: ai.schema present = %v", record, ok)
		}
		for _, k := range []string{"ai.schema.name", "ai.schema.description", "ai.settings.output"} {
			if _, ok := attrValue(object, k); !ok {
				t.Errorf("record=%v: %s missing", record, k)
			}
		}
	}
}

// TS open-telemetry.test.ts "omits malformed finish reason arrays on the chat
// span": an undefined finish reason makes `[undefined]`, which
// sanitizeAttributeValue drops. Go's unset finish reason is "": no
// gen_ai.response.finish_reasons on the chat span, the step span (legacy) or
// the roots, and no legacy ai.response.finishReason.
func TestR77Carry_UnsetFinishReasonIsOmitted(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	settings := &Settings{IsEnabled: Bool(true)}
	for _, integration := range []TelemetryIntegration{
		NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
		NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
	} {
		ctx := integration.OnStart(context.Background(), TelemetryStartEvent{CallID: "r77-fr", OperationType: "ai.generateText", ModelID: "m", Settings: settings})
		stepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "r77-fr", OperationType: "ai.generateText", ModelID: "m", Settings: settings})
		if h, ok := integration.(languageModelCallStartHandler); ok {
			callCtx := h.OnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: "r77-fr", ModelID: "m", Settings: settings})
			integration.(languageModelCallEndHandler).OnLanguageModelCallEnd(callCtx, LanguageModelCallEndEvent{CallID: "r77-fr", ModelID: "m", Settings: settings})
		}
		integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{CallID: "r77-fr", OperationType: "ai.generateText", Settings: settings})
		integration.OnFinish(ctx, TelemetryFinishEvent{CallID: "r77-fr", OperationType: "ai.generateText", Settings: settings})
	}
	_ = tp.Shutdown(context.Background())
	if len(rec.Ended()) != 5 { // GenAI root, step, chat; legacy root, step
		t.Fatalf("ended spans = %d, want 5", len(rec.Ended()))
	}
	for _, s := range rec.Ended() {
		for _, k := range []string{"gen_ai.response.finish_reasons", "ai.response.finishReason"} {
			if v, ok := attrValue(s, k); ok {
				t.Errorf("span %q: %s = %v, want none for an unset finish reason", s.Name(), k, v)
			}
		}
	}
}
