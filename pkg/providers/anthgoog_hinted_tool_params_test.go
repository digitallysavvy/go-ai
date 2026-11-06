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
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	vertexxai "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/xai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Providers called directly (not through core, which resolves tool schemas)
// send a jsonorder.Hinted tool parameter exactly like the same schema as an
// *schema.OrderedSchema.
func TestHintedToolParametersAnthropicGoogleFamilies(t *testing.T) {
	cases := map[string]func(t *testing.T, u string) provider.LanguageModel{
		"anthropic": func(t *testing.T, u string) provider.LanguageModel {
			m, err := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: u}).LanguageModel("claude-sonnet-4-5")
			must(t, err)
			return m
		},
		"bedrock": func(t *testing.T, u string) provider.LanguageModel {
			return bedrock.NewLanguageModel(bedrock.New(bedrock.Config{AWSAccessKeyID: "k", AWSSecretAccessKey: "s", Region: "us-east-1", BaseURL: u}), "anthropic.claude-3-haiku-20240307-v1:0")
		},
		"google": func(t *testing.T, u string) provider.LanguageModel {
			m, err := google.New(google.Config{APIKey: "k", BaseURL: u}).LanguageModel("gemini-2.0-flash")
			must(t, err)
			return m
		},
		"googlevertex": func(t *testing.T, u string) provider.LanguageModel {
			p, err := googlevertex.New(googlevertex.Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: u})
			must(t, err)
			m, err := p.LanguageModel("gemini-2.0-flash")
			must(t, err)
			return m
		},
		"googlevertex-xai": func(t *testing.T, u string) provider.LanguageModel {
			p, err := vertexxai.New(vertexxai.Config{Project: "p", Location: "global", AccessToken: "t", BaseURL: u})
			must(t, err)
			m, err := p.LanguageModel(vertexxai.ModelGrok41FastReasoning)
			must(t, err)
			return m
		},
	}
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
	const rich = `{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"number"}},"required":["zeta","alpha"]}`
	for name, model := range cases {
		model := model
		t.Run(name, func(t *testing.T) {
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
			zi, ai := strings.Index(got, `"zeta"`), strings.Index(got, `"alpha"`)
			if zi < 0 || ai < 0 || zi > ai {
				t.Fatalf("property order lost: %s", got)
			}
			if got != want {
				t.Fatalf("Hinted differs from OrderedSchema:\n got  %s\n want %s", got, want)
			}
		})
	}
}
