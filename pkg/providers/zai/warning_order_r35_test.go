package zai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS ZaiChatLanguageModel returns `[...result.warnings, ...zaiWarnings]`: the
// shared chat model's warnings (topK, responseFormat) come first, the wrapper's
// frequencyPenalty/presencePenalty/seed/toolChoice warnings last.
func TestZaiWarningsOrderMatchesTS(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "glm-5.3")
	topK := 3
	fp := 0.1
	seed := 1
	_, warnings, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		TopK:             &topK,
		FrequencyPenalty: &fp,
		Seed:             &seed,
		ToolChoice:       types.ToolChoice{Type: types.ToolChoiceRequired},
		ResponseFormat:   &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range warnings {
		got = append(got, w.Feature)
	}
	want := []string{"topK", "responseFormat", "frequencyPenalty", "seed", "toolChoice required"}
	if len(got) != len(want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warnings = %v, want %v", got, want)
		}
	}
}
