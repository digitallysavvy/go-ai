package responses

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func bpOpts(name string) map[string]interface{} {
	return map[string]interface{}{name: map[string]interface{}{"promptCacheBreakpoint": map[string]interface{}{"mode": "explicit"}}}
}

var explicitBP = map[string]interface{}{"mode": "explicit"}

func jsonRoundTrip(t *testing.T, v interface{}) interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TS convertToOpenAIResponsesInput forwards providerOptions.<name>.
// promptCacheBreakpoint as prompt_cache_breakpoint on system, user text and
// user file/image parts.
func TestConvert_PromptCacheBreakpointOnSystemAndUserParts(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, ProviderOptions: bpOpts("openai"), Content: []types.ContentPart{types.TextContent{Text: "be brief"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "a", ProviderOptions: bpOpts("openai")},
			types.TextContent{Text: "b"},
			types.ImageContent{URL: "https://x/y.png", ProviderOptions: bpOpts("openai")},
		}},
	}
	input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: msgs}, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := jsonRoundTrip(t, input)
	want := jsonRoundTrip(t, []interface{}{
		map[string]interface{}{"role": "system", "content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "be brief", "prompt_cache_breakpoint": explicitBP},
		}},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "a", "prompt_cache_breakpoint": explicitBP},
			map[string]interface{}{"type": "input_text", "text": "b"},
			map[string]interface{}{"type": "input_image", "image_url": "https://x/y.png", "prompt_cache_breakpoint": explicitBP},
		}},
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %s\nwant %s", mustJSON(got), mustJSON(want))
	}
}

func mustJSON(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

// Scalar tool outputs with a breakpoint become a one-element input_text array;
// content outputs carry per-part breakpoints; an azure request reads only the
// "azure" key.
func TestConvert_PromptCacheBreakpointOnToolOutputs(t *testing.T) {
	tool := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "f", ProviderOptions: bpOpts("openai"), Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}},
		types.ToolResultContent{ToolCallID: "c2", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"a": 1}, ProviderOptions: bpOpts("openai")}},
		types.ToolResultContent{ToolCallID: "c3", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
			types.TextContentBlock{Text: "t", ProviderOptions: bpOpts("openai")},
			types.TextContentBlock{Text: "u"},
		}}},
		types.ToolResultContent{ToolCallID: "c4", ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "plain"}},
	}}
	input, _, err := ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{tool}}, "system", ConvertOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := jsonRoundTrip(t, input)
	want := jsonRoundTrip(t, []interface{}{
		map[string]interface{}{"type": "function_call_output", "call_id": "c1", "output": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "ok", "prompt_cache_breakpoint": explicitBP}}},
		map[string]interface{}{"type": "function_call_output", "call_id": "c2", "output": []interface{}{
			map[string]interface{}{"type": "input_text", "text": `{"a":1}`, "prompt_cache_breakpoint": explicitBP}}},
		map[string]interface{}{"type": "function_call_output", "call_id": "c3", "output": []interface{}{
			map[string]interface{}{"type": "input_text", "text": "t", "prompt_cache_breakpoint": explicitBP},
			map[string]interface{}{"type": "input_text", "text": "u"}}},
		map[string]interface{}{"type": "function_call_output", "call_id": "c4", "output": "plain"},
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %s\nwant %s", mustJSON(got), mustJSON(want))
	}

	// azure: only providerOptions.azure counts.
	azTool := types.Message{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "f", ProviderOptions: bpOpts("openai"), Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "ok"}},
	}}
	input, _, err = ConvertPromptToInputWithOptions(types.Prompt{Messages: []types.Message{azTool}}, "system", ConvertOptions{ProviderOptionsName: "azure"})
	if err != nil {
		t.Fatal(err)
	}
	if out := jsonRoundTrip(t, input).([]interface{})[0].(map[string]interface{})["output"]; out != "ok" {
		t.Fatalf("azure output = %v, want plain ok", out)
	}
}

// A regrouped parallel call sends an input_text array when any child carries a
// breakpoint (children after the first are "\n"-prefixed), else a joined string.
func TestBuildParallelFunctionCallOutput_PromptCacheBreakpoint(t *testing.T) {
	mk := func(id string, bp bool, text string) types.ToolResultContent {
		r := types.ToolResultContent{ToolCallID: id, ToolName: "f", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: text}}
		if bp {
			r.ProviderOptions = bpOpts("openai")
		}
		return r
	}
	group := parallelToolResultGroup{
		metadata: ParallelToolCallMetadata{ToolCallID: "wrap"},
		results:  []types.ToolResultContent{mk("a", false, "one"), mk("b", true, "two")},
	}
	got := jsonRoundTrip(t, buildParallelFunctionCallOutput(group, ConvertOptions{}))
	want := jsonRoundTrip(t, map[string]interface{}{"type": "function_call_output", "call_id": "wrap", "output": []interface{}{
		map[string]interface{}{"type": "input_text", "text": "one"},
		map[string]interface{}{"type": "input_text", "text": "\ntwo", "prompt_cache_breakpoint": explicitBP},
	}})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %s\nwant %s", mustJSON(got), mustJSON(want))
	}
	group.results = []types.ToolResultContent{mk("a", false, "one"), mk("b", false, "two")}
	if out := buildParallelFunctionCallOutput(group, ConvertOptions{}).Output; out != "one\ntwo" {
		t.Fatalf("no-breakpoint output = %#v", out)
	}
}
