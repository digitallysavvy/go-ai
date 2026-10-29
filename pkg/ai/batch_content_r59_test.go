package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS batch convertGenerateResult runs convertLanguageModelContent, so a
// batched text result's tool results are provider-executed and take the
// call's input, as in GenerateText; a deferred result takes the tool's
// metadata.
func TestR59BatchResultToolResultsMatchGenerateContent(t *testing.T) {
	meta := map[string]interface{}{"k": "v"}
	tools := []types.Tool{
		{Name: "web_search", ProviderExecuted: true},
		{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true, Metadata: meta},
	}
	result := &types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: `{"q":"go"}`, ProviderExecuted: true},
			types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: "hits"},
			types.ToolResultContent{ToolCallID: "deferred", ToolName: "code_execution", Result: "ok"},
		},
		ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: `{"q":"go"}`, ProviderExecuted: true}},
		FinishReason: types.FinishReasonStop,
	}
	out, err := parseBatchToolCalls(context.Background(), result, tools)
	if err != nil {
		t.Fatal(err)
	}
	var results []types.ToolResultContent
	for _, part := range out.Content {
		if p, ok := part.(types.ToolResultContent); ok {
			results = append(results, p)
		}
	}
	if len(results) != 2 {
		t.Fatalf("tool results = %d, want 2", len(results))
	}
	if !results[0].ProviderExecuted || !results[1].ProviderExecuted {
		t.Errorf("ProviderExecuted = %v, %v; want true for every model tool result", results[0].ProviderExecuted, results[1].ProviderExecuted)
	}
	if !reflect.DeepEqual(results[0].Input, map[string]interface{}{"q": "go"}) {
		t.Errorf("tool result input = %#v, want the call's input", results[0].Input)
	}
	if !reflect.DeepEqual(results[1].ToolMetadata, meta) {
		t.Errorf("deferred result ToolMetadata = %v, want %v", results[1].ToolMetadata, meta)
	}
}
