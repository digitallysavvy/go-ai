package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS spreads agentOptions into the body, so empty-string options are kept.
func TestEmptyStringAgentOptionsKeptR33(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-5.1")
	body, _, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{
			"instructions": "", "previous_response_id": "", "language_preference": "",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"instructions", "previous_response_id", "language_preference"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("%s = %v (present=%v), want empty string kept", k, v, ok)
		}
	}
}

// Unset agent options are not sent (only an explicit "" is forwarded).
func TestUnsetAgentOptionsOmittedR33(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-5.1")
	body, _, err := model.getArgs(&provider.GenerateOptions{
		Prompt:          types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		ProviderOptions: map[string]interface{}{"perplexity": map[string]interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"instructions", "previous_response_id", "language_preference"} {
		if v, ok := body[k]; ok {
			t.Errorf("%s = %v sent although the option is unset", k, v)
		}
	}
}
