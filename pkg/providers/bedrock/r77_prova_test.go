package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS amazon-bedrock-chat-language-model.ts getArgs: for a non-Anthropic
// model it warns whenever reasoningConfig.budgetTokens is set
// (`amazonBedrockOptions.reasoningConfig?.budgetTokens != null`), not only
// for an enabled config.
func TestR77prova_BedrockBudgetTokensWarningForAnyThinkingType(t *testing.T) {
	const want = "budgetTokens applies only to Anthropic models on Bedrock and will be ignored for this model."
	for name, rc := range map[string]map[string]interface{}{
		"enabled":  {"type": "enabled", "budgetTokens": 2048},
		"disabled": {"type": "disabled", "budgetTokens": 2048},
		"untyped":  {"budgetTokens": 2048},
	} {
		t.Run(name, func(t *testing.T) {
			m := NewLanguageModel(New(Config{Region: "us-east-1"}), "us.amazon.nova-pro-v1:0")
			args, err := m.getArgs(&provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "hi"},
				ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"reasoningConfig": rc}},
			})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, w := range args.Warnings {
				if w.Feature == "budgetTokens" && w.Details == want {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("budgetTokens warnings = %d in %+v", count, args.Warnings)
			}
		})
	}

	// No budget, no warning.
	m := NewLanguageModel(New(Config{Region: "us-east-1"}), "us.amazon.nova-pro-v1:0")
	args, err := m.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"type": "enabled"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range args.Warnings {
		if w.Feature == "budgetTokens" {
			t.Fatalf("unexpected warning %+v", w)
		}
	}
}
