package harness

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type endCaptureIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu   sync.Mutex
	ends []telemetry.TelemetryFinishEvent
}

func (c *endCaptureIntegration) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	c.mu.Lock()
	c.ends = append(c.ends, e)
	c.mu.Unlock()
}

// TS turn-telemetry's end event carries `toolResults` of every step (and the
// final step's reasoning), which the GenAI integration turns into the
// tool_call_response parts of the root span's gen_ai.output.messages.
func TestAgent_TelemetryEndCarriesToolResults(t *testing.T) {
	rec := &endCaptureIntegration{}
	tool := types.Tool{
		Name: "t1", Description: "d", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		},
	}
	tools := map[string]types.Tool{"t1": tool}
	session := &slowToolTurnSession{toolName: "t1"}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), Model: "harness-model",
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{rec},
		},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ends) != 1 {
		t.Fatalf("ends = %d, want 1", len(rec.ends))
	}
	e := rec.ends[0]
	if len(e.ToolCalls) != 1 || len(e.ToolResults) != 1 || e.ToolResults[0].ToolCallID != e.ToolCalls[0].ID {
		t.Fatalf("end event toolCalls=%v toolResults=%v, want one result answering the call", e.ToolCalls, e.ToolResults)
	}
}
