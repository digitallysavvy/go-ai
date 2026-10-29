package bedrock

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r58: TS prepareTools reads reasoningConfig.budgetTokens after
// resolveAmazonBedrockReasoningConfig, which deletes it from a disabled config
// under a custom reasoning level. For an application inference profile ARN the
// tools are then not treated as Anthropic (no anthropic tool fields), although
// the request-level Anthropic detection saw the budget.
func TestR58PrepareToolsUsesResolvedReasoningBudget(t *testing.T) {
	arn := "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/abc"
	run := func(reasoning *types.ReasoningLevel) map[string]interface{} {
		p := New(Config{AWSAccessKeyID: "k", AWSSecretAccessKey: "s", Region: "us-east-1"})
		m := NewLanguageModel(p, arn)
		args, err := m.getArgs(&provider.GenerateOptions{
			Prompt:    types.Prompt{Text: "hi"},
			Tools:     []types.Tool{{Name: "anthropic.bash_20250124", ProviderExecuted: true}},
			Reasoning: reasoning,
			ProviderOptions: map[string]interface{}{"amazonBedrock": map[string]interface{}{
				"reasoningConfig": map[string]interface{}{"type": "disabled", "budgetTokens": 1024},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
		return fields
	}
	high := types.ReasoningHigh
	if f := run(&high); f["anthropic_beta"] != nil || f["tool_choice"] != nil || f["tools"] != nil {
		t.Fatalf("custom reasoning drops the disabled budget; tools must not be Anthropic: %v", f)
	}
	if f := run(nil); f["anthropic_beta"] == nil {
		t.Fatalf("without custom reasoning the budget stays and the bash tool is Anthropic: %v", f)
	}
}
