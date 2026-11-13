package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func r49Recorder(t *testing.T) (*tracetest.SpanRecorder, telemetry.TelemetryIntegration) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
}

func r49ToolCallStep(id string) types.GenerateResult {
	return types.GenerateResult{
		ToolCalls:    []types.ToolCall{{ID: id, ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
		FinishReason: types.FinishReasonToolCalls,
	}
}

var r49LookupTool = types.Tool{Name: "lookup", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
	return "found", nil
}}

// A root opened for a new integration set is a sibling of the loop's root
// (both start from the loop's base context), not nested under it, and it has
// a call id of its own: an integration in both sets keeps two separate roots
// and ends both.
func TestR49LoopTelemetrySwitchedRootIsSiblingWithOwnCallID(t *testing.T) {
	recA, intA := r49Recorder(t)
	recB, intB := r49Recorder(t)
	a := NewToolLoopAgent(AgentConfig{
		Model:     &mockLanguageModel{responses: []types.GenerateResult{r49ToolCallStep("c1"), {Text: "done", FinishReason: types.FinishReasonStop}}},
		Tools:     []types.Tool{r49LookupTool},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA}},
		MaxSteps:  5,
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			if c.StepNumber == 1 {
				c.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA, intB}}
			}
			return c
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	roots := func(rec *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
		if started, ended := len(rec.Started()), len(rec.Ended()); started != ended {
			t.Fatalf("%d spans started, %d ended", started, ended)
		}
		var out []sdktrace.ReadOnlySpan
		for _, s := range rec.Ended() {
			if s.Name() == "ai.generateText" {
				out = append(out, s)
			}
		}
		return out
	}
	rootsA, rootsB := roots(recA), roots(recB)
	if len(rootsA) != 2 || len(rootsB) != 1 {
		t.Fatalf("roots: A=%d B=%d, want 2 and 1", len(rootsA), len(rootsB))
	}
	for _, r := range append(rootsA, rootsB...) {
		if r.Parent().IsValid() {
			t.Fatalf("root %v is nested under %v, want a sibling", r.SpanContext().SpanID(), r.Parent().SpanID())
		}
	}
}

// The legacy loop's step-end event carries the step's reasoning parts, so
// ai.response.reasoning is recorded for a reasoning part with empty text.
func TestR49LoopStepEndCarriesEmptyReasoningParts(t *testing.T) {
	rec, integration := r49Recorder(t)
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{{
			Text:         "done",
			Content:      []types.ContentPart{types.ReasoningContent{Text: ""}, types.TextContent{Text: "done"}},
			FinishReason: types.FinishReasonStop,
		}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{integration}},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range rec.Ended() {
		if s.Name() != "ai.generateText.doGenerate" {
			continue
		}
		found = true
		got, ok := "", false
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.response.reasoning" {
				got, ok = kv.Value.AsString(), true
			}
		}
		if !ok || got != "" {
			t.Errorf("step ai.response.reasoning = %q (ok=%v), want \"\"", got, ok)
		}
	}
	if !found {
		t.Fatal("no step span")
	}
}
