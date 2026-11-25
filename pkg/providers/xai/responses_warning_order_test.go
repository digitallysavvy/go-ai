package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareRequest warns frequencyPenalty, presencePenalty, stopSequences,
// then input warnings, tool warnings and finally the reasoning warning.
func TestXAIResponsesWarningOrderMatchesTS(t *testing.T) {
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4.20-reasoning")
	effort := types.ReasoningHigh
	freq, pres := 0.5, 0.5
	opts := &provider.GenerateOptions{
		Prompt:           types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
		FrequencyPenalty: &freq,
		PresencePenalty:  &pres,
		StopSequences:    []string{"x"},
		Reasoning:        &effort,
		Tools:            []types.Tool{{Name: "xai.mystery", Type: "provider"}},
	}
	warnings := xaiResponsesWarnings(opts)
	_, bodyWarnings, err := m.buildRequestBody(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	warnings = append(warnings, bodyWarnings...)
	var got []string
	for _, w := range warnings {
		got = append(got, w.Feature)
	}
	want := []string{"frequencyPenalty", "presencePenalty", "stopSequences", "provider-defined tool xai.mystery", "reasoning"}
	if len(got) != len(want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warnings = %v, want %v", got, want)
		}
	}
}
