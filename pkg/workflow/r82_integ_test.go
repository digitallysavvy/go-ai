package workflow

import (
	"context"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r82integGenerateRoundTripModel answers the first DoGenerate with a tool
// call and the second with text.
func r82integGenerateRoundTripModel() *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
}

// TS workflow/src/model-call-iterator.ts dispatches onStepStart without
// promptMessages, stepTools or stepToolChoice, so LegacyOpenTelemetry records
// no ai.prompt.messages, ai.prompt.tools or ai.prompt.toolChoice on a
// WorkflowAgent step span, in generate and stream mode (carry CR r82 item 2).
func TestR82integWorkflowStepSpansOmitStepPrompt(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			model := r75integToolRoundTripModel()
			if mode == "generate" {
				model = r82integGenerateRoundTripModel()
			}
			w, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: []types.Tool{r75integTool()}})
			if err != nil {
				t.Fatal(err)
			}
			tel := &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
				telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("w")}),
			}}
			if mode == "generate" {
				if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", Telemetry: tel}); err != nil {
					t.Fatal(err)
				}
			} else {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", Telemetry: tel})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := res.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			steps := 0
			for _, s := range rec.Ended() {
				if s.Name() != "ai.generateText.doGenerate" {
					continue
				}
				steps++
				for _, kv := range s.Attributes() {
					switch kv.Key {
					case "ai.prompt.messages", "ai.prompt.tools", "ai.prompt.toolChoice":
						t.Errorf("step span %d has %s = %s", steps, kv.Key, kv.Value.String())
					}
				}
			}
			if steps != 2 {
				t.Fatalf("step spans = %d, want 2", steps)
			}
		})
	}
}
