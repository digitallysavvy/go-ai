package mistral

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

// Ported from mistral-chat-language-model.test.ts response format cases:
// strictJsonSchema defaults to false; JSON mode without a schema injects the
// generic JSON instruction into the system message.
func TestChatResponseFormatMapping(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-small-latest")
	schemaRF := &provider.ResponseFormat{Type: "json", Schema: responseFormatTestSchema}
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(schemaRF, nil), false), wantJSONSchemaLoose)
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(schemaRF, map[string]interface{}{"mistral": map[string]interface{}{"strictJsonSchema": true}}), false), wantJSONSchemaStrict)
	assertResponseFormat(t, m.buildRequestBody(responseFormatOpts(schemaRF, map[string]interface{}{"mistral": map[string]interface{}{"structuredOutputs": false}}), false), wantJSONObject)

	body := m.buildRequestBody(responseFormatOpts(&provider.ResponseFormat{Type: "json"}, nil), false)
	assertResponseFormat(t, body, wantJSONObject)
	messages := body["messages"].([]map[string]interface{})
	if messages[0]["role"] != "system" || messages[0]["content"] != "You MUST answer with JSON." {
		t.Fatalf("messages[0] = %#v, want injected JSON instruction", messages[0])
	}
}
