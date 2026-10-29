package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS convertLanguageModelContent marks every tool-result and tool-error the
// model returns as providerExecuted, so toResponseMessages keeps them in the
// assistant message next to their call. Go left a provider result without
// ProviderExecuted in a separate tool message.
func TestR58ModelToolResultsAreProviderExecuted(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ws", ToolName: "web_search", Input: "{}", ProviderExecuted: true},
				types.ToolResultContent{ToolCallID: "ws", ToolName: "web_search", Result: map[string]interface{}{"ok": true}},
				types.ToolCallContent{ToolCallID: "ig", ToolName: "image_generation", Input: "{}", ProviderExecuted: true},
				&types.ToolErrorContent{ToolCallID: "ig", ToolName: "image_generation", Error: "failed"},
				types.TextContent{Text: "done"},
			},
			ToolCalls: []types.ToolCall{
				{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true},
				{ID: "ig", ToolName: "image_generation", RawArguments: "{}", ProviderExecuted: true},
			},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	msgs := res.FinalStep.Response.Messages
	if len(msgs) != 1 || msgs[0].Role != types.RoleAssistant {
		roles := []types.MessageRole{}
		for _, m := range msgs {
			roles = append(roles, m.Role)
		}
		t.Fatalf("response message roles = %v, want one assistant message", roles)
	}
	if n := len(msgs[0].Content); n != 5 {
		t.Fatalf("assistant content parts = %d, want 5", n)
	}
	for _, part := range res.FinalStep.Content {
		switch p := part.(type) {
		case types.ToolResultContent:
			if !p.ProviderExecuted {
				t.Errorf("tool result %s ProviderExecuted = false", p.ToolCallID)
			}
		case types.ToolErrorContent:
			if !p.ProviderExecuted {
				t.Errorf("tool error %s ProviderExecuted = false", p.ToolCallID)
			}
		}
	}
}

// TS stream-language-model-call.ts sets providerExecuted: true on every model
// tool-result chunk, including one whose call did not carry the flag.
func TestR58StreamModelToolResultsAreProviderExecuted(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "ws", ToolName: "web_search", RawArguments: "{}", ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "ws", ToolName: "web_search", Result: "r"}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "late", ToolName: "code_execution", Result: "r"}},
			{Type: provider.ChunkTypeText, Text: "done"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := res.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	for _, m := range res.ResponseMessages() {
		if m.Role != types.RoleAssistant {
			t.Fatalf("response message role %s, want only assistant messages: %+v", m.Role, res.ResponseMessages())
		}
	}
}
