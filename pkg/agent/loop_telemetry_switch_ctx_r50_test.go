package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// A PrepareCall override that switches the legacy loop to another integration
// set opens that set's root from the loop's base context, but the steps after
// the switch keep the loop's own context: its total timeout, its run id and
// its telemetry state (so tool-call spans still reach the switched set, under
// the new root's call id).
func TestR50LoopTelemetrySwitchKeepsLoopContext(t *testing.T) {
	newRec := func() (*tracetest.SpanRecorder, telemetry.TelemetryIntegration) {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
		return rec, telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	}
	_, intA := newRec()
	recB, intB := newRec()
	total := 200 * time.Millisecond
	var mu sync.Mutex
	var toolDeadline bool
	var runID string
	toolCall := func(id string) types.GenerateResult {
		return types.GenerateResult{
			ToolCalls:    []types.ToolCall{{ID: id, ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
			FinishReason: types.FinishReasonToolCalls,
		}
	}
	a := NewToolLoopAgent(AgentConfig{
		Model: &mockLanguageModel{responses: []types.GenerateResult{
			{Text: "first", FinishReason: types.FinishReasonStop, ToolCalls: nil},
		}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA}},
		Timeout:   &ai.TimeoutConfig{Total: &total},
		MaxSteps:  3,
	})
	a.config.Model = &mockLanguageModel{responses: []types.GenerateResult{toolCall("c1"), toolCall("c2"), {Text: "done", FinishReason: types.FinishReasonStop}}}
	a.config.Tools = []types.Tool{{Name: "lookup", Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		_, ok := ctx.Deadline()
		mu.Lock()
		toolDeadline = ok
		if id, _ := ctx.Value(runIDKey).(string); id != "" {
			runID = id
		}
		mu.Unlock()
		return "found", nil
	}}}
	a.config.PrepareCall = func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
		if c.StepNumber >= 1 {
			c.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intB}}
		}
		return c
	}
	var startIDs []string
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go", OnToolCallStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
		mu.Lock()
		startIDs = append(startIDs, e.CallID)
		mu.Unlock()
	}})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	_ = res
	mu.Lock()
	defer mu.Unlock()
	if !toolDeadline {
		t.Fatal("tool executed after the switch has no deadline: the loop's total timeout was lost")
	}
	if runID == "" {
		t.Fatal("tool executed after the switch has no run id")
	}
	if len(startIDs) != 2 || startIDs[1] == "" || startIDs[1] == startIDs[0] {
		t.Fatalf("tool start call ids = %q, want two, the second the switched root's own", startIDs)
	}
	tools := 0
	for _, s := range recB.Ended() {
		if s.Name() == "ai.toolCall" {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("switched integration recorded %d tool-call spans, want 1", tools)
	}
}
