package agent

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR92integAgentToolSpanArgsKeepModelKeyOrder (cr-r92-carry-to-gen): the
// ToolLoopAgent tool span's ai.toolCall.args keeps the model's key order
// (TS toolCall.input), from RawArguments.
func TestR92integAgentToolSpanArgsKeepModelKeyOrder(t *testing.T) {
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
	tel := &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
	}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Telemetry: tel, Tools: []types.Tool{{Name: "w", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }}}})
	if _, err := a.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Ended() {
		if s.Name() != "ai.toolCall" {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.toolCall.args" {
				if got := kv.Value.AsString(); got != `{"zeta":1,"alpha":2}` {
					t.Fatalf("ai.toolCall.args = %s, want the model's key order", got)
				}
				return
			}
		}
	}
	t.Fatal("no ai.toolCall span with ai.toolCall.args")
}
