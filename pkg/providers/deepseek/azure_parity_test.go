package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestDeepSeekPenaltySamplingDefaultOff ports the TS
// deepseek-chat-language-model.test.ts coverage for
// this.config.supportsPenaltySampling: by default (upstream DeepSeek API),
// frequency/presence penalties are omitted with a deprecated warning.
func TestDeepSeekPenaltySamplingDefaultOff(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-chat")

	fp := 0.5
	pp := 0.3
	body, warnings, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		FrequencyPenalty: &fp,
		PresencePenalty:  &pp,
	}, false)

	if _, ok := body["frequency_penalty"]; ok {
		t.Fatalf("frequency_penalty should be omitted by default, got %#v", body["frequency_penalty"])
	}
	if _, ok := body["presence_penalty"]; ok {
		t.Fatalf("presence_penalty should be omitted by default, got %#v", body["presence_penalty"])
	}

	wantMessages := map[string]bool{
		"frequencyPenalty is deprecated by DeepSeek and has been omitted. Remove frequencyPenalty from the request.": false,
		"presencePenalty is deprecated by DeepSeek and has been omitted. Remove presencePenalty from the request.":   false,
	}
	for _, w := range warnings {
		if w.Type != "deprecated" {
			continue
		}
		if _, ok := wantMessages[w.Details]; ok {
			wantMessages[w.Details] = true
		}
	}
	for msg, found := range wantMessages {
		if !found {
			t.Errorf("missing deprecated warning: %q (warnings=%#v)", msg, warnings)
		}
	}
}

// TestDeepSeekPenaltySamplingEnabledForAzure covers the Azure config
// (0e36ab5): supportsPenaltySampling=true sends the penalties as-is with no
// warning.
func TestDeepSeekPenaltySamplingEnabledForAzure(t *testing.T) {
	supportsPenalty := true
	p := New(Config{APIKey: "test-key", SupportsPenaltySampling: &supportsPenalty})
	model := NewLanguageModel(p, "deepseek-chat")

	fp := 0.5
	pp := 0.3
	body, warnings, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		FrequencyPenalty: &fp,
		PresencePenalty:  &pp,
	}, false)

	if body["frequency_penalty"] != fp {
		t.Fatalf("frequency_penalty = %#v, want %v", body["frequency_penalty"], fp)
	}
	if body["presence_penalty"] != pp {
		t.Fatalf("presence_penalty = %#v, want %v", body["presence_penalty"], pp)
	}
	for _, w := range warnings {
		if w.Type == "deprecated" {
			t.Fatalf("unexpected deprecated warning: %#v", w)
		}
	}
}

// TestDeepSeekTemperatureTopPOmittedWhenThinkingEnabled covers the
// temperature/topP-vs-thinking warnings from 0e36ab5.
func TestDeepSeekTemperatureTopPOmittedWhenThinkingEnabled(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-reasoner")

	temp := 0.7
	topP := 0.9
	reasoning := types.ReasoningHigh
	body, warnings, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temp,
		TopP:        &topP,
		Reasoning:   &reasoning,
	}, false)

	if _, ok := body["temperature"]; ok {
		t.Fatalf("temperature should be omitted while thinking is enabled, got %#v", body["temperature"])
	}
	if _, ok := body["top_p"]; ok {
		t.Fatalf("top_p should be omitted while thinking is enabled, got %#v", body["top_p"])
	}

	var sawTemp, sawTopP bool
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "temperature" {
			sawTemp = true
		}
		if w.Type == "unsupported" && w.Feature == "topP" {
			sawTopP = true
		}
	}
	if !sawTemp || !sawTopP {
		t.Fatalf("expected unsupported temperature/topP warnings, got %#v", warnings)
	}
}

// TestDeepSeekTemperatureTopPSentWhenThinkingDisabled ensures the normal
// (non-reasoning) path still forwards temperature/topP unchanged.
func TestDeepSeekTemperatureTopPSentWhenThinkingDisabled(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-chat")

	temp := 0.7
	topP := 0.9
	body, warnings, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		Temperature: &temp,
		TopP:        &topP,
	}, false)

	if body["temperature"] != temp || body["top_p"] != topP {
		t.Fatalf("body = %#v", body)
	}
	for _, w := range warnings {
		if w.Type == "unsupported" && (w.Feature == "temperature" || w.Feature == "topP") {
			t.Fatalf("unexpected warning: %#v", w)
		}
	}
}

// TestDeepSeekStructuredOutputsDefaultOffUsesJSONObject covers 49ae04d: by
// default (upstream DeepSeek API), a JSON response format with a schema
// still degrades to json_object.
func TestDeepSeekStructuredOutputsDefaultOffUsesJSONObject(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(p, "deepseek-chat")

	body, _, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: map[string]interface{}{"type": "object"},
		},
	}, false)

	rf, ok := body["response_format"].(map[string]interface{})
	if !ok || rf["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", body["response_format"])
	}
}

// TestDeepSeekStructuredOutputsEnabledForAzure covers 49ae04d: Azure-hosted
// DeepSeek sends response_format:{type:json_schema,...} with strict
// defaulting to true, name defaulting to "response".
func TestDeepSeekStructuredOutputsEnabledForAzure(t *testing.T) {
	supportsStructured := true
	p := New(Config{APIKey: "test-key", SupportsStructuredOutputs: &supportsStructured})
	model := NewLanguageModel(p, "deepseek-chat")

	schema := map[string]interface{}{"type": "object"}
	body, _, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: schema,
		},
	}, false)

	rf, ok := body["response_format"].(map[string]interface{})
	if !ok || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %#v, want json_schema", body["response_format"])
	}
	jsonSchema, ok := rf["json_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("json_schema missing: %#v", rf)
	}
	if jsonSchema["name"] != "response" {
		t.Fatalf("name = %#v, want response", jsonSchema["name"])
	}
	if jsonSchema["strict"] != true {
		t.Fatalf("strict = %#v, want true", jsonSchema["strict"])
	}
	got, ok := jsonSchema["schema"].(map[string]interface{})
	if !ok || got["type"] != "object" {
		t.Fatalf("schema = %#v", jsonSchema["schema"])
	}
}

// TestDeepSeekStructuredOutputsRespectsNameDescriptionAndStrictOverride
// covers the name/description/strictJsonSchema passthrough.
func TestDeepSeekStructuredOutputsRespectsNameDescriptionAndStrictOverride(t *testing.T) {
	supportsStructured := true
	p := New(Config{APIKey: "test-key", SupportsStructuredOutputs: &supportsStructured})
	model := NewLanguageModel(p, "deepseek-chat")

	body, _, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{
			Type:        "json",
			Schema:      map[string]interface{}{"type": "object"},
			Name:        "my_output",
			Description: "the output",
		},
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{"strictJsonSchema": false},
		},
	}, false)

	rf := body["response_format"].(map[string]interface{})
	jsonSchema := rf["json_schema"].(map[string]interface{})
	if jsonSchema["name"] != "my_output" {
		t.Fatalf("name = %#v", jsonSchema["name"])
	}
	if jsonSchema["description"] != "the output" {
		t.Fatalf("description = %#v", jsonSchema["description"])
	}
	if jsonSchema["strict"] != false {
		t.Fatalf("strict = %#v, want false", jsonSchema["strict"])
	}
}
