package ollama

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-compatible getArgs "should pass settings with deprecated
// openai-compatible key and emit warning": the settings apply and a
// deprecated warning comes first; prepareTools warnings come last.
func TestR82provbOllamaDeprecatedCompatKeyWarning(t *testing.T) {
	topK := 3
	body, warnings := NewLanguageModel(New(Config{}), "llama3").buildRequestBody(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		TopK:            &topK,
		Tools:           []types.Tool{{Type: "provider", Name: "web", ProviderID: "x.web"}},
		ProviderOptions: map[string]interface{}{"openai-compatible": map[string]interface{}{"user": "u"}},
	}, false)
	if body["user"] != "u" {
		t.Fatalf("user = %v, want u", body["user"])
	}
	if len(warnings) != 3 || warnings[0].Type != "deprecated" || warnings[0].Message != "Use 'openaiCompatible' instead." ||
		warnings[1].Feature != "topK" || warnings[2].Type != "unsupported" || warnings[2].Feature == "topK" {
		t.Fatalf("warnings = %+v, want deprecated, topK, then the tool warning", warnings)
	}
}
