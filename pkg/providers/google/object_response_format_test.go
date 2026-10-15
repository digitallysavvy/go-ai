package google

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// GenerateObject sent responseFormat type json_schema (TS: json), so Gemini
// set neither responseMimeType nor responseJsonSchema.
func TestGenerateObjectSetsGeminiJSONMode(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"name\":\"a\"}"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := ai.GenerateObject(t.Context(), ai.GenerateObjectOptions{ //nolint:staticcheck // exercises the deprecated API
		Model:  NewLanguageModel(p, "gemini-2.5-flash"),
		Prompt: "x",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	gc, _ := body["generationConfig"].(map[string]interface{})
	if gc["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig = %#v, want responseMimeType application/json", gc)
	}
	sch, _ := gc["responseJsonSchema"].(map[string]interface{})
	if sch["type"] != "object" || sch["properties"] == nil {
		t.Fatalf("responseJsonSchema = %#v, want the object schema", gc["responseJsonSchema"])
	}
}
