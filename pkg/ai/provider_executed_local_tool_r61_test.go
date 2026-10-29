package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText and streamText execute only the client tool calls (the
// calls without providerExecuted): a call the provider executed is never run
// locally, even when a local tool has its name. Go decided by the tool
// definition alone, so it ran the local tool and added a second result.
func TestR61ProviderExecutedCallIsNotRunLocally(t *testing.T) {
	var executed int
	tools := []types.Tool{{Name: "action", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		executed++
		return "local", nil
	}}}
	call := types.ToolCall{ID: "call-1", ToolName: "action", RawArguments: `{}`, ProviderExecuted: true}
	result := types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "call-1", ToolName: "action", Input: `{}`, ProviderExecuted: true},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "action", Result: "remote"},
		},
		ToolCalls:    []types.ToolCall{call},
		FinishReason: types.FinishReasonToolCalls,
	}
	countResults := func(t *testing.T, parts []types.ContentPart) {
		t.Helper()
		n := 0
		for _, p := range parts {
			if r, ok := p.(types.ToolResultContent); ok {
				n++
				if !r.ProviderExecuted {
					t.Fatalf("local tool result in step content: %+v", r)
				}
			}
		}
		if n != 1 {
			t.Fatalf("tool results in step content = %d, want 1: %+v", n, parts)
		}
	}
	t.Run("generate", func(t *testing.T) {
		executed = 0
		model := &testutil.MockLanguageModel{ToolSupport: true, DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			r := result
			return &r, nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "go", Tools: tools, StopWhen: []StopCondition{IsStepCount(3)}})
		if err != nil {
			t.Fatal(err)
		}
		if executed != 0 {
			t.Fatalf("local tool executed %d times", executed)
		}
		if len(res.Steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(res.Steps))
		}
		countResults(t, res.Steps[0].Content)
	})
	t.Run("stream", func(t *testing.T) {
		executed = 0
		model := &testutil.MockLanguageModel{ToolSupport: true, DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			c := call
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &c},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "action", Result: "remote", ProviderExecuted: true}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}), nil
		}}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "go", Tools: tools, StopWhen: []StopCondition{IsStepCount(3)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := res.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		if executed != 0 {
			t.Fatalf("local tool executed %d times", executed)
		}
		steps := res.Steps()
		if len(steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(steps))
		}
		countResults(t, steps[0].Content)
	})
}
