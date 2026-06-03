package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func TestOpenAIModelWorkflowSerializationRoundTrip(t *testing.T) {
	p := openai.New(openai.Config{APIKey: "secret", BaseURL: "https://example.com/v1", Headers: map[string]string{"Authorization": "Bearer secret"}})
	model, err := p.LanguageModel("gpt-4o")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}
	serializable, ok := model.(provider.SerializableModel)
	if !ok {
		t.Fatalf("model does not implement provider.SerializableModel")
	}
	serialized := serializable.Serialize()
	if serialized.Provider != "openai.responses" || serialized.ModelID != "gpt-4o" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	if headers, ok := serialized.Config["headers"].(map[string]interface{}); !ok || headers["Authorization"] != "Bearer secret" {
		t.Fatalf("Headers should be serialized when JSON-compatible: %#v", serialized.Config)
	}
	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "openai.responses" || restored.ModelID() != "gpt-4o" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}
	asMap, err := providerutils.SerializeModel(model)
	if err != nil {
		t.Fatalf("providerutils.SerializeModel() error: %v", err)
	}
	if asMap["provider"] != "openai.responses" || asMap["modelId"] != "gpt-4o" {
		t.Fatalf("unexpected providerutils serialization: %#v", asMap)
	}
}

func TestAnthropicModelWorkflowSerializationRoundTrip(t *testing.T) {
	p := anthropic.New(anthropic.Config{APIKey: "secret", BaseURL: "https://example.com", Headers: map[string]string{"x-api-key": "secret"}})
	model, err := p.LanguageModel("claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("LanguageModel() error: %v", err)
	}
	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel() error: %v", err)
	}
	if serialized.Provider != "anthropic" || serialized.ModelID != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected serialized model: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("APIKey should not be serialized: %#v", serialized.Config)
	}
	restored, err := providerutils.DeserializeModel(serialized.Provider, serialized.ModelID, serialized.Config)
	if err != nil {
		t.Fatalf("DeserializeModel() error: %v", err)
	}
	if restored.Provider() != "anthropic" || restored.ModelID() != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected restored model provider=%q model=%q", restored.Provider(), restored.ModelID())
	}
}

func TestProviderSerializationRoundTripMatrix(t *testing.T) {
	cases := []struct {
		name string
		mk   func(t *testing.T) provider.LanguageModel
	}{
		{"google", func(t *testing.T) provider.LanguageModel {
			m, err := google.New(google.Config{APIKey: "k"}).LanguageModel("gemini-2.5-flash")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"xai", func(t *testing.T) provider.LanguageModel {
			m, err := xai.New(xai.Config{APIKey: "k"}).LanguageModel("grok-4")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"bedrock", func(t *testing.T) provider.LanguageModel {
			m, err := providerutils.DeserializeModel("bedrock", "us.anthropic.claude-3-5-sonnet-20241022-v2:0", map[string]interface{}{})
			if err != nil {
				t.Skip("bedrock deserializer requires config")
			}
			return m
		}},
		{"mistral", func(t *testing.T) provider.LanguageModel {
			m, err := mistral.New(mistral.Config{APIKey: "k"}).LanguageModel("mistral-large-latest")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"deepseek", func(t *testing.T) provider.LanguageModel {
			m, err := deepseek.New(deepseek.Config{APIKey: "k"}).LanguageModel("deepseek-chat")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"fireworks", func(t *testing.T) provider.LanguageModel {
			m, err := fireworks.New(fireworks.Config{APIKey: "k"}).LanguageModel("accounts/fireworks/models/mixtral-8x7b-instruct")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"together", func(t *testing.T) provider.LanguageModel {
			m, err := together.New(together.Config{APIKey: "k"}).LanguageModel("mistralai/Mixtral-8x7B-Instruct-v0.1")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"groq", func(t *testing.T) provider.LanguageModel {
			m, err := groq.New(groq.Config{APIKey: "k"}).LanguageModel("llama-3.1-8b-instant")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"cohere", func(t *testing.T) provider.LanguageModel {
			m, err := cohere.New(cohere.Config{APIKey: "k"}).LanguageModel("command-r")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"perplexity", func(t *testing.T) provider.LanguageModel {
			m, err := perplexity.New(perplexity.Config{APIKey: "k"}).LanguageModel("llama-3.1-sonar-small-128k-online")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"azure-openai", func(t *testing.T) provider.LanguageModel {
			m, err := azure.New(azure.Config{APIKey: "k", ResourceName: "r", DeploymentID: "gpt-4o-mini"}).LanguageModel("")
			if err != nil {
				t.Fatal(err)
			}
			return m
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.mk(t)
			serialized, err := provider.SerializeModel(m)
			if err != nil {
				t.Fatalf("SerializeModel() error = %v", err)
			}
			if _, ok := serialized.Config["APIKey"]; ok {
				t.Fatalf("APIKey leaked into config: %#v", serialized.Config)
			}
			restored, err := provider.DeserializeModel(serialized)
			if err != nil {
				t.Fatalf("DeserializeModel() error = %v", err)
			}
			if restored.Provider() == "" || restored.ModelID() == "" {
				t.Fatalf("unexpected restored model: provider=%q model=%q", restored.Provider(), restored.ModelID())
			}
		})
	}
}
