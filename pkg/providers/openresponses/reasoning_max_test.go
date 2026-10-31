package openresponses

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: portable max is forwarded as reasoning.effort "max".
func TestBuildRequestBody_ReasoningMax(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost:1234/v1"}), "gpt-5")
	level := types.ReasoningMax
	body, _, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, Reasoning: &level}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := body["reasoning"].(map[string]interface{})["effort"]; got != "max" {
		t.Fatalf("effort = %v, want max", got)
	}
}
