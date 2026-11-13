package bedrock

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parses providerOptions.amazonBedrock / bedrock / anthropic with zod and
// throws InvalidArgumentError for a value of the wrong type or outside an enum.
func TestInvalidChatProviderOptionsFailTheCall(t *testing.T) {
	model, err := New(Config{APIKey: "k", Region: "us-east-1", BaseURL: "http://127.0.0.1:1"}).LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]interface{}{
		"bad serviceTier":          {"bedrock": map[string]interface{}{"serviceTier": "bogus"}},
		"bad reasoning type":       {"amazonBedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"type": "maybe"}}},
		"bad budgetTokens":         {"bedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"budgetTokens": "many"}}},
		"bad effort":               {"bedrock": map[string]interface{}{"reasoningConfig": map[string]interface{}{"maxReasoningEffort": "extreme"}}},
		"bad anthropicBeta":        {"bedrock": map[string]interface{}{"anthropicBeta": "beta"}},
		"bad requestMetadata":      {"bedrock": map[string]interface{}{"requestMetadata": map[string]interface{}{"k": 1}}},
		"bad structuredOutputMode": {"bedrock": map[string]interface{}{"structuredOutputMode": "native"}},
		"bad disableParallel":      {"anthropic": map[string]interface{}{"disableParallelToolUse": "yes"}},
		"not an object":            {"bedrock": "oops"},
	}
	for name, po := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "hi"},
				ProviderOptions: po,
			})
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || ia.Field != "providerOptions" {
				t.Fatalf("err = %v, want InvalidArgumentError for providerOptions", err)
			}
		})
	}
}

func TestValidChatProviderOptionsPass(t *testing.T) {
	err := validateChatProviderOptions(map[string]interface{}{
		"bedrock": map[string]interface{}{
			"serviceTier":     "flex",
			"anthropicBeta":   []string{"a"},
			"requestMetadata": map[string]interface{}{"k": "v"},
			"reasoningConfig": map[string]interface{}{"type": "enabled", "budgetTokens": 1024, "maxReasoningEffort": "low", "display": "summarized"},
			"guardrailConfig": map[string]interface{}{"guardrailIdentifier": "g"}, // unknown keys are forwarded
		},
		"anthropic": map[string]interface{}{"disableParallelToolUse": true, "structuredOutputMode": "auto"},
	})
	if err != nil {
		t.Fatal(err)
	}
}
