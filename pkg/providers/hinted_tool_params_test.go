package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A tool whose Parameters is a jsonorder.Hinted (a decoded schema plus the
// ordered tree of its source JSON, what MCP tool definitions carry) reaches the
// wire with its parameters in source key order for every chat provider.
func TestHintedToolParametersKeepKeyOrderAcrossProviders(t *testing.T) {
	const text = `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta","alpha"]}`
	mk := func(f func(u string) (provider.LanguageModel, error)) func(t *testing.T, u string) provider.LanguageModel {
		return func(t *testing.T, u string) provider.LanguageModel {
			m, err := f(u)
			must(t, err)
			return m
		}
	}
	cases := map[string]func(t *testing.T, u string) provider.LanguageModel{
		"openai-chat": mk(func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o")
		}),
		"openai-responses": mk(func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ResponsesModel("gpt-4o")
		}),
		"moonshot": mk(func(u string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("kimi-k2.5")
		}),
		"openresponses": mk(func(u string) (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"mistral": mk(func(u string) (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("mistral-large-latest")
		}),
		"alibaba": mk(func(u string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus")
		}),
		"cohere": mk(func(u string) (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r")
		}),
		"deepseek": mk(func(u string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat")
		}),
		"groq": mk(func(u string) (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("llama-3.3-70b-versatile")
		}),
		"xai": mk(func(u string) (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4")
		}),
		"perplexity": mk(func(u string) (provider.LanguageModel, error) {
			return perplexity.New(perplexity.Config{APIKey: "k", BaseURL: u}).LanguageModel("sonar")
		}),
		"huggingface": mk(func(u string) (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: u}).LanguageModel("meta-llama/Llama-3.1-8B-Instruct")
		}),
		"ollama": mk(func(u string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: u}).LanguageModel("llama3")
		}),
		"together": mk(func(u string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"fireworks": mk(func(u string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"gmicloud": mk(func(u string) (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"zai": mk(func(u string) (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"cerebras": mk(func(u string) (provider.LanguageModel, error) {
			return cerebras.New(cerebras.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"deepinfra": mk(func(u string) (provider.LanguageModel, error) {
			return deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		}),
		"baseten": mk(func(u string) (provider.LanguageModel, error) {
			return baseten.New(baseten.Config{APIKey: "k", ModelURL: u}).LanguageModel("m")
		}),
		"gateway": mk(func(u string) (provider.LanguageModel, error) {
			p, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: u})
			if err != nil {
				return nil, err
			}
			return p.LanguageModel("openai/gpt-5")
		}),
	}
	// run sends one request with the given tool parameters and returns the body.
	run := func(t *testing.T, model func(t *testing.T, u string) provider.LanguageModel, params interface{}) string {
		var body string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		_, _ = model(t, srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "hi"},
			Tools:  []types.Tool{{Name: "t", Description: "d", Parameters: params}},
		})
		return body
	}
	// The Hinted form is sent exactly like the same schema as an
	// *schema.OrderedSchema (same normalization, same key order).
	const rich = `{"type":"object","properties":{"zeta":{"type":"string","pattern":"^(?=a)b"},"alpha":{"type":"number"}},"propertyNames":{"type":"string"},"required":["zeta","alpha"]}`
	for name, model := range cases {
		model := model
		t.Run(name+"/same-as-ordered", func(t *testing.T) {
			ordered, err := schema.NewOrderedSchema([]byte(rich))
			must(t, err)
			var decoded map[string]interface{}
			must(t, json.Unmarshal([]byte(rich), &decoded))
			hint, _ := jsonorder.Parse([]byte(rich))
			want := run(t, model, ordered)
			got := run(t, model, jsonorder.Hinted{V: decoded, Hint: hint})
			if want == "" {
				t.Fatal("no request was sent for the OrderedSchema")
			}
			if got != want {
				t.Fatalf("Hinted differs from OrderedSchema:\n got  %s\n want %s", got, want)
			}
		})
	}
	for name, model := range cases {
		model := model
		t.Run(name, func(t *testing.T) {
			var decoded map[string]interface{}
			if err := json.Unmarshal([]byte(text), &decoded); err != nil {
				t.Fatal(err)
			}
			hint, ok := jsonorder.Parse([]byte(text))
			if !ok {
				t.Fatal("hint")
			}
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			_, _ = model(t, srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"},
				Tools:  []types.Tool{{Name: "t", Description: "d", Parameters: jsonorder.Hinted{V: decoded, Hint: hint}}},
			})
			zi, ai := strings.Index(body, `"zeta"`), strings.Index(body, `"alpha"`)
			if zi < 0 || ai < 0 {
				t.Fatalf("tool schema missing from the request body: %s", body)
			}
			if zi > ai {
				t.Fatalf("property order lost: %s", body)
			}
		})
	}
}
