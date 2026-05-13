package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestAlibabaLanguageModelMetadataAndCapabilities(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "qwen-plus")
	if model.SpecificationVersion() != "v3" {
		t.Fatalf("SpecificationVersion() = %q", model.SpecificationVersion())
	}
	if model.Provider() != "alibaba" {
		t.Fatalf("Provider() = %q", model.Provider())
	}
	if model.ModelID() != "qwen-plus" {
		t.Fatalf("ModelID() = %q", model.ModelID())
	}
	if !model.SupportsTools() || !model.SupportsStructuredOutput() {
		t.Fatal("qwen-plus should support tools + structured output")
	}
	if model.SupportsImageInput() {
		t.Fatal("qwen-plus should not report image-input support")
	}

	vlModel := NewLanguageModel(New(Config{APIKey: "test-key"}), "qwen-vl-max")
	if !vlModel.SupportsImageInput() {
		t.Fatal("qwen-vl-max should report image-input support")
	}
}

func TestAlibabaLanguageModelBuildRequestBodyBasicFields(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "qwen-plus")

	maxTokens := 128
	temperature := 0.7
	topP := 0.9
	seed := 42
	opts := &provider.GenerateOptions{
		Prompt:        types.Prompt{Text: "hello"},
		MaxTokens:     &maxTokens,
		Temperature:   &temperature,
		TopP:          &topP,
		StopSequences: []string{"END"},
		Seed:          &seed,
	}

	body := model.buildRequestBody(opts, true)

	if body["model"] != "qwen-plus" {
		t.Fatalf("model = %#v", body["model"])
	}
	if body["stream"] != true {
		t.Fatalf("stream = %#v", body["stream"])
	}
	if body["max_tokens"] != 128 {
		t.Fatalf("max_tokens = %#v", body["max_tokens"])
	}
	if body["temperature"] != 0.7 {
		t.Fatalf("temperature = %#v", body["temperature"])
	}
	if body["top_p"] != 0.9 {
		t.Fatalf("top_p = %#v", body["top_p"])
	}
	if body["seed"] != 42 {
		t.Fatalf("seed = %#v", body["seed"])
	}
	if _, ok := body["messages"]; !ok {
		t.Fatalf("expected messages in request body: %#v", body)
	}
}

func TestAlibabaLanguageModelBuildRequestBodyResponseFormatAndTools(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "test-key"}), "qwen-plus")

	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hello"},
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json",
			Schema:      map[string]interface{}{"type": "object"},
			Name:        "Weather",
			Description: "weather response",
		},
		Tools: []types.Tool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]interface{}{"type": "object"},
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "get_weather"},
	}

	body := model.buildRequestBody(opts, false)
	responseFormat, ok := body["response_format"].(map[string]interface{})
	if !ok {
		t.Fatalf("response_format type = %T", body["response_format"])
	}
	if responseFormat["type"] != "json_schema" {
		t.Fatalf("response_format.type = %#v", responseFormat["type"])
	}
	if _, ok := body["tools"]; !ok {
		t.Fatalf("expected tools field in body: %#v", body)
	}
	toolChoice, ok := body["tool_choice"].(map[string]interface{})
	if !ok {
		t.Fatalf("tool_choice type = %T", body["tool_choice"])
	}
	if toolChoice["type"] != "function" {
		t.Fatalf("tool_choice.type = %#v", toolChoice["type"])
	}
}
