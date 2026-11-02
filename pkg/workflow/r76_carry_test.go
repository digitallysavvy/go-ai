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

func r76carryAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit(), true
		}
	}
	return "", false
}

// r76carryRoundTripModel is r75integToolRoundTripModel with a DoGenerate
// twin: a tool call to "t", then "done".
func r76carryRoundTripModel() *testutil.MockLanguageModel {
	m := r75integToolRoundTripModel()
	var mu sync.Mutex
	n := 0
	m.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}
	return m
}

// R76 item 3: TS OpenTelemetry (packages/otel open-telemetry.ts) under the
// ai.workflowAgent.stream root. onStart routes the root to onGenerateStart
// (one INTERNAL root named after mapOperationName, which has no entry for
// the workflow operation), onStepStart opens "step n" spans under the root,
// onLanguageModelCallStart opens "chat <model>" under the step with the
// root's state.settings (stop sequences included), and onToolExecutionStart
// opens "execute_tool <name>" under the open step span. The delegated
// StreamText opens no invoke_agent root of its own.
func TestR76Carry_WorkflowStreamGenAIOTelSpansUnderRoot(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) { r76carryGenAISpans(t, mode) })
	}
}

func r76carryGenAISpans(t *testing.T, mode string) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: r76carryRoundTripModel(),
		Tools: []types.Tool{r75integTool()},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("g")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "stream" {
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", StopSequences: []string{"STOP"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
	} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi", StopSequences: []string{"STOP"}}); err != nil {
		t.Fatal(err)
	}
	ended := rec.Ended()
	byID := map[string]sdktrace.ReadOnlySpan{}
	var roots []sdktrace.ReadOnlySpan
	for _, s := range ended {
		byID[s.SpanContext().SpanID().String()] = s
		if !s.Parent().IsValid() {
			roots = append(roots, s)
		}
	}
	if len(roots) != 1 {
		names := []string{}
		for _, s := range roots {
			names = append(names, s.Name())
		}
		t.Fatalf("root spans = %v, want one workflow root", names)
	}
	root := roots[0]
	if root.Name() != workflowRootOperation+" mock-model" {
		t.Errorf("root span name = %q, want %q", root.Name(), workflowRootOperation+" mock-model")
	}
	if op, _ := r76carryAttr(root, "gen_ai.operation.name"); op != workflowRootOperation {
		t.Errorf("root gen_ai.operation.name = %q, want %q (TS mapOperationName identity)", op, workflowRootOperation)
	}
	parentName := func(s sdktrace.ReadOnlySpan) string {
		if p, ok := byID[s.Parent().SpanID().String()]; ok {
			return p.Name()
		}
		return ""
	}
	counts := map[string]int{}
	for _, s := range ended {
		switch s.Name() {
		case "step 1", "step 2":
			counts["step"]++
			if s.Parent().SpanID() != root.SpanContext().SpanID() {
				t.Errorf("%s parent = %q, want the workflow root", s.Name(), parentName(s))
			}
			if op, _ := r76carryAttr(s, "gen_ai.operation.name"); op != "agent_step" {
				t.Errorf("%s gen_ai.operation.name = %q, want agent_step", s.Name(), op)
			}
		case "chat mock-model":
			counts["chat"]++
			if pn := parentName(s); pn != "step 1" && pn != "step 2" {
				t.Errorf("chat span parent = %q, want a step span", pn)
			}
			if v, ok := r76carryAttr(s, "gen_ai.request.stop_sequences"); !ok || v != `["STOP"]` {
				t.Errorf("chat span stop_sequences = %q (%v), want [STOP] from the root's settings", v, ok)
			}
		case "execute_tool t":
			counts["tool"]++
			if pn := parentName(s); pn != "step 1" {
				t.Errorf("tool span parent = %q, want step 1", pn)
			}
		default:
			if s.SpanContext().SpanID() != root.SpanContext().SpanID() {
				t.Errorf("unexpected span %q", s.Name())
			}
		}
	}
	if counts["step"] != 2 || counts["chat"] != 2 || counts["tool"] != 1 {
		t.Errorf("span counts = %v, want 2 steps, 2 chats, 1 tool", counts)
	}
}
