package groq

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports "should handle structured outputs with Kimi K2 model" (TS
// groq-chat-language-model.test.ts): the TS wire replay cannot choose the model
// id, so the request body is checked here. The schema keeps its $schema key.
func TestStructuredOutputsWithKimiK2Model(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "moonshotai/kimi-k2-instruct-0905")
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"recipe": map[string]interface{}{"type": "object"},
		},
		"required":             []interface{}{"recipe"},
		"additionalProperties": false,
		"$schema":              "http://json-schema.org/draft-07/schema#",
	}
	body, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Generate a simple pasta recipe"},
		ResponseFormat: &provider.ResponseFormat{
			Type: "json", Name: "recipe_response", Description: "A recipe with ingredients and instructions", Schema: schema,
		},
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"structuredOutputs": true}},
	}, false)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if body["model"] != "moonshotai/kimi-k2-instruct-0905" {
		t.Fatalf("model = %v", body["model"])
	}
	gotJSON, _ := json.Marshal(body["response_format"])
	var got, want interface{}
	_ = json.Unmarshal(gotJSON, &got)
	wantJSON, _ := json.Marshal(map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name": "recipe_response", "description": "A recipe with ingredients and instructions",
			"strict": true, "schema": schema,
		},
	})
	_ = json.Unmarshal(wantJSON, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response_format = %s, want %s", gotJSON, wantJSON)
	}
}
