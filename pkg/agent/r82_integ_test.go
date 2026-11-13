package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// GenerateAgent runs the agent's own loop (startStepTelemetry), not
// ai.GenerateText: its step span records the prepared tool choice, auto when
// none is set (TS generate-text.ts stepToolChoice; mutation survivor of gen
// fix 2 in round 82).
func TestR82integGenerateAgentStepToolChoiceAuto(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	a := NewToolLoopAgent(AgentConfig{
		Model: r72agentTextModel("Hello!"),
		Telemetry: &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Ended() {
		if s.Name() != "ai.generateText.doGenerate" {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.prompt.toolChoice" {
				if got := kv.Value.AsString(); got != `{"type":"auto"}` {
					t.Fatalf("ai.prompt.toolChoice = %s", got)
				}
				return
			}
		}
	}
	t.Fatal(`ai.prompt.toolChoice missing on the step span, want {"type":"auto"}`)
}
