package telemetry

import (
	"context"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS onToolExecutionStart/End return when the integration has no call state
// or tool span for the event's callId. Go fell back to the span in ctx, which
// may belong to the caller or to another integration: it nested a tool span
// under it and then ended that span.
func TestToolExecutionWithoutOwnStateLeavesCtxSpanAlone(t *testing.T) {
	for _, tc := range []struct {
		name        string
		integration TelemetryIntegration
	}{
		{"legacy", NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{})},
		{"genai", NewOpenTelemetry(OpenTelemetryOptions{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			ctx, other := tp.Tracer("caller").Start(context.Background(), "caller span")
			defer other.End()

			e := TelemetryToolCallStartEvent{CallID: "call-no-state-" + tc.name, ToolCallID: "t1", ToolName: "tool"}
			toolCtx := tc.integration.OnToolExecutionStart(ctx, e)
			tc.integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{CallID: e.CallID, ToolCallID: "t1", ToolName: "tool", Result: "ok"})

			if n := len(rec.Started()); n != 1 {
				t.Fatalf("started %d spans, want only the caller's", n)
			}
			if n := len(rec.Ended()); n != 0 {
				t.Fatalf("the caller's span was ended by the integration (%d ended)", n)
			}
			for name, states := range map[string]*sync.Map{"GenAI": &genAICallStates, "legacy": &legacyCallStates} {
				states.Range(func(key, _ interface{}) bool {
					if strings.HasSuffix(key.(string), e.CallID) {
						t.Errorf("tool events created %s call state %q", name, key)
					}
					return true
				})
			}
		})
	}
}
