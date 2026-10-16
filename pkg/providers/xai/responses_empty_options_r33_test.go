package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS forwards z.string() options that are not undefined (including "") and an
// empty include array (truthy in JS).
func TestResponsesEmptyStringOptionsAndIncludeR33(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4")
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{
			"promptCacheKey": "", "safetyIdentifier": "", "user": "", "previousResponseId": "",
			"include": []interface{}{},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"prompt_cache_key", "safety_identifier", "user", "previous_response_id"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("%s = %v (present=%v), want empty string", k, v, ok)
		}
	}
	if inc, ok := body["include"].([]string); !ok || len(inc) != 0 {
		t.Errorf("include = %#v, want empty array", body["include"])
	}
}

// Unset options are not sent (only an explicit "" is forwarded).
func TestResponsesUnsetStringOptionsOmittedR33(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4")
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"prompt_cache_key", "safety_identifier", "user", "previous_response_id"} {
		if v, ok := body[k]; ok {
			t.Errorf("%s = %v sent although the option is unset", k, v)
		}
	}
}
