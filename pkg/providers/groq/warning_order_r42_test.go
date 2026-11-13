package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs pushes topK, then the responseFormat "unsupported" warning, and
// only then the reasoning warnings (round 42).
func TestWarningOrderResponseFormatBeforeReasoning(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "llama-3.3-70b-versatile")
	none := types.ReasoningNone
	topK := 3
	opts := &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		TopK:           &topK,
		Reasoning:      &none,
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: responseFormatTestSchema},
		ProviderOptions: map[string]interface{}{
			"groq": map[string]interface{}{"structuredOutputs": false},
		},
	}
	_, warnings := m.buildRequestBodyWithWarnings(opts, false)
	var features []string
	for _, w := range warnings {
		features = append(features, w.Feature)
	}
	want := []string{"topK", "responseFormat", "reasoning"}
	if len(features) != len(want) {
		t.Fatalf("features = %v, want %v", features, want)
	}
	for i := range want {
		if features[i] != want[i] {
			t.Fatalf("features = %v, want %v", features, want)
		}
	}
}
