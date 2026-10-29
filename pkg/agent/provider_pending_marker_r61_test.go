package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// A provider-executed call (a configured provider tool whose approval is
// not-applicable) adds no content of its own in the legacy loop: its result
// is the provider's content part, as in GenerateText (TS
// convertLanguageModelContent has no output for it). The loop used to append
// a second, null tool-result that also reached the assistant message.
func TestR61GenerateAgentProviderCallAddsNoNullResult(t *testing.T) {
	newResp := func() types.GenerateResult {
		return types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: "r"},
				types.TextContent{Text: "done"},
			},
			ToolCalls:    []types.ToolCall{{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
			FinishReason: types.FinishReasonStop,
		}
	}
	tools := []types.Tool{{Name: "web_search", ProviderExecuted: true}}
	countResults := func(parts []types.ContentPart) int {
		n := 0
		for _, p := range parts {
			if _, ok := p.(types.ToolResultContent); ok {
				n++
			}
		}
		return n
	}

	a := NewToolLoopAgent(AgentConfig{Model: &mockLanguageModel{responses: []types.GenerateResult{newResp()}}, Tools: tools})
	res, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	gen, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: &mockLanguageModel{responses: []types.GenerateResult{newResp()}}, Prompt: "go", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || len(gen.Steps) != 1 {
		t.Fatalf("steps: agent %d, generate %d", len(res.Steps), len(gen.Steps))
	}
	if got, want := countResults(res.Steps[0].Content), countResults(gen.Steps[0].Content); got != want || got != 1 {
		t.Fatalf("step tool results: agent %d, generate %d (want 1): %+v", got, want, res.Steps[0].Content)
	}
	var agentMsgResults int
	for _, m := range res.Steps[0].Response.Messages {
		agentMsgResults += countResults(m.Content)
	}
	if agentMsgResults != 1 {
		t.Fatalf("response message tool results = %d, want 1: %+v", agentMsgResults, res.Steps[0].Response.Messages)
	}
}
