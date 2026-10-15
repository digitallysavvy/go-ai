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
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// An *schema.OrderedSchema used as a tool's Parameters and as the response
// schema reaches the wire in its source key order (a JS object keeps
// insertion order and structured-output models follow `properties` order),
// for every provider family that sends a schema.
func TestOrderedSchemaKeepsKeyOrderAcrossProviders(t *testing.T) {
	const text = `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta","alpha"]}`

	cases := []struct {
		name string
		// wantTwo: the schema is sent for both the tool and the response format.
		wantTwo bool
		model   func(t *testing.T, url string) provider.LanguageModel
	}{
		{"moonshot", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("kimi-k2.5")
			must(t, err)
			return m
		}},
		{"openresponses", false, func(t *testing.T, u string) provider.LanguageModel {
			m, err := openresponses.New(openresponses.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
			must(t, err)
			return m
		}},
		{"mistral", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("mistral-large-latest")
			must(t, err)
			return m
		}},
		{"alibaba", false, func(t *testing.T, u string) provider.LanguageModel {
			m, err := alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus")
			must(t, err)
			return m
		}},
		{"cohere", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r")
			must(t, err)
			return m
		}},
		{"deepseek", false, func(t *testing.T, u string) provider.LanguageModel {
			m, err := deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat")
			must(t, err)
			return m
		}},
		{"groq", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("llama-3.3-70b-versatile")
			must(t, err)
			return m
		}},
		{"xai", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4")
			must(t, err)
			return m
		}},
		{"perplexity", false, func(t *testing.T, u string) provider.LanguageModel {
			m, err := perplexity.New(perplexity.Config{APIKey: "k", BaseURL: u}).LanguageModel("sonar")
			must(t, err)
			return m
		}},
		{"huggingface", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := huggingface.New(huggingface.Config{APIKey: "k", BaseURL: u}).LanguageModel("meta-llama/Llama-3.1-8B-Instruct")
			must(t, err)
			return m
		}},
		{"ollama", true, func(t *testing.T, u string) provider.LanguageModel {
			m, err := ollama.New(ollama.Config{BaseURL: u}).LanguageModel("llama3")
			must(t, err)
			return m
		}},
		{"gateway", true, func(t *testing.T, u string) provider.LanguageModel {
			p, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: u})
			must(t, err)
			m, err := p.LanguageModel("openai/gpt-5")
			must(t, err)
			return m
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ordered, err := schema.NewOrderedSchema([]byte(text))
			must(t, err)
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			m := tc.model(t, srv.URL)
			_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:         types.Prompt{Text: "hi"},
				ResponseFormat: &provider.ResponseFormat{Type: "json", Name: "r", Schema: ordered},
				Tools:          []types.Tool{{Name: "t", Description: "d", Parameters: ordered}},
			})
			zeta, alpha := strings.Count(body, `"zeta"`), strings.Count(body, `"alpha"`)
			if zeta == 0 {
				t.Fatalf("schema not in the request body: %s", body)
			}
			if tc.wantTwo && zeta < 2 {
				t.Fatalf("schema sent %d times, want tool and response format: %s", zeta, body)
			}
			rest := body
			for i := 0; i < zeta; i++ {
				zi, ai := strings.Index(rest, `"zeta"`), strings.Index(rest, `"alpha"`)
				if zi < 0 || ai < 0 || zi > ai {
					t.Fatalf("property order lost (occurrence %d of %d): %s", i+1, zeta, body)
				}
				rest = rest[ai+1:]
			}
			_ = alpha
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
