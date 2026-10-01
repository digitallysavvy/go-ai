package cerebras

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The OpenAI-compatible chat model this provider builds on sends
// response_format as json_schema (TS supportsStructuredOutputs: true) to
// /chat/completions.
func TestInheritedChatResponseFormat(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", gotPath)
	}
	want := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}},
			"strict": true,
			"name":   "response",
		},
	}
	if !reflect.DeepEqual(gotBody["response_format"], want) {
		t.Fatalf("response_format = %#v", gotBody["response_format"])
	}
}
