package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// carry CR-2 (r70): a provider-executed dynamic call of a tool that is not in
// the set is the provider's (TS parseToolCall keeps it; only client calls
// run locally), so the legacy loop gives it no "tool not found" error result
// and OnToolError does not fire.
func TestR70Integ_LegacyLoopProviderDynamicUnknownToolIsNotAnError(t *testing.T) {
	model := &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			FinishReason: types.FinishReasonStop,
			ToolCalls: []types.ToolCall{{
				ID: "ws-1", ToolName: "WebSearch", Arguments: map[string]interface{}{"q": "x"},
				ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true,
			}},
		}, nil
	}}
	toolErrors := 0
	var seen []types.ToolResult
	a := NewToolLoopAgent(AgentConfig{
		Model:        model,
		OnToolError:  func(types.ToolCall, error) { toolErrors++ },
		OnToolResult: func(r types.ToolResult) { seen = append(seen, r) },
	})
	result, err := a.Execute(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if toolErrors != 0 {
		t.Errorf("OnToolError fired %d times", toolErrors)
	}
	for _, s := range result.Steps {
		for _, r := range s.ToolResults {
			if r.Error != nil && strings.Contains(r.Error.Error(), "tool not found") {
				t.Errorf("step tool result = %+v, want no tool not found error", r)
			}
		}
	}
	if len(seen) != 1 || !seen[0].ProviderExecuted || !seen[0].Dynamic || seen[0].Error != nil || seen[0].ToolCallID != "ws-1" {
		t.Errorf("OnToolResult = %+v, want one pending provider-executed dynamic result", seen)
	}
}
