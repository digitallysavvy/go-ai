package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS OpenAICompatibleChatLanguageModel defaults supportsStructuredOutputs to
// false: a schema'd JSON request goes out as json_object with a warning.
func TestCompatStructuredOutputsDefaultsFalse(t *testing.T) {
	run := func(structured *bool) (map[string]interface{}, int) {
		var body map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
		}))
		defer srv.Close()
		p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "host", ChatProviderName: "host.chat", SupportsStructuredOutputs: structured})
		m, _ := p.ChatModel("m")
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:         chatTestPrompt(),
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body, len(res.Warnings)
	}
	body, warns := run(nil)
	rf, _ := body["response_format"].(map[string]interface{})
	if rf["type"] != "json_object" || warns != 1 {
		t.Fatalf("default: response_format=%v warnings=%d", rf, warns)
	}
	yes := true
	body, _ = run(&yes)
	rf, _ = body["response_format"].(map[string]interface{})
	if rf["type"] != "json_schema" {
		t.Fatalf("enabled: %v", rf)
	}
}
