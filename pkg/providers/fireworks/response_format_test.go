package fireworks

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var responseFormatTestSchema = map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
}

func assertResponseFormat(t *testing.T, body map[string]interface{}, want string) {
	t.Helper()
	if want == "" {
		if rf, ok := body["response_format"]; ok {
			t.Fatalf("response_format = %#v, want omitted", rf)
		}
		return
	}
	gotJSON, _ := json.Marshal(body["response_format"])
	var gotV, wantV interface{}
	_ = json.Unmarshal(gotJSON, &gotV)
	_ = json.Unmarshal([]byte(want), &wantV)
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatalf("response_format = %s, want %s", gotJSON, want)
	}
}

func responseFormatOpts(rf *provider.ResponseFormat, providerOptions map[string]interface{}) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ResponseFormat:  rf,
		ProviderOptions: providerOptions,
	}
}

const (
	wantJSONObject       = `{"type":"json_object"}`
	wantJSONSchemaStrict = `{"type":"json_schema","json_schema":{"schema":{"type":"object","properties":{"value":{"type":"string"}}},"strict":true,"name":"response"}}`
	wantJSONSchemaLoose  = `{"type":"json_schema","json_schema":{"schema":{"type":"object","properties":{"value":{"type":"string"}}},"strict":false,"name":"response"}}`
)

// Fireworks uses the openai-compatible chat model with supportsStructuredOutputs: true.
func TestChatResponseFormatMapping(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "accounts/fireworks/models/llama-v3p1-8b-instruct")
	schemaRF := &provider.ResponseFormat{Type: "json", Schema: responseFormatTestSchema}
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(schemaRF, nil), false), wantJSONSchemaStrict)
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(schemaRF, map[string]interface{}{"fireworks": map[string]interface{}{"strictJsonSchema": false}}), false), wantJSONSchemaLoose)
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(&provider.ResponseFormat{Type: "json"}, nil), false), wantJSONObject)
}
