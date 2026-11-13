package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type r82integWeather struct {
	Zeta  string  `json:"zeta" validate:"required" jsonschema:"description=City"`
	Alpha *string `json:"alpha"`
}

// A struct-tag tool sends its generated JSON Schema, with the properties in
// field order (zeta before alpha) and the required list, through every
// provider's tool conversion, as TS sends the zod object's JSON Schema
// (asSchema(...).jsonSchema). v0.5 sent {"type":"object"}.
func TestR82integStructToolSchemaOnTheWire(t *testing.T) {
	strict := true
	tool := types.Tool{Name: "weather", Description: "d", Strict: &strict,
		Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r82integWeather{})),
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		}}
	models := map[string]func(base string) provider.LanguageModel{
		"openai chat": func(b string) provider.LanguageModel {
			m, _ := openai.New(openai.Config{APIKey: "k", BaseURL: b}).ChatModel("gpt-4o")
			return m
		},
		"openai responses": func(b string) provider.LanguageModel {
			m, _ := openai.New(openai.Config{APIKey: "k", BaseURL: b}).ResponsesModel("gpt-4o")
			return m
		},
		"anthropic": func(b string) provider.LanguageModel {
			m, _ := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: b}).LanguageModel("claude-sonnet-4-5")
			return m
		},
		"google": func(b string) provider.LanguageModel {
			m, _ := google.New(google.Config{APIKey: "k", BaseURL: b}).LanguageModel("gemini-2.5-flash")
			return m
		},
		"mistral": func(b string) provider.LanguageModel {
			m, _ := mistral.New(mistral.Config{APIKey: "k", BaseURL: b}).LanguageModel("mistral-large-latest")
			return m
		},
		"cohere": func(b string) provider.LanguageModel {
			m, _ := cohere.New(cohere.Config{APIKey: "k", BaseURL: b}).LanguageModel("command-r")
			return m
		},
	}
	for name, build := range models {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				body = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
			}))
			defer srv.Close()
			zero := 0
			_, _ = ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: build(srv.URL), Prompt: "hi", Tools: []types.Tool{tool}, MaxRetries: &zero})
			mu.Lock()
			defer mu.Unlock()
			z, a := strings.Index(body, `"zeta"`), strings.Index(body, `"alpha"`)
			if z < 0 || a < 0 || z > a {
				t.Fatalf("want zeta before alpha in the tool schema: %s", body)
			}
			if !strings.Contains(body, `"required":["zeta"]`) || !strings.Contains(body, `"City"`) {
				t.Fatalf("want the required list and the description: %s", body)
			}
		})
	}
}

// An ObjectOutput / GenerateObject struct schema reaches the response format
// in field order too.
func TestR82integStructObjectSchemaOrderOnTheWire(t *testing.T) {
	for _, mode := range []string{"ObjectOutput", "GenerateObject"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var body string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				body = string(b)
				mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
			}))
			defer srv.Close()
			m, _ := openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
			s := schema.NewSimpleStructSchema(reflect.TypeOf(r82integWeather{}))
			zero := 0
			if mode == "ObjectOutput" {
				_, _ = ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: m, Prompt: "hi", MaxRetries: &zero,
					Output: ai.ObjectOutput[r82integWeather](ai.ObjectOutputOptions{Schema: s})})
			} else {
				//nolint:staticcheck // the deprecated GenerateObject path keeps the order too
				_, _ = ai.GenerateObject(context.Background(), ai.GenerateObjectOptions{Model: m, Prompt: "hi", MaxRetries: &zero, Schema: s})
			}
			mu.Lock()
			defer mu.Unlock()
			want := `"schema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"zeta":{"type":"string","description":"City"},"alpha":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["zeta"],"additionalProperties":false}`
			if !strings.Contains(body, want) {
				t.Fatalf("body %s\nwant it to contain %s", body, want)
			}
		})
	}
}
