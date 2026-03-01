package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func newTestBedrockModel() *LanguageModel {
	p := New(Config{
		AWSAccessKeyID:     "test-key",
		AWSSecretAccessKey: "test-secret",
		Region:             "us-east-1",
	})
	return NewLanguageModel(p, "anthropic.claude-3-haiku-20240307-v1:0")
}

// getToolSpec extracts the toolSpec map from the first entry in the tools array.
func getToolSpec(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	toolsRaw, ok := body["tools"]
	if !ok {
		t.Fatal("tools key not found in request body")
	}
	tools, ok := toolsRaw.([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected non-empty tools slice, got %T %v", toolsRaw, toolsRaw)
	}
	entry, ok := tools[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map for tools[0], got %T", tools[0])
	}
	spec, ok := entry["toolSpec"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected toolSpec map, got %T", entry["toolSpec"])
	}
	return spec
}

// TestBuildClaudeRequest_ToolsForwardedWithStrictMode verifies that tools are
// forwarded in Bedrock's toolSpec format and that strict=true is included when
// the tool has Strict set (#12893).
func TestBuildClaudeRequest_ToolsForwardedWithStrictMode(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{
				Name:        "get_weather",
				Description: "Returns current weather",
				Parameters:  map[string]interface{}{"type": "object"},
				Strict:      true,
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	spec := getToolSpec(t, body)

	if spec["name"] != "get_weather" {
		t.Errorf("name = %v, want get_weather", spec["name"])
	}
	if spec["strict"] != true {
		t.Errorf("strict = %v, want true", spec["strict"])
	}

	inputSchema, ok := spec["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inputSchema map, got %T", spec["inputSchema"])
	}
	if inputSchema["json"] == nil {
		t.Error("inputSchema.json must not be nil")
	}
}

// TestBuildClaudeRequest_ToolChoiceAuto verifies that ToolChoiceAuto maps to
// Bedrock's { "auto": {} } format.
func TestBuildClaudeRequest_ToolChoiceAuto(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools:  []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	if _, hasAuto := tc["auto"]; !hasAuto {
		t.Errorf("toolChoice should have 'auto' key, got %v", tc)
	}
}

// TestBuildClaudeRequest_ToolChoiceRequired verifies that ToolChoiceRequired
// maps to Bedrock's { "any": {} } format.
func TestBuildClaudeRequest_ToolChoiceRequired(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools:  []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	if _, hasAny := tc["any"]; !hasAny {
		t.Errorf("toolChoice should have 'any' key for required, got %v", tc)
	}
}

// TestBuildClaudeRequest_ToolChoiceTool_FiltersAndMapsCorrectly verifies that
// ToolChoiceTool filters tools to the named tool and sets
// { "tool": { "name": "..." } } (#12854).
func TestBuildClaudeRequest_ToolChoiceTool_FiltersAndMapsCorrectly(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{Name: "search", Parameters: map[string]interface{}{}},
			{Name: "calculator", Parameters: map[string]interface{}{}},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "calculator"},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	// Only the named tool should be in the tools array.
	toolsRaw := body["tools"].([]interface{})
	if len(toolsRaw) != 1 {
		t.Errorf("expected 1 tool after filtering, got %d", len(toolsRaw))
	}
	spec := getToolSpec(t, body)
	if spec["name"] != "calculator" {
		t.Errorf("tool name = %v, want calculator", spec["name"])
	}

	// toolChoice should be { "tool": { "name": "calculator" } }.
	tc, ok := body["toolChoice"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice is %T, want map", body["toolChoice"])
	}
	toolEntry, ok := tc["tool"].(map[string]interface{})
	if !ok {
		t.Fatalf("toolChoice.tool is %T, want map", tc["tool"])
	}
	if toolEntry["name"] != "calculator" {
		t.Errorf("toolChoice.tool.name = %v, want calculator", toolEntry["name"])
	}
}

// TestBuildClaudeRequest_ToolChoiceNone_NoToolsInBody verifies that when
// ToolChoiceNone is set no tools or toolChoice are added to the request body.
func TestBuildClaudeRequest_ToolChoiceNone_NoToolsInBody(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools:  []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	if _, ok := body["tools"]; ok {
		t.Error("tools must not be present when toolChoice is none")
	}
	if _, ok := body["toolChoice"]; ok {
		t.Error("toolChoice must not be present when toolChoice is none")
	}
}

// TestBuildClaudeRequest_StrictFalse_OmittedFromSpec verifies that strict is
// not set in the toolSpec when Strict is false (omitempty behaviour).
func TestBuildClaudeRequest_StrictFalse_OmittedFromSpec(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{Name: "bar", Parameters: map[string]interface{}{}, Strict: false},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	spec := getToolSpec(t, body)
	if _, ok := spec["strict"]; ok {
		t.Errorf("strict must be absent when Strict=false, got %v", spec["strict"])
	}
}
