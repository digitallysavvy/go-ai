package deepseek

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs returns [...allWarnings, ...toolWarnings] with allWarnings in
// the order topK, seed, penalties, thinking.type, temperature, topP, then the
// reasoning-effort mapping.
func TestDeepSeekWarningOrderMatchesTS(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-reasoner")
	high := types.ReasoningXHigh
	temp := 0.5
	freq := 0.1
	_, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		Temperature:      &temp,
		FrequencyPenalty: &freq,
		Reasoning:        &high,
		Tools:            []types.Tool{{Type: "provider", ProviderID: "x.y", Name: "y"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range warnings {
		got = append(got, w.Type+":"+w.Feature+w.Setting)
	}
	want := []string{
		"deprecated:frequencyPenalty",
		"unsupported:temperature",
		"compatibility:reasoning",
		"unsupported:provider-defined tool x.y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
}
