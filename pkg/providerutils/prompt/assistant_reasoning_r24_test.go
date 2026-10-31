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

// TS getOpenAIMetadata: providerOptions.openaiCompatible of messages, parts,
// tool calls and tool results are spread into the wire objects.
func TestSpreadCompatMetadata(t *testing.T) {
	meta := func(k string) map[string]interface{} {
		return map[string]interface{}{"openaiCompatible": map[string]interface{}{k: true}}
	}
	msgs := []types.Message{
		{Role: types.RoleSystem, ProviderOptions: meta("sys"), Content: []types.ContentPart{types.TextContent{Text: "s"}}},
		{Role: types.RoleUser, ProviderOptions: meta("msg"), Content: []types.ContentPart{types.TextContent{Text: "u", ProviderOptions: meta("part")}}},
		{Role: types.RoleAssistant, ProviderOptions: meta("asst"), Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "1", ToolName: "t", ProviderOptions: meta("call")},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "r"}, ProviderOptions: meta("res")},
		}},
	}
	out := ToOpenAIMessages(msgs, ToOpenAIMessagesOptions{SpreadCompatMetadata: true})
	if out[0]["sys"] != true || out[0]["msg"] != nil {
		t.Fatalf("system: %#v", out[0])
	}
	if out[1]["part"] != true || out[1]["msg"] != nil {
		t.Fatalf("lone user text uses the part's metadata: %#v", out[1])
	}
	if out[2]["asst"] != true {
		t.Fatalf("assistant: %#v", out[2])
	}
	calls := out[2]["tool_calls"].([]map[string]interface{})
	if calls[0]["call"] != true {
		t.Fatalf("tool call: %#v", calls[0])
	}
	if out[3]["res"] != true {
		t.Fatalf("tool result: %#v", out[3])
	}
	plain := ToOpenAIMessages(msgs)
	if plain[0]["sys"] != nil || plain[3]["res"] != nil {
		t.Fatal("metadata must not be spread without the option")
	}
}

// TS chat converters send a PDF as {filename, file_data: data URI}; the
// default filename is part-<index>.pdf for OpenAI and document.pdf for
// OpenAI-compatible.
func TestPDFFilePartUsesDataURI(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "x"},
		types.FileContent{Data: []byte("%PDF"), MediaType: "application/pdf"},
		types.FileContent{Data: []byte("%PDF"), MediaType: "application/pdf", Filename: "a.pdf"},
	}}}
	for _, tc := range []struct {
		opt  ToOpenAIMessagesOptions
		name string
	}{
		{ToOpenAIMessagesOptions{}, "part-1.pdf"},
		{ToOpenAIMessagesOptions{SpreadCompatMetadata: true}, "document.pdf"},
	} {
		parts := ToOpenAIMessages(msgs, tc.opt)[0]["content"].([]map[string]interface{})
		f := parts[1]["file"].(map[string]interface{})
		if f["file_data"] != "data:application/pdf;base64,JVBERg==" || f["filename"] != tc.name || f["media_type"] != nil {
			t.Fatalf("%s: %#v", tc.name, f)
		}
		if parts[2]["file"].(map[string]interface{})["filename"] != "a.pdf" {
			t.Fatalf("explicit filename lost: %#v", parts[2])
		}
	}
}
