package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// ToolLoopAgent runs TS generateText, whose telemetry step start carries
// stepToolChoice = prepareToolChoice(...): {"type":"auto"} when no tool
// choice is set. The agent passed the unset choice, so ai.prompt.toolChoice
// was missing from its step spans. (carry CR r82.) Stream mode runs
// ai.StreamText, whose half is cr-r82-carry-to-stream.md item 1.
func TestR82genAgentTelemetryStepToolChoiceDefaultsToAuto(t *testing.T) {
	for _, mode := range []string{"generate"} {
		t.Run(mode, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			a := NewToolLoopAgent(AgentConfig{
				Model: r72agentTextModel("Hello!"),
				Telemetry: &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
					telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
				}},
			})
			if _, err := r72agentRun(t, a, mode, AgentGenerateOptions{Prompt: "hi"}); err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, s := range rec.Ended() {
				names = append(names, s.Name())
				if s.Name() != "ai.generateText.doGenerate" && s.Name() != "ai.streamText.doStream" {
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
			t.Fatalf(`ai.prompt.toolChoice missing, want {"type":"auto"}; spans %v`, names)
		})
	}
}
