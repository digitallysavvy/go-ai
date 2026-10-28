package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS LegacyOpenTelemetry.onStart routes every operation the embed, rerank,
// object and audio handlers do not take (the WorkflowAgent roots included)
// to onGenerateStart, which records ai.settings.stopSequences; the object
// handler does not.
func TestR52LegacyStopSequencesForGenerateStartOperations(t *testing.T) {
	for _, tc := range []struct {
		op   string
		want bool
	}{
		{"ai.generateText", true},
		{"ai.workflowAgent.generate", true},
		{"ai.workflowAgent.stream", true},
		{"ai.generateObject", false},
	} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		legacy := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		settings := &Settings{Integrations: []TelemetryIntegration{legacy}}
		callID := "call-r52-stop-" + tc.op
		ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: settings, CallID: callID, OperationType: tc.op, ModelProvider: "p", ModelID: "m", StopSequences: []string{"END"}})
		FireOnEnd(ctx, TelemetryFinishEvent{Settings: settings, CallID: callID, OperationType: tc.op})
		got := false
		for _, s := range rec.Ended() {
			for _, kv := range s.Attributes() {
				if kv.Key == "ai.settings.stopSequences" {
					got = true
				}
			}
		}
		if got != tc.want {
			t.Errorf("%s: ai.settings.stopSequences recorded = %v, want %v", tc.op, got, tc.want)
		}
		_ = tp.Shutdown(context.Background())
	}
}
