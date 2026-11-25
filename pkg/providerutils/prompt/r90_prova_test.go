package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r90provaCC() map[string]interface{} {
	return map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}
}

// TS convert-to-anthropic-prompt.ts (assistant section): the cache control
// of every assistant part is read before the part switch, so a message's
// cache control read at a reasoning last part uses a breakpoint. Expected
// values recorded by running TS createAnthropic at 9f7a842ae1: the fourth
// user breakpoint is dropped with "found 5".
func TestR90provaAssistantCacheControlAtNonCacheablePart(t *testing.T) {
	cc := r90provaCC()
	user := func(text string) types.TextContent { return types.TextContent{Text: text, ProviderOptions: cc} }
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, ProviderOptions: cc, Content: []types.ContentPart{
			types.TextContent{Text: "a"},
			types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"signature": "s"}}},
		}},
		{Role: types.RoleUser, Content: []types.ContentPart{user("1"), user("2"), user("3"), user("4")}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out.Messages)
	want := `[{"content":[{"text":"q","type":"text"}],"role":"user"},{"content":[{"text":"a","type":"text"},{"signature":"s","thinking":"r","type":"thinking"}],"role":"assistant"},{"content":[{"cache_control":{"type":"ephemeral"},"text":"1","type":"text"},{"cache_control":{"type":"ephemeral"},"text":"2","type":"text"},{"cache_control":{"type":"ephemeral"},"text":"3","type":"text"},{"text":"4","type":"text"}],"role":"user"}]`
	if string(got) != want {
		t.Errorf("messages = %s\nwant %s", got, want)
	}
	if len(out.Warnings) != 1 || out.Warnings[0].Details != "Maximum 4 cache breakpoints exceeded (found 5). This breakpoint will be ignored." {
		t.Errorf("warnings = %#v", out.Warnings)
	}
}

// Same TS rule for a reasoning part with its own cache control: it uses a
// breakpoint and also warns that thinking blocks cannot be cached (TS
// recorded at 9f7a842ae1).
func TestR90provaReasoningCacheControlUsesBreakpoint(t *testing.T) {
	cc := r90provaCC()
	user := func(text string) types.TextContent { return types.TextContent{Text: text, ProviderOptions: cc} }
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "r", ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"signature": "s", "cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
			types.TextContent{Text: "a"},
		}},
		{Role: types.RoleUser, Content: []types.ContentPart{user("1"), user("2"), user("3"), user("4")}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	last, _ := json.Marshal(out.Messages[2])
	if want := `{"content":[{"cache_control":{"type":"ephemeral"},"text":"1","type":"text"},{"cache_control":{"type":"ephemeral"},"text":"2","type":"text"},{"cache_control":{"type":"ephemeral"},"text":"3","type":"text"},{"text":"4","type":"text"}],"role":"user"}`; string(last) != want {
		t.Errorf("last message = %s", last)
	}
	if len(out.Warnings) != 2 || out.Warnings[0].Details != "cache_control cannot be set on thinking block. It will be ignored." ||
		out.Warnings[1].Details != "Maximum 4 cache breakpoints exceeded (found 5). This breakpoint will be ignored." {
		t.Errorf("warnings = %#v", out.Warnings)
	}
}

// TS getAnthropicToolsetName: any string toolsetName in the metadata, ""
// included, makes the call and its result toolset members (TS recorded at
// 9f7a842ae1).
func TestR90provaEmptyToolsetName(t *testing.T) {
	opts := map[string]interface{}{"anthropic": map[string]interface{}{"toolsetName": ""}}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{"action":"x"}`, ProviderOptions: opts}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}, ProviderOptions: opts}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out.Messages[1:])
	want := `[{"content":[{"id":"c1","input":{"action":"x"},"name":"x","toolset_name":"","type":"tool_use"}],"role":"assistant"},{"content":[{"content":"ok","tool_use_id":"c1","toolset_name":"","type":"tool_result"}],"role":"user"}]`
	if string(got) != want {
		t.Errorf("messages = %s\nwant %s", got, want)
	}
}

// TS sends a provider-executed call's input as is: a JSON null input is
// `input: null` on server_tool_use and mcp_tool_use (TS recorded at
// 9f7a842ae1).
func TestR90provaProviderExecutedNullInput(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "web_search", Input: "null", ProviderExecuted: true},
			types.ToolCallContent{ToolCallID: "c2", ToolName: "m", Input: "null", ProviderExecuted: true,
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"type": "mcp-tool-use", "serverName": "s"}}},
		}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(out.Messages[1])
	want := `{"content":[{"id":"c1","input":null,"name":"web_search","type":"server_tool_use"},{"id":"c2","input":null,"name":"m","server_name":"s","type":"mcp_tool_use"}],"role":"assistant"}`
	if string(got) != want {
		t.Errorf("message = %s\nwant %s", got, want)
	}
}
