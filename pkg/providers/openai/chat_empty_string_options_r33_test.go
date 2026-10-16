package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// user, safetyIdentifier and promptCacheKey are z.string() in TS, so an empty
// string is forwarded rather than dropped.
func TestChatEmptyStringOptionsForwardedR33(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	body, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"user": "", "safetyIdentifier": "", "promptCacheKey": "",
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "safety_identifier", "prompt_cache_key"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("%s = %v (present=%v), want empty string", k, v, ok)
		}
	}
}

// Unset options are not sent (only an explicit "" is forwarded).
func TestChatUnsetStringOptionsOmittedR33(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	body, _, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "safety_identifier", "prompt_cache_key"} {
		if v, ok := body[k]; ok {
			t.Errorf("%s = %v sent although the option is unset", k, v)
		}
	}
}
