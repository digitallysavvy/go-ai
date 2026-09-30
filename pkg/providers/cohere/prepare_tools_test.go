package cohere

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Mirrors TS cohere-prepare-tools.test.ts: "should return undefined tools
// when no tools are provided".
func TestPrepareCohereToolsEmptyReturnsNil(t *testing.T) {
	tools, toolChoice, warnings := prepareCohereTools(nil, types.ToolChoice{}, false)
	if tools != nil || toolChoice != nil || warnings != nil {
		t.Fatalf("expected all nil, got tools=%#v toolChoice=%#v warnings=%#v", tools, toolChoice, warnings)
	}
}

// Mirrors "should process function tools correctly".
func TestPrepareCohereToolsFunction(t *testing.T) {
	tools, toolChoice, warnings := prepareCohereTools([]types.Tool{
		{
			Type:        types.ToolTypeFunction,
			Name:        "testFunction",
			Description: "test description",
			Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}, types.ToolChoice{}, false)

	if toolChoice != nil {
		t.Fatalf("expected toolChoice=nil, got %#v", toolChoice)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", warnings)
	}
	want := []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "testFunction",
				"description": "test description",
				"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
	}
	if !reflect.DeepEqual(tools, want) {
		t.Fatalf("tools = %#v, want %#v", tools, want)
	}
}

// Mirrors "should add warnings for provider-defined tools".
func TestPrepareCohereToolsProviderDefinedWarns(t *testing.T) {
	tools, toolChoice, warnings := prepareCohereTools([]types.Tool{
		{
			Type:       types.ToolTypeProviderDefined,
			ProviderID: "provider.tool",
			Name:       "tool",
		},
	}, types.ToolChoice{}, false)

	if toolChoice != nil {
		t.Fatalf("expected toolChoice=nil, got %#v", toolChoice)
	}
	if len(tools) != 0 {
		t.Fatalf("expected empty tools slice, got %#v", tools)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "provider-defined tool provider.tool" {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}

func basicCohereTool() types.Tool {
	return types.Tool{
		Type:        types.ToolTypeFunction,
		Name:        "testFunction",
		Description: "test description",
		Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

// Mirrors "tool choice handling > should handle auto tool choice".
func TestPrepareCohereToolsChoiceAuto(t *testing.T) {
	_, toolChoice, _ := prepareCohereTools([]types.Tool{basicCohereTool()}, types.ToolChoice{Type: types.ToolChoiceAuto}, true)
	if toolChoice != nil {
		t.Fatalf("toolChoice = %#v, want nil", toolChoice)
	}
}

// Mirrors "tool choice handling > should handle none tool choice".
func TestPrepareCohereToolsChoiceNone(t *testing.T) {
	tools, toolChoice, warnings := prepareCohereTools([]types.Tool{basicCohereTool()}, types.ToolChoice{Type: types.ToolChoiceNone}, true)
	if toolChoice != "NONE" {
		t.Fatalf("toolChoice = %#v, want NONE", toolChoice)
	}
	if len(tools) != 1 || len(warnings) != 0 {
		t.Fatalf("unexpected tools/warnings: %#v %#v", tools, warnings)
	}
}

// Mirrors "tool choice handling > should handle required tool choice".
func TestPrepareCohereToolsChoiceRequired(t *testing.T) {
	tools, toolChoice, _ := prepareCohereTools([]types.Tool{basicCohereTool()}, types.ToolChoice{Type: types.ToolChoiceRequired}, true)
	if toolChoice != "REQUIRED" {
		t.Fatalf("toolChoice = %#v, want REQUIRED", toolChoice)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %#v", tools)
	}
}

// Mirrors "tool choice handling > should handle tool type tool choice by
// filtering tools".
func TestPrepareCohereToolsChoiceNamedToolFilters(t *testing.T) {
	other := basicCohereTool()
	other.Name = "otherFunction"
	tools, toolChoice, _ := prepareCohereTools(
		[]types.Tool{basicCohereTool(), other},
		types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "testFunction"},
		true,
	)
	if toolChoice != "REQUIRED" {
		t.Fatalf("toolChoice = %#v, want REQUIRED", toolChoice)
	}
	if len(tools) != 1 {
		t.Fatalf("expected filtered to 1 tool, got %#v", tools)
	}
	fn := tools[0]["function"].(map[string]interface{})
	if fn["name"] != "testFunction" {
		t.Fatalf("expected filtered tool to be testFunction, got %#v", fn["name"])
	}
}
