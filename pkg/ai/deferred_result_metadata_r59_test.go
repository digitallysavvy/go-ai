package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS convertLanguageModelContent: a deferred provider tool result (no tool
// call in the response) takes `toolMetadata: tool.metadata` from the tool
// definition, since there is no call to copy it from.
func TestR59GenerateDeferredProviderResultCarriesToolMetadata(t *testing.T) {
	t.Parallel()
	meta := map[string]interface{}{"owner": "search"}
	tool := types.Tool{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true, Metadata: meta}
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "deferred", ToolName: "code_execution", Result: "ok"},
				&types.ToolErrorContent{ToolCallID: "deferred-err", ToolName: "code_execution", Error: "boom"},
			},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, part := range res.FinalStep.Content {
		switch p := part.(type) {
		case types.ToolResultContent:
			got++
			if !reflect.DeepEqual(p.ToolMetadata, meta) {
				t.Errorf("deferred tool result ToolMetadata = %v, want %v", p.ToolMetadata, meta)
			}
		case types.ToolErrorContent:
			got++
			if !reflect.DeepEqual(p.ToolMetadata, meta) {
				t.Errorf("deferred tool error ToolMetadata = %v, want %v", p.ToolMetadata, meta)
			}
		}
	}
	if got != 2 {
		t.Fatalf("tool outputs in content = %d, want 2", got)
	}
}
