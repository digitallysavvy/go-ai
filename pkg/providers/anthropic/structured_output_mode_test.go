package anthropic

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// testSchema is a reusable JSON schema for structured output tests.
var testSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"name": map[string]interface{}{"type": "string"},
		"age":  map[string]interface{}{"type": "integer"},
	},
	"required": []string{"name"},
}

// testOpts returns a minimal GenerateOptions with a JSON ResponseFormat using testSchema.
func testOpts() *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "Return JSON"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: testSchema},
	}
}

// --- SOM-T09: auto mode on a model where SupportsStructuredOutput=true ---

// TestAutoModeOnSupportedModel verifies that auto mode (default) on claude-sonnet-4-6
// uses output_config.format because SupportsStructuredOutput() returns true.
func TestAutoModeOnSupportedModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	// claude-sonnet-4-6 supports native structured output
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	body := model.buildRequestBody(testOpts(), false)

	// Must have output_config.format
	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config should be present for auto mode on supported model")
	}
	formatVal, ok := outputConfig["format"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config.format should be set")
	}
	if formatVal["type"] != "json_schema" {
		t.Errorf("output_config.format.type = %v, want json_schema", formatVal["type"])
	}

	// Must NOT inject a synthetic json tool
	if tools, ok := body["tools"]; ok {
		toolSlice, _ := tools.([]map[string]interface{})
		for _, tool := range toolSlice {
			if tool["name"] == "json" {
				t.Error("auto mode on supported model must not inject synthetic json tool")
			}
		}
	}
	// Must NOT set tool_choice to the json tool
	if tc, ok := body["tool_choice"].(map[string]interface{}); ok {
		if tc["name"] == "json" {
			t.Error("auto mode on supported model must not set tool_choice to json tool")
		}
	}
}

// --- SOM-T10: auto mode on a model where SupportsStructuredOutput=false ---

// TestAutoModeOnUnsupportedModel verifies that auto mode (default) on
// claude-sonnet-4-20250514 falls back to jsonTool mode because
// SupportsStructuredOutput() returns false for that model.
func TestAutoModeOnUnsupportedModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	// claude-sonnet-4-20250514 does NOT support native structured output
	model := NewLanguageModel(prov, ClaudeSonnet4_20250514, nil)

	body := model.buildRequestBody(testOpts(), false)

	// Must NOT set output_config.format (though output_config may exist for other reasons)
	if outputConfig, ok := body["output_config"].(map[string]interface{}); ok {
		if _, hasFormat := outputConfig["format"]; hasFormat {
			t.Error("auto mode on unsupported model must not set output_config.format")
		}
	}

	// Must inject the synthetic json tool
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("auto mode on unsupported model must inject synthetic json tool")
	}
	var found bool
	for _, tool := range tools {
		if tool["name"] == "json" {
			found = true
			if tool["description"] != "Respond with a JSON object." {
				t.Errorf("json tool description = %v, want 'Respond with a JSON object.'", tool["description"])
			}
			if tool["input_schema"] == nil {
				t.Error("json tool input_schema must not be nil")
			}
		}
	}
	if !found {
		t.Error("synthetic 'json' tool not found in tools list")
	}

	// tool_choice must be {type:"any", disable_parallel_tool_use:true}
	// (matches TypeScript prepareTools({toolChoice:{type:'required'}, disableParallelToolUse:true}))
	tc, ok := body["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatal("tool_choice must be set in jsonTool mode")
	}
	if tc["type"] != "any" {
		t.Errorf("tool_choice.type = %v, want any", tc["type"])
	}
	if tc["disable_parallel_tool_use"] != true {
		t.Errorf("tool_choice.disable_parallel_tool_use = %v, want true", tc["disable_parallel_tool_use"])
	}
}

// --- SOM-T11: explicit outputFormat mode always uses output_config.format ---

// TestExplicitOutputFormatMode verifies that StructuredOutputFormat mode always uses
// output_config.format even on models that would otherwise fall back to jsonTool.
func TestExplicitOutputFormatMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	// Use an older model that doesn't support native structured output
	model := NewLanguageModel(prov, ClaudeSonnet4_20250514, &ModelOptions{
		StructuredOutputMode: StructuredOutputFormat,
	})

	body := model.buildRequestBody(testOpts(), false)

	// Must have output_config.format
	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config must be present in explicit outputFormat mode")
	}
	formatVal, ok := outputConfig["format"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config.format must be set in explicit outputFormat mode")
	}
	if formatVal["type"] != "json_schema" {
		t.Errorf("output_config.format.type = %v, want json_schema", formatVal["type"])
	}

	// Must NOT inject synthetic json tool
	if tools, ok := body["tools"].([]map[string]interface{}); ok {
		for _, tool := range tools {
			if tool["name"] == "json" {
				t.Error("explicit outputFormat mode must not inject synthetic json tool")
			}
		}
	}
}

// TestExplicitOutputFormatModeOnSupportedModel verifies outputFormat mode
// also works correctly on a supported model (no regression).
func TestExplicitOutputFormatModeOnSupportedModel(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		StructuredOutputMode: StructuredOutputFormat,
	})

	body := model.buildRequestBody(testOpts(), false)

	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config must be present in explicit outputFormat mode")
	}
	if _, hasFormat := outputConfig["format"]; !hasFormat {
		t.Error("output_config.format must be set in explicit outputFormat mode")
	}
}

// --- SOM-T12: explicit jsonTool mode always injects synthetic tool ---

// TestExplicitJSONToolMode verifies that StructuredOutputJSONTool mode always injects
// the synthetic json tool even on models that support native structured output.
func TestExplicitJSONToolMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	// Use a model that supports native structured output — jsonTool should still be used
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		StructuredOutputMode: StructuredOutputJSONTool,
	})

	body := model.buildRequestBody(testOpts(), false)

	// Must NOT use output_config.format
	if outputConfig, ok := body["output_config"].(map[string]interface{}); ok {
		if _, hasFormat := outputConfig["format"]; hasFormat {
			t.Error("explicit jsonTool mode must not set output_config.format")
		}
	}

	// Must inject the synthetic json tool
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("explicit jsonTool mode must inject synthetic json tool")
	}
	var found bool
	for _, tool := range tools {
		if tool["name"] == "json" {
			found = true
			if tool["input_schema"] == nil {
				t.Error("synthetic json tool input_schema must not be nil")
			}
		}
	}
	if !found {
		t.Error("synthetic 'json' tool not found in tools list")
	}

	// tool_choice must be {type:"any", disable_parallel_tool_use:true}
	tc, ok := body["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatal("tool_choice must be set in explicit jsonTool mode")
	}
	if tc["type"] != "any" {
		t.Errorf("tool_choice.type = %v, want any", tc["type"])
	}
	if tc["disable_parallel_tool_use"] != true {
		t.Errorf("tool_choice.disable_parallel_tool_use = %v, want true", tc["disable_parallel_tool_use"])
	}
}

// TestExplicitJSONToolModeAppendsToExistingTools verifies that in jsonTool mode,
// the synthetic json tool is appended to any existing user-provided tools rather
// than replacing them.
func TestExplicitJSONToolModeAppendsToExistingTools(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		StructuredOutputMode: StructuredOutputJSONTool,
	})

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Return JSON"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: testSchema,
		},
		Tools: []types.Tool{
			{
				Name:        "calculator",
				Description: "A simple calculator",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
	}

	body := model.buildRequestBody(opts, false)

	tools, ok := body["tools"].([]map[string]interface{})
	if !ok {
		t.Fatal("tools must be present")
	}

	// Should have both the user tool and the synthetic json tool
	hasCalculator := false
	hasJSON := false
	for _, tool := range tools {
		if tool["name"] == "calculator" {
			hasCalculator = true
		}
		if tool["name"] == "json" {
			hasJSON = true
		}
	}
	if !hasCalculator {
		t.Error("user-provided calculator tool must be preserved in jsonTool mode")
	}
	if !hasJSON {
		t.Error("synthetic json tool must be appended to existing tools")
	}
}

// --- SOM-T13: jsonTool response extraction ---

// TestJSONToolResponseExtraction verifies that a tool_use block named 'json'
// is extracted as the text result and does NOT appear in result.ToolCalls.
func TestJSONToolResponseExtraction(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_20250514, nil)

	input := map[string]interface{}{
		"name": "Alice",
		"age":  float64(30),
	}

	response := anthropicResponse{
		ID:         "msg_test",
		Type:       "message",
		Role:       "assistant",
		StopReason: "tool_use",
		Content: []anthropicContent{
			{
				Type:  "tool_use",
				ID:    "toolu_json_001",
				Name:  "json",
				Input: input,
			},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 20},
	}

	// Pass usesJsonResponseTool=true to simulate a jsonTool mode request.
	result := model.convertResponse(response, true)

	// The tool input must be the text result
	if result.Text == "" {
		t.Fatal("result.Text must be set from json tool input")
	}

	// Verify the JSON content is correct
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Text), &parsed); err != nil {
		t.Fatalf("result.Text is not valid JSON: %v", err)
	}
	if parsed["name"] != "Alice" {
		t.Errorf("parsed.name = %v, want Alice", parsed["name"])
	}
	if parsed["age"] != float64(30) {
		t.Errorf("parsed.age = %v, want 30", parsed["age"])
	}

	// The synthetic json tool must NOT appear in ToolCalls
	for _, tc := range result.ToolCalls {
		if tc.ToolName == "json" {
			t.Error("synthetic json tool must not appear in result.ToolCalls")
		}
	}
}

// TestJSONToolResponseDoesNotAffectRegularToolCalls verifies that regular tool calls
// are unaffected by the json tool response handling.
func TestJSONToolResponseDoesNotAffectRegularToolCalls(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	response := anthropicResponse{
		ID:         "msg_test",
		Type:       "message",
		Role:       "assistant",
		StopReason: "tool_use",
		Content: []anthropicContent{
			{
				Type:  "tool_use",
				ID:    "toolu_regular_001",
				Name:  "calculator",
				Input: map[string]interface{}{"x": 1.0, "y": 2.0},
			},
		},
		Usage: anthropicUsage{InputTokens: 10, OutputTokens: 20},
	}

	result := model.convertResponse(response, false)

	// Regular tool call must appear in ToolCalls
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ToolName != "calculator" {
		t.Errorf("ToolCalls[0].ToolName = %v, want calculator", result.ToolCalls[0].ToolName)
	}
}

// --- SOM-T14: Effort preserved in output_config with outputFormat mode ---

// TestEffortPreservedInOutputFormatMode verifies that the Effort option still appears
// in output_config when outputFormat mode is active.
func TestEffortPreservedInOutputFormatMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Effort: EffortHigh,
	})

	body := model.buildRequestBody(testOpts(), false)

	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config must be present when Effort and ResponseFormat are both set")
	}

	// Both format and effort must be in the same output_config object
	if _, hasFormat := outputConfig["format"]; !hasFormat {
		t.Error("output_config.format must be present alongside Effort")
	}
	if outputConfig["effort"] != string(EffortHigh) {
		t.Errorf("output_config.effort = %v, want %v", outputConfig["effort"], string(EffortHigh))
	}
}

// TestEffortWithoutResponseFormat verifies Effort alone still produces output_config.effort.
func TestEffortWithoutResponseFormat(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Effort: EffortMedium,
	})

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
	}

	body := model.buildRequestBody(opts, false)

	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config must be present when Effort is set")
	}
	if outputConfig["effort"] != string(EffortMedium) {
		t.Errorf("output_config.effort = %v, want %v", outputConfig["effort"], string(EffortMedium))
	}
	if _, hasFormat := outputConfig["format"]; hasFormat {
		t.Error("output_config.format must not be present when ResponseFormat is nil")
	}
}

// TestEffortPreservedInJSONToolMode verifies that Effort still appears in output_config
// even when jsonTool mode is active (they don't interfere with each other).
func TestEffortPreservedInJSONToolMode(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_20250514, &ModelOptions{
		Effort:               EffortHigh,
		StructuredOutputMode: StructuredOutputJSONTool,
	})

	body := model.buildRequestBody(testOpts(), false)

	// output_config must have effort
	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatal("output_config must be present when Effort is set")
	}
	if outputConfig["effort"] != string(EffortHigh) {
		t.Errorf("output_config.effort = %v, want %v", outputConfig["effort"], string(EffortHigh))
	}
	// output_config must NOT have format (jsonTool mode)
	if _, hasFormat := outputConfig["format"]; hasFormat {
		t.Error("output_config.format must not be set in jsonTool mode")
	}

	// Synthetic json tool must be present
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) == 0 {
		t.Fatal("synthetic json tool must be present in jsonTool mode")
	}
}

// --- SOM-T15: Integration test stub ---

// TestStructuredOutputModeIntegration is a stub for live API testing.
// It only runs when ANTHROPIC_API_KEY is set in the environment.
func TestStructuredOutputModeIntegration(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: ANTHROPIC_API_KEY not configured")
	}

	// This test validates that jsonTool mode works against the real API on an
	// older model. When run, it should produce a valid JSON response without error.
	prov := New(Config{APIKey: apiKey})
	model := NewLanguageModel(prov, ClaudeSonnet4_20250514, &ModelOptions{
		StructuredOutputMode: StructuredOutputJSONTool,
	})

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Return a JSON object with a name field set to 'test'."},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: testSchema,
		},
		MaxTokens: intPtr(256),
	}

	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("DoGenerate failed: %v", err)
	}

	if result.Text == "" {
		t.Error("expected non-empty text result from jsonTool mode")
	}

	// Verify the result is valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Text), &parsed); err != nil {
		t.Errorf("result.Text is not valid JSON: %v (text=%q)", err, result.Text)
	}

	// The synthetic json tool must not appear in ToolCalls
	for _, tc := range result.ToolCalls {
		if tc.ToolName == "json" {
			t.Error("synthetic json tool must not appear in ToolCalls")
		}
	}
}
