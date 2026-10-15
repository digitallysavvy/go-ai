package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
)

// Tool-call arguments and tool results are JSON text the model sees; like
// JSON.stringify they must keep <, > and & literal.
func TestToolJSONKeepsHTMLCharactersLiteral(t *testing.T) {
	prompt := types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
			ToolCallID: "c1", ToolName: "t", Input: `{"q":"<b>&</b>"}`, Arguments: map[string]interface{}{"q": "<b>&</b>"}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
			ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"html": "<b>&</b>"}}}}},
	}}
	models := map[string]func(base string) (provider.LanguageModel, error){
		"openai-chat": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).ChatModel("gpt-4o")
		},
		"openai-responses": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).ResponsesModel("gpt-4o")
		},
		"together": func(b string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
	}
	for name, mk := range models {
		t.Run(name, func(t *testing.T) {
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
			}))
			defer srv.Close()
			m, err := mk(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt})
			var decoded interface{}
			if err := json.Unmarshal([]byte(body), &decoded); err != nil {
				t.Fatalf("request is not JSON: %v: %s", err, body)
			}
			var texts []string
			var walk func(v interface{})
			walk = func(v interface{}) {
				switch x := v.(type) {
				case string:
					texts = append(texts, x)
				case []interface{}:
					for _, e := range x {
						walk(e)
					}
				case map[string]interface{}:
					for _, e := range x {
						walk(e)
					}
				}
			}
			walk(decoded)
			var args, result bool
			for _, s := range texts {
				if strings.Contains(s, "\\u003c") || strings.Contains(s, "\\u0026") {
					t.Fatalf("model-visible JSON text is HTML-escaped: %q", s)
				}
				if strings.Contains(s, `{"q":"<b>&</b>"}`) {
					args = true
				}
				if strings.Contains(s, `{"html":"<b>&</b>"}`) {
					result = true
				}
			}
			if !args || !result {
				t.Fatalf("tool arguments (%v) / result (%v) not found in request: %s", args, result, body)
			}
		})
	}
}
