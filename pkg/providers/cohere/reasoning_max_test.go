package cohere

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: max uses 95% of the 32768 token budget.
func TestCohereReasoningMaxBudget(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "command-a-reasoning-08-2025")
	level := types.ReasoningMax
	got := m.resolveThinking(&provider.GenerateOptions{Reasoning: &level})
	if got["type"] != "enabled" || got["token_budget"] != 31130 {
		t.Fatalf("thinking = %#v", got)
	}
}
