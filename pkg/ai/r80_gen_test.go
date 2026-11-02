package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r80genDeferredResultParts are the forms a model response can answer a
// provider-executed call with: every tool-result or tool-error part is a
// provider result (TS convertLanguageModelContent).
func r80genDeferredResultParts(id string) map[string]types.ContentPart {
	return map[string]types.ContentPart{
		"tool-result":          types.ToolResultContent{ToolCallID: id, ToolName: "code_execution", Result: "ok"},
		"tool-result pointer":  &types.ToolResultContent{ToolCallID: id, ToolName: "code_execution", Result: "ok"},
		"tool-error":           types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed"},
		"tool-error pointer":   &types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed"},
		"tool-error unflagged": types.ToolErrorContent{ToolCallID: id, ToolName: "code_execution", Error: "failed", ProviderExecuted: false},
	}
}

// A deferred provider call answered in the same response is not pending (TS
// pendingDeferredToolCalls deletes every tool-result part's ID), so the run
// ends after one step. GenerateText counted only a ToolResultContent value;
// a tool-error part (or a pointer part) left the call pending and the loop
// called the model again, while the legacy loop ended.
func TestR80GenDeferredCallResolvedByAnyResultPart(t *testing.T) {
	for name, part := range r80genDeferredResultParts("call-1") {
		t.Run(name, func(t *testing.T) {
			calls := 0
			model := &testutil.MockLanguageModel{
				DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
					calls++
					return &types.GenerateResult{
						ToolCalls: []types.ToolCall{{ID: "call-1", ToolName: "code_execution", Arguments: map[string]interface{}{}, ProviderExecuted: true}},
						Content: []types.ContentPart{
							types.ToolCallContent{ToolCallID: "call-1", ToolName: "code_execution", Input: "{}", ProviderExecuted: true},
							part,
						},
						FinishReason: types.FinishReasonStop,
					}, nil
				},
			}
			result, err := GenerateText(context.Background(), GenerateTextOptions{
				Model:    model,
				Prompt:   "x",
				Tools:    []types.Tool{{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true}},
				StopWhen: []StopCondition{StepCountIs(3)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(result.Steps) != 1 {
				t.Fatalf("model calls = %d, steps = %d, want 1 and 1", calls, len(result.Steps))
			}
		})
	}
}
