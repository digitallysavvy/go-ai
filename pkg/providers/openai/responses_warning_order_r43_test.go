package openai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func responsesWarningFeatures(warnings []types.Warning) []string {
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		out = append(out, w.Feature)
	}
	return out
}

// TS prepareRequest order: call-setting warnings, unsupported reasoningEffort,
// conversation + previousResponseId, input warnings, response format schema
// warnings, promptCacheRetention (GPT-6), reasoning-model warnings
// (temperature/topP), serviceTier, and the tool warnings last.
func TestResponsesWarningOrderMatchesTS(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	m := NewResponsesLanguageModel(p, ModelGPT6Astra)
	temperature := 0.5
	_, _, warnings, err := m.buildRequest(&provider.GenerateOptions{
		Prompt:      types.Prompt{Messages: []types.Message{userMsg("hi")}},
		Temperature: &temperature,
		ResponseFormat: &provider.ResponseFormat{
			Type:   "json",
			Schema: map[string]interface{}{"type": "object", "propertyNames": map[string]interface{}{"type": "string"}, "properties": map[string]interface{}{}},
		},
		Tools: []types.Tool{{
			Type:        types.ToolTypeFunction,
			Name:        "lookup",
			Description: "d",
			Parameters:  map[string]interface{}{"type": "object", "propertyNames": map[string]interface{}{"type": "string"}, "properties": map[string]interface{}{}},
		}},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{
				"reasoningEffort":      "none",
				"conversation":         "conv_1",
				"previousResponseId":   "resp_1",
				"promptCacheRetention": "24h",
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got := responsesWarningFeatures(warnings)
	want := []string{
		"reasoningEffort", "conversation",
		"JSON Schema propertyNames", // response format schema
		"promptCacheRetention",
		"temperature",
		"JSON Schema propertyNames", // tool schema, always last
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("warning order = %v, want %v", got, want)
	}
}
