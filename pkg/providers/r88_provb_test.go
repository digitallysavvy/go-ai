package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestR88provbCompatRefusalTSCases replays the refusal tests of TS
// openai-compatible-chat-language-model.test.ts at 0f3a85f92d (646a7e3c2e:
// refusal text parts, content-filter finish, non-string refusals ignored)
// through every Go model built on the shared OpenAI-compatible chat base and
// the OpenAI chat model in openai-compatible mode. The three
// prepareJsonResponse cases carry the body that helper builds.
func TestR88provbCompatRefusalTSCases(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/compat_chat_r88.json.gz")
	if len(cases) != 10 {
		t.Fatalf("want 10 refusal cases, got %d", len(cases))
	}
	factories := map[string]func(base, id string) (provider.LanguageModel, error){
		"together": func(base, id string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"fireworks": func(base, id string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"gmicloud": func(base, id string) (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"zai": func(base, id string) (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		},
		"ollama": func(base, id string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: base}).LanguageModel(id)
		},
		"openai-compat": func(base, id string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: base, Name: "test-provider", OpenAICompatible: true}).ChatModel(id)
		},
	}
	keys := map[string]string{"together": "togetherai", "fireworks": "fireworks", "gmicloud": "gmicloud", "zai": "zai", "ollama": "ollama", "openai-compat": "test-provider"}
	for name, mk := range factories {
		mk := mk
		name := name
		t.Run(name, func(t *testing.T) {
			tsfixture.MetadataKeyAlias = map[string]string{"test-provider": keys[name]}
			defer func() { tsfixture.MetadataKeyAlias = nil }()
			tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
				if id == "" {
					id = "grok-beta"
				}
				m, err := mk(base, id)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}, nil)
		})
	}
}
