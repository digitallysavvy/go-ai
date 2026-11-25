package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TestOpenAICompatibleChatTSCases replays the TS openai-compatible chat
// tests (inline wire payloads + vitest snapshots) against every Go provider
// built on the shared OpenAI-compatible chat base.
func TestOpenAICompatibleChatTSCases(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/compat_chat.json.gz")
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
	}
	keys := map[string]string{"together": "togetherai", "fireworks": "fireworks", "gmicloud": "gmicloud", "zai": "zai", "ollama": "ollama"}
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
			}, map[string]string{
				"should process metadata from complete response":  "needs a TS metadataExtractor",
				"should process metadata from streaming response": "needs a TS metadataExtractor",
			})
		})
	}
}
