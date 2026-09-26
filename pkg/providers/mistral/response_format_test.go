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

// TestChatResponseFormatMappingMergesJSONInstructionIntoArrayContentSystemMessage
// guards against F6(b): TS always merges the generic JSON instruction into a
// single leading system message, because its LanguageModelV4Message system
// role only ever carries string content. Go's unified Message allows a
// system message with multiple content parts, which ToOpenAIMessages
// serializes as a content array rather than a string; that must still merge
// into the existing system message instead of prepending a second one.
func TestChatResponseFormatMappingMergesJSONInstructionIntoArrayContentSystemMessage(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-small-latest")
	opts := &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{
				Role: types.RoleSystem,
				Content: []types.ContentPart{
					types.TextContent{Text: "Be terse."},
					types.TextContent{Text: "Avoid jargon."},
				},
			},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	}
	body := m.buildRequestBody(opts, false)
	assertResponseFormat(t, body, wantJSONObject)

	messages := body["messages"].([]map[string]interface{})
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want exactly one merged system message + the user message", messages)
	}
	if messages[0]["role"] != "system" {
		t.Fatalf("messages[0] role = %v, want system", messages[0]["role"])
	}
	content, ok := messages[0]["content"].([]map[string]interface{})
	if !ok {
		t.Fatalf("messages[0][\"content\"] = %#v (%T), want a content-part array", messages[0]["content"], messages[0]["content"])
	}
	if len(content) != 3 {
		t.Fatalf("system content parts = %#v, want the original 2 parts plus the injected instruction", content)
	}
	last := content[len(content)-1]
	if last["type"] != "text" || last["text"] != "You MUST answer with JSON." {
		t.Fatalf("last system content part = %#v, want the injected JSON instruction", last)
	}
}
