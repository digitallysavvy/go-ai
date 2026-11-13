package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS convertLanguageModelContent looks a deferred provider result's tool up
// in the call's tools (generateText passes `tools`, not the step's active
// tools), so a deferred result of a provider tool that ActiveTools leaves out
// of the step is accepted and gets the tool's metadata.
func TestR63DeferredResultOfAnInactiveProviderToolIsAccepted(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "srv-1", ToolName: "code_execution", Result: map[string]interface{}{"ok": true}},
			},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "go",
		Tools: []types.Tool{
			{Name: "local", Parameters: map[string]interface{}{"type": "object"}},
			{Name: "code_execution", Type: types.ToolTypeProviderDefined, ProviderExecuted: true, SupportsDeferredResults: true, Metadata: map[string]interface{}{"k": "v"}},
		},
		ActiveTools: []string{"local"},
	})
	if err != nil {
		t.Fatalf("GenerateText: %v", err)
	}
	var found bool
	for _, p := range res.Steps[0].Content {
		if tr, ok := p.(types.ToolResultContent); ok && tr.ToolCallID == "srv-1" {
			found = true
			if tr.ToolMetadata["k"] != "v" {
				t.Errorf("deferred result tool metadata = %v, want the tool's", tr.ToolMetadata)
			}
		}
	}
	if !found {
		t.Errorf("content has no deferred result: %+v", res.Steps[0].Content)
	}
}
