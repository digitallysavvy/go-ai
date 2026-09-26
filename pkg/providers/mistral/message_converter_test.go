package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestConvertMistralAssistantReasoningRoundTrip guards 21d2a2f: assistant
// reasoning content must survive conversion back into Mistral's wire format
// as {"type":"thinking","thinking":[{"type":"text","text":...}],"closed":true}
// blocks, mirroring TS convert-to-mistral-chat-messages.ts, instead of being
// silently dropped (as prompt.ToOpenAIMessages does).
func TestConvertMistralAssistantReasoningRoundTrip(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{
			Role: types.RoleAssistant,
			Content: []types.ContentPart{
				types.ReasoningContent{Text: "let me think"},
				types.TextContent{Text: "the answer"},
			},
		},
	}

	out := ConvertToMistralChatMessages(messages)
	if len(out) != 2 {
		t.Fatalf("messages = %#v, want 2", out)
	}

	assistant := out[1]
	if assistant["role"] != "assistant" {
		t.Fatalf("role = %v, want assistant", assistant["role"])
	}
	content, ok := assistant["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("content = %#v (%T), want content-part array", assistant["content"], assistant["content"])
	}
	if len(content) != 2 {
		t.Fatalf("content parts = %#v, want 2 (thinking + text)", content)
	}
	if content[0]["type"] != "thinking" {
		t.Fatalf("content[0] = %#v, want thinking block first", content[0])
	}
	thinking, ok := content[0]["thinking"].([]map[string]interface{})
	if !ok || len(thinking) != 1 || thinking[0]["text"] != "let me think" || thinking[0]["type"] != "text" {
		t.Fatalf("thinking block = %#v, want [{type:text,text:'let me think'}]", content[0]["thinking"])
	}
	if content[0]["closed"] != true {
		t.Fatalf("thinking block closed = %v, want true", content[0]["closed"])
	}
	if content[1]["type"] != "text" || content[1]["text"] != "the answer" {
		t.Fatalf("content[1] = %#v, want text block", content[1])
	}
	// The last message in the prompt is the assistant message, so it should
	// carry Mistral's continuation "prefix" marker.
	if assistant["prefix"] != true {
		t.Fatalf("prefix = %v, want true for the last (assistant) message", assistant["prefix"])
	}
}

// TestConvertMistralAssistantNoReasoningPlainStringContent guards that
// assistant messages without any reasoning still use the plain-string content
// shape (not an array), matching TS exactly.
func TestConvertMistralAssistantNoReasoningPlainStringContent(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}
	out := ConvertToMistralChatMessages(messages)
	assistant := out[1]
	if assistant["content"] != "hello" {
		t.Fatalf("content = %#v, want plain string 'hello'", assistant["content"])
	}
	// Not the last message, so no prefix marker.
	if _, ok := assistant["prefix"]; ok {
		t.Fatalf("prefix should be omitted for a non-last assistant message, got %#v", assistant["prefix"])
	}
}

// TestConvertMistralAssistantToolCalls verifies tool_calls are emitted in the
// OpenAI-shaped array Mistral expects, with JSON-encoded arguments.
func TestConvertMistralAssistantToolCalls(t *testing.T) {
	messages := []types.Message{
		{
			Role:      types.RoleAssistant,
			ToolCalls: []types.ToolCall{{ID: "tc1", ToolName: "lookup", Arguments: map[string]interface{}{"q": "x"}}},
		},
	}
	out := ConvertToMistralChatMessages(messages)
	toolCalls, ok := out[0]["tool_calls"].([]map[string]interface{})
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("tool_calls = %#v, want one entry", out[0]["tool_calls"])
	}
	if toolCalls[0]["id"] != "tc1" || toolCalls[0]["type"] != "function" {
		t.Fatalf("tool call = %#v", toolCalls[0])
	}
	fn, ok := toolCalls[0]["function"].(map[string]interface{})
	if !ok || fn["name"] != "lookup" || fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("function = %#v", toolCalls[0]["function"])
	}
}

// TestConvertMistralToolMessages verifies tool-result content is stringified
// per Mistral's rules and one Mistral message is emitted per tool result.
func TestConvertMistralToolMessages(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleTool,
			Content: []types.ContentPart{
				types.ToolResultContent{
					ToolCallID: "tc1",
					ToolName:   "lookup",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "result text"},
				},
				types.ToolResultContent{
					ToolCallID: "tc2",
					ToolName:   "search",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"k": "v"}},
				},
				types.ToolResultContent{
					ToolCallID: "tc3",
					ToolName:   "denied",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied},
				},
			},
		},
	}
	out := ConvertToMistralChatMessages(messages)
	if len(out) != 3 {
		t.Fatalf("messages = %#v, want 3 (one per tool result)", out)
	}
	if out[0]["role"] != "tool" || out[0]["tool_call_id"] != "tc1" || out[0]["content"] != "result text" {
		t.Fatalf("out[0] = %#v", out[0])
	}
	if out[1]["content"] != `{"k":"v"}` {
		t.Fatalf("out[1] content = %#v, want JSON-stringified value", out[1]["content"])
	}
	if out[2]["content"] != "Tool call execution denied." {
		t.Fatalf("out[2] content = %#v, want default execution-denied message", out[2]["content"])
	}
}

// TestConvertMistralUserMessageImageURL verifies image parts use Mistral's
// bare-string image_url shape (not an {"url": ...} object like OpenAI).
func TestConvertMistralUserMessageImageURL(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "look at this"},
				types.ImageContent{URL: "https://example.com/cat.png"},
			},
		},
	}
	out := ConvertToMistralChatMessages(messages)
	content, ok := out[0]["content"].([]map[string]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want 2 parts", out[0]["content"])
	}
	if content[1]["type"] != "image_url" || content[1]["image_url"] != "https://example.com/cat.png" {
		t.Fatalf("image part = %#v, want bare URL string", content[1])
	}
}

// TestConvertMistralSystemMessageMultiPart guards the Go-specific case where a
// system message has multiple content parts (impossible in TS, whose system
// content is always a single string): it must become a content-part array
// rather than dropping parts.
func TestConvertMistralSystemMessageMultiPart(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{
			types.TextContent{Text: "Be terse."},
			types.TextContent{Text: "Avoid jargon."},
		}},
	}
	out := ConvertToMistralChatMessages(messages)
	parts, ok := out[0]["content"].([]map[string]interface{})
	if !ok || len(parts) != 2 {
		t.Fatalf("content = %#v, want 2 parts", out[0]["content"])
	}
}
