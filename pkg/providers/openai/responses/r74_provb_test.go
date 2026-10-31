package responses

import (
	"encoding/json"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// TestR74ProvbToolArgsTypeValidationText: TS prepareResponsesTools calls
// validateTypes without a context, so invalid provider tool args throw
// TypeValidationError "Type validation failed: Value: <JSON.stringify(args)>.
// \nError message: <zod issues>". The mcpArgsSchema refine issue is zod's
// custom issue exactly (zod run on the TS schema); v0.5/r73 Go said
// "validation error on field 'tools': Type validation failed for <tool>: ...".
func TestR74ProvbToolArgsTypeValidationText(t *testing.T) {
	_, err := PrepareToolsWithError([]types.Tool{openaitool.MCP(openaitool.MCPConfig{ServerLabel: "x"})})
	want := "Type validation failed: Value: {\"serverLabel\":\"x\"}.\nError message: [\n  {\n    \"code\": \"custom\",\n    \"path\": [],\n    \"message\": \"One of serverUrl or connectorId must be provided.\"\n  }\n]"
	var tv *providererrors.TypeValidationError
	if !errors.As(err, &tv) || err.Error() != want {
		t.Fatalf("mcp refine err =\n%v\nwant\n%s", err, want)
	}
	// A hand-built tool (typed config only) reports its config as the value.
	_, err = PrepareToolsWithError([]types.Tool{{Name: "openai.mcp", ProviderOptions: openaitool.MCPConfig{ServerLabel: "x"}}})
	if err == nil || err.Error() != want {
		t.Fatalf("hand-built mcp refine err =\n%v\nwant\n%s", err, want)
	}

	_, err = PrepareToolsWithError([]types.Tool{openaitool.WebSearch(openaitool.WebSearchConfig{SearchContextSize: "huge"})})
	const prefix = "Type validation failed: Value: {\"searchContextSize\":\"huge\"}.\nError message: "
	if !errors.As(err, &tv) || len(err.Error()) <= len(prefix) || err.Error()[:len(prefix)] != prefix {
		t.Fatalf("web search err = %v, want prefix %q", err, prefix)
	}
}

// TestR74ProvbTSShapedShellEnvironment: a TS-shaped openai.shell tool
// (e.g. restored from a workflow) is mapped as TS mapShellEnvironment does:
// a skillReference resolves providerReference.openai (TS
// resolveProviderReference throws NoSuchProviderReferenceError without it;
// Go sent a skill without skill_id), and a disabled network policy is sent as
// {type: "disabled"} only.
func TestR74ProvbTSShapedShellEnvironment(t *testing.T) {
	shell := func(env map[string]interface{}) types.Tool {
		return types.Tool{Name: "shell", Type: types.ToolTypeProviderDefined, ProviderID: "openai.shell", ProviderArgs: map[string]interface{}{"environment": env}}
	}
	_, err := PrepareToolsWithError([]types.Tool{shell(map[string]interface{}{
		"type":   "containerAuto",
		"skills": []interface{}{map[string]interface{}{"type": "skillReference", "providerReference": map[string]interface{}{"anthropic": "sk_a"}}},
	})})
	var nsr *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &nsr) || err.Error() != "No provider reference found for provider 'openai'. Available providers: anthropic" {
		t.Fatalf("missing openai reference err = %v", err)
	}

	got, err := PrepareToolsWithError([]types.Tool{shell(map[string]interface{}{
		"type":          "containerAuto",
		"networkPolicy": map[string]interface{}{"type": "disabled", "allowedDomains": []interface{}{"x.com"}},
		"skills":        []interface{}{map[string]interface{}{"type": "skillReference", "providerReference": map[string]interface{}{"openai": "sk_1"}}},
	})})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got[0])
	want := `{"type":"shell","environment":{"type":"container_auto","network_policy":{"type":"disabled"},"skills":[{"type":"skill_reference","skill_id":"sk_1","version":"latest"}]}}`
	if string(b) != want {
		t.Fatalf("shell tool =\n%s\nwant\n%s", b, want)
	}
}

// TestR74ProvbAllowedToolsAliasesAndUnknownProviderTools: TS records an
// allowedTools alias for every provider tool it sends, from the
// providerToolNames table and whether or not the tool can be allow-listed
// (a tool_search tool named "my_search" is reachable as "tool_search" and
// dropped as unsupported); a provider tool with an id TS does not send is not
// recorded at all, so its name is "not part of the tools for this request".
func TestR74ProvbAllowedToolsAliasesAndUnknownProviderTools(t *testing.T) {
	fn := types.Tool{Name: "get_weather", Parameters: map[string]interface{}{"type": "object"}}
	search := types.Tool{Name: "my_search", Type: types.ToolTypeProviderDefined, ProviderID: "openai.tool_search", ProviderArgs: map[string]interface{}{}}
	choice, warnings, err := ResolveAllowedTools([]types.Tool{fn, search}, []string{"get_weather", "tool_search"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Name != "get_weather" || len(warnings) != 1 ||
		warnings[0].Details != "OpenAI does not support tool_search tools in tool_choice.allowed_tools; the tool is removed from the allowed tools" {
		t.Fatalf("alias: choice=%#v warnings=%#v", choice, warnings)
	}

	other := types.Tool{Name: "other.thing", Type: types.ToolTypeProviderDefined, ProviderID: "other.thing"}
	choice, warnings, err = ResolveAllowedTools([]types.Tool{fn, other}, []string{"other.thing"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Type != "function" || choice.Tools[0].Name != "other.thing" || len(warnings) != 1 ||
		warnings[0].Details != "the tool is not part of the tools for this request and is sent as a function tool" {
		t.Fatalf("unknown provider tool: choice=%#v warnings=%#v", choice, warnings)
	}
}

// TestR74ProvbExpandParallelToolCall ports expand-parallel-tool-call.test.ts,
// plus the key order of a nested call's input: TS JSON.stringify(parameters)
// keeps the model's key order.
func TestR74ProvbExpandParallelToolCall(t *testing.T) {
	tools := []types.Tool{
		{Name: "weather", Parameters: map[string]interface{}{"type": "object"}},
		{Name: "cityAttractions", Parameters: map[string]interface{}{"type": "object"}},
	}
	input := `{"tool_uses":[{"recipient_name":"functions.weather","parameters":{"location":"San Francisco"}},{"recipient_name":"functions.cityAttractions","parameters":{"city":"Rome"}}]}`
	calls, ok := ExpandParallelToolCall("call_parallel", "parallel", input, "fc_parallel", tools, "openai")
	if !ok || len(calls) != 2 {
		t.Fatalf("expand = %v, %v", calls, ok)
	}
	b, _ := json.Marshal([]interface{}{
		[]interface{}{calls[0].ID, calls[0].ToolName, calls[0].RawArguments, calls[0].ProviderMetadata},
		[]interface{}{calls[1].ID, calls[1].ToolName, calls[1].RawArguments, calls[1].ProviderMetadata},
	})
	meta := func(i int) string {
		return `{"openai":{"parallelToolCall":{"itemId":"fc_parallel","toolCallId":"call_parallel","toolName":"parallel","input":` + r74provbJSONString(input) + `,"index":` + string(rune('0'+i)) + `,"count":2}}}`
	}
	want := `[["call_parallel_0","weather","{\"location\":\"San Francisco\"}",` + meta(0) + `],["call_parallel_1","cityAttractions","{\"city\":\"Rome\"}",` + meta(1) + `]]`
	var g, w interface{}
	_ = json.Unmarshal(b, &g)
	_ = json.Unmarshal([]byte(want), &w)
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("expanded =\n%s\nwant\n%s", gb, wb)
	}

	if _, ok := ExpandParallelToolCall("call_parallel", "parallel", `{"tool_uses":[{"recipient_name":"functions.weather","parameters":{"location":"Rome"}}]}`, "fc_parallel",
		append(append([]types.Tool{}, tools...), types.Tool{Name: "parallel", Parameters: map[string]interface{}{"type": "object"}}), "openai"); ok {
		t.Error("a declared tool named parallel was expanded")
	}
	for name, in := range map[string]string{
		"invalid JSON":          "not JSON",
		"an empty wrapper":      `{"tool_uses":[]}`,
		"an unavailable tool":   `{"tool_uses":[{"recipient_name":"functions.unknown","parameters":{}}]}`,
		"no functions prefix":   `{"tool_uses":[{"recipient_name":"weather","parameters":{}}]}`,
		"non-object parameters": `{"tool_uses":[{"recipient_name":"functions.weather","parameters":null}]}`,
	} {
		if _, ok := ExpandParallelToolCall("call_parallel", "parallel", in, "fc_parallel", tools, "openai"); ok {
			t.Errorf("does not expand %s: expanded", name)
		}
	}

	calls, ok = ExpandParallelToolCall("c", "parallel", `{"tool_uses":[{"recipient_name":"functions.weather","parameters":{"zip":"1","city":"Rome","1":true}}]}`, "fc", tools, "openai")
	if !ok || calls[0].RawArguments != `{"1":true,"zip":"1","city":"Rome"}` {
		t.Fatalf("nested input = %q, want the model's key order (JS integer keys first)", calls[0].RawArguments)
	}
}

func r74provbJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
