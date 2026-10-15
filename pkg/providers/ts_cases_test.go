package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// TestProviderTSCases replays each provider's own TS language-model tests
// (canned wire payloads + vitest snapshots) through the Go provider.
func TestProviderTSCases(t *testing.T) {
	type tc struct {
		name, file string
		mk         func(base, id string) (provider.LanguageModel, error)
		defaultID  string
		skip       map[string]string
	}
	suites := []tc{
		{"groq", "groq/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "gemma2-9b-it", map[string]string{
			"should send request body":                            "TS groq returns request.body as a JSON string; Go keeps the object",
			"should handle structured outputs with Kimi K2 model": "model id comes from a custom provider call in the TS test",
		}},
		{"mistral", "mistral/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "mistral-small-latest", nil},
		{"deepseek", "deepseek/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "deepseek-chat", map[string]string{
			"should send correct request body with schema": "the injected schema text keeps TS object key order, which a Go map cannot",
		}},
		{"xai-responses", "xai/testdata/tscases/responses.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "grok-4", map[string]string{
			"should include x_search tool call with real response":         "provider-executed call has no result part, so its position among content cannot be compared",
			"should include code_interpreter tool call with real response": "provider-executed call has no result part, so its position among content cannot be compared",
		}},
		{"cohere", "cohere/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "command-r-plus", nil},
		{"huggingface", "huggingface/testdata/tscases/responses.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "moonshotai/Kimi-K2-Instruct", map[string]string{
			"should handle function call tool responses": "vitest snapshot of a string with embedded quotes is not machine-extractable",
		}},
		{"openresponses", "openresponses/testdata/tscases/responses.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{Name: "openresponses", BaseURL: b}).LanguageModel(id)
		}, "gpt-4o", map[string]string{
			"should send instructions from system message":       "model id comes from a custom provider call in the TS test",
			"should join multiple system messages with newlines": "model id comes from a custom provider call in the TS test",
		}},
		{"alibaba", "alibaba/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "qwen-plus", nil},
		{"moonshot", "moonshot/testdata/tscases/chat.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: b}).LanguageModel(id)
		}, "kimi-k2", nil},
		{"openai-completion", "openai/testdata/tscases/completion.json.gz", func(b, id string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).CompletionModel(id)
		}, "gpt-3.5-turbo-instruct", nil},
	}
	for _, s := range suites {
		s := s
		t.Run(s.name, func(t *testing.T) {
			cases := tsfixture.LoadCases(t, s.file)
			tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
				if id == "" {
					id = s.defaultID
				}
				m, err := s.mk(base, id)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}, s.skip)
		})
	}
}
