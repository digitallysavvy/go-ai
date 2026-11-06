package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// TS stringifies the parsed tool input of an assistant tool call, which keeps
// the wire key order. Go replays the raw JSON text the call arrived with, so
// the request carries the same key order (a Go map would sort the keys).
// (huggingface is left out: TS drops tool-call parts there.)
func TestAssistantHistoryToolInputKeepsKeyOrder(t *testing.T) {
	const raw = `{"zeta":1,"alpha":{"yy":1,"bb":2}}`
	type factory func(t *testing.T, url string) provider.LanguageModel
	lm := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	cases := map[string]factory{
		"openai-chat": func(t *testing.T, u string) provider.LanguageModel {
			return lm(openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o"))
		},
		"openai-responses": func(t *testing.T, u string) provider.LanguageModel {
			return lm(openai.New(openai.Config{APIKey: "k", BaseURL: u}).ResponsesModel("gpt-4o"))
		},
		"openresponses": func(t *testing.T, u string) provider.LanguageModel {
			return lm(openresponses.New(openresponses.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"moonshot": func(t *testing.T, u string) provider.LanguageModel {
			return lm(moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("kimi-k2.5"))
		},
		"mistral": func(t *testing.T, u string) provider.LanguageModel {
			return lm(mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("mistral-large-latest"))
		},
		"alibaba": func(t *testing.T, u string) provider.LanguageModel {
			return lm(alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus"))
		},
		"cohere": func(t *testing.T, u string) provider.LanguageModel {
			return lm(cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r"))
		},
		"deepseek": func(t *testing.T, u string) provider.LanguageModel {
			return lm(deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat"))
		},
		"groq": func(t *testing.T, u string) provider.LanguageModel {
			return lm(groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("llama-3.3-70b-versatile"))
		},
		"xai": func(t *testing.T, u string) provider.LanguageModel {
			return lm(xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4"))
		},
		"ollama": func(t *testing.T, u string) provider.LanguageModel {
			return lm(ollama.New(ollama.Config{BaseURL: u}).LanguageModel("llama3"))
		},
		"together": func(t *testing.T, u string) provider.LanguageModel {
			return lm(together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("meta-llama/Llama-3-8b-chat-hf"))
		},
		"fireworks": func(t *testing.T, u string) provider.LanguageModel {
			return lm(fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).LanguageModel("accounts/fireworks/models/llama-v3p1-8b-instruct"))
		},
		"cerebras": func(t *testing.T, u string) provider.LanguageModel {
			return lm(cerebras.New(cerebras.Config{APIKey: "k", BaseURL: u}).LanguageModel("llama3.1-8b"))
		},
		"deepinfra": func(t *testing.T, u string) provider.LanguageModel {
			return lm(deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: u}).LanguageModel("meta-llama/Meta-Llama-3.1-8B-Instruct"))
		},
		"gateway": func(t *testing.T, u string) provider.LanguageModel {
			p, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: u})
			if err != nil {
				t.Fatal(err)
			}
			return lm(p.LanguageModel("openai/gpt-5"))
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			m := mk(t, srv.URL)
			_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}},
					{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
						ToolCallID: "c1", ToolName: "t", Input: raw,
						Arguments: map[string]interface{}{"zeta": float64(1), "alpha": map[string]interface{}{"yy": float64(1), "bb": float64(2)}},
					}}},
					{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Result: "ok"}}},
				}},
				Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
			})
			// The input is either a JSON string (escaped quotes) or an object.
			flat := strings.ReplaceAll(body, `\"`, `"`)
			if !strings.Contains(flat, `"zeta":1,"alpha":{"yy":1,"bb":2}`) {
				t.Fatalf("tool input key order lost: %s", body)
			}
		})
	}
}
