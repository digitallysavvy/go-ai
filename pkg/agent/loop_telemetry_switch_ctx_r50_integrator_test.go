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

type r50Key string

// switchedRootCtx takes from the root only the values its integrations added
// or changed relative to the base; every other value, the deadline and the
// cancellation come from the loop's context.
func TestR50IntegratorSwitchedRootCtxValues(t *testing.T) {
	base := context.WithValue(context.Background(), r50Key("shared"), "base")
	loop, cancel := context.WithTimeout(context.WithValue(base, r50Key("shared"), "loop"), time.Hour)
	defer cancel()
	loop = context.WithValue(loop, r50Key("loopOnly"), "loop")
	root := context.WithValue(base, r50Key("span"), "root")
	c := switchedRootCtx{Context: loop, root: root, base: base}
	for key, want := range map[r50Key]interface{}{
		"shared":   "loop", // the root kept the base's value: the loop's wins
		"loopOnly": "loop",
		"span":     "root", // added by the root's integrations
		"missing":  nil,
	} {
		if got := c.Value(key); got != want {
			t.Errorf("Value(%q) = %v, want %v", key, got, want)
		}
	}
	if _, ok := c.Deadline(); !ok {
		t.Error("no deadline: the loop's timeout was lost")
	}
}

// Switching back to an integration set used earlier reuses its root (the
// sibling-root branch) and also keeps the loop's context.
func TestR50IntegratorLoopTelemetrySwitchBackKeepsLoopContext(t *testing.T) {
	newInt := func() telemetry.TelemetryIntegration {
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tracetest.NewSpanRecorder()))
		t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
		return telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	}
	intA, intB := newInt(), newInt()
	total := 5 * time.Second
	var mu sync.Mutex
	deadline := map[string]bool{}
	runIDs := map[string]string{}
	toolCall := func(id string) types.GenerateResult {
		return types.GenerateResult{
			ToolCalls:    []types.ToolCall{{ID: id, ToolName: "lookup", Arguments: map[string]interface{}{"q": id}}},
			FinishReason: types.FinishReasonToolCalls,
		}
	}
	a := NewToolLoopAgent(AgentConfig{
		Model:     &mockLanguageModel{responses: []types.GenerateResult{toolCall("c0"), toolCall("c1"), toolCall("c2"), {Text: "done", FinishReason: types.FinishReasonStop}}},
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA}},
		Timeout:   &ai.TimeoutConfig{Total: &total},
		MaxSteps:  4,
		Tools: []types.Tool{{Name: "lookup", Execute: func(ctx context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			_, ok := ctx.Deadline()
			id, _ := in["q"].(string)
			mu.Lock()
			deadline[id] = ok
			runIDs[id], _ = ctx.Value(runIDKey).(string)
			mu.Unlock()
			return "found", nil
		}}},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			switch c.StepNumber {
			case 1:
				c.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intB}}
			case 2:
				c.Telemetry = &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{intA}}
			}
			return c
		},
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, id := range []string{"c0", "c1", "c2"} {
		if !deadline[id] {
			t.Errorf("tool %s has no deadline", id)
		}
		if runIDs[id] == "" || runIDs[id] != runIDs["c0"] {
			t.Errorf("tool %s run id = %q, want the loop's %q", id, runIDs[id], runIDs["c0"])
		}
	}
}
