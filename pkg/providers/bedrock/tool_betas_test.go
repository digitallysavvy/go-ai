package bedrock

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareTools returns the anthropic-beta flags of Anthropic provider tools
// and getArgs merges them (after the caller's anthropicBeta) into
// additionalModelRequestFields.anthropic_beta.
func TestBedrockProviderToolBetasMerged(t *testing.T) {
	model := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{{Name: "anthropic.bash_20250124", ProviderExecuted: true}},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"anthropicBeta": []string{"caller-beta"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	got, _ := fields["anthropic_beta"].([]interface{})
	want := []interface{}{"caller-beta", "computer-use-2025-01-24"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anthropic_beta = %#v, want %#v", fields["anthropic_beta"], want)
	}
	if _, leaked := args.Body["anthropicBeta"]; leaked {
		t.Fatal("anthropicBeta must not be sent at the top level")
	}
}

func TestBedrockAnthropicBetaStringSliceOnly(t *testing.T) {
	model := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"anthropicBeta": []string{"caller-beta"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	if got, _ := fields["anthropic_beta"].([]interface{}); !reflect.DeepEqual(got, []interface{}{"caller-beta"}) {
		t.Fatalf("anthropic_beta = %#v", fields["anthropic_beta"])
	}
}

// TS: `if (betas.size > 0 || amazonBedrockOptions.anthropicBeta)`; an empty
// array is truthy, so anthropic_beta: [] is still sent.
func TestBedrockEmptyAnthropicBetaStillSent(t *testing.T) {
	model := newBedrockModelWithID("us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	args, err := model.getArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{
			"amazonBedrock": map[string]interface{}{"anthropicBeta": []string{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	if got, ok := fields["anthropic_beta"].([]interface{}); !ok || len(got) != 0 {
		t.Fatalf("anthropic_beta = %#v, want empty array", fields["anthropic_beta"])
	}
}
