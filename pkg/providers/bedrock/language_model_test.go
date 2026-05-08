package bedrock

import (
	"net/http"
	"os"
	"strings"
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

func TestAWSSignerExplicitKeysDoNotUseEnvSessionToken(t *testing.T) {
	t.Setenv("AWS_SESSION_TOKEN", "env-session-token")

	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	signer := NewAWSSigner("explicit-key", "explicit-secret", "", "us-east-1")
	if err := signer.SignRequest(req, []byte("{}")); err != nil {
		t.Fatalf("SignRequest error = %v", err)
	}
	if got := req.Header.Get("X-Amz-Security-Token"); got != "" {
		t.Fatalf("X-Amz-Security-Token = %q, want empty despite AWS_SESSION_TOKEN=%q", got, os.Getenv("AWS_SESSION_TOKEN"))
	}
}

func TestBuildClaudeRequest_DropsUnsignedReasoningBlocks(t *testing.T) {
	model := newTestBedrockModel()
	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleAssistant,
				Content: []types.ContentPart{
					types.TextContent{Text: "visible"},
					types.ReasoningContent{Text: "foreign reasoning without signature"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest error = %v", err)
	}
	messages := body["messages"].([]map[string]interface{})
	if messages[0]["content"] != "visible" {
		t.Fatalf("content = %#v, want only visible text", messages[0]["content"])
	}
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
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
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
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
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
		Prompt:     types.Prompt{Text: "hello"},
		Tools:      []types.Tool{{Name: "foo", Parameters: map[string]interface{}{}}},
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

func TestBuildClaudeRequest_AnthropicToolSearchProviderTools(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		Tools: []types.Tool{
			{
				Type:       types.ToolTypeProviderDefined,
				ProviderID: "anthropic.tool_search_bm25_20251119",
				Name:       "tool_search",
			},
			{
				Type:       types.ToolTypeProviderDefined,
				ProviderID: "anthropic.tool_search_regex_20251119",
				Name:       "tool_search",
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v, want 2 provider tools", body["tools"])
	}
	first := tools[0].(map[string]interface{})
	second := tools[1].(map[string]interface{})
	if first["type"] != "tool_search_tool_bm25_20251119" || first["name"] != "tool_search_tool_bm25" {
		t.Errorf("bm25 tool = %#v", first)
	}
	if second["type"] != "tool_search_tool_regex_20251119" || second["name"] != "tool_search_tool_regex" {
		t.Errorf("regex tool = %#v", second)
	}
}

func TestBuildClaudeRequest_ServiceTier(t *testing.T) {
	model := newTestBedrockModel()
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ProviderOptions: map[string]interface{}{
			"bedrock": map[string]interface{}{"serviceTier": "priority"},
		},
	}

	body, err := model.buildClaudeRequest(opts)
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	serviceTier, ok := body["serviceTier"].(map[string]interface{})
	if !ok {
		t.Fatalf("serviceTier = %#v, want map", body["serviceTier"])
	}
	if serviceTier["type"] != "priority" {
		t.Errorf("serviceTier.type = %v, want priority", serviceTier["type"])
	}
}

func TestBuildClaudeRequest_PartialReasoningConfigMerge(t *testing.T) {
	p := New(Config{AWSAccessKeyID: "test-key", AWSSecretAccessKey: "test-secret", Region: "us-east-1"})
	model := NewLanguageModel(p, "anthropic.claude-3-5-sonnet-20241022-v2:0", &ModelOptions{
		ReasoningConfig: &ReasoningConfig{Display: "summarized"},
	})
	level := types.ReasoningHigh

	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hello"},
		Reasoning: &level,
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	rc, ok := body["reasoningConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("reasoningConfig = %#v, want map", body["reasoningConfig"])
	}
	if rc["type"] != "enabled" || rc["budgetTokens"] != 16000 || rc["display"] != "summarized" {
		t.Errorf("reasoningConfig = %#v, want derived type/budget plus display", rc)
	}
}

func TestBuildClaudeRequest_OutputObjectSupport(t *testing.T) {
	model := newTestBedrockModel()
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	}

	body, err := model.buildClaudeRequest(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	})
	if err != nil {
		t.Fatalf("buildClaudeRequest: %v", err)
	}

	outputConfig, ok := body["output_config"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config = %#v, want map", body["output_config"])
	}
	format, ok := outputConfig["format"].(map[string]interface{})
	if !ok {
		t.Fatalf("output_config.format = %#v, want map", outputConfig["format"])
	}
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("format = %#v, want json_schema with schema", format)
	}
}
