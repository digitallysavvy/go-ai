package together

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

// Together uses the openai-compatible chat model with
// supportsStructuredOutputs = getModelStructuredOutputSupport(modelId).
func TestChatResponseFormatMapping(t *testing.T) {
	schemaRF := &provider.ResponseFormat{Type: "json", Schema: responseFormatTestSchema}

	plain := NewLanguageModel(New(Config{APIKey: "k"}), "meta-llama/Llama-3.3-70B-Instruct-Turbo")
	body, warnings := plain.buildRequestBodyWithWarnings(responseFormatOpts(schemaRF, nil), false)
	assertResponseFormat(t, body, wantJSONObject)
	if len(warnings) != 1 || warnings[0].Feature != "responseFormat" {
		t.Fatalf("warnings = %#v", warnings)
	}

	structured := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-ai/DeepSeek-V4-Flash-0731")
	body, warnings = structured.buildRequestBodyWithWarnings(responseFormatOpts(schemaRF, nil), false)
	assertResponseFormat(t, body, wantJSONSchemaStrict)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
	body, _ = structured.buildRequestBodyWithWarnings(responseFormatOpts(schemaRF, map[string]interface{}{"togetherai": map[string]interface{}{"strictJsonSchema": false}}), false)
	assertResponseFormat(t, body, wantJSONSchemaLoose)
}
