package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TS core asLanguageModelUsage: totalTokens = inputTokens + outputTokens for
// every provider; a wire total_tokens that disagrees is not used.
func TestGenerateTotalTokensIsInputPlusOutput(t *testing.T) {
	const (
		chatBody       = `{"id":"x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":99}}`
		responsesBody  = `{"id":"r","object":"response","created_at":1,"model":"m","status":"completed","output":[{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":99}}`
		hfBody         = `{"id":"r","model":"m","object":"response","created_at":1,"status":"completed","error":null,"instructions":null,"max_output_tokens":null,"metadata":null,"tool_choice":"auto","tools":[],"temperature":1.0,"top_p":1.0,"incomplete_details":null,"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":99},"output":[{"id":"m1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"output_text":"ok"}`
		completionBody = `{"id":"c","object":"text_completion","created":1,"model":"m","choices":[{"index":0,"text":"ok","finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":99}}`
	)
	cases := map[string]struct {
		body string
		mk   func(url string) (provider.LanguageModel, error)
	}{
		"together": {chatBody, func(u string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"fireworks": {chatBody, func(u string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"gmicloud": {chatBody, func(u string) (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"zai": {chatBody, func(u string) (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"ollama": {chatBody, func(u string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: u}).LanguageModel("m")
		}},
		"alibaba": {chatBody, func(u string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"huggingface": {hfBody, func(u string) (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}},
		"openresponses": {responsesBody, func(u string) (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{Name: "x", BaseURL: u}).LanguageModel("m")
		}},
		"openai-completion": {completionBody, func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).CompletionModel("gpt-3.5-turbo-instruct")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			m, err := tc.mk(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			if res.Usage.TotalTokens == nil || *res.Usage.TotalTokens != 15 {
				t.Fatalf("TotalTokens = %v, want 15 (input + output)", res.Usage.TotalTokens)
			}
		})
	}
}
