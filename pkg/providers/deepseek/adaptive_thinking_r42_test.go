package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS warns about thinking.type "adaptive" before checking supportsThinking, so
// an endpoint that omits `thinking` (Azure) still reports the mapping.
func TestAdaptiveThinkingWarnsWithoutThinkingSupport(t *testing.T) {
	off := false
	p := New(Config{APIKey: "k", SupportsThinking: &off})
	model := NewLanguageModel(p, "deepseek-v4-flash")
	body, warnings, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"deepseek": map[string]interface{}{"thinking": map[string]interface{}{"type": "adaptive"}},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking = %#v, want omitted", body["thinking"])
	}
	found := false
	for _, w := range warnings {
		if w.Feature == "thinking.type" && w.Type == "compatibility" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v, want thinking.type compatibility warning", warnings)
	}
}
