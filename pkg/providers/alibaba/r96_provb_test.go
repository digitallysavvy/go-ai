package alibaba

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provbFractionalThinkingBudget: TS thinkingBudget is
// z.number().positive(), so a fractional budget is sent as given (Go
// truncated it).
func TestR96provbFractionalThinkingBudget(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"alibaba": map[string]interface{}{"thinkingBudget": 1500.5}}}
	body, _, err := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus").buildRequestBodyChecked(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if body["thinking_budget"] != 1500.5 {
		t.Fatalf("thinking_budget = %#v, want 1500.5", body["thinking_budget"])
	}
	opts.ProviderOptions = map[string]interface{}{"alibaba": map[string]interface{}{"thinkingBudget": 2000.0}}
	body, _, _ = NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus").buildRequestBodyChecked(opts, false)
	if body["thinking_budget"] != 2000 {
		t.Fatalf("integral thinking_budget = %#v, want int 2000", body["thinking_budget"])
	}
}
