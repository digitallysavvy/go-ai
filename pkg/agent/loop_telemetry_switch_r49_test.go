package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// A PrepareCall override that routes the steps to a different integration
// set gives that set a root span of its own (TS applies prepareCall before
// generateText opens the root, so the integrations that see the steps always
// have one). Switching back reuses the first root. Every root ends.
func TestR49LoopTelemetryOverrideSwitchingIntegrationsHasRoot(t *testing.T) {
	newRec := func() (*tracetest.SpanRecorder, telemetry.TelemetryIntegration) {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
		return rec, telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	}
	recA, intA := newRec()
	recB, intB := newRec()
	setA := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA}}
	setB := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intB}}
	toolCall := func(id string) types.GenerateResult {
		return types.GenerateResult{
			ToolCalls:    []types.ToolCall{{ID: id, ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
			FinishReason: types.FinishReasonToolCalls,
		}
	}
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{
			toolCall("c1"), toolCall("c2"),
			{Text: "done", FinishReason: types.FinishReasonStop},
		}},
		Tools: []types.Tool{{Name: "lookup", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "found", nil
		}}},
		Telemetry: setA,
		MaxSteps:  5,
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			// Step 0 stays on A, step 1 switches to B, step 2 back to A.
			switch c.StepNumber {
			case 1:
				c.Telemetry = &telemetry.Options{Integrations: setB.Integrations}
			case 2:
				c.Telemetry = &telemetry.Options{Integrations: setA.Integrations}
			}
			return c
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	count := func(rec *tracetest.SpanRecorder) (roots, steps int) {
		if started, ended := len(rec.Started()), len(rec.Ended()); started != ended {
			t.Fatalf("%d spans started, %d ended", started, ended)
		}
		for _, s := range rec.Ended() {
			switch s.Name() {
			case "ai.generateText":
				roots++
			case "ai.generateText.doGenerate":
				steps++
				if !s.Parent().IsValid() {
					t.Fatalf("step span %q has no parent", s.Name())
				}
			}
		}
		return roots, steps
	}
	if roots, steps := count(recA); roots != 1 || steps != 2 {
		t.Fatalf("integration A: roots=%d steps=%d, want 1 and 2", roots, steps)
	}
	if roots, steps := count(recB); roots != 1 || steps != 1 {
		t.Fatalf("integration B: roots=%d steps=%d, want 1 and 1", roots, steps)
	}
}

// An override that keeps the same integrations (a fresh settings value over
// copies of the same integration, as a PrepareCall that only changes the
// function id builds) keeps the one root.
func TestR49LoopTelemetryOverrideSameIntegrationsKeepsOneRoot(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{
		Tracer:     tp.Tracer("t"),
		EnrichSpan: func(context.Context, telemetry.EnrichSpanOptions) map[string]interface{} { return nil },
	})
	a := NewToolLoopAgent(AgentConfig{
		Model:     &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{integration}},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			copied := integration
			c.Telemetry = &telemetry.Options{FunctionID: "fn", Integrations: []telemetry.TelemetryIntegration{copied}}
			return c
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	roots := 0
	for _, s := range rec.Ended() {
		if s.Name() == "ai.generateText" {
			roots++
		}
	}
	if roots != 1 || len(rec.Started()) != len(rec.Ended()) {
		t.Fatalf("roots=%d started=%d ended=%d, want one root and every span ended", roots, len(rec.Started()), len(rec.Ended()))
	}
}

func TestR49SameIntegrations(t *testing.T) {
	enrich := func(context.Context, telemetry.EnrichSpanOptions) map[string]interface{} { return nil }
	x := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{EnrichSpan: enrich})
	y := telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{EnrichSpan: enrich})
	list := func(i ...telemetry.TelemetryIntegration) *telemetry.Options {
		return &telemetry.Options{Integrations: i}
	}
	if !sameIntegrations(nil, &telemetry.Options{}) {
		t.Fatal("two global integration sets differ")
	}
	if !sameIntegrations(list(x), list(x)) {
		t.Fatal("copies of one integration differ")
	}
	if sameIntegrations(list(x), list(y)) {
		t.Fatal("two constructed integrations are the same")
	}
	if sameIntegrations(list(x), list(x, y)) || sameIntegrations(list(x), nil) {
		t.Fatal("different lists are the same")
	}
}
