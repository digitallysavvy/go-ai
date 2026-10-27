package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A tool declared with schema.NewSimpleJSONSchema reached the provider as
// parameters:{}.
func TestToolSchemaValueReachesRequestBody(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := ai.GenerateText(t.Context(), ai.GenerateTextOptions{
		Model:  NewLanguageModel(p, "gpt-4o"),
		Prompt: "x",
		Tools: []types.Tool{{
			Name:        "search",
			Description: "search",
			Parameters: schema.NewSimpleJSONSchema(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
			}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := body["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	fn, _ := tools[0].(map[string]interface{})["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if props["q"] == nil {
		t.Fatalf("parameters = %#v, want the q property", fn["parameters"])
	}
}
