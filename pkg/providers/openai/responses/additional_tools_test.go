package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func additionalToolsResult(providerOptions map[string]interface{}) types.Message {
	return types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
		ToolCallID:      "call_search",
		ToolName:        "connection_search",
		Output:          &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"tool": "linear__list_issues"}},
		ProviderOptions: providerOptions,
	}}}
}

var discoveredTool = map[string]interface{}{
	"type": "function", "name": "linear__list_issues", "description": "List Linear issues.",
	"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false},
}

// TS #23291: an additional_tools item follows the function_call_output.
func TestConvertToolItems_AdditionalToolsFollowResult(t *testing.T) {
	items, err := convertToolItems(additionalToolsResult(map[string]interface{}{
		"openai": map[string]interface{}{"additionalTools": []interface{}{discoveredTool}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if _, ok := items[0].(FunctionCallOutputItem); !ok {
		t.Fatalf("items[0] = %T", items[0])
	}
	raw, _ := json.Marshal(items[1])
	want := `{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"linear__list_issues","description":"List Linear issues.","parameters":{"additionalProperties":false,"properties":{},"type":"object"}}]}`
	if string(raw) != want {
		t.Fatalf("additional_tools = %s\nwant %s", raw, want)
	}
}

func TestConvertToolItems_AdditionalToolsAbsentOrEmpty(t *testing.T) {
	for name, po := range map[string]map[string]interface{}{
		"absent": nil,
		"empty":  {"openai": map[string]interface{}{"additionalTools": []interface{}{}}},
		"other":  {"other": map[string]interface{}{"additionalTools": []interface{}{discoveredTool}}},
	} {
		items, err := convertToolItems(additionalToolsResult(po))
		if err != nil || len(items) != 1 {
			t.Fatalf("%s: items=%d err=%v", name, len(items), err)
		}
	}
}

func TestConvertToolItems_AdditionalToolsValidation(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"wrong type":      {"type": "custom", "name": "x", "parameters": map[string]interface{}{}},
		"empty name":      {"type": "function", "name": "", "parameters": map[string]interface{}{}},
		"no parameters":   {"type": "function", "name": "x"},
		"unknown key":     {"type": "function", "name": "x", "parameters": map[string]interface{}{}, "extra": 1},
		"strict not bool": {"type": "function", "name": "x", "parameters": map[string]interface{}{}, "strict": "yes"},
	}
	for name, entry := range cases {
		_, err := convertToolItems(additionalToolsResult(map[string]interface{}{
			"openai": map[string]interface{}{"additionalTools": []interface{}{entry}},
		}))
		if err == nil || !strings.Contains(err.Error(), "additionalTools") {
			t.Errorf("%s: err = %v, want an additionalTools validation error", name, err)
		}
	}
	if _, err := convertToolItems(additionalToolsResult(map[string]interface{}{
		"openai": map[string]interface{}{"additionalTools": nil},
	})); err == nil {
		t.Error("null additionalTools must be rejected")
	}
}

func omittedTool() types.Tool {
	return types.Tool{
		Name: "linear__list_issues", Description: "List", Parameters: map[string]interface{}{"type": "object"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"omitFromRequestTools": true}},
	}
}

// TS #23291: omitFromRequestTools keeps the tool out of the request tools array.
func TestPrepareTools_OmitFromRequestTools(t *testing.T) {
	normal := types.Tool{Name: "connection_search", Parameters: map[string]interface{}{"type": "object"}}
	got, err := PrepareToolsWithError([]types.Tool{normal, omittedTool()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].(FunctionToolDef).Name != "connection_search" {
		t.Fatalf("prepared tools = %#v", got)
	}
	notOmitted := omittedTool()
	notOmitted.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"omitFromRequestTools": false}}
	if got, _ := PrepareToolsWithError([]types.Tool{notOmitted}); len(got) != 1 {
		t.Fatalf("omitFromRequestTools=false must keep the tool: %#v", got)
	}
}

func TestCheckOmittedToolChoice(t *testing.T) {
	tools := []types.Tool{omittedTool()}
	if err := CheckOmittedToolChoice(tools, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "linear__list_issues"}); err == nil ||
		!strings.Contains(err.Error(), "forcing tool \"linear__list_issues\" omitted from the request tools array; use automatic tool choice for history-loaded tools") {
		t.Fatalf("err = %v", err)
	}
	if err := CheckOmittedToolChoice(tools, types.ToolChoice{Type: types.ToolChoiceRequired}); err != nil {
		t.Fatal(err)
	}
	if err := CheckOmittedToolChoice(tools, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "other"}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAllowedTools_OmittedToolIsUnsupported(t *testing.T) {
	normal := types.Tool{Name: "connection_search", Parameters: map[string]interface{}{"type": "object"}}
	choice, warnings, err := ResolveAllowedTools([]types.Tool{normal, omittedTool()}, []string{"connection_search", "linear__list_issues"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(choice.Tools) != 1 || choice.Tools[0].Name != "connection_search" {
		t.Fatalf("entries = %#v", choice.Tools)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Details, "tools omitted from the request tools array cannot be allow-listed") {
		t.Fatalf("warnings = %#v", warnings)
	}
	if _, _, err := ResolveAllowedTools([]types.Tool{omittedTool()}, []string{"linear__list_issues"}, ""); err == nil {
		t.Fatal("expected error when every allowed tool is omitted")
	}
}
