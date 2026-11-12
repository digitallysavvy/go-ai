package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
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
