package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// A caller that opened its own root under callID (WorkflowAgent, through
// WithTelemetryCallID) and a PrepareCall telemetry override that opens a late
// loop root must not share call state: the late root gets its own call id, so
// the caller's end event still finds and ends the caller's root.
func TestLateLoopRootDoesNotTakeOverCallerRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(*sdktrace.TracerProvider) telemetry.TelemetryIntegration
	}{
		{"legacy", func(tp *sdktrace.TracerProvider) telemetry.TelemetryIntegration {
			return telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}},
		{"genai", func(tp *sdktrace.TracerProvider) telemetry.TelemetryIntegration {
			return telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			integration := tc.make(tp)
			outer := &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integration}}
			override := &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integration}}

			a := NewToolLoopAgent(AgentConfig{
				Model:     &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop}}},
				Telemetry: outer,
				PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
					c.Telemetry = override
					return c
				},
			})
			const callID = "call-outer-r47"
			ctx := telemetry.FireOnStart(context.Background(), telemetry.TelemetryStartEvent{CallID: callID, OperationType: "ai.workflowAgent.generate", Settings: outer})
			if _, err := a.GenerateAgent(WithTelemetryCallID(ctx, callID), AgentGenerateOptions{Prompt: "go"}); err != nil {
				t.Fatal(err)
			}
			telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{CallID: callID, OperationType: "ai.workflowAgent.generate", Settings: outer, FinishReason: "stop"})

			if started, ended := len(rec.Started()), len(rec.Ended()); started != ended {
				var open []string
				done := map[string]bool{}
				for _, s := range rec.Ended() {
					done[s.SpanContext().SpanID().String()] = true
				}
				for _, s := range rec.Started() {
					if !done[s.SpanContext().SpanID().String()] {
						open = append(open, s.Name())
					}
				}
				t.Fatalf("%d spans started, %d ended; still open: %q", started, ended, open)
			}
		})
	}
}
