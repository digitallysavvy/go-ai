package cohere

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// GenerateObject sent responseFormat type json_schema (TS: json); Cohere
// keys JSON mode off json, so response_format was dropped.
func TestGenerateObjectSendsCohereResponseFormat(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":[{"type":"text","text":"{\"name\":\"a\"}"}]},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":1,"output_tokens":1}}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, err := p.LanguageModel("command-r")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ai.GenerateObject(t.Context(), ai.GenerateObjectOptions{ //nolint:staticcheck // exercises the deprecated API
		Model:  m,
		Prompt: "x",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		}),
	})
	rf, _ := body["response_format"].(map[string]interface{})
	sch, _ := rf["json_schema"].(map[string]interface{})
	if rf["type"] != "json_object" || sch["type"] != "object" || sch["properties"] == nil {
		t.Fatalf("response_format = %#v, want json_object with the schema", body["response_format"])
	}
}
