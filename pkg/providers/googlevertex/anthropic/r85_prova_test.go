package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// google-vertex-anthropic-structured-output.test.ts (TS f4c5f1e959): Claude
// models that reject forced tool use send output_config.format on Vertex,
// although Vertex disables native structured outputs by default.
func TestR85provaVertexAnthropicStructuredOutput(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
		"required":             []interface{}{"answer"},
		"additionalProperties": false,
	}
	generate := func(modelID string) (map[string]interface{}, *types.GenerateResult) {
		var body map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"` + modelID + `","content":[{"type":"text","text":"{\"answer\":\"42\"}"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":20}}`))
		}))
		defer srv.Close()
		p := NewGoogleVertexAnthropicProvider(Options{
			BaseURL:    srv.URL,
			AuthToken:  func(context.Context) (string, error) { return "t", nil },
			HTTPClient: srv.Client(),
		})
		m, err := p.LanguageModel(modelID)
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:         types.Prompt{Text: "Hello"},
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
		})
		if err != nil {
			t.Fatal(err)
		}
		return body, res
	}
	for _, id := range []string{"claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1", "claude-fable-5-2"} {
		body, res := generate(id)
		oc, _ := body["output_config"].(map[string]interface{})
		format, _ := oc["format"].(map[string]interface{})
		if format["type"] != "json_schema" || body["tools"] != nil {
			t.Errorf("%s: body = %v", id, body)
		}
		if res.Text != `{"answer":"42"}` {
			t.Errorf("%s: text = %q", id, res.Text)
		}
	}
	body, _ := generate("claude-haiku-5-5")
	if tc, _ := body["tool_choice"].(map[string]interface{}); body["output_config"] != nil || tc["type"] != "any" {
		t.Errorf("claude-haiku-5-5: body = %v", body)
	}
}
