package workflow

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type r92carryEventRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
}

func (r *r92carryEventRecorder) add(e string) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *r92carryEventRecorder) OnStepEnd(context.Context, telemetry.TelemetryStepEndEvent) {
	r.add("onStepEnd")
}
func (r *r92carryEventRecorder) OnEnd(context.Context, telemetry.TelemetryFinishEvent) { r.add("onEnd") }

// TS workflow-agent-telemetry.integration.test.ts (2ebc78b654) "%s: ends
// every step span when stopping on a client-side tool": two steps, the
// last tool askUser, the public onStepEnd callback called twice for
// generate and once for stream, two telemetry step ends followed by onEnd,
// and the span tree invoke_agent > step 1 (chat, execute_tool addNumbers),
// step 2 (chat).
func TestR92carryWorkflowClientToolStopSpanTree(t *testing.T) {
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
			model := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "workflow-test-model", ToolSupport: true,
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
			rec := &r92carryEventRecorder{}
			spans := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			defer func() { _ = tp.Shutdown(context.Background()) }()
			var cbMu sync.Mutex
			stepEndCallbacks := 0
			a := &WorkflowAgent{
				Model: model,
				Tools: []types.Tool{
					{Name: "addNumbers", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						return 5, nil
					}},
					{Name: "askUser"},
				},
				OnStepEnd: func(context.Context, ai.OnStepFinishEvent) { cbMu.Lock(); stepEndCallbacks++; cbMu.Unlock() },
				Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
					rec, telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("w")}),
				}},
			}
			var steps []types.StepResult
			if mode == "generate" {
				res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "Add 2 and 3, then ask to continue."})
				if err != nil {
					t.Fatal(err)
				}
				steps = res.Steps
			} else {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "Add 2 and 3, then ask to continue."})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := st.ReadAll(); err != nil {
					t.Fatal(err)
				}
				steps = st.Steps()
			}
			if len(steps) != 2 {
				t.Fatalf("stepCount = %d, want 2", len(steps))
			}
			last := steps[1].ToolCalls
			if len(last) == 0 || last[len(last)-1].ToolName != "askUser" {
				t.Fatalf("lastTool = %+v", last)
			}
			wantCallbacks := 2
			if mode == "stream" {
				wantCallbacks = 1
			}
			cbMu.Lock()
			gotCallbacks := stepEndCallbacks
			cbMu.Unlock()
			if gotCallbacks != wantCallbacks {
				t.Fatalf("stepEndCallbacks = %d, want %d", gotCallbacks, wantCallbacks)
			}
			rec.mu.Lock()
			events := append([]string(nil), rec.events...)
			rec.mu.Unlock()
			n := 0
			for _, e := range events {
				if e == "onStepEnd" {
					n++
				}
			}
			if n != 2 || len(events) < 2 || !reflect.DeepEqual(events[len(events)-2:], []string{"onStepEnd", "onEnd"}) {
				t.Fatalf("events = %v", events)
			}
			// Span tree: name -> child names, in start order.
			ended := spans.Ended()
			if len(spans.Started()) != len(ended) {
				t.Fatalf("spans started %d, ended %d", len(spans.Started()), len(ended))
			}
			byID := map[string]string{}
			for _, s := range ended {
				byID[s.SpanContext().SpanID().String()] = s.Name()
			}
			children := map[string][]string{}
			for _, s := range spans.Started() {
				parent := byID[s.Parent().SpanID().String()]
				children[parent] = append(children[parent], s.Name())
			}
			if got := children[""]; !reflect.DeepEqual(got, []string{"invoke_agent workflow-test-model"}) {
				t.Fatalf("roots = %v", got)
			}
			if got := children["invoke_agent workflow-test-model"]; !reflect.DeepEqual(got, []string{"step 1", "step 2"}) {
				t.Fatalf("steps = %v", got)
			}
			if got := children["step 1"]; !reflect.DeepEqual(got, []string{"chat workflow-test-model", "execute_tool addNumbers"}) {
				t.Fatalf("step 1 = %v", got)
			}
			if got := children["step 2"]; !reflect.DeepEqual(got, []string{"chat workflow-test-model"}) {
				t.Fatalf("step 2 = %v", got)
			}
		})
	}
}

// The same rule for a step that pauses on a tool approval (TS
// pausedToolCalls includes approvalNeeded calls): stream calls no public
// onStepEnd for it, generate does.
func TestR92carryWorkflowApprovalPauseStepEnd(t *testing.T) {
	for mode, want := range map[string]int{"generate": 1, "stream": 0} {
		t.Run(mode, func(t *testing.T) {
			tc := types.ToolCall{ID: "c1", ToolName: "deleteFile", Arguments: map[string]interface{}{"path": "/x"}}
			model := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true,
				DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
					return &types.GenerateResult{ToolCalls: []types.ToolCall{tc}, FinishReason: types.FinishReasonToolCalls}, nil
				},
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					c := tc
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeToolCall, ToolCall: &c},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				},
			}
			var mu sync.Mutex
			got := 0
			a := &WorkflowAgent{
				Model: model,
				Tools: []types.Tool{{Name: "deleteFile", ToolApproval: true, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "deleted", nil
				}}},
				OnStepEnd: func(context.Context, ai.OnStepFinishEvent) { mu.Lock(); got++; mu.Unlock() },
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
			mu.Lock()
			defer mu.Unlock()
			if got != want {
				t.Fatalf("onStepEnd calls = %d, want %d", got, want)
			}
		})
	}
}
