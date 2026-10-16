package prompt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-compatible: assistant text parts are concatenated into one content
// string, other part types are ignored, and reasoning is replayed as
// reasoning_content (TS #12049); Groq replays it as "reasoning".
func TestAssistantTextJoinedAndReasoningReplayed(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ReasoningContent{Text: "think "},
		types.TextContent{Text: "a"},
		types.ReasoningContent{Text: "more"},
		types.TextContent{Text: "b"},
	}}}

	plain := ToOpenAIMessages(msgs)[0]
	if plain["content"] != "ab" {
		t.Fatalf("content = %#v, want joined string", plain["content"])
	}
	if _, ok := plain["reasoning_content"]; ok {
		t.Fatalf("reasoning must not be replayed without ReasoningKey: %#v", plain)
	}

	compat := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{ReasoningKey: "reasoning_content"})[0]
	if compat["content"] != "ab" || compat["reasoning_content"] != "think more" {
		t.Fatalf("compat = %#v", compat)
	}

	groq := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{ReasoningKey: "reasoning"})[0]
	if groq["reasoning"] != "think more" {
		t.Fatalf("groq = %#v", groq)
	}

	withCall := append([]types.ContentPart{}, msgs[0].Content...)
	withCall = append(withCall, types.ToolCallContent{ToolCallID: "1", ToolName: "t"})
	tc := ToOpenAIMessages([]types.Message{{Role: types.RoleAssistant, Content: withCall}}, ToOpenAIMessagesOptions{ReasoningKey: "reasoning_content"})[0]
	if tc["reasoning_content"] != "think more" || tc["tool_calls"] == nil {
		t.Fatalf("tool-call message = %#v", tc)
	}
}
