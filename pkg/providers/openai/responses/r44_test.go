package responses

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// TS forwards a string code_interpreter container as is (including ""), the
// container fileIds as is (including []), and mcp `headers` as is (including {}).
func TestR44_PrepareToolsPassesEmptyValuesThrough(t *testing.T) {
	prep := func(tool types.Tool) map[string]interface{} {
		out, err := PrepareToolsWithError([]types.Tool{tool})
		if err != nil || len(out) != 1 {
			t.Fatalf("out=%v err=%v", out, err)
		}
		m, _ := out[0].(map[string]interface{})
		return m
	}
	ci := func(c interface{}) types.Tool {
		return openaitool.CodeInterpreter(openaitool.CodeInterpreterConfig{Container: c})
	}
	if got := prep(ci("")); got["container"] != "" {
		t.Errorf("empty string container = %#v", got["container"])
	}
	if got := prep(ci(nil)); !reflect.DeepEqual(got["container"], map[string]interface{}{"type": "auto"}) {
		t.Errorf("nil container = %#v", got["container"])
	}
	got := prep(ci(openaitool.CodeInterpreterContainer{FileIDs: []string{}}))
	if !reflect.DeepEqual(got["container"], map[string]interface{}{"type": "auto", "file_ids": []string{}}) {
		t.Errorf("empty fileIds container = %#v", got["container"])
	}
	mcp := func(h map[string]string) types.Tool {
		return openaitool.MCP(openaitool.MCPConfig{ServerLabel: "s", ServerURL: "https://x", Headers: h})
	}
	if _, ok := prep(mcp(map[string]string{}))["headers"]; !ok {
		t.Error("empty headers object dropped")
	}
	if _, ok := prep(mcp(nil))["headers"]; ok {
		t.Error("nil headers must stay absent")
	}
}

func TestR44_CustomToolCallReplaysRawInputText(t *testing.T) {
	// How the model decodes a custom_tool_call: Arguments {"input": text}, and
	// RawArguments the JSON string literal of the text (TS
	// `input: JSON.stringify(part.input)`).
	for name, call := range map[string]types.ToolCallContent{
		"arguments only": {ToolCallID: "c1", ToolName: "grammar_tool", Arguments: map[string]interface{}{"input": "raw input"}},
		"quoted input":   {ToolCallID: "c1", ToolName: "grammar_tool", Input: `"raw input"`},
		"both":           {ToolCallID: "c1", ToolName: "grammar_tool", Input: `"raw input"`, Arguments: map[string]interface{}{"input": "raw input"}},
	} {
		input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{{
			Role:    types.RoleAssistant,
			Content: []types.ContentPart{call},
		}}}, "system", ConvertOptions{CustomToolNames: map[string]bool{"grammar_tool": true}})
		if err != nil || len(input) != 1 {
			t.Fatalf("%s: input=%v err=%v", name, input, err)
		}
		item, ok := input[0].(CustomToolCallItem)
		if !ok || item.Input != "raw input" {
			t.Errorf("%s: item = %#v, want custom_tool_call with the raw text", name, input[0])
		}
	}
}
