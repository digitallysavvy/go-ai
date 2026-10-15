package agent

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TS #22480: a bare ToolLoopAgent.GenerateAgent dispatches root, model-call
// and tool-call telemetry to registered integrations (it emitted none).
func TestGenerateAgentDispatchesTelemetry(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t1", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})},
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	for _, s := range rec.Ended() {
		names[s.Name()]++
	}
	want := map[string]int{"ai.generateText": 1, "ai.generateText.doGenerate": 2, "ai.toolCall": 1}
	for n, c := range want {
		if names[n] != c {
			t.Errorf("span %q count = %d, want %d (all: %v)", n, names[n], c, names)
		}
	}
}

func TestGenerateAgentToolCallConcurrencyBoundsLegacyLoop(t *testing.T) {
	run := func(limit *int) bool {
		overlapped := false
		active := 0
		var mu = make(chan struct{}, 1)
		mu <- struct{}{}
		both := make(chan struct{})
		mk := func(name string) types.Tool {
			return types.Tool{Name: name, Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					<-mu
					active++
					if active == 2 {
						overlapped = true
						close(both)
					}
					mu <- struct{}{}
					select {
					case <-both:
					case <-timeAfterShort():
					}
					<-mu
					active--
					mu <- struct{}{}
					return name, nil
				}}
		}
		model := &mockLanguageModel{responses: []types.GenerateResult{{
			FinishReason: types.FinishReasonToolCalls,
			ToolCalls: []types.ToolCall{
				{ID: "1", ToolName: "first", Arguments: map[string]interface{}{}},
				{ID: "2", ToolName: "second", Arguments: map[string]interface{}{}},
			},
		}, {Text: "done", FinishReason: types.FinishReasonStop}}}
		a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{mk("first"), mk("second")}, ToolCallConcurrency: limit})
		res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.ToolResults) != 2 || res.ToolResults[0].ToolName != "first" || res.ToolResults[1].ToolName != "second" {
			t.Fatalf("tool results out of call order: %+v", res.ToolResults)
		}
		return overlapped
	}
	one, two := 1, 2
	if run(&one) {
		t.Fatal("limit 1 must run tools sequentially")
	}
	if !run(nil) {
		t.Fatal("unset limit must run tools concurrently (TS parity)")
	}
	if !run(&two) {
		t.Fatal("limit 2 should run both tool calls concurrently")
	}
	zero := 0
	a := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{}, ToolCallConcurrency: &zero})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err == nil {
		t.Fatal("ToolCallConcurrency 0 should be rejected")
	}
}

func timeAfterShort() <-chan time.Time { return time.After(300 * time.Millisecond) }

type stepEndToolResultsRecorder struct {
	telemetry.NoopTelemetryIntegration
	steps []telemetry.TelemetryStepEndEvent
}

func (r *stepEndToolResultsRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.steps = append(r.steps, e)
}

func TestGenerateAgentStepEndTelemetryCarriesToolResults(t *testing.T) {
	rec := &stepEndToolResultsRecorder{}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "t1", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	a := NewToolLoopAgent(AgentConfig{
		Model: model,
		Tools: []types.Tool{{Name: "t1", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "ok", nil
			}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.steps) != 2 {
		t.Fatalf("step-end events = %d, want 2", len(rec.steps))
	}
	if trs := rec.steps[0].ToolResults; len(trs) != 1 || trs[0].ToolCallID != "1" || trs[0].Result != "ok" {
		t.Fatalf("step 0 ToolResults = %+v", trs)
	}
}
