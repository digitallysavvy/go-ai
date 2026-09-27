package moonshot

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func newMoonshotTestModel(modelID string) *LanguageModel {
	prov := New(Config{APIKey: "test-key"})
	return NewLanguageModel(prov, modelID)
}

func textPrompt(text string) types.Prompt {
	return types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}},
	}}
}

func wireMessages(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["messages"].([]interface{})
	if !ok {
		t.Fatalf("expected messages to be []interface{}, got %T", body["messages"])
	}
	out := make([]map[string]interface{}, len(raw))
	for i, m := range raw {
		mm, ok := m.(map[string]interface{})
		if !ok {
			t.Fatalf("message %d is not a map: %#v", i, m)
		}
		out[i] = mm
	}
	return out
}

// TestBuildRequestBody_MoonshotAIProviderOptionsKeyReachesWireBody guards
// against providerOptions.moonshotai.* (the key TS docs/examples and tests
// use — moonshotai-provider.ts's provider config is "moonshotai.chat") being
// silently dropped: ResolveOpenAICompatibleProviderOptions only ever
// resolved this Go SDK's own "moonshot" key.
func TestBuildRequestBody_MoonshotAIProviderOptionsKeyReachesWireBody(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{
			"moonshotai": map[string]interface{}{"promptCacheKey": "cache-key-123"},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["prompt_cache_key"] != "cache-key-123" {
		t.Errorf("prompt_cache_key = %#v, want cache-key-123", body["prompt_cache_key"])
	}
}

func TestBuildRequestBody_MaxOutputTokensAsMaxCompletionTokens(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	maxTokens := 17
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:    textPrompt("Hello"),
		MaxTokens: &maxTokens,
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["max_completion_tokens"] != 17 {
		t.Errorf("expected max_completion_tokens=17, got %v", body["max_completion_tokens"])
	}
	if _, has := body["max_tokens"]; has {
		t.Error("expected max_tokens to never be sent")
	}
}

func TestBuildRequestBody_OmitsMaxCompletionTokensWhenUnset(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello")}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := body["max_completion_tokens"]; has {
		t.Error("expected max_completion_tokens to be omitted")
	}
	if _, has := body["max_tokens"]; has {
		t.Error("expected max_tokens to be omitted")
	}
	if _, has := body["stream"]; has {
		t.Error("doGenerate body should never include a stream key")
	}
}

func TestBuildRequestBody_StreamAddsStreamOptions(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello")}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["stream"] != true {
		t.Errorf("expected stream=true, got %v", body["stream"])
	}
	so, ok := body["stream_options"].(map[string]interface{})
	if !ok || so["include_usage"] != true {
		t.Errorf("expected stream_options.include_usage=true, got %#v", body["stream_options"])
	}
}

func TestBuildRequestBody_OmitsFixedSamplingForKimiModels(t *testing.T) {
	for _, modelID := range []string{"kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k3"} {
		t.Run(modelID, func(t *testing.T) {
			model := newMoonshotTestModel(modelID)
			temp, topP, freq, pres := 0.2, 0.4, 0.5, 0.6
			body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				Prompt:           textPrompt("Hello"),
				Temperature:      &temp,
				TopP:             &topP,
				FrequencyPenalty: &freq,
				PresencePenalty:  &pres,
			}, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, key := range []string{"temperature", "top_p", "frequency_penalty", "presence_penalty"} {
				if _, has := body[key]; has {
					t.Errorf("expected %s to be omitted for %s", key, modelID)
				}
			}
			wantFeatures := map[string]bool{"temperature": false, "topP": false, "frequencyPenalty": false, "presencePenalty": false}
			for _, w := range warnings {
				if w.Type == "unsupported" {
					if _, ok := wantFeatures[w.Feature]; ok {
						wantFeatures[w.Feature] = true
					}
				}
			}
			for feature, found := range wantFeatures {
				if !found {
					t.Errorf("expected an unsupported warning for %s, got %#v", feature, warnings)
				}
			}
		})
	}
}

func TestBuildRequestBody_PreservesSamplingForCustomModels(t *testing.T) {
	model := newMoonshotTestModel("custom-model")
	temp, topP := 0.2, 0.4
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      textPrompt("Hello"),
		Temperature: &temp,
		TopP:        &topP,
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["temperature"] != 0.2 || body["top_p"] != 0.4 {
		t.Errorf("expected sampling options preserved for custom model, got %#v", body)
	}
}

func TestBuildRequestBody_ToolChoiceRequiredOmittedForRestrictedModels(t *testing.T) {
	for _, modelID := range []string{"kimi-k2.6", "kimi-k2.7-code", "kimi-k2.7-code-highspeed"} {
		t.Run(modelID, func(t *testing.T) {
			model := newMoonshotTestModel(modelID)
			body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				Prompt:     textPrompt("Hello"),
				Tools:      []types.Tool{{Name: "get_weather", Description: "Get the weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}},
				ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
			}, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, has := body["tool_choice"]; has {
				t.Errorf("expected tool_choice to be omitted for %s, got %v", modelID, body["tool_choice"])
			}
			found := false
			for _, w := range warnings {
				if w.Type == "unsupported" && w.Feature == `tool choice "required" for model "`+modelID+`"` {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a required-tool-choice warning, got %#v", warnings)
			}
		})
	}
}

func TestBuildRequestBody_ToolChoiceRequiredPreservedForKimiK3(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:     textPrompt("Hello"),
		Tools:      []types.Tool{{Name: "get_weather", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["tool_choice"] != "required" {
		t.Errorf("expected tool_choice=required, got %v", body["tool_choice"])
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %#v", warnings)
	}
}

func TestBuildRequestBody_ToolSchemaTupleNormalizedToPrefixItems(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		Tools: []types.Tool{{
			Name:        "probe",
			Description: "probe",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{
						"type":  "array",
						"items": []interface{}{map[string]interface{}{"type": "number"}, map[string]interface{}{"type": "number"}},
					},
				},
				"required": []interface{}{"a"},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools, ok := body["tools"].([]moonshotFunctionTool)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %#v", body["tools"])
	}
	params, ok := tools[0].Function.Parameters.(map[string]interface{})
	if !ok {
		t.Fatalf("expected parameters map, got %#v", tools[0].Function.Parameters)
	}
	props := params["properties"].(map[string]interface{})
	a := props["a"].(map[string]interface{})
	if _, hasItems := a["items"]; hasItems {
		t.Errorf("expected tuple items removed, got %#v", a)
	}
	prefixItems, ok := a["prefixItems"].([]interface{})
	if !ok || len(prefixItems) != 2 {
		t.Errorf("expected 2 prefixItems, got %#v", a["prefixItems"])
	}
}

func TestBuildRequestBody_StructuredOutputGating(t *testing.T) {
	schema := map[string]interface{}{
		"$schema":    "http://json-schema.org/draft-07/schema#",
		"type":       "object",
		"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
	}

	t.Run("kimi models use json_schema natively", func(t *testing.T) {
		for _, modelID := range []string{"kimi-k3", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "kimi-k2"} {
			model := newMoonshotTestModel(modelID)
			body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				Prompt:         textPrompt("Hello"),
				ResponseFormat: &provider.ResponseFormat{Type: "json", Name: "response", Schema: schema},
			}, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", modelID, err)
			}
			rf, ok := body["response_format"].(map[string]interface{})
			if !ok || rf["type"] != "json_schema" {
				t.Fatalf("%s: expected json_schema response_format, got %#v", modelID, body["response_format"])
			}
			js := rf["json_schema"].(map[string]interface{})
			if js["strict"] != true {
				t.Errorf("%s: expected strict=true by default, got %v", modelID, js["strict"])
			}
			schemaOut := js["schema"].(map[string]interface{})
			if _, has := schemaOut["$schema"]; has {
				t.Errorf("%s: expected $schema stripped from wire schema", modelID)
			}
		}
	})

	t.Run("official moonshot-v1 non-vision and vision-preview models use json_schema", func(t *testing.T) {
		for _, modelID := range []string{
			"moonshot-v1-8k", "moonshot-v1-32k", "moonshot-v1-128k", "moonshot-v1-auto",
			"moonshot-v1-8k-vision-preview", "moonshot-v1-32k-vision-preview", "moonshot-v1-128k-vision-preview",
		} {
			model := newMoonshotTestModel(modelID)
			body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				Prompt:         textPrompt("Hello"),
				ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
			}, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", modelID, err)
			}
			rf, ok := body["response_format"].(map[string]interface{})
			if !ok || rf["type"] != "json_schema" {
				t.Fatalf("%s: expected json_schema response_format, got %#v", modelID, body["response_format"])
			}
		}
	})

	t.Run("unknown custom models fall back to json_object", func(t *testing.T) {
		model := newMoonshotTestModel("custom-model-id")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:         textPrompt("Hello"),
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["response_format"].(map[string]interface{})["type"] != "json_object" {
			t.Errorf("expected json_object fallback, got %#v", body["response_format"])
		}
	})

	t.Run("no schema falls back to json_object even on supported models", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k3")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:         textPrompt("Hello"),
			ResponseFormat: &provider.ResponseFormat{Type: "json"},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["response_format"].(map[string]interface{})["type"] != "json_object" {
			t.Errorf("expected json_object fallback, got %#v", body["response_format"])
		}
	})

	t.Run("strictJsonSchema can be disabled", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k3")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{
				"moonshot": map[string]interface{}{"strictJsonSchema": false},
			},
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		js := body["response_format"].(map[string]interface{})["json_schema"].(map[string]interface{})
		if js["strict"] != false {
			t.Errorf("expected strict=false, got %v", js["strict"])
		}
	})
}

func TestBuildRequestBody_LogprobsOptions(t *testing.T) {
	model := newMoonshotTestModel("moonshot-v1-8k")

	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{"logprobs": true, "topLogprobs": 20},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["logprobs"] != true || body["top_logprobs"] != 20 {
		t.Errorf("expected logprobs=true top_logprobs=20, got %#v", body)
	}

	body, _, err = model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{"topLogprobs": 0},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["logprobs"] != true || body["top_logprobs"] != 0 {
		t.Errorf("expected logprobs=true (auto-enabled) top_logprobs=0, got %#v", body)
	}

	body, _, err = model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello")}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := body["logprobs"]; has {
		t.Errorf("expected logprobs omitted by default, got %v", body["logprobs"])
	}

	for _, invalid := range []interface{}{-1, 1.5, 21} {
		_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{
				"moonshot": map[string]interface{}{"topLogprobs": invalid},
			},
		}, false)
		if err == nil {
			t.Errorf("expected an error for invalid topLogprobs=%v", invalid)
		}
	}
}

func TestBuildRequestBody_PredictionPassthrough(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")

	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"prediction": map[string]interface{}{"type": "content", "content": "Hello, world!"},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pred := body["prediction"].(map[string]interface{})
	if pred["type"] != "content" || pred["content"] != "Hello, world!" {
		t.Errorf("unexpected prediction: %#v", pred)
	}

	body, _, err = model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello")}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := body["prediction"]; has {
		t.Error("expected prediction omitted when not configured")
	}

	for _, invalid := range []interface{}{
		map[string]interface{}{"type": "text", "content": "Hello, world!"},
		map[string]interface{}{"type": "content", "content": []interface{}{map[string]interface{}{"type": "image", "text": "Hello"}}},
		map[string]interface{}{"type": "content", "content": 123},
	} {
		_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"prediction": invalid}},
		}, false)
		if err == nil {
			t.Errorf("expected an error for invalid prediction %#v", invalid)
		}
	}
}

func TestBuildRequestBody_PromptCacheKeyAndSafetyIdentifier(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		ProviderOptions: map[string]interface{}{
			"moonshot": map[string]interface{}{
				"promptCacheKey":   "session-42",
				"safetyIdentifier": "user-hash-7",
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["prompt_cache_key"] != "session-42" || body["safety_identifier"] != "user-hash-7" {
		t.Errorf("unexpected body: %#v", body)
	}
}

func TestBuildRequestBody_MessageNamesAndPartialMode(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role:            types.RoleSystem,
				Content:         []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"name": "guide"}},
			},
			{
				Role:            types.RoleUser,
				Content:         []types.ContentPart{types.TextContent{Text: "Hello"}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"name": "alice"}},
			},
			{
				Role:    types.RoleAssistant,
				Content: []types.ContentPart{types.TextContent{Text: "The sky is"}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{
					"name":    "writer",
					"partial": true,
				}},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := wireMessages(t, body)
	if messages[0]["name"] != "guide" || messages[0]["content"] != "You are a helpful assistant." {
		t.Errorf("unexpected system message: %#v", messages[0])
	}
	if messages[1]["name"] != "alice" || messages[1]["content"] != "Hello" {
		t.Errorf("unexpected user message: %#v", messages[1])
	}
	if messages[2]["name"] != "writer" || messages[2]["content"] != "The sky is" || messages[2]["partial"] != true {
		t.Errorf("unexpected assistant message: %#v", messages[2])
	}
}

func TestBuildRequestBody_PartialModeRejectedWithJSONObjectFormat(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role:            types.RoleAssistant,
				Content:         []types.ContentPart{types.TextContent{Text: "{"}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"partial": true}},
			},
		}},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}, false)
	if err == nil {
		t.Fatal("expected an error rejecting Partial Mode with JSON object response format")
	}
}

func TestBuildRequestBody_PartialModeAllowedWithJSONSchemaFormat(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role:            types.RoleAssistant,
				Content:         []types.ContentPart{types.TextContent{Text: "{"}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"partial": true}},
			},
		}},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Name: "result", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}}}},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := wireMessages(t, body)
	if messages[0]["partial"] != true {
		t.Errorf("expected partial=true, got %#v", messages[0])
	}
	if body["response_format"].(map[string]interface{})["type"] != "json_schema" {
		t.Errorf("expected json_schema response_format, got %#v", body["response_format"])
	}
}

func TestBuildRequestBody_DynamicToolLoading(t *testing.T) {
	dynTools := []interface{}{map[string]interface{}{
		"type":        "function",
		"name":        "calculator",
		"description": "Evaluate an expression",
		"inputSchema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}}

	t.Run("kimi-k3 gets a system tools message", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k3")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Calculate."}}},
				{
					Role:            types.RoleSystem,
					Content:         nil,
					ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"tools": dynTools}},
				},
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		messages := wireMessages(t, body)
		last := messages[len(messages)-1]
		if last["role"] != "system" {
			t.Fatalf("expected last message to be a system tools message, got %#v", last)
		}
		tools, ok := last["tools"].([]moonshotFunctionTool)
		if !ok || len(tools) != 1 || tools[0].Function.Name != "calculator" {
			t.Errorf("unexpected dynamic tools: %#v", last["tools"])
		}
	})

	t.Run("non-K3 official models omit and warn", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k2.6")
		body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleSystem, Content: nil, ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"tools": dynTools}}},
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		messages := wireMessages(t, body)
		if len(messages) != 0 {
			t.Errorf("expected the dynamic system message to be dropped, got %#v", messages)
		}
		found := false
		for _, w := range warnings {
			if w.Type == "unsupported" && w.Feature == `dynamic tool loading for model "kimi-k2.6"` {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a dynamic tool loading warning, got %#v", warnings)
		}
	})

	t.Run("unknown custom models preserve dynamic messages", func(t *testing.T) {
		model := newMoonshotTestModel("custom-model")
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleSystem, Content: nil, ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"tools": dynTools}}},
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		messages := wireMessages(t, body)
		if len(messages) != 1 || messages[0]["role"] != "system" {
			t.Errorf("expected the dynamic system message preserved, got %#v", messages)
		}
	})
}

func TestBuildRequestBody_ToolMessageNameWarning(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	_, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleTool,
				Content: []types.ContentPart{types.ToolResultContent{
					ToolCallID: "call-1",
					ToolName:   "weather",
					Output:     &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "sunny"},
				}},
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"name": "weather_tool"}},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "message name on tool messages" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a tool-message-name warning, got %#v", warnings)
	}
}

func TestBuildRequestBody_TopKAndSeedWarnings(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")
	topK, seed := 5, 42
	_, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: textPrompt("Hello"),
		TopK:   &topK,
		Seed:   &seed,
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 2 || warnings[0].Feature != "topK" || warnings[1].Feature != "seed" {
		t.Errorf("unexpected warnings: %#v", warnings)
	}
}

func TestBuildRequestBody_ReasoningFamilyGating(t *testing.T) {
	t.Run("kimi-k3 maps generic reasoning to reasoning_effort", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k3")
		low := types.ReasoningLow
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"), Reasoning: &low}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body["reasoning_effort"] != "low" {
			t.Errorf("expected reasoning_effort=low, got %v", body["reasoning_effort"])
		}
	})

	t.Run("kimi-k3 warns and omits reasoning none", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k3")
		none := types.ReasoningNone
		body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"), Reasoning: &none}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, has := body["reasoning_effort"]; has {
			t.Error("expected reasoning_effort omitted")
		}
		if len(warnings) != 1 || warnings[0].Feature != `reasoning "none"` {
			t.Errorf("unexpected warnings: %#v", warnings)
		}
	})

	t.Run("kimi-k2.6 omits reasoningEffort with a warning", func(t *testing.T) {
		model := newMoonshotTestModel("kimi-k2.6")
		body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:          textPrompt("Hello"),
			ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"reasoningEffort": "high"}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, has := body["reasoning_effort"]; has {
			t.Error("expected reasoning_effort omitted for kimi-k2.6")
		}
		if len(warnings) != 1 || warnings[0].Feature != "reasoningEffort" {
			t.Errorf("unexpected warnings: %#v", warnings)
		}
	})

	t.Run("kimi-k2.5/k2.6 map generic reasoning to thinking", func(t *testing.T) {
		for _, modelID := range []string{"kimi-k2.5", "kimi-k2.6"} {
			model := newMoonshotTestModel(modelID)
			low := types.ReasoningLow
			body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"), Reasoning: &low}, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", modelID, err)
			}
			thinking := body["thinking"].(map[string]interface{})
			if thinking["type"] != "enabled" {
				t.Errorf("%s: expected thinking.type=enabled, got %v", modelID, thinking)
			}

			none := types.ReasoningNone
			body, _, err = model.buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: textPrompt("Hello"), Reasoning: &none}, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", modelID, err)
			}
			thinking = body["thinking"].(map[string]interface{})
			if thinking["type"] != "disabled" {
				t.Errorf("%s: expected thinking.type=disabled, got %v", modelID, thinking)
			}
		}
	})

	t.Run("kimi-k2.7 cannot disable thinking", func(t *testing.T) {
		for _, modelID := range []string{"kimi-k2.7-code", "kimi-k2.7-code-highspeed"} {
			model := newMoonshotTestModel(modelID)
			body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
				Prompt:          textPrompt("Hello"),
				ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{"thinking": map[string]interface{}{"type": "disabled"}}},
			}, false)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", modelID, err)
			}
			if _, has := body["thinking"]; has {
				t.Errorf("%s: expected thinking omitted, got %v", modelID, body["thinking"])
			}
			if len(warnings) != 1 || warnings[0].Feature != `thinking.type "disabled"` {
				t.Errorf("%s: unexpected warnings: %#v", modelID, warnings)
			}
		}
	})

	t.Run("moonshot-v1 omits thinking and reasoning entirely", func(t *testing.T) {
		model := newMoonshotTestModel("moonshot-v1-8k")
		high := types.ReasoningHigh
		body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:    textPrompt("Hello"),
			Reasoning: &high,
			ProviderOptions: map[string]interface{}{"moonshot": map[string]interface{}{
				"reasoningEffort":  "high",
				"thinking":         map[string]interface{}{"type": "enabled"},
				"reasoningHistory": "preserved",
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, has := body["reasoning_effort"]; has {
			t.Error("expected reasoning_effort omitted")
		}
		if _, has := body["thinking"]; has {
			t.Error("expected thinking omitted")
		}
		if len(warnings) != 4 {
			t.Errorf("expected 4 warnings, got %#v", warnings)
		}
	})
}

func TestBuildRequestBody_MediaTypeValidation(t *testing.T) {
	model := newMoonshotTestModel("kimi-k3")

	t.Run("rejects unsupported image media types", func(t *testing.T) {
		_, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.FileContent{Data: []byte("<svg></svg>"), MediaType: "image/svg+xml"},
				}},
			}},
		}, false)
		if err == nil {
			t.Fatal("expected an error for an unsupported image media type")
		}
		var unsupported *providererrors.UnsupportedFunctionalityError
		if !errors.As(err, &unsupported) {
			t.Fatalf("expected *UnsupportedFunctionalityError, got %T: %v", err, err)
		}
	})

	t.Run("accepts video data as a data URI", func(t *testing.T) {
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.TextContent{Text: "Describe this video"},
					types.FileContent{Data: []byte{0, 1, 2, 3}, MediaType: "video/mp4"},
				}},
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		messages := wireMessages(t, body)
		parts := messages[0]["content"].([]interface{})
		videoPart := parts[1].(map[string]interface{})
		if videoPart["type"] != "video_url" {
			t.Fatalf("expected video_url part, got %#v", videoPart)
		}
		videoURL := videoPart["video_url"].(map[string]interface{})
		if videoURL["url"] != "data:video/mp4;base64,AAECAw==" {
			t.Errorf("unexpected video data URI: %v", videoURL["url"])
		}
	})

	t.Run("passes through ms:// image and video references", func(t *testing.T) {
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt: types.Prompt{Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{
					types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"moonshot": "ms://image-file-123"}}, MediaType: "image/png"},
				}},
			}},
		}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		messages := wireMessages(t, body)
		parts := messages[0]["content"].([]interface{})
		part := parts[0].(map[string]interface{})
		if part["type"] != "image_url" {
			t.Fatalf("expected image_url part, got %#v", part)
		}
		imageURL := part["image_url"].(map[string]interface{})
		if imageURL["url"] != "ms://image-file-123" {
			t.Errorf("unexpected image url: %v", imageURL["url"])
		}
	})
}

func TestParseMoonshotProviderError_PreservesCode(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid request: invalid part type: file","type":"invalid_request_error","code":"invalid_parameter"}}`)
	statusErr := &internalhttp.HTTPStatusError{StatusCode: 400, Body: body}

	err := parseMoonshotProviderError(statusErr)
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.ErrorCode != "invalid_parameter" {
		t.Errorf("expected error code 'invalid_parameter', got %q", provErr.ErrorCode)
	}
	if provErr.Message != "Invalid request: invalid part type: file" {
		t.Errorf("unexpected message: %q", provErr.Message)
	}
	if provErr.StatusCode != 400 {
		t.Errorf("expected status code 400, got %d", provErr.StatusCode)
	}
	dataMap, ok := provErr.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected Data to be the full decoded envelope, got %#v", provErr.Data)
	}
	if _, has := dataMap["error"]; !has {
		t.Errorf("expected Data to contain the 'error' envelope, got %#v", dataMap)
	}
}

func TestParseMoonshotProviderError_FallsBackToTypeWhenCodeMissing(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid request with nullable code","type":"invalid_request_error","code":null}}`)
	statusErr := &internalhttp.HTTPStatusError{StatusCode: 400, Body: body}

	err := parseMoonshotProviderError(statusErr)
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.ErrorCode != "invalid_request_error" {
		t.Errorf("expected error code fallback to type, got %q", provErr.ErrorCode)
	}
}

func TestParseMoonshotProviderError_MessageOnlyEnvelope(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid request"}}`)
	statusErr := &internalhttp.HTTPStatusError{StatusCode: 400, Body: body}

	err := parseMoonshotProviderError(statusErr)
	var provErr *providererrors.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected a *providererrors.ProviderError, got %T: %v", err, err)
	}
	if provErr.Message != "Invalid request" {
		t.Errorf("unexpected message: %q", provErr.Message)
	}
	if provErr.ErrorCode != "" {
		t.Errorf("expected empty error code, got %q", provErr.ErrorCode)
	}
}

func TestNormalizeJSONSchemaForMFJS_RejectsNonObjectRoot(t *testing.T) {
	_, err := NormalizeJSONSchemaForMFJS(map[string]interface{}{"type": "string"})
	if err == nil {
		t.Fatal("expected an error for a non-object root schema")
	}
}

func TestNormalizeJSONSchemaForMFJS_MovesTypeIntoAnyOfBranches(t *testing.T) {
	input := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"value": map[string]interface{}{
				"type": "string",
				"anyOf": []interface{}{
					map[string]interface{}{"minLength": 1},
					map[string]interface{}{"type": "number"},
				},
			},
		},
	}
	out, err := NormalizeJSONSchemaForMFJS(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := out.(map[string]interface{})
	props := result["properties"].(map[string]interface{})
	value := props["value"].(map[string]interface{})
	if _, hasType := value["type"]; hasType {
		t.Errorf("expected type removed from parent when anyOf present, got %#v", value)
	}
	anyOf := value["anyOf"].([]interface{})
	first := anyOf[0].(map[string]interface{})
	if first["type"] != "string" {
		t.Errorf("expected type pushed into first branch, got %#v", first)
	}
	second := anyOf[1].(map[string]interface{})
	if second["type"] != "number" {
		t.Errorf("expected second branch to keep its own type, got %#v", second)
	}
}
