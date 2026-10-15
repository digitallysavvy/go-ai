package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func toolErrorOptions() *provider.GenerateOptions {
	return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "boom"},
		}}},
	}}}
}

func toolContent(t *testing.T, cfg Config) interface{} {
	t.Helper()
	m := NewLanguageModel(New(cfg), "gpt-4o")
	body, _, err := m.buildRequestBodyWithWarnings(toolErrorOptions(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range body["messages"].([]map[string]interface{}) {
		if msg["role"] == "tool" {
			return msg["content"]
		}
	}
	t.Fatal("no tool message")
	return nil
}

// TS OpenAI chat wraps error tool outputs as {"error": ...}; the
// openai-compatible chat model (baseten, cerebras, deepinfra, vertex MaaS)
// sends them verbatim.
func TestChatToolErrorWrappingOnlyForNativeOpenAI(t *testing.T) {
	if got := toolContent(t, Config{APIKey: "k"}); got != `{"error":"boom"}` {
		t.Errorf("native openai tool error = %#v", got)
	}
	if got := toolContent(t, Config{APIKey: "k", Name: "bedrock-mantle", ChatProviderName: "bedrock-mantle.chat"}); got != `{"error":"boom"}` {
		t.Errorf("mantle tool error = %#v", got)
	}
	if got := toolContent(t, Config{APIKey: "k", ChatProviderName: "baseten.chat", OpenAICompatible: true}); got != "boom" {
		t.Errorf("compatible tool error = %#v, want verbatim", got)
	}
}
