package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// TS sends tool_name: anthropicOptions.toolName! for a tool-reference
// custom content part; an absent toolName is undefined and JSON.stringify
// leaves the key out (Go sent "tool_name": null).
func TestR99provaToolReferenceWithoutToolName(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "search", Arguments: map[string]interface{}{}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "search",
			Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
				types.CustomContentBlock{ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"type": "tool-reference"}}},
			}},
		}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out.Messages[1]["content"])
	want := `[{"content":[{"type":"tool_reference"}],"tool_use_id":"c1","type":"tool_result"}]`
	if string(b) != want {
		t.Fatalf("content = %s, want %s", b, want)
	}
}

// TS replays a text part's providerMetadata.anthropic.citations from the
// parsed metadata object: a number past the double range (1e400, Infinity
// in JS, ruling 4) does not drop the metadata, and JSON.stringify sends it
// as null (gen CR r99).
func TestR99provaReplayedMetadataOutOfRangeNumber(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{
			Text:             "a",
			ProviderMetadata: json.RawMessage(`{"anthropic":{"citations":[{"type":"char_location","cited_text":"x","document_index":1e400}]}}`),
		}}},
	}
	out, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonstr.Marshal(out.Messages[1]["content"])
	if err != nil {
		t.Fatal(err)
	}
	b := string(raw)
	want := `[{"citations":[{"cited_text":"x","document_index":null,"type":"char_location"}],"text":"a","type":"text"}]`
	if b != want {
		t.Fatalf("content = %s, want %s", b, want)
	}
}
