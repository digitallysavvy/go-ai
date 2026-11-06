package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func deferredProviderTool() types.Tool {
	return types.Tool{
		Name:                    "code_exec",
		Description:             "provider tool",
		Parameters:              map[string]interface{}{"type": "object"},
		ProviderExecuted:        true,
		SupportsDeferredResults: true,
	}
}

// A provider-executed call whose result is deferred keeps the loop going until
// a later response delivers the result (TS pendingDeferredToolCalls).
func TestLegacyLoop_PendingDeferredProviderToolContinues(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls == 1 {
				return &types.GenerateResult{
					FinishReason: types.FinishReasonStop,
					ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "code_exec", Arguments: map[string]interface{}{}}},
				}, nil
			}
			return &types.GenerateResult{
				Text:         "done",
				FinishReason: types.FinishReasonStop,
				Content:      []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "code_exec", Result: "ok"}},
			}, nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{deferredProviderTool()}, MaxSteps: 5})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 2 || res.Text != "done" {
		t.Fatalf("steps=%d text=%q, want 2 steps and done", len(res.Steps), res.Text)
	}
}

// A provider-executed call whose result arrived inline is not a client tool
// call, so the run ends instead of sending another step.
func TestLegacyLoop_ProviderOnlyStepWithInlineResultStops(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			return &types.GenerateResult{
				Text:         "answer",
				FinishReason: types.FinishReasonStop,
				ToolCalls:    []types.ToolCall{{ID: "c1", ToolName: "code_exec", ProviderExecuted: true, Arguments: map[string]interface{}{}}},
				Content:      []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "code_exec", Result: "ok"}},
			}, nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{deferredProviderTool()}, MaxSteps: 5})
	res, err := a.Execute(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(res.Steps) != 1 {
		t.Fatalf("calls=%d steps=%d, want 1 and 1", calls, len(res.Steps))
	}
}
