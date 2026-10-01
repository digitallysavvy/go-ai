package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from ai/packages/alibaba/src/convert-to-alibaba-chat-messages.test.ts
// reasoning/tool-call assistant-message behavior.

func TestConvertToAlibabaChatMessages_CurrentRoundReasoningAlwaysIncluded(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "thinking..."},
				types.TextContent{Text: "hello"},
			},
		},
	}

	// preserveThinking=false: reasoning from the current round (after the
	// last user message) is still included.
	result := ConvertToAlibabaChatMessages(messages, nil, false)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(result), result)
	}
	assistant := result[1]
	if assistant["reasoning_content"] != "thinking..." {
		t.Fatalf("reasoning_content = %#v", assistant["reasoning_content"])
	}
	if assistant["content"] != "hello" {
		t.Fatalf("content = %#v", assistant["content"])
	}
}

func TestConvertToAlibabaChatMessages_EarlierRoundReasoningDroppedByDefault(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "earlier thinking"},
				types.TextContent{Text: "first reply"},
			},
		},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "second"}}},
	}

	result := ConvertToAlibabaChatMessages(messages, nil, false)
	// messages: [user, assistant, user] -> assistant is result index 1
	assistant := result[1]
	if _, hasReasoning := assistant["reasoning_content"]; hasReasoning {
		t.Fatalf("expected earlier-round reasoning to be dropped, got %#v", assistant["reasoning_content"])
	}
	if assistant["content"] != "first reply" {
		t.Fatalf("content = %#v", assistant["content"])
	}
}

func TestConvertToAlibabaChatMessages_PreserveThinkingReplaysEarlierRounds(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "first"}}},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "earlier thinking"},
				types.TextContent{Text: "first reply"},
			},
		},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "second"}}},
	}

	result := ConvertToAlibabaChatMessages(messages, nil, true)
	assistant := result[1]
	if assistant["reasoning_content"] != "earlier thinking" {
		t.Fatalf("expected preserved reasoning, got %#v", assistant["reasoning_content"])
	}
}

func TestConvertToAlibabaChatMessages_ReasoningOnlyTurnHasNullContent(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{
			Role:    types.RoleAssistant,
			Content: []types.ContentPart{types.ReasoningContent{Text: "just thinking"}},
		},
	}

	result := ConvertToAlibabaChatMessages(messages, nil, false)
	assistant := result[1]
	if assistant["content"] != nil {
		t.Fatalf("expected null content for a reasoning-only turn, got %#v", assistant["content"])
	}
	if assistant["reasoning_content"] != "just thinking" {
		t.Fatalf("reasoning_content = %#v", assistant["reasoning_content"])
	}
}

func TestConvertToAlibabaChatMessages_EmptyAssistantTurnDropped(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: nil},
	}

	result := ConvertToAlibabaChatMessages(messages, nil, false)
	if len(result) != 1 {
		t.Fatalf("expected the empty assistant turn to be dropped, got %+v", result)
	}
}

func TestConvertToAlibabaChatMessages_AssistantToolCalls(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "weather?"}}},
		{
			Role:    types.RoleAssistant,
			Content: []types.ContentPart{},
			ToolCalls: []types.ToolCall{
				{ID: "call_1", ToolName: "get_weather", Arguments: map[string]interface{}{"city": "SF"}},
			},
		},
	}

	result := ConvertToAlibabaChatMessages(messages, nil, false)
	assistant := result[1]
	toolCalls, ok := assistant["tool_calls"].([]map[string]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("tool_calls = %#v", assistant["tool_calls"])
	}
	if toolCalls[0]["id"] != "call_1" {
		t.Fatalf("tool_calls[0].id = %#v", toolCalls[0]["id"])
	}
	function, ok := toolCalls[0]["function"].(map[string]interface{})
	if !ok || function["name"] != "get_weather" {
		t.Fatalf("tool_calls[0].function = %#v", toolCalls[0]["function"])
	}
	// No text and no reasoning, but tool calls present: content is null.
	if assistant["content"] != nil {
		t.Fatalf("expected null content alongside tool_calls, got %#v", assistant["content"])
	}
}

func TestSupportsJsonSchemaOutput(t *testing.T) {
	cases := map[string]bool{
		"qwen3.7-plus":      true,
		"qwen3.7-plus-2026": true,
		"qwen3.7-flash":     true,
		"qwen3.7-max":       true,
		"qwen3.8-max":       true,
		"qwen3.8-flash":     true,
		"qwen-plus":         false,
		"qwen3-max":         false,
		"qwen3.7-plusextra": false, // must be an exact match or "-" separated prefix
	}
	for modelID, want := range cases {
		if got := supportsJsonSchemaOutput(modelID); got != want {
			t.Errorf("supportsJsonSchemaOutput(%q) = %v, want %v", modelID, got, want)
		}
	}
}

func TestSupportsPreservedThinking(t *testing.T) {
	cases := map[string]bool{
		"qwen3.7-max":    true,
		"qwen3.7-plus":   true,
		"qwen3.8-max":    true,
		"kimi-k2.7-code": true,
		"qwen-plus":      false,
		"qwen3-max":      false,
	}
	for modelID, want := range cases {
		if got := supportsPreservedThinking(modelID); got != want {
			t.Errorf("supportsPreservedThinking(%q) = %v, want %v", modelID, got, want)
		}
	}
}
