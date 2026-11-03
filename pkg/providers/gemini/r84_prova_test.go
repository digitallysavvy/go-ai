package gemini

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR84provaEmptyContinuationTokenSent: TS spreads
// `continuationToken` into the body when it is `!= null`, so an empty string
// option is sent.
func TestR84provaEmptyContinuationTokenSent(t *testing.T) {
	m := &LanguageModel{modelID: "gemini-2.5-flash", cfg: Config{ProviderOptionsKeys: []string{"google"}}}
	body := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"continuationToken": ""}},
	}, false)
	if v, ok := body["continuationToken"]; !ok || v != "" {
		t.Errorf("continuationToken = %#v (present %v), want \"\"", v, ok)
	}
}
