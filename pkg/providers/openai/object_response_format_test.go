package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// GenerateObject sent responseFormat type json_schema (TS: json); the OpenAI
// chat model keys JSON mode off json, so the schema never reached the wire.
func TestGenerateObjectSendsJSONSchemaResponseFormat(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"{\"name\":\"a\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := ai.GenerateObject(t.Context(), ai.GenerateObjectOptions{ //nolint:staticcheck // exercises the deprecated API
		Model:  NewLanguageModel(p, "gpt-4o"),
		Prompt: "x",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, _ := body["response_format"].(map[string]interface{})
	if rf["type"] != "json_schema" {
		t.Fatalf("response_format = %#v, want type json_schema", rf)
	}
	js, _ := rf["json_schema"].(map[string]interface{})
	sch, _ := js["schema"].(map[string]interface{})
	if sch["type"] != "object" || sch["properties"] == nil {
		t.Fatalf("json_schema.schema = %#v, want the object schema", js["schema"])
	}
}
