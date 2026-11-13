package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// r91carryLoopModel calls the tool on each of the first n calls, then
// answers.
func r91carryLoopModel(n int) (*testutil.MockLanguageModel, func() int) {
	var mu sync.Mutex
	calls := 0
	m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
	m.DoStreamFunc = func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= n {
			tc := types.ToolCall{ID: fmt.Sprintf("call-%d", c), ToolName: "t", Arguments: map[string]interface{}{}}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}
	m.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c <= n {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: fmt.Sprintf("call-%d", c), ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}
	return m, func() int { mu.Lock(); defer mu.Unlock(); return calls }
}

func r91carryTool() types.Tool {
	return types.Tool{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
}

// core1-W01: a struct-literal WorkflowAgent with no StopWhen streams until
// the model stops calling tools (TS workflow-agent.ts 1739-1742: stream()
// has no default stop condition), and generate keeps isStepCount(20).
func TestR91carryLiteralWorkflowAgentStopDefaults(t *testing.T) {
	model, calls := r91carryLoopModel(30)
	a := &WorkflowAgent{Model: model, Tools: []types.Tool{r91carryTool()}}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 31 {
		t.Fatalf("stream model calls = %d, want 31", got)
	}

	gmodel, gcalls := r91carryLoopModel(30)
	g := &WorkflowAgent{Model: gmodel, Tools: []types.Tool{r91carryTool()}}
	if _, err := g.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if got := gcalls(); got != 20 {
		t.Fatalf("generate model calls = %d, want 20", got)
	}
}

type r91carryStepRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu           sync.Mutex
	starts, ends int
}

func (r *r91carryStepRecorder) OnStepStart(ctx context.Context, _ telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	r.starts++
	r.mu.Unlock()
	return ctx
}

func (r *r91carryStepRecorder) OnStepEnd(_ context.Context, _ telemetry.TelemetryStepEndEvent) {
	r.mu.Lock()
	r.ends++
	r.mu.Unlock()
}

// TS efd2905173 (workflow-agent.ts 2977-2985): when a run stops on a
// client-side tool (no execute), the final step's telemetry step end is
// dispatched in both modes, so no step or model-call span is left open;
// the public OnStepEnd callback count is unchanged.
func TestR91carryWorkflowClientToolFinalStepTelemetry(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			next := func() int { mu.Lock(); defer mu.Unlock(); calls++; return calls }
			call := func(n int) types.ToolCall {
				if n == 1 {
					return types.ToolCall{ID: "c1", ToolName: "addNumbers", Arguments: map[string]interface{}{"a": 2, "b": 3}}
				}
				return types.ToolCall{ID: "c2", ToolName: "askUser", Arguments: map[string]interface{}{"question": "Continue?"}}
			}
			model := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true,
				DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
					return &types.GenerateResult{ToolCalls: []types.ToolCall{call(next())}, FinishReason: types.FinishReasonToolCalls}, nil
				},
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					tc := call(next())
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				},
			}
			rec := &r91carryStepRecorder{}
			spans := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			defer func() { _ = tp.Shutdown(context.Background()) }()
			a := &WorkflowAgent{
				Model: model,
				Tools: []types.Tool{
					{Name: "addNumbers", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						return 5, nil
					}},
					{Name: "askUser"},
				},
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
					rec, telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("w")}),
				}},
			}
			if mode == "generate" {
				if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "go"}); err != nil {
					t.Fatal(err)
				}
			} else {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "go"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			if n := next() - 1; n != 2 {
				t.Fatalf("model calls = %d, want 2 (stop on the client tool)", n)
			}
			rec.mu.Lock()
			starts, ends := rec.starts, rec.ends
			rec.mu.Unlock()
			if starts != 2 || ends != 2 {
				t.Fatalf("telemetry step starts/ends = %d/%d, want 2/2", starts, ends)
			}
			if s, e := len(spans.Started()), len(spans.Ended()); s != e {
				t.Fatalf("spans started %d, ended %d", s, e)
			}
		})
	}
}
