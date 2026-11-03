package agent

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS execute-tool-call.ts: a tool that still returns after its deadline keeps
// its result (the legacy loop, like GenerateText).
func TestR84genLegacyLoopToolResultAfterTimeoutKept(t *testing.T) {
	late := types.Tool{
		Name:       "late",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			<-ctx.Done()
			return "finished anyway", nil
		},
	}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		FinishReason: types.FinishReasonToolCalls,
		ToolCalls:    []types.ToolCall{{ID: "1", ToolName: "late", Arguments: map[string]interface{}{}}},
	}, {Text: "done", FinishReason: types.FinishReasonStop}}}
	d := 10 * time.Millisecond
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{late}, Timeout: &ai.TimeoutConfig{ToolMs: &d}})
	res, err := a.Execute(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolResults) != 1 || res.ToolResults[0].Error != nil || res.ToolResults[0].Result != "finished anyway" {
		t.Fatalf("tool results = %+v", res.ToolResults)
	}
}
