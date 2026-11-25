package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r75integRecorder records every lifecycle event a per-call integration
// receives, with the event's call ID.
type r75integRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu      sync.Mutex
	events  []string
	callIDs []string
	startOp string
}

func (r *r75integRecorder) add(name, callID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
	r.callIDs = append(r.callIDs, callID)
}

func (r *r75integRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.add("onStart", e.CallID)
	r.mu.Lock()
	if r.startOp == "" {
		r.startOp = e.OperationType
	}
	r.mu.Unlock()
	return ctx
}

func (r *r75integRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.add("onStepStart", e.CallID)
	return ctx
}

func (r *r75integRecorder) OnToolExecutionStart(ctx context.Context, e telemetry.TelemetryToolCallStartEvent) context.Context {
	r.add("onToolExecutionStart", e.CallID)
	return ctx
}

func (r *r75integRecorder) OnToolExecutionEnd(_ context.Context, e telemetry.TelemetryToolCallFinishEvent) {
	r.add("onToolExecutionEnd", e.CallID)
}

func (r *r75integRecorder) OnStepEnd(_ context.Context, e telemetry.TelemetryStepEndEvent) {
	r.add("onStepEnd", e.CallID)
}

func (r *r75integRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.add("onEnd", e.CallID)
}

func (r *r75integRecorder) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.add("onError", e.CallID)
}

func (r *r75integRecorder) OnAbort(_ context.Context, e telemetry.TelemetryAbortEvent) {
	r.add("onAbort", e.CallID)
}

// r75integToolRoundTripModel: step 1 calls tool "t", step 2 answers.
func r75integToolRoundTripModel() *testutil.MockLanguageModel {
	var mu sync.Mutex
	n := 0
	return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if call == 1 {
			tc := types.ToolCall{ID: "call-1", ToolName: "t", Arguments: map[string]interface{}{}}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
}

func r75integTool() types.Tool {
	return types.Tool{Name: "t", Description: "d", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "r", nil
	}}
}

// TS workflow-agent-compat.test.ts "telemetry integrations > should call
// per-call integration listeners for all lifecycle events": one root
// (ai.workflowAgent.stream) per stream run, and every step, tool and end
// event under its call ID. The delegated StreamText opens no root of its own
// (review round 75, carry CR to stream).
func TestR75Integ_WorkflowStreamPerCallIntegrationEvents(t *testing.T) {
	rec := &r75integRecorder{}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model:     r75integToolRoundTripModel(),
		Tools:     []types.Tool{r75integTool()},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := "onStart,onStepStart,onToolExecutionStart,onToolExecutionEnd,onStepEnd,onStepStart,onStepEnd,onEnd"
	if got := strings.Join(rec.events, ","); got != want {
		t.Fatalf("events = %s\nwant     %s", got, want)
	}
	if rec.startOp != workflowRootOperation {
		t.Fatalf("root operation = %q, want %q", rec.startOp, workflowRootOperation)
	}
	for i, id := range rec.callIDs {
		if id == "" || id != rec.callIDs[0] {
			t.Fatalf("event %d (%s) call ID = %q, want the root's %q", i, rec.events[i], id, rec.callIDs[0])
		}
	}
}

// TS LegacyOpenTelemetry shapes the step spans by the root's operation
// (state.operationId): under the ai.workflowAgent.stream root the step span
// is "ai.generateText.doGenerate" (TS stepOperationId ternary), nested
// directly under the workflow root, with no ai.streamText span.
func TestR75Integ_WorkflowStreamLegacyOTelStepSpansUnderRoot(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: r75integToolRoundTripModel(),
		Tools: []types.Tool{r75integTool()},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("a")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", StopSequences: []string{"STOP"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	var rootID string
	for _, s := range rec.Ended() {
		if s.Name() == workflowRootOperation {
			rootID = s.SpanContext().SpanID().String()
		}
	}
	if rootID == "" {
		t.Fatal("no ai.workflowAgent.stream root span")
	}
	steps := 0
	for _, s := range rec.Ended() {
		switch s.Name() {
		case "ai.streamText", "ai.streamText.doStream":
			t.Fatalf("span %q recorded, want only the workflow root and its doGenerate steps", s.Name())
		case "ai.generateText.doGenerate":
			steps++
			if got := s.Parent().SpanID().String(); got != rootID {
				t.Fatalf("step span parent = %s, want the workflow root %s", got, rootID)
			}
			hasStop := false
			for _, kv := range s.Attributes() {
				if kv.Key == "ai.response.msToFirstChunk" {
					t.Fatalf("step span has %s, which TS records for ai.streamText roots only", kv.Key)
				}
				if kv.Key == "gen_ai.request.stop_sequences" {
					hasStop = true
				}
			}
			if !hasStop {
				t.Fatal("step span has no gen_ai.request.stop_sequences (TS records state.settings.stopSequences)")
			}
		}
	}
	if steps != 2 {
		t.Fatalf("doGenerate step spans = %d, want 2", steps)
	}
}
