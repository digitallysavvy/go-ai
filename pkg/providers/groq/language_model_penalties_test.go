package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs sends frequency_penalty / presence_penalty and warns that topK
// is unsupported.
func TestPenaltiesSentAndTopKWarned(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "llama-3.3-70b-versatile")
	fp, pp, tk := 0.25, 0.5, 40
	body, warnings := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"}, FrequencyPenalty: &fp, PresencePenalty: &pp, TopK: &tk,
	}, false)
	if body["frequency_penalty"] != fp || body["presence_penalty"] != pp {
		t.Fatalf("body = %#v", body)
	}
	found := false
	for _, w := range warnings {
		if w.Type == "unsupported" && w.Feature == "topK" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", warnings)
	}
}
