package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS keeps one callStates map per integration instance, so two instances of
// the same integration class (two exporters) each end their own spans. In Go
// the per-call state was keyed by callId alone: the second instance's root
// span replaced the first's, which leaked and got the other's end data.
func TestTwoInstancesOfAnIntegrationKeepTheirOwnCallState(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(*sdktrace.TracerProvider) TelemetryIntegration
	}{
		{"legacy", func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}},
		{"genai", func(tp *sdktrace.TracerProvider) TelemetryIntegration {
			return NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recA, recB := tracetest.NewSpanRecorder(), tracetest.NewSpanRecorder()
			tpA := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recA))
			tpB := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recB))
			t.Cleanup(func() { _ = tpA.Shutdown(context.Background()); _ = tpB.Shutdown(context.Background()) })
			settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true,
				Integrations: []TelemetryIntegration{tc.make(tpA), tc.make(tpB)}}
			callID := "call-two-instances-" + tc.name
			ctx := FireOnStart(context.Background(), TelemetryStartEvent{CallID: callID, OperationType: "ai.generateText", ModelProvider: "p", ModelID: "m", Settings: settings})
			stepCtx := FireOnStepStart(ctx, TelemetryStepStartEvent{CallID: callID, Settings: settings, OperationType: "ai.generateText", StepNumber: 0, ModelProvider: "p", ModelID: "m"})
			lmCtx := FireOnLanguageModelCallStart(stepCtx, LanguageModelCallStartEvent{CallID: callID, Settings: settings, ModelProvider: "p", ModelID: "m"})
			FireOnLanguageModelCallEnd(lmCtx, LanguageModelCallEndEvent{CallID: callID, Settings: settings, ModelProvider: "p", ModelID: "m", FinishReason: "stop"})
			toolCtx := FireOnToolCallStart(stepCtx, TelemetryToolCallStartEvent{CallID: callID, Settings: settings, ToolCallID: "t1", ToolName: "tool"})
			FireOnToolCallFinish(toolCtx, TelemetryToolCallFinishEvent{CallID: callID, Settings: settings, ToolCallID: "t1", ToolName: "tool", Result: "ok"})
			FireOnStepEnd(stepCtx, TelemetryStepEndEvent{CallID: callID, Settings: settings, OperationType: "ai.generateText", StepNumber: 0, FinishReason: "stop"})
			FireOnEnd(ctx, TelemetryFinishEvent{CallID: callID, Settings: settings, OperationType: "ai.generateText", FinishReason: "stop"})
			for name, rec := range map[string]*tracetest.SpanRecorder{"A": recA, "B": recB} {
				if len(rec.Ended()) == 0 || len(rec.Ended()) != len(rec.Started()) {
					var names []string
					for _, s := range rec.Started() {
						names = append(names, s.Name())
					}
					t.Fatalf("instance %s: started %d spans %v, ended %d", name, len(rec.Started()), names, len(rec.Ended()))
				}
				for _, s := range rec.Ended() {
					if p := s.Parent(); p.IsValid() {
						found := false
						for _, o := range rec.Ended() {
							if o.SpanContext().SpanID() == p.SpanID() {
								found = true
							}
						}
						if !found {
							t.Fatalf("instance %s: span %q has a parent from the other instance", name, s.Name())
						}
					}
				}
			}
		})
	}
}
