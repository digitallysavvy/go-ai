package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR92integGenerateTextToolSpanArgsKeepModelKeyOrder (cr-r92-carry-to-gen):
// TS records toolCall.input, the object parsed from the model's text, so
// ai.toolCall.args keeps the model's key order; Go passes RawArguments as
// ArgsText for the order.
func TestR92integGenerateTextToolSpanArgsKeepModelKeyOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	calls := 0
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		calls++
		if calls == 1 {
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{
				ID: "c1", ToolName: "w", RawArguments: `{"zeta":1,"alpha":2}`, Arguments: map[string]interface{}{"zeta": float64(1), "alpha": float64(2)},
			}}}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hi",
		Tools: []types.Tool{{Name: "w", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		}}},
		StopWhen: []StopCondition{StepCountIs(2)},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	span := findSpanByName(rec.Ended(), "ai.toolCall")
	if span == nil {
		t.Fatal("no ai.toolCall span")
	}
	for _, kv := range span.Attributes() {
		if kv.Key == "ai.toolCall.args" {
			if got := kv.Value.AsString(); got != `{"zeta":1,"alpha":2}` {
				t.Fatalf("ai.toolCall.args = %s, want the model's key order", got)
			}
			return
		}
	}
	t.Fatal("no ai.toolCall.args")
}
