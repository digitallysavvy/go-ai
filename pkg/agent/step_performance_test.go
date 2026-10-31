package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS generateText fills StepResult.performance: toolExecutionMs by tool call
// id, and a stepTimeMs that covers the tool execution.
func TestToolLoopStepPerformanceCoversToolExecution(t *testing.T) {
	tool := defaultStopTestTool()
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		time.Sleep(40 * time.Millisecond)
		return "result", nil
	}
	a := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{responses: toolLoopResponses(2, 2)}, Tools: []types.Tool{tool}})
	result, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d", len(result.Steps))
	}
	first := result.Steps[0].Performance
	if ms, ok := first.ToolExecutionMs["call-1"]; !ok || ms < 30 {
		t.Fatalf("toolExecutionMs = %v, want call-1 >= 30", first.ToolExecutionMs)
	}
	if first.StepTimeMs < first.ToolExecutionMs["call-1"] || first.StepTimeMs < first.ResponseTimeMs {
		t.Fatalf("stepTimeMs %d must cover tool %d and response %d ms", first.StepTimeMs, first.ToolExecutionMs["call-1"], first.ResponseTimeMs)
	}
	last := result.Steps[1].Performance
	if last.ToolExecutionMs == nil {
		t.Fatal("a step without tools still has an empty toolExecutionMs map")
	}
	if last.StepTimeMs < last.ResponseTimeMs {
		t.Fatalf("last step stepTimeMs %d < responseTimeMs %d", last.StepTimeMs, last.ResponseTimeMs)
	}
}
