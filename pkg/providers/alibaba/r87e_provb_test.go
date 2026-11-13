package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// cr-r87e-prova-to-provb 2 with the coordinator's drop-in ruling: v0.5.2 read
// preserveThinking under its snake / kebab spellings, so they keep working,
// TS's camelCase key wins when both are given, and the old spellings warn
// "deprecated" like the other Alibaba v0.5 spellings.
func TestR87eprovbPreserveThinkingV05SpellingsWarn(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus")
	for _, key := range []string{"preserve_thinking", "preserve-thinking"} {
		body, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{key: true}},
		}, false)
		if body["preserve_thinking"] != true {
			t.Fatalf("%s: preserve_thinking = %v, want true", key, body["preserve_thinking"])
		}
		found := false
		for _, w := range warnings {
			if w.Type == "deprecated" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: warnings = %#v, want a deprecated warning", key, warnings)
		}
	}
	body, _ := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"preserveThinking": false, "preserve_thinking": true}},
	}, false)
	if body["preserve_thinking"] != false {
		t.Fatalf("camelCase preserveThinking should win: %v", body["preserve_thinking"])
	}
	_, warnings := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"preserveThinking": true}},
	}, false)
	for _, w := range warnings {
		if w.Type == "deprecated" {
			t.Fatalf("TS key warned: %#v", w)
		}
	}
}
