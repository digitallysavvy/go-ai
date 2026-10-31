package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r74provbTSShapedTool builds a TS provider tool as is ({type: "provider",
// id, name, args}): the shape prepareResponsesTools receives in TS, and a Go
// tool restored from a workflow.
func r74provbTSShapedTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	if args == nil {
		args = map[string]interface{}{}
	}
	args, _ = r74provbDropUndefined(args).(map[string]interface{})
	return types.Tool{Name: name, Type: types.ToolTypeProviderDefined, ProviderID: id, ProviderArgs: args}, true
}

// r74provbDropUndefined removes the fixture's undefined markers
// ({"__undef": true}) from object values, as JSON.stringify drops undefined.
func r74provbDropUndefined(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, e := range x {
			if m, ok := e.(map[string]interface{}); ok && len(m) == 1 && m["__undef"] == true {
				continue
			}
			out[k] = r74provbDropUndefined(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = r74provbDropUndefined(e)
		}
		return out
	}
	return v
}

// TestR74ProvbTSCasesPrepareTools replays openai-responses-prepare-tools.test.ts
// (TS prepareResponsesTools) through the whole Responses request builder:
// tools, tool_choice (or tool_choice.allowed_tools from
// providerOptions.openai.allowedTools) and the tool warnings.
func TestR74ProvbTSCasesPrepareTools(t *testing.T) {
	tsfixture.ProviderToolHook = r74provbTSShapedTool
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		tools, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		for i := range tools {
			// BuildTools marks provider tools provider-executed; the request
			// builder does not read it.
			tools[i].ProviderExecuted = false
		}
		// TS supportsAsyncToolCalling defaults to true (a GPT-6 model).
		modelID := "gpt-6"
		if v, ok := in["supportsAsyncToolCalling"].(bool); ok && !v {
			modelID = "gpt-5"
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: tools}
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok && tc["__undef"] == nil {
			opts.ToolChoice = tsfixture.BuildToolChoice(tc)
		}
		if at, ok := in["allowedTools"].(map[string]interface{}); ok {
			opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"allowedTools": at}}
		}
		m, err := New(Config{APIKey: "k"}).ResponsesModel(modelID)
		if err != nil {
			return nil, nil, err
		}
		body, _, warnings, err := m.(*ResponsesLanguageModel).buildRequest(opts, false)
		if err != nil {
			return nil, nil, err
		}
		out := map[string]interface{}{"toolWarnings": tsfixture.WarningsToTS(warnings)}
		for key, field := range map[string]string{"tools": "tools", "tool_choice": "toolChoice"} {
			if v, ok := body[key]; ok {
				b, _ := json.Marshal(v)
				var g interface{}
				_ = json.Unmarshal(b, &g)
				out[field] = g
			}
		}
		// `tools` in the assertions is the test's input list.
		return out, map[string]interface{}{"tools": in["tools"]}, nil
	}, map[string]string{
		"should apply the tool name mapping for a tool that is not part of the request": "the TS test passes an arbitrary toolNameMapping (my_tool -> mapped_tool); Go derives the mapping from the tools",
		"should throw when a providerReference cannot be resolved for openai":           "the assertions read the caught error object (NoSuchProviderReferenceError.isInstance); covered by responses.TestR74ProvbTSShapedShellEnvironment",
	})
}

// TestR74ProvbHostedToolSearchItemReferences ports
// convert-to-openai-responses-input-tool-search.test.ts: with store, a hosted
// tool_search call and its output replay as item references from their own
// providerMetadata itemIds.
func TestR74ProvbHostedToolSearchItemReferences(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.ToolCallContent{
			ToolCallID: "tsc_hosted_123", ToolName: "tool_search",
			Input:            `{"arguments":{"paths":["get_weather"]},"call_id":null}`,
			Arguments:        map[string]interface{}{"arguments": map[string]interface{}{"paths": []interface{}{"get_weather"}}, "call_id": nil},
			ProviderExecuted: true,
			ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"tsc_hosted_123"}}`),
		},
		types.ToolResultContent{
			ToolCallID: "tsc_hosted_123", ToolName: "tool_search",
			Output:           &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"tools": []interface{}{map[string]interface{}{"name": "get_weather", "type": "function"}}}},
			ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"tso_hosted_456"}}`),
		},
	}}}
	input, _, err := responses.ConvertPromptToInputWithOptions(types.Prompt{Messages: msgs}, "system", responses.ConvertOptions{
		Store: true, ProviderOptionsName: "openai", ToolSearchToolName: "tool_search",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(input)
	if want := `[{"id":"tsc_hosted_123","type":"item_reference"},{"id":"tso_hosted_456","type":"item_reference"}]`; string(b) != want {
		t.Fatalf("input =\n%s\nwant\n%s", b, want)
	}
}
