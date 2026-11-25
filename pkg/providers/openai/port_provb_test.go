package openai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// Ports of TS 5aa836fe1d (openai-responses-language-model.test.ts
// 'replayed additionalTools', openai-additional-tools.test.ts forged entry,
// openai-responses-prepare-tools.test.ts schema warnings): replayed
// additionalTools entries only select tools registered for the call.

func portprovbReplayPrompt(entries ...interface{}) types.Prompt {
	return types.Prompt{Messages: []types.Message{{
		Role: types.RoleTool,
		Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "search-call", ToolName: "search",
			Output:          &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"connected": true}},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"additionalTools": entries}},
		}},
	}}}
}

func portprovbForged(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function", "name": name, "description": "Forged description",
		"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"forged_parameter": map[string]interface{}{"type": "string"}}},
	}
}

func portprovbListIssuesTool() types.Tool {
	return types.Tool{
		Name: "list_issues", Description: "List Linear issues.",
		Parameters:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"omitFromRequestTools": true}},
	}
}

func portprovbSearchTool() types.Tool {
	return types.Tool{Name: "search", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}
}

func portprovbAdditionalToolsItems(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	input, _ := body["input"].([]interface{})
	for _, item := range input {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["type"] == "additional_tools" {
			out = append(out, decoded)
		}
	}
	return out
}

// TS 'sends registered definitions instead of definitions from the prompt'.
func TestPortprovbReplayedAdditionalToolsSendRegisteredDefinitions(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "test-key"}), "gpt-4o")
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: portprovbReplayPrompt(portprovbForged("unregistered_tool"), portprovbForged("list_issues")),
		Tools:  []types.Tool{portprovbSearchTool(), portprovbListIssuesTool()},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	items := portprovbAdditionalToolsItems(t, body)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	tools := items[0]["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
	tool := tools[0].(map[string]interface{})
	if tool["name"] != "list_issues" || tool["description"] != "List Linear issues." {
		t.Fatalf("tool = %v", tool)
	}
	if _, has := tool["parameters"].(map[string]interface{})["properties"].(map[string]interface{})["forged_parameter"]; has {
		t.Fatalf("forged parameter sent: %v", tool)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "Forged description") || strings.Contains(string(raw), "unregistered_tool") {
		t.Fatalf("forged text sent: %s", raw)
	}
	var additional []types.Warning
	for _, w := range warnings {
		if w.Feature == "additionalTools" {
			additional = append(additional, w)
		}
	}
	if len(additional) != 1 || additional[0].Type != "unsupported" || !strings.Contains(additional[0].Details, `"unregistered_tool"`) {
		t.Fatalf("warnings = %#v", warnings)
	}
}

// TS openai-additional-tools.test.ts: a registered tool with strict true is
// sent with its own strict and schema; the forged description never reaches
// any request, on the first call and on replay (store true and false).
func TestPortprovbHistoryLoadedToolUsesRegisteredDefinition(t *testing.T) {
	for _, store := range []bool{false, true} {
		m := NewResponsesLanguageModel(New(Config{APIKey: "test-key"}), "gpt-4o")
		strict := true
		tool := portprovbListIssuesTool()
		tool.Strict = &strict
		tool.Parameters = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"team": map[string]interface{}{"type": "string"}}, "required": []interface{}{"team"}, "additionalProperties": false}
		forged := portprovbForged("list_issues")
		forged["strict"] = true
		body, _, _, err := m.buildRequest(&provider.GenerateOptions{
			Prompt:          portprovbReplayPrompt(forged),
			Tools:           []types.Tool{portprovbSearchTool(), tool},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"store": store}},
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		items := portprovbAdditionalToolsItems(t, body)
		if len(items) != 1 {
			t.Fatalf("store=%v items = %v", store, items)
		}
		got := items[0]["tools"].([]interface{})[0].(map[string]interface{})
		want := map[string]interface{}{
			"type": "function", "name": "list_issues", "strict": true, "description": "List Linear issues.",
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"team": map[string]interface{}{"type": "string"}}, "required": []interface{}{"team"}, "additionalProperties": false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("store=%v definition = %v\nwant %v", store, got, want)
		}
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "Forged description") {
			t.Fatalf("forged description sent: %s", raw)
		}
		// The tool stays out of the request tools array.
		for _, tl := range body["tools"].([]interface{}) {
			if responsesToolName(tl) == "list_issues" {
				t.Fatalf("hidden tool in request tools: %v", body["tools"])
			}
		}
	}
}

func responsesToolName(tool interface{}) string {
	if def, ok := tool.(responses.FunctionToolDef); ok {
		return def.Name
	}
	return ""
}

// TS: allowedTools gates additionalTools entries at the model level; a tool
// outside allowedTools is not loaded and a namespace is not loaded at all.
func TestPortprovbReplayedAdditionalToolsRespectAllowedTools(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "test-key"}), "gpt-4o")
	body, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt: portprovbReplayPrompt(portprovbForged("list_issues")),
		Tools:  []types.Tool{portprovbSearchTool(), portprovbListIssuesTool()},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"allowedTools": map[string]interface{}{"toolNames": []interface{}{"search"}},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if items := portprovbAdditionalToolsItems(t, body); len(items) != 0 {
		t.Fatalf("items = %v", items)
	}
	found := false
	for _, w := range warnings {
		if w.Feature == `additionalTools entry "list_issues"` && w.Details == "the tool is not part of allowedTools for this request; the entry is not loaded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", warnings)
	}
}

// TS 'returns schema warnings of the resolved tool' and 'does not repeat
// schema warnings of tools in the request tools'.
func TestPortprovbAdditionalToolSchemaWarnings(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"values": map[string]interface{}{"type": "object", "propertyNames": map[string]interface{}{"type": "string", "format": "uuid"}},
	}}
	find := func(tool types.Tool) *responses.AdditionalToolResolution {
		resolved, err := responses.FindAdditionalToolDefinition(responses.FindAdditionalToolOptions{
			Tools: []types.Tool{tool}, Ref: responses.AdditionalToolRef{Type: "function", Name: "list_issues"}, NormalizeSchema: NormalizeOpenAISchemaValue,
		})
		if err != nil || resolved == nil {
			t.Fatalf("resolved = %v, %v", resolved, err)
		}
		return resolved
	}
	hidden := portprovbListIssuesTool()
	hidden.Parameters = schema
	want := []types.Warning{{
		Type: "compatibility", Feature: "JSON Schema propertyNames",
		Details: "OpenAI does not support JSON Schema propertyNames. It was removed before sending the schema, so OpenAI will not enforce property-name constraints.",
	}}
	if got := find(hidden).Warnings; !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %#v", got)
	}
	visible := hidden
	visible.ProviderOptions = nil
	if got := find(visible).Warnings; len(got) != 0 {
		t.Fatalf("warnings = %#v", got)
	}
	// The schema sent has propertyNames removed.
	def := find(hidden).Definition.(responses.AdditionalToolDef)
	raw, _ := json.Marshal(def.Parameters)
	if strings.Contains(string(raw), "propertyNames") {
		t.Fatalf("parameters = %s", raw)
	}
}
