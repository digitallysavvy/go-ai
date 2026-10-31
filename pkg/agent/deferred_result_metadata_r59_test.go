package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertLanguageModelContent: a deferred provider tool result (no call in
// the response) takes the tool definition's metadata.
func TestR59GenerateAgentDeferredResultCarriesToolMetadata(t *testing.T) {
	meta := map[string]interface{}{"owner": "exec"}
	model := &mockLanguageModel{responses: []types.GenerateResult{{
		Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "deferred", ToolName: "code_execution", Result: "ok"},
			types.TextContent{Text: "done"},
		},
		FinishReason: types.FinishReasonStop,
	}}}
	a := NewToolLoopAgent(AgentConfig{Model: model, Tools: []types.Tool{{Name: "code_execution", ProviderExecuted: true, SupportsDeferredResults: true, Metadata: meta}}})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	step := res.Steps[len(res.Steps)-1]
	found := false
	for _, part := range step.Content {
		if p, ok := part.(types.ToolResultContent); ok {
			found = true
			if !reflect.DeepEqual(p.ToolMetadata, meta) {
				t.Fatalf("deferred result ToolMetadata = %v, want %v", p.ToolMetadata, meta)
			}
		}
	}
	if !found {
		t.Fatal("no tool result in the step content")
	}
}
