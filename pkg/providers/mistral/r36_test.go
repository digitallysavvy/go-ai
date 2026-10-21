package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS forwards promptCacheKey whenever it is not undefined, including "".
func TestMistralForwardsEmptyPromptCacheKey(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "mistral-large-latest")
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"promptCacheKey": ""}},
	}, false)
	if v, ok := body["prompt_cache_key"]; !ok || v != "" {
		t.Fatalf("prompt_cache_key = %#v (present %v), want empty string", v, ok)
	}
}
